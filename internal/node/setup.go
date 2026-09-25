package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/kwa0x2/tunploy/internal/deploy"
	"github.com/kwa0x2/tunploy/internal/docker"
)

type Step string

const (
	StepConnect   Step = "connect"
	StepAuthorize Step = "authorize"
	StepDocker    Step = "docker"
	StepWireGuard Step = "wireguard"
	StepImage     Step = "image"
)

const (
	installTimeout = 10 * time.Minute
	setupLogLines  = 30
)

// SetupRequest carries credentials used once: afterwards the panel logs in
// with its own key, which setup adds to the login user's authorized_keys.
// With neither a password nor a private key, that key must already be there.
type SetupRequest struct {
	Target     Target
	HostKey    string
	Password   string
	PrivateKey string
	Passphrase string
}

type SetupError struct {
	Step   Step
	Reason string
	Log    []string
}

func (e *SetupError) Error() string { return e.Reason }

// Setup readies a machine to be a node: the panel's key, Docker, a kernel
// with WireGuard and the WireGuard image. It changes nothing in the database.
type Setup struct {
	Signer ssh.Signer
	// LocalDaemon is the panel's own Docker daemon ID; a node sharing it would
	// fight the panel over the same containers.
	LocalDaemon string
	Prepare     func(ctx context.Context, h deploy.Host) error
	Progress    func(Step)
}

func (s Setup) Run(ctx context.Context, req SetupRequest) (docker.Daemon, error) {
	step := func(st Step) {
		if s.Progress != nil {
			s.Progress(st)
		}
	}
	fail := func(st Step, err error, log ...string) error {
		return &SetupError{Step: st, Reason: err.Error(), Log: log}
	}

	auth, err := s.auth(req)
	if err != nil {
		return docker.Daemon{}, fail(StepConnect, err)
	}
	client, err := dial(ctx, req.Target, auth, req.HostKey)
	if err != nil {
		return docker.Daemon{}, fail(StepConnect, err)
	}
	defer client.Close()
	sh := shell{client: client, sudo: req.Target.Username != "root"}
	if sh.sudo {
		if _, err := sh.run(ctx, "true", nil); err != nil {
			return docker.Daemon{}, fail(StepConnect, fmt.Errorf(
				"%s needs to run sudo without a password; log in as root or allow it in sudoers (%v)", req.Target.Username, err))
		}
	}
	step(StepConnect)

	if err := s.authorize(ctx, sh, req); err != nil {
		return docker.Daemon{}, fail(StepAuthorize, err)
	}
	step(StepAuthorize)

	if log, err := installDocker(ctx, sh); err != nil {
		return docker.Daemon{}, fail(StepDocker, err, log...)
	}
	h, err := newHost(sh)
	if err != nil {
		return docker.Daemon{}, fail(StepDocker, err)
	}
	defer h.Close()
	daemon, err := h.Daemon(ctx)
	if err != nil {
		return docker.Daemon{}, fail(StepDocker, err)
	}
	if s.LocalDaemon != "" && daemon.ID == s.LocalDaemon {
		return docker.Daemon{}, fail(StepDocker, errors.New("this is the machine the panel runs on; its servers need no node"))
	}
	step(StepDocker)

	if _, err := sh.run(ctx, "modprobe wireguard 2>/dev/null; [ -d /sys/module/wireguard ]", nil); err != nil {
		return docker.Daemon{}, fail(StepWireGuard, fmt.Errorf(
			"kernel %s has no WireGuard support; it is built in from Linux 5.6", daemon.KernelVersion))
	}
	step(StepWireGuard)

	if err := s.Prepare(ctx, h); err != nil {
		return docker.Daemon{}, fail(StepImage, err)
	}
	step(StepImage)
	return daemon, nil
}

