package node

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/kwa0x2/tunploy/internal/docker"
)

// Host is a node as the deploy manager sees it: its Docker daemon plus the
// few file operations the WireGuard configs need.
type Host struct {
	*docker.Client
	sh shell
}

func newHost(sh shell) (*Host, error) {
	dk, err := docker.NewDialer(sh.dialDocker)
	if err != nil {
		return nil, err
	}
	return &Host{Client: dk, sh: sh}, nil
}

func (h *Host) Root() string { return Root }

// Nothing outside the data directory is ever written or removed.
func (h *Host) inRoot(p string) error {
	if clean := path.Clean(p); clean != p || !strings.HasPrefix(p, Root+"/") {
		return fmt.Errorf("refusing to touch %s outside %s", p, Root)
	}
	return nil
}

func (h *Host) WriteFile(ctx context.Context, p string, body []byte) error {
	if err := h.inRoot(p); err != nil {
		return err
	}
	dir, tmp := path.Dir(p), path.Join(path.Dir(p), "."+path.Base(p)+".tmp")
	script := fmt.Sprintf("umask 077 && mkdir -p %s && cat > %s && mv -f %s %s",
		quote(dir), quote(tmp), quote(tmp), quote(p))
	if _, err := h.sh.run(ctx, script, body); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

func (h *Host) RemoveAll(ctx context.Context, p string) error {
	if err := h.inRoot(p); err != nil {
		return err
	}
	if _, err := h.sh.run(ctx, "rm -rf -- "+quote(p), nil); err != nil {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}

func (h *Host) ReadDir(ctx context.Context, p string) ([]string, error) {
	if err := h.inRoot(p); err != nil {
		return nil, err
	}
	out, err := h.sh.run(ctx, fmt.Sprintf("if [ -d %[1]s ]; then ls -1A -- %[1]s; fi", quote(p)), nil)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", p, err)
	}
	return strings.Fields(string(out)), nil
}
