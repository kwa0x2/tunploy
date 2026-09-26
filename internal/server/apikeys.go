package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kwa0x2/tunploy/internal/auth"
	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/store"
)

const (
	scopeDevicesRead  = "devices:read"
	scopeDevicesWrite = "devices:write"
	scopeServersRead  = "servers:read"
	scopeEventsRead   = "events:read"
	// One scope for reading too: a webhook list shows where device data goes.
	scopeWebhooksWrite = "webhooks:write"
)

var apiScopes = []string{scopeDevicesRead, scopeDevicesWrite, scopeServersRead, scopeEventsRead, scopeWebhooksWrite}

const (
	apiKeyPrefix   = "tp_"
	maxAPIKeys     = 50
	maxAPIKeyName  = 64
	maxIdemKeyLen  = 255
	apiRatePerSec  = 10
	apiRateBurst   = 60
	touchEvery     = time.Minute
	maxAPIBodySize = 1 << 20
)

type apiKeyRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type createdAPIKey struct {
	store.APIKey
	// Shown once; only its hash is kept.
	Token string `json:"token"`
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) error {
	keys, err := s.store.APIKeys(r.Context())
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, keys)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) error {
	var req apiKeyRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	fields := map[string]string{}
	switch {
	case req.Name == "":
		fields["name"] = "name is required"
	case utf8.RuneCountInString(req.Name) > maxAPIKeyName:
		fields["name"] = "name must be at most 64 characters"
	case strings.IndexFunc(req.Name, unicode.IsControl) >= 0:
		fields["name"] = "name must not contain control characters"
	}
	var scopes []string
	for _, sc := range apiScopes {
		if slices.Contains(req.Scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	for _, sc := range req.Scopes {
		if !slices.Contains(apiScopes, sc) {
			fields["scopes"] = "unknown scope " + strconv.Quote(sc)
		}
	}
	if len(scopes) == 0 && fields["scopes"] == "" {
		fields["scopes"] = "choose at least one scope"
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		fields["expires_at"] = "expiry must be in the future"
	}
	if len(fields) > 0 {
		return httpx.Invalid(fields)
	}

	existing, err := s.store.APIKeys(r.Context())
	if err != nil {
		return err
	}
	if len(existing) >= maxAPIKeys {
		return httpx.Conflict("the panel holds at most %d API keys; revoke one first", maxAPIKeys)
	}

	secret, _, err := auth.NewSessionToken()
	if err != nil {
		return err
	}
	token := apiKeyPrefix + secret
	k, err := s.store.CreateAPIKey(r.Context(), store.APIKey{
		Name:      req.Name,
		Prefix:    token[:len(apiKeyPrefix)+6],
		TokenHash: auth.HashToken(token),
		Scopes:    scopes,
		ExpiresAt: req.ExpiresAt,
	})
	var dup *store.DuplicateError
	if errors.As(err, &dup) && dup.Column == "name" {
		return httpx.Invalid(map[string]string{"name": "an API key with this name already exists"})
	}
	if err != nil {
		return err
	}
	s.record(r.Context(), store.Event{Kind: "apikey.created", IP: clientIP(r), Detail: apiKeyDetail(k)})
	return httpx.JSON(w, http.StatusCreated, createdAPIKey{APIKey: *k, Token: token})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return httpx.NotFound("API key not found")
	}
	k, err := s.store.APIKeyByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.NotFound("API key not found")
	}
	if err != nil {
		return err
	}
	if err := s.store.DeleteAPIKey(r.Context(), id); err != nil {
		return err
	}
	s.limiter.forget(id)
	s.record(r.Context(), store.Event{Kind: "apikey.revoked", IP: clientIP(r), Detail: k.Name})
	return httpx.NoContent(w)
}

func apiKeyDetail(k *store.APIKey) string {
	d := k.Name + " (" + strings.Join(k.Scopes, ", ") + ")"
	if k.ExpiresAt != nil {
		d += ", until " + k.ExpiresAt.In(time.Local).Format("2 Jan 2006")
	}
	return d
}

type apiKeyCtx struct{}

func apiKeyFrom(ctx context.Context) *store.APIKey {
	k, _ := ctx.Value(apiKeyCtx{}).(*store.APIKey)
	return k
}

// Events a key causes name it, so the activity log shows who did what.
func actorFrom(ctx context.Context) string {
	if k := apiKeyFrom(ctx); k != nil {
		return "api:" + k.Name
	}
	return ""
}

