package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/TheOriUHD/patchpilot/internal/core"
)

// AuditRepository persists audit events.
type AuditRepository struct {
	db *sql.DB
}

// NewAuditRepository creates a SQLite-backed audit repository.
func NewAuditRepository(db *DB) *AuditRepository {
	return &AuditRepository{db: db.SQLDB()}
}

// Create inserts an audit event.
func (r *AuditRepository) Create(ctx context.Context, event core.AuditEvent) error {
	details := event.Details
	if details == "" {
		details = "{}"
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO audit_events (actor, action, target_type, target_id, details, source_ip)
        VALUES (?, ?, ?, ?, ?, ?)
    `, event.Actor, event.Action, event.TargetType, event.TargetID, details, event.SourceIP)
	if err != nil {
		return fmt.Errorf("create audit event: %w", err)
	}
	return nil
}
