ALTER TABLE schedules ADD COLUMN max_concurrent INTEGER NOT NULL DEFAULT 1 CHECK (max_concurrent BETWEEN 1 AND 10);

INSERT OR IGNORE INTO settings (key, value) VALUES ('experimental_concurrency_enabled', 'false');
