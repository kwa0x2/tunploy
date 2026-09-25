ALTER TABLE wg_peers ADD COLUMN data_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE wg_peers ADD COLUMN expires_at INTEGER;
ALTER TABLE wg_peers ADD COLUMN last_handshake INTEGER;

-- WireGuard's own counters as last read; they restart from zero whenever a
-- peer leaves the interface, so usage is the growth between two reads.
CREATE TABLE wg_peer_counters (
    peer_id  INTEGER PRIMARY KEY REFERENCES wg_peers (id) ON DELETE CASCADE,
    rx_bytes INTEGER NOT NULL,
    tx_bytes INTEGER NOT NULL
);

-- day is a local calendar date (YYYY-MM-DD), so months sum by prefix.
CREATE TABLE wg_peer_usage (
    peer_id  INTEGER NOT NULL REFERENCES wg_peers (id) ON DELETE CASCADE,
    day      TEXT    NOT NULL,
    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (peer_id, day)
) WITHOUT ROWID;