// No cookies here, so there is nothing for CSRF to ride on.
func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		token = strings.TrimSpace(token)
		if !strings.EqualFold(scheme, "Bearer") || !strings.HasPrefix(token, apiKeyPrefix) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tunploy"`)
			httpx.WriteError(w, r, httpx.Unauthorized("send an API key as Authorization: Bearer tp_..."))
			return
		}
		now := time.Now()
		k, err := s.store.APIKeyByToken(r.Context(), auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) || (err == nil && k.Expired(now)) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tunploy", error="invalid_token"`)
			httpx.WriteError(w, r, httpx.Unauthorized("API key is unknown, revoked or expired"))
			return
		}
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}

		if ok, wait := s.limiter.allow(k.ID, now); !ok {
			w.Header().Set("Retry-After", retryAfterSeconds(wait))
			httpx.WriteError(w, r, httpx.Errorf(http.StatusTooManyRequests, "rate_limited",
				"too many requests for this API key, slow down to %d a second", apiRatePerSec))
			return
		}

		ip := clientIP(r)
		if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) >= touchEvery || k.LastUsedIP != ip {
			if err := s.store.TouchAPIKey(r.Context(), k.ID, ip, now); err != nil {
				slog.Warn("record api key use", "key", k.Name, "error", err)
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyCtx{}, k)))
	})
}

func requireScope(scope string, h httpx.Handler) http.Handler {
	return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		k := apiKeyFrom(r.Context())
		if k == nil || !slices.Contains(k.Scopes, scope) {
			return httpx.Errorf(http.StatusForbidden, "missing_scope", "this API key lacks the %s scope", scope)
		}
		return h(w, r)
	})
}

// A token bucket per key; in memory, like the login throttle.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{buckets: map[int64]*bucket{}} }

func (l *rateLimiter) allow(id int64, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[id]
	if !ok {
		b = &bucket{tokens: apiRateBurst, at: now}
		l.buckets[id] = b
	}
	b.tokens = min(apiRateBurst, b.tokens+now.Sub(b.at).Seconds()*apiRatePerSec)
	b.at = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / apiRatePerSec * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

func (l *rateLimiter) forget(id int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, id)
}

// idempotent replays the first reply to a retried POST with the same
// Idempotency-Key, so a payment webhook that fires twice makes one device.
func (s *Server) idempotent(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		k := apiKeyFrom(r.Context())
		if key == "" || k == nil {
			h.ServeHTTP(w, r)
			return
		}
		if len(key) > maxIdemKeyLen {
			httpx.WriteError(w, r, httpx.BadRequest("Idempotency-Key must be at most %d bytes", maxIdemKeyLen))
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAPIBodySize))
		if err != nil {
			httpx.WriteError(w, r, httpx.BadRequest("request body is too large"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256([]byte(r.Method + " " + r.URL.RequestURI() + "\n" + string(body)))

		stored, err := s.store.ClaimIdempotencyKey(r.Context(), k.ID, key, hex.EncodeToString(sum[:]), time.Now())
		switch {
		case errors.Is(err, store.ErrIdempotencyMismatch):
			httpx.WriteError(w, r, httpx.Errorf(http.StatusUnprocessableEntity, "idempotency_key_reused",
				"this Idempotency-Key was already used for a different request"))
			return
		case errors.Is(err, store.ErrIdempotencyBusy):
			httpx.WriteError(w, r, httpx.Errorf(http.StatusConflict, "idempotency_key_in_use",
				"a request with this Idempotency-Key is still running"))
			return
		case err != nil:
			httpx.WriteError(w, r, err)
			return
		case stored != nil:
			w.Header().Set("Content-Type", stored.ContentType)
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(stored.Status)
			w.Write(stored.Body)
			return
		}

		rec := &capture{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)

		ctx := context.WithoutCancel(r.Context())
		// Nothing useful to replay: let the caller try again.
		if rec.status == http.StatusInternalServerError || rec.status == http.StatusTooManyRequests {
			if err := s.store.ReleaseIdempotencyKey(ctx, k.ID, key); err != nil {
				slog.Error("release idempotency key", "error", err)
			}
			return
		}
		err = s.store.FinishIdempotencyKey(ctx, k.ID, key, store.StoredResponse{
			Status: rec.status, ContentType: w.Header().Get("Content-Type"), Body: rec.body.Bytes(),
		})
		if err != nil {
			slog.Error("store idempotent reply", "error", err)
		}
	})
}

type capture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *capture) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *capture) Write(b []byte) (int, error) {
	c.body.Write(b)
	return c.ResponseWriter.Write(b)
}
