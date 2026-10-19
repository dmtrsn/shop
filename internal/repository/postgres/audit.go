package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"sweetshop/internal/domain"
)

// Every staff business mutation is a Store method built on InStaffMutation
// (ADR 012); there is no other way to write staff_audit_events, and nothing
// updates or deletes its rows.

// StaffTx is the transaction of one audited staff mutation, limited to
// statements: the mutation cannot commit, roll back or nest a transaction,
// so its writes commit only together with its audit event. Its SQL must not
// contain transaction-control statements either.
type StaffTx struct{ tx pgx.Tx }

func (t StaffTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, sql, args...)
}

func (t StaffTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

func (t StaffTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.tx.QueryRow(ctx, sql, args...)
}

// InStaffMutation runs work and appends its audit event in one transaction:
//
//  1. the actor's staff row is locked FOR SHARE, so a missing or disabled
//     actor is ErrAuditActorInactive before any business statement, and a
//     concurrent disable waits for the commit;
//  2. work performs the business mutation and returns the resource it
//     changed;
//  3. the audit event is inserted;
//  4. the transaction commits.
//
// Any error, from work, the audit insert or the commit, leaves neither the
// mutation nor the event. The returned event carries the database time.
func (s *Store) InStaffMutation(ctx context.Context, mutation domain.StaffMutation, work func(context.Context, StaffTx) (domain.AuditTarget, error)) (domain.StaffAuditEvent, error) {
	if !mutation.Valid() {
		return domain.StaffAuditEvent{}, errors.New("staff mutation: an authorized staff mutation is required")
	}
	id, err := newAuditID()
	if err != nil {
		return domain.StaffAuditEvent{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.StaffAuditEvent{}, err
	}
	defer tx.Rollback(ctx)
	var active bool
	err = tx.QueryRow(ctx, `SELECT disabled_at IS NULL FROM staff_users WHERE id=$1 FOR SHARE`, mutation.Actor()).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return domain.StaffAuditEvent{}, domain.ErrAuditActorInactive
	}
	if err != nil {
		return domain.StaffAuditEvent{}, fmt.Errorf("lock audit actor: %w", err)
	}
	target, err := work(ctx, StaffTx{tx})
	if err != nil {
		return domain.StaffAuditEvent{}, err
	}
	event, err := mutation.Event(id, target)
	if err != nil {
		return domain.StaffAuditEvent{}, err
	}
	if err := appendStaffAudit(ctx, tx, &event); err != nil {
		return domain.StaffAuditEvent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.StaffAuditEvent{}, fmt.Errorf("commit staff mutation: %w", err)
	}
	return event, nil
}

// appendStaffAudit inserts event in the caller's transaction; it never
// begins or commits one.
func appendStaffAudit(ctx context.Context, tx pgx.Tx, event *domain.StaffAuditEvent) error {
	err := tx.QueryRow(ctx, `
		INSERT INTO staff_audit_events(id,staff_user_id,action,entity_type,entity_id,request_id)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')) RETURNING created_at`,
		event.ID, event.StaffUserID, string(event.Action), event.EntityType, event.EntityID, event.RequestID).Scan(&event.CreatedAt)
	if err != nil {
		return fmt.Errorf("append staff audit event: %w", err)
	}
	event.CreatedAt = event.CreatedAt.UTC()
	return nil
}

func newAuditID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create audit event id: %w", err)
	}
	return "aud_" + hex.EncodeToString(b), nil
}
