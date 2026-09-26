-- kbit/s each way; 0 means no limit.
ALTER TABLE wg_peers ADD COLUMN speed_limit INTEGER NOT NULL DEFAULT 0;
