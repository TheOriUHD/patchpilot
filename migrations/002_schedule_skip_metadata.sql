ALTER TABLE schedules ADD COLUMN last_skipped_at DATETIME;
ALTER TABLE schedules ADD COLUMN last_skip_reason TEXT DEFAULT '';
