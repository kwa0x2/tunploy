package node

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// testServer accepts one password, or the given key, and answers every exec
// request with the command it was asked to run.
type testServer struct {
	target  Target
	hostKey string
	authKey ssh.PublicKey
}

func newTestServer(t *testing.T, authKey ssh.PublicKey) *testServer {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if string(pw) == "secret" {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if authKey != nil && string(key.Marshal()) == string(authKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(conn, cfg)
		}
	}()

	port, _ := strconv.Atoi(strings.Split(ln.Addr().String(), ":")[1])
	return &testServer{
		target:  Target{Host: "127.0.0.1", Port: port, Username: "root"},
		hostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))),
		authKey: authKey,
	}
}

func serveConn(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		ch, requests, err := nc.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range requests {
				if req.Type != "exec" {
					req.Reply(false, nil)
					continue
				}
				n := binary.BigEndian.Uint32(req.Payload[:4])
				cmd := string(req.Payload[4 : 4+n])
				req.Reply(true, nil)
				status := uint32(0)
				if strings.Contains(cmd, "fail") {
					ch.Stderr().Write([]byte("something broke\n"))
					status = 1
				} else {
					ch.Write([]byte(cmd))
				}
				ch.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, status))
				return
			}
		}()
	}
}

func TestScanAndPinHostKey(t *testing.T) {
	srv := newTestServer(t, nil)
	ctx := context.Background()

	key, err := ScanHostKey(ctx, srv.target)
	if err != nil {
		t.Fatal(err)
	}
	if key != srv.hostKey {
		t.Fatalf("scanned %q, want %q", key, srv.hostKey)
	}
	if !strings.HasPrefix(Fingerprint(key), "SHA256:") {
		t.Fatalf("fingerprint = %q", Fingerprint(key))
	}

	c, err := dial(ctx, srv.target, []ssh.AuthMethod{ssh.Password("secret")}, key)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	other := newTestServer(t, nil)
	if _, err := dial(ctx, srv.target, []ssh.AuthMethod{ssh.Password("secret")}, other.hostKey); !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("another host key: err = %v", err)
	}
}

func TestLoginFailures(t *testing.T) {
	srv := newTestServer(t, nil)
	ctx := context.Background()

	s := Setup{}
	auth, _ := s.auth(SetupRequest{Password: "wrong"})
	_, err := dial(ctx, srv.target, auth, srv.hostKey)
	if err == nil || !strings.Contains(err.Error(), "refused the credentials") {
		t.Fatalf("wrong password: err = %v", err)
	}

	if _, err := s.auth(SetupRequest{PrivateKey: "not a key"}); err == nil {
		t.Fatal("a broken private key was accepted")
	}
}

func TestShellRun(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	if line := AuthorizedKey(signer); !strings.HasPrefix(line, "ssh-ed25519 ") || !strings.HasSuffix(line, " tunploy") {
		t.Fatalf("authorized key = %q", line)
	}

	srv := newTestServer(t, signer.PublicKey())
	ctx := context.Background()
	c, err := dial(ctx, srv.target, []ssh.AuthMethod{ssh.PublicKeys(signer)}, srv.hostKey)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	out, err := shell{client: c, sudo: true}.run(ctx, "echo it's", nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `sudo -n sh -c 'echo it'\''s'`; string(out) != want {
		t.Fatalf("ran %q, want %q", out, want)
	}
	if _, err := (shell{client: c}).run(ctx, "fail", nil); err == nil || err.Error() != "something broke" {
		t.Fatalf("failing command: err = %v", err)
	}
}

func TestHostStaysInRoot(t *testing.T) {
	h := &Host{}
	for _, p := range []string{Root + "/wireguard/1/wg0.conf", Root + "/x"} {
		if err := h.inRoot(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{Root, "/etc/passwd", Root + "/../etc", Root + "/a/../../b", "/var/lib/tunploy-other/x"} {
		if err := h.inRoot(p); err == nil {
			t.Errorf("%s was allowed", p)
		}
	}
}

func TestHostKeyAlgorithms(t *testing.T) {
	if got := hostKeyAlgorithms(ssh.KeyAlgoRSA); got[0] != ssh.KeyAlgoRSASHA512 {
		t.Fatalf("rsa = %v", got)
	}
	if got := hostKeyAlgorithms(ssh.KeyAlgoED25519); len(got) != 1 || got[0] != ssh.KeyAlgoED25519 {
		t.Fatalf("ed25519 = %v", got)
	}
}
