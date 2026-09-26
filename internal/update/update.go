// Package update finds new Tunploy releases and swaps the panel's container
// for one running the new image.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kwa0x2/tunploy/internal/docker"
	"github.com/kwa0x2/tunploy/internal/hostcli"
	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	DefaultRepo = "kwa0x2/tunploy"

	checkEvery = 12 * time.Hour
	// Covers the image pull and the updater's own swap.
	updateTimeout = 10 * time.Minute
	resultPoll    = 2 * time.Second
	resultWindow  = 5 * time.Minute
)

var (
	ErrBusy        = errors.New("an update is already running")
	ErrNoUpdate    = errors.New("no newer version is available")
	ErrUnsupported = errors.New("this panel can't update itself")
)

type Docker interface {
	FindSelf(ctx context.Context) (docker.Self, error)
	PullImage(ctx context.Context, ref string) error
	StartUpdater(ctx context.Context, self docker.Self, image string, cmd []string) error
	Running(ctx context.Context, id string) (bool, int, error)
	RunHelper(ctx context.Context, name, image string, cmd, binds []string) error
}

type Release struct {
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

type Status struct {
	Current    string     `json:"current"`
	Latest     *Release   `json:"latest,omitempty"`
	Available  bool       `json:"available"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
	CheckError string     `json:"check_error,omitempty"`
	// Why the panel can't update itself; empty when it can.
	Unsupported string `json:"unsupported,omitempty"`
	// The version being installed while an update runs.
	Updating string `json:"updating,omitempty"`
	// Why the last update failed.
	Error string `json:"error,omitempty"`
}

type Service struct {
	current string
	dataDir string
	docker  Docker
	auto    bool
	onEvent func(context.Context, store.Event)

	// Overridable in tests.
	API    string
	Client *http.Client

	resultMu sync.Mutex

	mu        sync.Mutex
	self      *docker.Self
	selfErr   error
	latest    *Release
	checkedAt time.Time
	checkErr  string
	updating  string
	lastErr   string
}

// With auto off, GitHub is only asked when someone presses Check.
func New(current, dataDir string, dk Docker, auto bool) *Service {
	return &Service{
		current: current,
		dataDir: dataDir,
		docker:  dk,
		auto:    auto,
		API:     "https://api.github.com",
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *Service) OnEvent(fn func(context.Context, store.Event)) { s.onEvent = fn }

// ReportLast records how the last update went. Call it before serving, so
// the page waiting on an update never sees the old panel without the reason.
func (s *Service) ReportLast(ctx context.Context) { s.consumeResult(ctx) }

// Run checks for releases until ctx ends.
func (s *Service) Run(ctx context.Context) {
	go s.awaitResult(ctx)
	if !s.auto {
		return
	}
	for {
		if _, err := s.Check(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("check for tunploy updates", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkEvery):
		}
	}
}

func (s *Service) Status(ctx context.Context) Status {
	self, selfErr := s.findSelf(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{
		Current:    s.current,
		Latest:     s.latest,
		CheckError: s.checkErr,
		Updating:   s.updating,
		Error:      s.lastErr,
	}
	if !s.checkedAt.IsZero() {
		at := s.checkedAt
		st.CheckedAt = &at
	}
	st.Available = s.latest != nil && newer(s.latest.Version, s.current)
	st.Unsupported = unsupported(s.current, self, selfErr)
	return st
}

// Check asks GitHub for the newest release. A failure is kept in the status too.
func (s *Service) Check(ctx context.Context) (Status, error) {
	self, _ := s.findSelf(ctx)
	rel, err := s.fetchLatest(ctx, repoOf(self.Source))

	s.mu.Lock()
	s.checkedAt = time.Now()
	s.checkErr = ""
	if err != nil {
		s.checkErr = err.Error()
	} else {
		s.latest = rel
	}
	s.mu.Unlock()

	if err == nil && rel != nil && newer(rel.Version, s.current) {
		slog.Info("a new tunploy version is available", "current", s.current, "latest", rel.Version)
	}
	return s.Status(ctx), err
}

// Start pulls the new image and hands over to the updater container, which
// stops this process. It returns once the pull has begun.
func (s *Service) Start(ctx context.Context) (string, error) {
	self, selfErr := s.findSelf(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updating != "" {
		return "", ErrBusy
	}
	if reason := unsupported(s.current, self, selfErr); reason != "" {
		return "", fmt.Errorf("%w: %s", ErrUnsupported, reason)
	}
	if s.latest == nil || !newer(s.latest.Version, s.current) {
		return "", ErrNoUpdate
	}
	target := s.latest.Version
	s.updating, s.lastErr = target, ""

	go s.apply(self, target)
	return target, nil
}

func (s *Service) apply(self docker.Self, target string) {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	image := Repository(self.Image) + ":" + target
	removeResult(s.dataDir)
	slog.Info("updating tunploy", "from", s.current, "to", target, "image", image)

	if err := s.docker.PullImage(ctx, image); err != nil {
		s.fail(ctx, target, fmt.Errorf("download %s: %w", image, err))
		return
	}
	cmd := []string{"self-update", "--container", self.ID, "--image", image, "--from", s.current, "--to", target}
	if err := s.docker.StartUpdater(ctx, self, image, cmd); err != nil {
		s.fail(ctx, target, err)
		return
	}

	// On success the updater stops this process before it gets here; it only
	// leaves a result behind while this panel still runs if it gave up early.
	ticker := time.NewTicker(resultPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.fail(ctx, target, errors.New("the update did not finish in time"))
			return
		case <-ticker.C:
			if s.consumeResult(ctx) {
				return
			}
			if s.updaterGone(ctx) && !s.consumeResult(ctx) {
				s.fail(ctx, target, errors.New("the updater stopped before it replaced the panel"))
				return
			}
		}
	}
}

// The updater writes its result once this panel answers, which is after it
// started; a panel that just replaced the old one hears about it here.
func (s *Service) awaitResult(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, resultWindow)
	defer cancel()
	ticker := time.NewTicker(resultPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.consumeResult(ctx) {
				return
			}
		}
	}
}

// updaterGone reports an updater that quit without a result, such as one
// that crashed; it removes itself when it exits.
func (s *Service) updaterGone(ctx context.Context) bool {
	_, _, err := s.docker.Running(ctx, docker.UpdaterName)
	return errors.Is(err, docker.ErrNotFound)
}

func (s *Service) fail(ctx context.Context, target string, err error) {
	slog.Error("update tunploy", "to", target, "error", err)
	s.mu.Lock()
	s.updating, s.lastErr = "", err.Error()
	s.mu.Unlock()
	s.emit(ctx, store.Event{Kind: "panel.update_failed", Detail: target})
}

// consumeResult reports what the updater wrote, once.
func (s *Service) consumeResult(ctx context.Context) bool {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	r, ok, err := readResult(s.dataDir)
	if err != nil {
		slog.Error("read update result", "error", err)
	}
	if !ok {
		return false
	}
	removeResult(s.dataDir)

	if r.Error != "" {
		s.fail(ctx, r.To, errors.New(r.Error))
		return true
	}
	if r.To == s.current {
		slog.Info("tunploy updated", "from", r.From, "to", r.To)
		s.emit(ctx, store.Event{Kind: "panel.updated", Detail: r.To})
	}
	return true
}

func (s *Service) emit(ctx context.Context, e store.Event) {
	if s.onEvent != nil {
		s.onEvent(context.WithoutCancel(ctx), e)
	}
}

// The container never changes while this process lives, so a hit is kept.
func (s *Service) findSelf(ctx context.Context) (docker.Self, error) {
	s.mu.Lock()
	if s.self != nil {
		defer s.mu.Unlock()
		return *s.self, nil
	}
	s.mu.Unlock()

	self, err := s.docker.FindSelf(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.self = &self
	}
	s.selfErr = err
	return self, err
}

// installName is the container the install script creates.
const installName = "tunploy"

// EnsureHostCLI puts the tunploy command on the server. The install script
// does too, but a panel installed before the command existed and updated
// from the panel since would never get it.
func (s *Service) EnsureHostCLI(ctx context.Context) {
	self, err := s.findSelf(ctx)
	if err != nil || self.Compose != "" || self.Name != installName {
		return
	}
	dir := filepath.Dir(hostcli.Path)
	cmd := []string{"host-cli", "/host/" + filepath.Base(hostcli.Path), Repository(self.Image)}
	if err := s.docker.RunHelper(ctx, "tunploy-host-cli", self.Image, cmd, []string{dir + ":/host"}); err != nil {
		slog.Warn("install the tunploy command on the server; run the install script again to add it", "error", err)
	}
}

// ContainerName is the panel's own container, or empty outside Docker.
func (s *Service) ContainerName(ctx context.Context) string {
	self, err := s.findSelf(ctx)
	if err != nil {
		return ""
	}
	return self.Name
}

func unsupported(current string, self docker.Self, selfErr error) string {
	switch {
	case !IsRelease(current):
		return fmt.Sprintf("This is a %s build, not a release. Install a release to update from the panel.", current)
	case selfErr != nil:
		return "The panel isn't running in the container the install script creates. Run the install command again to update."
	case self.Compose != "":
		return fmt.Sprintf("Docker Compose manages this panel (project %s). Update it with docker compose pull and docker compose up -d.", self.Compose)
	}
	return ""
}

// Repository strips the tag and digest from an image reference.
func Repository(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref
}

// repoOf reads owner/name from an image's source label, so a fork's image
// follows the fork's releases.
func repoOf(source string) string {
	rest, ok := strings.CutPrefix(source, "https://github.com/")
	if !ok {
		return DefaultRepo
	}
	parts := strings.Split(strings.TrimSuffix(rest, ".git"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return DefaultRepo
	}
	return parts[0] + "/" + parts[1]
}

// Drafts and pre-releases never count as the latest release. A repository
// without any release yet is not an error.
func (s *Service) fetchLatest(ctx context.Context, repo string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.API+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tunploy/"+s.current)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, nil
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		var msg struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &msg) == nil && msg.Message != "" {
			return nil, fmt.Errorf("GitHub answered %d: %s", resp.StatusCode, msg.Message)
		}
		return nil, fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}

	var rel struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("read GitHub's answer: %w", err)
	}
	if !IsRelease(rel.TagName) {
		return nil, fmt.Errorf("the latest release %q is not a version number", rel.TagName)
	}
	return &Release{
		Version:     strings.TrimPrefix(rel.TagName, "v"),
		URL:         rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
	}, nil
}
