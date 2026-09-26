-- Clients ask a resolver on the server's tunnel address, which forwards to
-- the server's dns list; otherwise they ask that list themselves.
ALTER TABLE wg_instances ADD COLUMN dns_on_server INTEGER NOT NULL DEFAULT 0;
