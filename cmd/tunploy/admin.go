package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"golang.org/x/term"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/config"
	"github.com/kwa0x2/tunploy/internal/store"
)

const adminUsage = `usage: tunploy admin <command> [flags]

commands:
  create           create the admin account; only works while there is none
  reset-password   set a new password and sign out every session

The password is read from the terminal without echo, or as one line from
stdin when it is piped in. Run it inside the container:

  docker exec -it tunploy tunploy admin create
`

var errAdminExists = errors.New("an admin account already exists; use 'tunploy admin reset-password' to change its password")

// No sign-up page: on a fresh public panel it would belong to whoever came first.
func runAdmin(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(os.Stderr, adminUsage)
		return 2
	}

	// Keeps migration chatter out of what is an interactive command.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	p := newPrompter(os.Stdin, os.Stderr)
	var err error
	switch args[0] {
	case "create":
		err = adminCreate(args[1:], p)
	case "reset-password":
		err = adminResetPassword(args[1:], p)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], adminUsage)
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

func adminCreate(args []string, p *prompter) error {
	fs := flag.NewFlagSet("tunploy admin create", flag.ContinueOnError)
	name := fs.String("name", "", "display name")
	email := fs.String("email", "", "sign-in email")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	// Checked before prompting, so nobody types a password for nothing.
	ctx := context.Background()
	if n, err := st.CountUsers(ctx); err != nil {
		return err
	} else if n > 0 {
		return errAdminExists
	}

	if *name == "" {
		if *name, err = p.line("Name: "); err != nil {
			return err
		}
	}
	if *email == "" {
		if *email, err = p.line("Email: "); err != nil {
			return err
		}
	}
	password, err := p.newPassword()
	if err != nil {
		return err
	}

	user, err := createAdmin(ctx, st, *name, *email, password)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Admin account created. Sign in as %s.\n", user.Email)
	return nil
}

func adminResetPassword(args []string, p *prompter) error {
	fs := flag.NewFlagSet("tunploy admin reset-password", flag.ContinueOnError)
	email := fs.String("email", "", "email of the account")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	if *email == "" {
		if *email, err = p.line("Email: "); err != nil {
			return err
		}
	}
	password, err := p.newPassword()
	if err != nil {
		return err
	}

	if err := resetPassword(context.Background(), st, *email, password); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Password changed. Every session has been signed out.")
	return nil
}

func createAdmin(ctx context.Context, st *store.Store, name, email, password string) (*store.User, error) {
	if err := fieldsError(auth.ValidateAccount(name, email, password)); err != nil {
		return nil, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user, err := st.CreateFirstUser(ctx, strings.TrimSpace(name), email, hash)
	if errors.Is(err, store.ErrDuplicate) {
		return nil, errAdminExists
	}
	return user, err
}

func resetPassword(ctx context.Context, st *store.Store, email, password string) error {
	if msg := auth.CheckPassword(password); msg != "" {
		return errors.New(msg)
	}
	user, err := st.UserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no account uses %s", store.NormalizeEmail(email))
	}
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := st.UpdateUserPassword(ctx, user.ID, hash); err != nil {
		return err
	}
	return st.DeleteUserSessions(ctx, user.ID)
}

func fieldsError(fields map[string]string) error {
	if len(fields) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(fields))
	for _, msg := range fields {
		msgs = append(msgs, msg)
	}
	slices.Sort(msgs)
	return errors.New(strings.Join(msgs, "; "))
}

func openStore() (*store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return store.Open(cfg.DBPath())
}

type prompter struct {
	in       *bufio.Reader
	out      io.Writer
	fd       int
	terminal bool
}

func newPrompter(in *os.File, out io.Writer) *prompter {
	fd := int(in.Fd())
	return &prompter{in: bufio.NewReader(in), out: out, fd: fd, terminal: term.IsTerminal(fd)}
}

func (p *prompter) line(label string) (string, error) {
	if !p.terminal {
		return "", fmt.Errorf("%s is required; pass it as a flag", strings.ToLower(strings.TrimSuffix(label, ": ")))
	}
	fmt.Fprint(p.out, label)
	s, err := p.in.ReadString('\n')
	if err != nil && s == "" {
		return "", fmt.Errorf("read %s: %w", strings.TrimSuffix(label, ": "), err)
	}
	return strings.TrimSpace(s), nil
}

// Read from stdin, never argv, where ps could see it.
func (p *prompter) newPassword() (string, error) {
	if !p.terminal {
		s, err := p.in.ReadString('\n')
		if err != nil && s == "" {
			return "", errors.New("password is required on stdin")
		}
		return strings.TrimRight(s, "\r\n"), nil
	}

	first, err := p.secret("Password: ")
	if err != nil {
		return "", err
	}
	if msg := auth.CheckPassword(first); msg != "" {
		return "", errors.New(msg)
	}
	second, err := p.secret("Repeat password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passwords do not match")
	}
	return first, nil
}

func (p *prompter) secret(label string) (string, error) {
	fmt.Fprint(p.out, label)
	b, err := term.ReadPassword(p.fd)
	fmt.Fprintln(p.out)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(b), nil
}
