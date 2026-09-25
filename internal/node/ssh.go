// Package node reaches the machines the panel manages over SSH: it keeps a
// connection to each, runs Docker through it and sets new ones up.
package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	dialTimeout = 15 * time.Second
	// Docker keeps its data here too; the path is the same on every node.
	Root = "/var/lib/tunploy"
)

// Target is where a node's SSH server listens and who to log in as.
type Target struct {
	Host     string
	Port     int
	Username string
}

func (t Target) addr() string { return net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) }

// GenerateKey returns a new ed25519 private key in OpenSSH PEM form.
func GenerateKey() (string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ssh key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "tunploy")
	if err != nil {
		return "", fmt.Errorf("encode ssh key: %w", err)
	}
	return string(pem.EncodeToMemory(block)), nil
}

// AuthorizedKey is the line for a node's ~/.ssh/authorized_keys.
func AuthorizedKey(signer ssh.Signer) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " tunploy"
}

// Fingerprint is the SHA256 form ssh-keygen -l prints.
func Fingerprint(authorizedKey string) string {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		return ""
	}
	return ssh.FingerprintSHA256(key)
}

// dial connects and checks the server's key against hostKey, an
// authorized_keys line.
func dial(ctx context.Context, t Target, auth []ssh.AuthMethod, hostKey string) (*ssh.Client, error) {
	want, _, _, _, err := ssh.ParseAuthorizedKey([]byte(hostKey))
	if err != nil {
		return nil, fmt.Errorf("stored host key is corrupt: %w", err)
	}
	return dialWith(ctx, t, auth, ssh.FixedHostKey(want), hostKeyAlgorithms(want.Type()))
}

// An RSA key signs with SHA-2 on any server that still allows it.
func hostKeyAlgorithms(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{keyType}
}

var ErrHostKeyMismatch = errors.New("the server's host key changed")

func dialWith(ctx context.Context, t Target, auth []ssh.AuthMethod, check ssh.HostKeyCallback, algos []string) (*ssh.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", t.addr())
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", t.addr(), err)
	}
	// The handshake has no context of its own.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	cfg := &ssh.ClientConfig{
		User:              t.Username,
		Auth:              auth,
		HostKeyCallback:   check,
		HostKeyAlgorithms: algos,
		Timeout:           dialTimeout,
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, t.addr(), cfg)
	if err != nil {
		conn.Close()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("connect to %s: timed out", t.addr())
		}
		return nil, sshError(err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func sshError(err error) error {
	msg := err.Error()
	switch {
	// A refused password followed by keyboard-interactive ends in message 51,
	// SSH_MSG_USERAUTH_FAILURE, where x/crypto expects a prompt.
	case strings.Contains(msg, "unable to authenticate"), strings.Contains(msg, "unexpected message type 51"):
		return errors.New("ssh login failed: the server refused the credentials")
	case strings.Contains(msg, "host key mismatch"), errors.Is(err, ErrHostKeyMismatch):
		return fmt.Errorf("ssh: %w; if the server was reinstalled, remove the node and add it again", ErrHostKeyMismatch)
	case strings.HasSuffix(msg, "EOF"):
		return errors.New("ssh: the server closed the connection during the handshake")
	case strings.HasPrefix(msg, "ssh: "):
		return err
	}
	return fmt.Errorf("ssh: %w", err)
}

// ScanHostKey connects without logging in and returns the server's host key.
func ScanHostKey(ctx context.Context, t Target) (string, error) {
	var got ssh.PublicKey
	check := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		got = key
		return errStopAfterKey
	}
	// The ed25519 key is the one people usually compare against.
	algos := []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
		ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	_, err := dialWith(ctx, t, nil, check, algos)
	if got == nil {
		if err == nil {
			err = errors.New("the server sent no host key")
		}
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(got))), nil
}

var errStopAfterKey = errors.New("host key read")

