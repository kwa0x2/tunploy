-- Machines the panel reaches over SSH. The panel's own machine has no row:
-- node_id 0 stands for it.
CREATE TABLE nodes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    host         TEXT    NOT NULL,
    port         INTEGER NOT NULL,
    username     TEXT    NOT NULL,
    -- authorized_keys format, pinned when the node was added.
    host_key     TEXT    NOT NULL,
    last_seen_at INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE UNIQUE INDEX nodes_name_idx ON nodes (name COLLATE NOCASE);
CREATE UNIQUE INDEX nodes_host_idx ON nodes (host COLLATE NOCASE, port);

ALTER TABLE wg_instances ADD COLUMN node_id INTEGER NOT NULL DEFAULT 0;

-- Ports only clash on the same machine.
DROP INDEX wg_instances_port_idx;
CREATE UNIQUE INDEX wg_instances_port_idx ON wg_instances (node_id, listen_port);
CREATE INDEX wg_instances_node_idx ON wg_instances (node_id);

-- Kept by name as well, like servers: history outlives the node.
ALTER TABLE events ADD COLUMN node_name TEXT NOT NULL DEFAULT '';
