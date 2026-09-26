package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DeliveryPending   = "pending"
	DeliverySucceeded = "succeeded"
	DeliveryFailed    = "failed"

	// Subscribes a webhook to every kind it may receive.
	AllEvents = "*"
)

type Webhook struct {
	ID          int64     `json:"id"`
	URL         string    `json:"url"`
	Secret      string    `json:"-"`
	Events      []string  `json:"events"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Delivery struct {
	ID             int64           `json:"id"`
	WebhookID      int64           `json:"webhook_id"`
	EventID        int64           `json:"event_id,omitempty"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload"`
	State          string          `json:"state"`
	Attempts       int             `json:"attempts"`
	NextAttemptAt  *time.Time      `json:"next_attempt_at,omitempty"`
	LastAttemptAt  *time.Time      `json:"last_attempt_at,omitempty"`
	ResponseStatus int             `json:"response_status,omitempty"`
	Error          string          `json:"error,omitempty"`
	DurationMS     int64           `json:"duration_ms,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`

	// Filled in by DueDeliveries for the sender.
	URL    string `json:"-"`
	Secret string `json:"-"`
}

// Attempt is how one try at a delivery went. Next is nil once no more
// tries are left, or after a success.
type Attempt struct {
	At          time.Time
	Status      int
	Error       string
	Duration    time.Duration
	Succeeded   bool
	NextAttempt *time.Time
}

const webhookColumns = `id, url, secret, events, description, enabled, created_by, created_at, updated_at`

func (s *Store) CreateWebhook(ctx context.Context, w Webhook) (*Webhook, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO webhooks (url, secret, events, description, enabled, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		w.URL, w.Secret, strings.Join(w.Events, ","), w.Description, w.Enabled, w.CreatedBy, now, now)
	if err != nil {
		return nil, writeError("create webhook", err)
	}
	if w.ID, err = res.LastInsertId(); err != nil {
		return nil, fmt.Errorf("read inserted webhook id: %w", err)
	}
	w.CreatedAt = time.Unix(now, 0).UTC()
	w.UpdatedAt = w.CreatedAt
	return &w, nil
}

func (s *Store) Webhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+webhookColumns+` FROM webhooks ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer rows.Close()

	hooks := []Webhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, *w)
	}
	return hooks, rows.Err()
}

func (s *Store) WebhookByID(ctx context.Context, id int64) (*Webhook, error) {
	return scanWebhook(s.db.QueryRowContext(ctx, `SELECT `+webhookColumns+` FROM webhooks WHERE id = ?`, id))
}

// UpdateWebhook leaves the secret alone. Turning a webhook off gives up on
// what it still had to send, so turning it on again does not replay old events.
func (s *Store) UpdateWebhook(ctx context.Context, w Webhook) (*Webhook, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update webhook: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`UPDATE webhooks SET url = ?, events = ?, description = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		w.URL, strings.Join(w.Events, ","), w.Description, w.Enabled, now, w.ID)
	if err != nil {
		return nil, writeError("update webhook", err)
	}
	if err := expectOneRow(res, "update webhook"); err != nil {
		return nil, err
	}
	if !w.Enabled {
		if _, err := tx.ExecContext(ctx,
			`UPDATE webhook_deliveries SET state = ?, next_attempt_at = NULL, error = 'the webhook was turned off'
			 WHERE webhook_id = ? AND state = ?`, DeliveryFailed, w.ID, DeliveryPending); err != nil {
			return nil, fmt.Errorf("cancel webhook deliveries: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update webhook: %w", err)
	}
	return s.WebhookByID(ctx, w.ID)
}

func (s *Store) DeleteWebhook(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	return expectOneRow(res, "delete webhook")
}

func scanWebhook(row rowScanner) (*Webhook, error) {
	var (
		w                Webhook
		events           string
		created, updated int64
	)
	err := row.Scan(&w.ID, &w.URL, &w.Secret, &events, &w.Description, &w.Enabled, &w.CreatedBy, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan webhook: %w", err)
	}
	w.Events = []string{}
	if events != "" {
		w.Events = strings.Split(events, ",")
	}
	w.CreatedAt = time.Unix(created, 0).UTC()
	w.UpdatedAt = time.Unix(updated, 0).UTC()
	return &w, nil
}

