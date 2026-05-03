CREATE TABLE hosts (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL UNIQUE,
    hostname        TEXT    NOT NULL,
    ssh_port        INTEGER NOT NULL DEFAULT 22,
    ssh_user        TEXT    NOT NULL,
    host_type       TEXT    NOT NULL CHECK (host_type IN ('standard', 'truenas', 'haos', 'self')),
    package_manager TEXT,
    os_pretty       TEXT,
    arch            TEXT,
    is_proxmox_host INTEGER NOT NULL DEFAULT 0 CHECK (is_proxmox_host IN (0, 1)),
    is_self         INTEGER NOT NULL DEFAULT 0 CHECK (is_self IN (0, 1)),
    api_token       TEXT,
    enabled         INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    notes           TEXT    DEFAULT '',
    last_seen_at    DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE schedules (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL UNIQUE,
    frequency       TEXT    NOT NULL CHECK (frequency IN ('hourly', 'daily', 'weekly', 'monthly')),
    minute          INTEGER,
    hour            INTEGER,
    day_of_week     INTEGER,
    day_of_month    INTEGER,
    enabled         INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    last_run_at     DATETIME,
    next_run_at     DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE schedule_hosts (
    schedule_id INTEGER NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    host_id     INTEGER NOT NULL REFERENCES hosts(id)     ON DELETE CASCADE,
    order_index INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (schedule_id, host_id)
);

CREATE TABLE runs (
    id           INTEGER PRIMARY KEY,
    trigger      TEXT    NOT NULL CHECK (trigger IN ('manual', 'scheduled', 'cli')),
    schedule_id  INTEGER REFERENCES schedules(id) ON DELETE SET NULL,
    status       TEXT    NOT NULL CHECK (status IN ('queued', 'running', 'success', 'partial_failure', 'failed', 'cancelled')),
    started_at   DATETIME NOT NULL,
    finished_at  DATETIME,
    summary      TEXT     DEFAULT ''
);

CREATE TABLE host_runs (
    id                       INTEGER PRIMARY KEY,
    run_id                   INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    host_id                  INTEGER NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    status                   TEXT    NOT NULL CHECK (status IN ('pending', 'running', 'success', 'failed', 'skipped')),
    exit_code                INTEGER,
    started_at               DATETIME,
    finished_at              DATETIME,
    log_path                 TEXT,
    packages_upgraded_count  INTEGER DEFAULT 0,
    reboot_required          INTEGER DEFAULT 0 CHECK (reboot_required IN (0, 1)),
    error_summary            TEXT
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE audit_events (
    id          INTEGER PRIMARY KEY,
    occurred_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor       TEXT     NOT NULL,
    action      TEXT     NOT NULL,
    target_type TEXT     NOT NULL,
    target_id   INTEGER,
    details     TEXT     NOT NULL DEFAULT '{}',
    source_ip   TEXT
);

CREATE INDEX idx_hosts_enabled ON hosts(enabled);
CREATE INDEX idx_hosts_is_self ON hosts(is_self);
CREATE INDEX idx_schedule_hosts_order ON schedule_hosts(schedule_id, order_index);
CREATE INDEX idx_runs_status ON runs(status);
CREATE INDEX idx_runs_schedule_id ON runs(schedule_id);
CREATE INDEX idx_host_runs_run_id ON host_runs(run_id);
CREATE INDEX idx_host_runs_host_id ON host_runs(host_id);
CREATE INDEX idx_audit_events_occurred_at ON audit_events(occurred_at);
CREATE INDEX idx_audit_events_target ON audit_events(target_type, target_id);

CREATE TRIGGER hosts_updated_at
AFTER UPDATE ON hosts
FOR EACH ROW
BEGIN
    UPDATE hosts SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
END;

CREATE TRIGGER schedules_updated_at
AFTER UPDATE ON schedules
FOR EACH ROW
BEGIN
    UPDATE schedules SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
END;
