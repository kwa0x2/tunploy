CREATE TABLE wg_instances (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 TEXT    NOT NULL,
    address              TEXT    NOT NULL,
    listen_port          INTEGER NOT NULL,
    private_key          TEXT    NOT NULL,
    public_key           TEXT    NOT NULL,
    endpoint             TEXT    NOT NULL,
    dns                  TEXT    NOT NULL DEFAULT '',
    mtu                  INTEGER NOT NULL DEFAULT 0,
    persistent_keepalive INTEGER NOT NULL DEFAULT 0,
    client_allowed_ips   TEXT    NOT NULL,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);

CREATE UNIQUE INDEX wg_instances_name_idx ON wg_instances (name COLLATE NOCASE);
CREATE UNIQUE INDEX wg_instances_port_idx ON wg_instances (listen_port);

CREATE TABLE wg_peers (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id   INTEGER NOT NULL REFERENCES wg_instances (id) ON DELETE CASCADE,
    name          TEXT    NOT NULL,
    address       TEXT    NOT NULL,
    private_key   TEXT    NOT NULL,
    public_key    TEXT    NOT NULL,
    preshared_key TEXT    NOT NULL,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE UNIQUE INDEX wg_peers_name_idx ON wg_peers (instance_id, name COLLATE NOCASE);
CREATE UNIQUE INDEX wg_peers_address_idx ON wg_peers (instance_id, address);
CREATE UNIQUE INDEX wg_peers_public_key_idx ON wg_peers (public_key);
