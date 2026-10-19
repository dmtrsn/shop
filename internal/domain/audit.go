package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Staff audit events record staff business mutations. An event is written
// only inside the transaction of the mutation it describes, so both commit or
// neither does; nothing updates or deletes one (ADR 012).

// ErrAuditActorInactive: the actor is missing or disabled when the mutation
// runs. It is an ErrUnauthorized, like a revoked staff session.
var ErrAuditActorInactive = fmt.Errorf("audit actor is not an active staff account: %w", ErrUnauthorized)

var (
	auditNamePattern      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	auditReferencePattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	staffIDPattern        = regexp.MustCompile(`^stf_[0-9a-f]{32}$`)
)

// AuditAction names a staff business mutation "<entity>.<verb>", for example
// "product.update" or "order.status_change": two lowercase snake_case words of
// at most 32 characters each. Actions are constants of the slice that
// implements the mutation, never client input.
type AuditAction string

func (a AuditAction) Valid() bool {
	entity, verb, ok := strings.Cut(string(a), ".")
	return ok && auditNamePattern.MatchString(entity) && auditNamePattern.MatchString(verb)
}

// AuditTarget is the resource a mutation changed: a lowercase snake_case type
// ("product") and the resource's own ID. It is a reference, not a foreign
// key, so history stays readable after the resource is archived.
type AuditTarget struct {
	Type string
	ID   string
}

func (t AuditTarget) Valid() bool {
	return auditNamePattern.MatchString(t.Type) && auditReferencePattern.MatchString(t.ID)
}

// StaffMutation is the authorized context of one audited staff mutation: the
// authenticated actor, the action and the request ID. Its fields are
// unexported, so it is built only by NewStaffMutation from a StaffUser
// principal; the zero value is invalid.
type StaffMutation struct {
	actor     string
	action    AuditAction
	requestID string
}

// NewStaffMutation takes the actor from the authenticated staff principal
// only. requestID correlates the event with logs; it is "" outside HTTP, and
// a present one must be 1–128 characters of [A-Za-z0-9._:-].
func NewStaffMutation(staff StaffUser, action AuditAction, requestID string) (StaffMutation, error) {
	if !staffIDPattern.MatchString(staff.ID) {
		return StaffMutation{}, ErrForbidden
	}
	if !action.Valid() {
		return StaffMutation{}, fmt.Errorf("invalid audit action %q", action)
	}
	if requestID != "" && !auditReferencePattern.MatchString(requestID) {
		return StaffMutation{}, &ValidationError{Fields: []FieldError{{Field: "X-Request-ID", Message: "Идентификатор запроса: до 128 символов A–Z, a–z, 0–9, «.», «_», «:», «-»"}}}
	}
	return StaffMutation{actor: staff.ID, action: action, requestID: requestID}, nil
}

func (m StaffMutation) Actor() string       { return m.actor }
func (m StaffMutation) Action() AuditAction { return m.action }
func (m StaffMutation) RequestID() string   { return m.requestID }
func (m StaffMutation) Valid() bool         { return staffIDPattern.MatchString(m.actor) && m.action.Valid() }

// Event is the audit row of this mutation for the resource it changed; the
// database sets CreatedAt.
func (m StaffMutation) Event(id string, target AuditTarget) (StaffAuditEvent, error) {
	if !m.Valid() {
		return StaffAuditEvent{}, errors.New("audit event: an authorized staff mutation is required")
	}
	if !target.Valid() {
		return StaffAuditEvent{}, fmt.Errorf("audit event: invalid target %q/%q", target.Type, target.ID)
	}
	return StaffAuditEvent{ID: id, StaffUserID: m.actor, Action: m.action, EntityType: target.Type, EntityID: target.ID, RequestID: m.requestID}, nil
}

// StaffAuditEvent is one stored audit row. It holds identifiers only and no
// free-form details, so no password, hash, token, cookie or request body has
// a field to land in. Typed change details join with the first mutation that
// needs them.
type StaffAuditEvent struct {
	ID          string
	StaffUserID string
	Action      AuditAction
	EntityType  string
	EntityID    string
	RequestID   string
	CreatedAt   time.Time
}
