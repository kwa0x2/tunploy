CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    -- The start of the key, so the admin can tell keys apart.
    prefix       TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL,
    scopes       TEXT    NOT NULL,
    expires_at   INTEGER,
    last_used_at INTEGER,
    last_used_ip TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);

CREATE UNIQUE INDEX api_keys_name_idx ON api_keys (name COLLATE NOCASE);
CREATE UNIQUE INDEX api_keys_token_idx ON api_keys (token_hash);

-- A retried POST gets the stored response instead of running twice.
-- status 0 marks a request that is still running.
CREATE TABLE api_idempotency (
    api_key_id   INTEGER NOT NULL REFERENCES api_keys (id) ON DELETE CASCADE,
    key          TEXT    NOT NULL,
    request_hash TEXT    NOT NULL,
    status       INTEGER NOT NULL DEFAULT 0,
    content_type TEXT    NOT NULL DEFAULT '',
    body         BLOB    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (api_key_id, key)
) WITHOUT ROWID;

-- '' private_key: the client made its own key pair and kept the private half.
ALTER TABLE wg_peers ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE wg_peers ADD COLUMN metadata TEXT NOT NULL DEFAULT '';
CREATE INDEX wg_peers_external_idx ON wg_peers (external_id) WHERE external_id != '';

-- Who made a change: '' for the admin, otherwise "api:<key name>".
ALTER TABLE events ADD COLUMN actor TEXT NOT NULL DEFAULT '';