// shell runs commands on one node, through sudo unless it logs in as root.
type shell struct {
	client *ssh.Client
	sudo   bool
}

// asUser runs as the login user even where sudo is needed for the rest.
func (sh shell) asUser() shell { return shell{client: sh.client} }

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (sh shell) command(script string) string {
	if sh.sudo {
		return "sudo -n sh -c " + quote(script)
	}
	return "sh -c " + quote(script)
}

// run waits for script and returns its output; stderr becomes the error.
func (sh shell) run(ctx context.Context, script string, stdin []byte) ([]byte, error) {
	sess, err := sh.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer sess.Close()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	if stdin != nil {
		sess.Stdin = bytes.NewReader(stdin)
	}
	stop := context.AfterFunc(ctx, func() { sess.Close() })
	defer stop()

	if err := sess.Run(sh.command(script)); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s", lastLine(msg))
		}
		return nil, fmt.Errorf("remote command: %w", err)
	}
	return stdout.Bytes(), nil
}

// stream runs script and hands its output over as it comes, a line at a time.
func (sh shell) stream(ctx context.Context, script string, line func(string)) error {
	sess, err := sh.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer sess.Close()

	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw
	stop := context.AfterFunc(ctx, func() { sess.Close() })
	defer stop()

	var last string
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 0, 4096)
		chunk := make([]byte, 4096)
		for {
			n, err := pr.Read(chunk)
			buf = append(buf, chunk[:n]...)
			for {
				i := bytes.IndexAny(buf, "\r\n")
				if i < 0 {
					break
				}
				if s := strings.TrimSpace(string(buf[:i])); s != "" {
					last = s
					line(s)
				}
				buf = buf[i+1:]
			}
			if err != nil {
				return
			}
		}
	}()

	err = sess.Run(sh.command(script))
	pw.Close()
	<-done
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if last != "" {
			return fmt.Errorf("%s", last)
		}
		return fmt.Errorf("remote command: %w", err)
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// dialDocker opens one connection to the node's Docker daemon, the way the
// docker CLI does for ssh:// hosts.
func (sh shell) dialDocker(ctx context.Context, _, _ string) (net.Conn, error) {
	sess, err := sh.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, err
	}
	c := &stdioConn{sess: sess, stdin: stdin, stdout: stdout}
	sess.Stderr = &c.stderr
	cmd := "docker system dial-stdio"
	if sh.sudo {
		cmd = "sudo -n " + cmd
	}
	if err := sess.Start(cmd); err != nil {
		sess.Close()
		return nil, fmt.Errorf("start docker over ssh: %w", err)
	}
	return c, nil
}

// stdioConn is a net.Conn over a remote command's stdin and stdout.
type stdioConn struct {
	sess   *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader

	stderr lockedBuffer
	once   sync.Once
}

// The session writes stderr from its own goroutine.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (c *stdioConn) Read(p []byte) (int, error) {
	n, err := c.stdout.Read(p)
	if n == 0 && errors.Is(err, io.EOF) {
		msg := strings.TrimSpace(c.stderr.String())
		if msg != "" {
			return 0, fmt.Errorf("docker over ssh: %s", lastLine(msg))
		}
	}
	return n, err
}

func (c *stdioConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

func (c *stdioConn) Close() error {
	c.once.Do(func() {
		c.stdin.Close()
		c.sess.Close()
	})
	return nil
}

func (c *stdioConn) LocalAddr() net.Addr              { return stdioAddr{} }
func (c *stdioConn) RemoteAddr() net.Addr             { return stdioAddr{} }
func (c *stdioConn) SetDeadline(time.Time) error      { return nil }
func (c *stdioConn) SetReadDeadline(time.Time) error  { return nil }
func (c *stdioConn) SetWriteDeadline(time.Time) error { return nil }

type stdioAddr struct{}

func (stdioAddr) Network() string { return "ssh" }
func (stdioAddr) String() string  { return "docker" }
