package deploy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The manager reports the first two steps, image/tunploy-wg.sh the rest.
type Step string

const (
	StepImage     Step = "image"
	StepContainer Step = "container"
	StepInterface Step = "interface"
	StepFirewall  Step = "firewall"
	StepNAT       Step = "nat"
)

type Progress func(Step)

func (p Progress) report(s Step) {
	if p != nil {
		p(s)
	}
}

const (
	stepMarker   = "tunploy:step "
	readyMarker  = "tunploy:ready"
	bootTimeout  = 30 * time.Second
	bootLogLines = 50
)

var errBootTimeout = errors.New("boot timed out")

// Log outlives the container, which a failed create removes.
type BootError struct {
	Reason string
	Log    []string
}

func (e *BootError) Error() string { return e.Reason }

func (m *Manager) waitReady(ctx context.Context, name string, progress Progress) error {
	ctx, cancel := context.WithTimeoutCause(ctx, bootTimeout, errBootTimeout)
	defer cancel()

	logs, err := m.docker.Logs(ctx, name, bootLogLines*4, true)
	if err != nil {
		return err
	}
	defer logs.Close()

	var tail []string
	sc := bufio.NewScanner(logs)
	for sc.Scan() {
		line := stripTimestamp(sc.Text())
		if line == readyMarker {
			return nil
		}
		if step, ok := strings.CutPrefix(line, stepMarker); ok {
			progress.report(Step(step))
			continue
		}
		tail = append(tail, line)
		if len(tail) > bootLogLines {
			tail = tail[1:]
		}
	}

	switch {
	case errors.Is(context.Cause(ctx), errBootTimeout):
		return &BootError{Reason: fmt.Sprintf("wireguard did not come up within %s", bootTimeout), Log: tail}
	case ctx.Err() != nil:
		return ctx.Err()
	case sc.Err() != nil:
		return fmt.Errorf("read container output: %w", sc.Err())
	}

	// The stream only ends on its own when the container stops.
	reason := "container stopped before wireguard came up"
	if ct, err := m.docker.InspectContainer(ctx, name); err == nil && ct.ExitCode != 0 {
		reason = fmt.Sprintf("container exited with code %d before wireguard came up", ct.ExitCode)
	}
	return &BootError{Reason: reason, Log: tail}
}

func stripTimestamp(line string) string {
	if ts, rest, ok := strings.Cut(line, " "); ok {
		if _, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			return rest
		}
	}
	return line
}
