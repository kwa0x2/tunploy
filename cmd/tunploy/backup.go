package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/kwa0x2/tunploy/internal/backup"
	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/store"
)

const backupUsage = `usage: tunploy backup <command> [flags]

commands:
  list                        list the backups in the connected S3 bucket
  restore <file>              restore from a backup file
  restore --s3 <name>         restore from a backup in the connected S3 bucket

A restore replaces every server, device, setting and user with the backup's,
keeps the backup storage settings, and signs everyone out. It works while the
panel is stopped or broken; restart the panel afterwards to apply it:

  docker exec -it tunploy tunploy backup restore --s3 tunploy-backup-20260925-030000.tar.gz
  docker restart tunploy

An encrypted backup is opened with the panel's saved passphrase, or you are
asked for it (as one line on stdin when it is piped in).
`

func runBackup(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(os.Stderr, backupUsage)
		return 2
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	p := newPrompter(os.Stdin, os.Stderr)
	var err error
	switch args[0] {
	case "list":
		err = backupList(os.Stdout)
	case "restore":
		err = backupRestore(args[1:], p, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], backupUsage)
		return 2
	}
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		return 1
	}
	return 0
}

func backupList(out io.Writer) error {
	_, st, err := openPanel()
	if err != nil {
		return err
	}
	defer st.Close()

	remote, err := bucket(context.Background(), st)
	if err != nil {
		return err
	}
	objects, err := remote.List(context.Background())
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		fmt.Fprintln(os.Stderr, "No backups in the bucket yet.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSIZE\tMADE")
	for _, o := range objects {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", o.Name, backup.FormatSize(o.Size), o.Modified.Local().Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

func backupRestore(args []string, p *prompter, out io.Writer) error {
	fs := flag.NewFlagSet("tunploy backup restore", flag.ContinueOnError)
	fromS3 := fs.String("s3", "", "name of a backup in the connected S3 bucket")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	file := fs.Arg(0)
	if (file == "") == (*fromS3 == "") {
		return errors.New("give either a backup file or --s3 <name>")
	}

	cfg, st, err := openPanel()
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	tmpDir := filepath.Join(cfg.DataDir, "tmp")

	name := filepath.Base(file)
	if *fromS3 != "" {
		name = *fromS3
		if file, err = download(ctx, st, name, tmpDir); err != nil {
			return err
		}
		defer os.Remove(file)
	}

	if !*yes {
		if !p.terminal {
			return errors.New("add --yes to restore without a terminal to confirm on")
		}
		answer, err := p.line(fmt.Sprintf("Replace everything on this panel with %s? Type 'restore' to continue: ", name))
		if err != nil {
			return err
		}
		if answer != "restore" {
			return errors.New("cancelled")
		}
	}

	stored, err := st.Settings(ctx)
	if err != nil {
		return err
	}
	passphrase := backup.LoadPassphrase(stored)
	m, warnings, err := restoreFile(ctx, st, cfg.DataDir, tmpDir, file, passphrase)
	if errors.Is(err, backup.ErrPassphraseRequired) || errors.Is(err, backup.ErrWrongPassphrase) {
		if passphrase != "" {
			fmt.Fprintln(out, "The panel's saved passphrase does not open this backup.")
		}
		if passphrase, err = p.passphrase(); err != nil {
			return err
		}
		m, warnings, err = restoreFile(ctx, st, cfg.DataDir, tmpDir, file, passphrase)
	}
	if err != nil {
		return err
	}
	if err := backup.MarkRebuild(cfg.DataDir); err != nil {
		return fmt.Errorf("restored, but could not ask the panel to rebuild its VPN servers: %w", err)
	}

	for _, w := range warnings {
		fmt.Fprintln(out, "warning:", w)
	}
	fmt.Fprintf(out, "Restored %s, made %s by Tunploy %s.\n", name, m.CreatedAt.Local().Format("2006-01-02 15:04"), m.Version)
	fmt.Fprintln(out, "Restart the panel to apply it; its VPN servers are rebuilt as it starts:")
	fmt.Fprintln(out, "  docker restart tunploy")
	return nil
}

func restoreFile(ctx context.Context, st *store.Store, dataDir, tmpDir, path, passphrase string) (backup.Manifest, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return backup.Manifest{}, nil, err
	}
	defer f.Close()
	m, warnings, err := backup.Restore(ctx, st, dataDir, tmpDir, f, passphrase)
	switch {
	case errors.Is(err, store.ErrNewerBackup), errors.Is(err, backup.ErrNewerArchive):
		return m, nil, errors.New("this backup was made by a newer version of Tunploy; update the panel first")
	case errors.Is(err, store.ErrInvalidBackup):
		return m, nil, backup.ErrInvalidArchive
	}
	return m, warnings, err
}

// download saves the object to a file, so a wrong passphrase can be retried
// without fetching it again.
func download(ctx context.Context, st *store.Store, name, tmpDir string) (string, error) {
	if !backup.IsFileName(name) {
		return "", fmt.Errorf("%q is not a backup name; see 'tunploy backup list'", name)
	}
	remote, err := bucket(ctx, st)
	if err != nil {
		return "", err
	}
	body, _, err := remote.Get(ctx, remote.Key(name))
	if err != nil {
		return "", err
	}
	defer body.Close()

	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(tmpDir, "download-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	return f.Name(), f.Close()
}

func bucket(ctx context.Context, st *store.Store) (*backup.S3, error) {
	stored, err := st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	cfg := backup.LoadConfig(stored)
	if cfg == nil {
		return nil, errors.New("no S3 bucket is connected; connect one under Settings → Backup storage, or restore from a file")
	}
	return backup.NewS3(cfg.S3), nil
}

func openPanel() (config.Config, *store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, nil, err
	}
	st, err := store.Open(cfg.DBPath())
	return cfg, st, err
}

func (p *prompter) passphrase() (string, error) {
	if !p.terminal {
		s, err := p.in.ReadString('\n')
		if err != nil && s == "" {
			return "", errors.New("this backup is encrypted; pass its passphrase on stdin")
		}
		return strings.TrimRight(s, "\r\n"), nil
	}
	return p.secret("Backup passphrase: ")
}
