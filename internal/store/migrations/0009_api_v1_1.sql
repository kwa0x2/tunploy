-- monthly counts toward the limit from the first of each month, total from
-- the start; a usage reset starts either count again.
ALTER TABLE wg_peers ADD COLUMN limit_period TEXT NOT NULL DEFAULT 'monthly';
ALTER TABLE wg_peers ADD COLUMN usage_reset_at INTEGER;
-- Usage is kept per day, so the reset day's bytes before the reset are
-- remembered and left out of the count.
ALTER TABLE wg_peers ADD COLUMN usage_reset_day TEXT NOT NULL DEFAULT '';
ALTER TABLE wg_peers ADD COLUMN usage_reset_rx INTEGER NOT NULL DEFAULT 0;
ALTER TABLE wg_peers ADD COLUMN usage_reset_tx INTEGER NOT NULL DEFAULT 0;

-- ISO 3166 country code and a free-form city, for location pickers.
ALTER TABLE wg_instances ADD COLUMN country TEXT NOT NULL DEFAULT '';
ALTER TABLE wg_instances ADD COLUMN city TEXT NOT NULL DEFAULT '';

CREATE TABLE webhooks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    url         TEXT    NOT NULL,
    -- Kept in the clear: every delivery is signed with it.
    secret      TEXT    NOT NULL,
    -- Comma-separated event kinds, or '*' for all of them.
    events      TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_by  TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE webhook_deliveries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    webhook_id      INTEGER NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    event_id        INTEGER NOT NULL DEFAULT 0,
    kind            TEXT    NOT NULL,
    payload         BLOB    NOT NULL,
    -- pending, succeeded or failed.
    state           TEXT    NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER,
    last_attempt_at INTEGER,
    response_status INTEGER NOT NULL DEFAULT 0,
    error           TEXT    NOT NULL DEFAULT '',
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL
);

CREATE INDEX webhook_deliveries_due_idx ON webhook_deliveries (next_attempt_at) WHERE state = 'pending';
CREATE INDEX webhook_deliveries_hook_idx ON webhook_deliveries (webhook_id, id);
