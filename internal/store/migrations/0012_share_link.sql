-- A link that shows a device's owner its config and usage without signing
-- in. Kept in the clear like the private key, which it hands out anyway.
ALTER TABLE wg_peers ADD COLUMN share_token TEXT;
ALTER TABLE wg_peers ADD COLUMN share_expires_at INTEGER;
CREATE UNIQUE INDEX wg_peers_share_token_idx ON wg_peers (share_token);