// QueueDeliveries queues the event for every enabled webhook that wants its
// kind and reports how many did.
func (s *Store) QueueDeliveries(ctx context.Context, kind string, eventID int64, payload []byte, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, event_id, kind, payload, next_attempt_at, created_at)
		 SELECT id, ?, ?, ?, ?, ? FROM webhooks
		 WHERE enabled = 1 AND (events = ? OR instr(',' || events || ',', ',' || ? || ',') > 0)`,
		eventID, kind, payload, now.Unix(), now.Unix(), AllEvents, kind)
	if err != nil {
		return 0, fmt.Errorf("queue webhook deliveries: %w", err)
	}
	return res.RowsAffected()
}

// QueueDelivery queues a payload for one webhook, whatever it subscribes to.
func (s *Store) QueueDelivery(ctx context.Context, webhookID int64, kind string, payload []byte, now time.Time) (*Delivery, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (webhook_id, kind, payload, next_attempt_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		webhookID, kind, payload, now.Unix(), now.Unix())
	if err != nil {
		return nil, fmt.Errorf("queue webhook delivery: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read inserted delivery id: %w", err)
	}
	return s.DeliveryByID(ctx, id)
}

// RetryDelivery sends a delivery again soon, whatever became of it.
func (s *Store) RetryDelivery(ctx context.Context, id int64, now time.Time) (*Delivery, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET state = ?, next_attempt_at = ? WHERE id = ?`, DeliveryPending, now.Unix(), id)
	if err != nil {
		return nil, fmt.Errorf("retry webhook delivery: %w", err)
	}
	if err := expectOneRow(res, "retry webhook delivery"); err != nil {
		return nil, err
	}
	return s.DeliveryByID(ctx, id)
}

const deliveryColumns = `d.id, d.webhook_id, d.event_id, d.kind, d.payload, d.state, d.attempts, d.next_attempt_at,
	d.last_attempt_at, d.response_status, d.error, d.duration_ms, d.created_at`

// DueDeliveries returns pending deliveries whose time has come, oldest first,
// for webhooks that are still on.
func (s *Store) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+deliveryColumns+`, w.url, w.secret
		 FROM webhook_deliveries d JOIN webhooks w ON w.id = d.webhook_id
		 WHERE d.state = ? AND d.next_attempt_at <= ? AND w.enabled = 1
		 ORDER BY d.next_attempt_at, d.id LIMIT ?`, DeliveryPending, now.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("list due deliveries: %w", err)
	}
	defer rows.Close()

	var out []Delivery
	for rows.Next() {
		d, err := scanDelivery(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) FinishAttempt(ctx context.Context, id int64, a Attempt) error {
	state := DeliveryPending
	switch {
	case a.Succeeded:
		state = DeliverySucceeded
	case a.NextAttempt == nil:
		state = DeliveryFailed
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET state = ?, attempts = attempts + 1, next_attempt_at = ?, last_attempt_at = ?,
			response_status = ?, error = ?, duration_ms = ?
		 WHERE id = ?`,
		state, nullTime(a.NextAttempt), a.At.Unix(), a.Status, a.Error, a.Duration.Milliseconds(), id)
	if err != nil {
		return fmt.Errorf("record webhook attempt: %w", err)
	}
	return nil
}

func (s *Store) DeliveryByID(ctx context.Context, id int64) (*Delivery, error) {
	return scanDelivery(s.db.QueryRowContext(ctx,
		`SELECT `+deliveryColumns+` FROM webhook_deliveries d WHERE d.id = ?`, id), false)
}

// Deliveries lists a webhook's deliveries newest first, below before when set.
func (s *Store) Deliveries(ctx context.Context, webhookID, before int64, limit int) ([]Delivery, error) {
	where, args := `d.webhook_id = ?`, []any{webhookID}
	if before > 0 {
		where += ` AND d.id < ?`
		args = append(args, before)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+deliveryColumns+` FROM webhook_deliveries d WHERE `+where+` ORDER BY d.id DESC LIMIT ?`,
		append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()

	out := []Delivery{}
	for rows.Next() {
		d, err := scanDelivery(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDeliveriesBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM webhook_deliveries WHERE created_at < ? AND state != ?`, t.Unix(), DeliveryPending)
	if err != nil {
		return 0, fmt.Errorf("delete old deliveries: %w", err)
	}
	return res.RowsAffected()
}

func scanDelivery(row rowScanner, withTarget bool) (*Delivery, error) {
	var (
		d          Delivery
		payload    []byte
		next, last sql.NullInt64
		created    int64
	)
	dest := []any{&d.ID, &d.WebhookID, &d.EventID, &d.Kind, &payload, &d.State, &d.Attempts, &next, &last,
		&d.ResponseStatus, &d.Error, &d.DurationMS, &created}
	if withTarget {
		dest = append(dest, &d.URL, &d.Secret)
	}
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan delivery: %w", err)
	}
	d.Payload = payload
	d.NextAttemptAt = timeOf(next)
	d.LastAttemptAt = timeOf(last)
	d.CreatedAt = time.Unix(created, 0).UTC()
	return &d, nil
}
