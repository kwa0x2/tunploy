-- No foreign keys: history outlives the servers and devices it mentions.
CREATE TABLE events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at    INTEGER NOT NULL,
    kind          TEXT    NOT NULL,
    instance_id   INTEGER,
    instance_name TEXT    NOT NULL DEFAULT '',
    peer_id       INTEGER,
    peer_name     TEXT    NOT NULL DEFAULT '',
    ip            TEXT    NOT NULL DEFAULT '',
    country       TEXT    NOT NULL DEFAULT '',
    detail        TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX events_created_idx ON events (created_at);
CREATE INDEX events_instance_idx ON events (instance_id, id);
CREATE INDEX events_peer_idx ON events (peer_id, id);