func (s Setup) auth(req SetupRequest) ([]ssh.AuthMethod, error) {
	switch {
	case req.PrivateKey != "":
		var (
			signer ssh.Signer
			err    error
		)
		if req.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(req.PrivateKey), []byte(req.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(req.PrivateKey))
		}
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, errors.New("the private key is protected; enter its passphrase")
		}
		if err != nil {
			return nil, fmt.Errorf("private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case req.Password != "":
		// Many servers ask for the password through keyboard-interactive.
		answer := func(_, _ string, questions []string, _ []bool) ([]string, error) {
			out := make([]string, len(questions))
			for i := range out {
				out[i] = req.Password
			}
			return out, nil
		}
		return []ssh.AuthMethod{ssh.Password(req.Password), ssh.KeyboardInteractive(answer)}, nil
	default:
		return []ssh.AuthMethod{ssh.PublicKeys(s.Signer)}, nil
	}
}

// The key goes to the login user; a second connection proves it works.
func (s Setup) authorize(ctx context.Context, sh shell, req SetupRequest) error {
	line := AuthorizedKey(s.Signer)
	script := fmt.Sprintf(`umask 077
mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys
grep -qF %[1]s ~/.ssh/authorized_keys && exit 0
[ -z "$(tail -c1 ~/.ssh/authorized_keys)" ] || echo >> ~/.ssh/authorized_keys
echo %[2]s >> ~/.ssh/authorized_keys`, quote(keyBlob(line)), quote(line))
	if _, err := sh.asUser().run(ctx, script, nil); err != nil {
		return fmt.Errorf("add the panel's key: %w", err)
	}

	check, err := dial(ctx, req.Target, []ssh.AuthMethod{ssh.PublicKeys(s.Signer)}, req.HostKey)
	if err != nil {
		return fmt.Errorf("the panel's key was added but the server refuses it; check that sshd allows public keys (%v)", err)
	}
	check.Close()
	return nil
}

// Deauthorize removes the panel's key from a node that is being let go.
func Deauthorize(ctx context.Context, h deploy.Host, signer ssh.Signer) error {
	nh, ok := h.(*Host)
	if !ok {
		return nil
	}
	blob := quote(keyBlob(AuthorizedKey(signer)))
	script := fmt.Sprintf(`f=~/.ssh/authorized_keys
[ -f "$f" ] || exit 0
grep -vF %[1]s "$f" > "$f.tunploy" || true
cat "$f.tunploy" > "$f" && rm -f "$f.tunploy"`, blob)
	_, err := nh.sh.asUser().run(ctx, script, nil)
	return err
}

// RemoveData deletes the panel's data directory on a node.
func RemoveData(ctx context.Context, h deploy.Host) error {
	nh, ok := h.(*Host)
	if !ok {
		return nil
	}
	_, err := nh.sh.run(ctx, "rm -rf -- "+quote(Root), nil)
	return err
}

func keyBlob(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return line
	}
	return fields[1]
}

// installDocker uses Docker's own install script when docker is missing.
func installDocker(ctx context.Context, sh shell) ([]string, error) {
	if _, err := sh.run(ctx, "command -v docker >/dev/null", nil); err == nil {
		if _, err := sh.run(ctx, "docker version >/dev/null 2>&1 || systemctl start docker", nil); err != nil {
			return nil, fmt.Errorf("docker is installed but not running: %w", err)
		}
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	var log []string
	script := `set -e
if command -v curl >/dev/null; then curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
elif command -v wget >/dev/null; then wget -qO /tmp/get-docker.sh https://get.docker.com
else echo "neither curl nor wget is installed" >&2; exit 1; fi
sh /tmp/get-docker.sh
rm -f /tmp/get-docker.sh
command -v systemctl >/dev/null && systemctl enable --now docker || true
docker version >/dev/null`
	err := sh.stream(ctx, script, func(line string) {
		log = append(log, line)
		if len(log) > setupLogLines {
			log = log[1:]
		}
	})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("installing docker took longer than %s", installTimeout)
		}
		return log, fmt.Errorf("install docker: %w", err)
	}
	return nil, nil
}
