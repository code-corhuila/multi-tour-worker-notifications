// Package usecase holds the job of the worker: an event in, at most one e-mail out.
package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/port/out"
	"github.com/code-corhuila/multi-tour-worker-notifications/internal/templates"
)

const (
	recentEvents   = 10000 // piece plan D-03
	platformSender = "Multitour"

	roleTenantAdministrator   = "TENANT_ADMINISTRATOR"
	rolePlatformAdministrator = "PLATFORM_ADMINISTRATOR"
)

// ErrUnknownType and ErrInvalidPayload are answered as failures, so job 7 retries and the event ends failed where a person
// sees it, never lost in silence (spec AC-WK4-01.6).
var (
	ErrUnknownType    = errors.New("no template for this event type")
	ErrInvalidPayload = errors.New("the payload lacks what the notice needs")
)

// Event is what job 7 delivers: the envelope of ADR-004 D4 without the fields the notices do not use.
type Event struct {
	ID       string
	Type     string
	TenantID string
	Payload  json.RawMessage
}

// Outcome says what Handle did.
type Outcome int

const (
	Sent      Outcome = iota + 1 // one e-mail went out
	NotNeeded                    // the event asks for no e-mail (a customer's reset)
	Repeated                     // the same event was already handled
)

// Notify turns an event into at most one e-mail.
type Notify struct {
	// mu makes "has it been handled, send, remember it" one step: the same event delivered twice at once (a retry of job 7
	// over a slow send) sends one e-mail. Events are handled one at a time; the volume of notices is low and job 7 delivers
	// serially.
	mu     sync.Mutex
	mailer out.Mailer
	log    *slog.Logger
	seen   *seen
}

// NewNotify returns the job over its only dependency, the mailer.
func NewNotify(mailer out.Mailer, log *slog.Logger) *Notify {
	return &Notify{mailer: mailer, log: log, seen: newSeen(recentEvents)}
}

// Handle sends the notice of the event. An error means nothing was recorded as handled, so a retry sends it.
func (n *Notify) Handle(ctx context.Context, e Event) (Outcome, error) {
	if e.Type != "PasswordReset" {
		return 0, fmt.Errorf("%w: %s", ErrUnknownType, e.Type)
	}
	if e.ID == "" {
		return 0, fmt.Errorf("%w: the event has no id", ErrInvalidPayload)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.seen.has(e.ID) {
		return Repeated, nil
	}
	outcome, err := n.passwordReset(ctx, e)
	if err != nil {
		return 0, err
	}
	n.seen.add(e.ID)
	return outcome, nil
}

func (n *Notify) passwordReset(ctx context.Context, e Event) (Outcome, error) {
	var p struct {
		Email      string    `json:"email"`
		TenantName string    `json:"tenantName"`
		Roles      []string  `json:"roles"`
		ResetAt    time.Time `json:"resetAt"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	// Only the reset of an administrative account is notified (PDR section 9); a customer's reset sends nothing.
	if !slices.Contains(p.Roles, roleTenantAdministrator) && !slices.Contains(p.Roles, rolePlatformAdministrator) {
		n.log.InfoContext(ctx, "reset of a non-administrative account: no e-mail", "eventId", e.ID)
		return NotNeeded, nil
	}
	if p.Email == "" || p.ResetAt.IsZero() {
		return 0, fmt.Errorf("%w: email and resetAt are required", ErrInvalidPayload)
	}
	// Both values end in a header of the message: a bare address and no line break, so nothing can add a header.
	if a, err := mail.ParseAddress(p.Email); err != nil || a.Address != p.Email {
		return 0, fmt.Errorf("%w: email is not one bare address", ErrInvalidPayload)
	}
	sender := p.TenantName
	if sender == "" {
		sender = platformSender
	}
	if strings.ContainsAny(sender, "\r\n") {
		return 0, fmt.Errorf("%w: tenantName has a line break", ErrInvalidPayload)
	}
	subject, body, err := templates.PasswordReset(templates.PasswordResetData{TenantName: sender, ResetAt: p.ResetAt})
	if err != nil {
		return 0, err
	}
	if err := n.mailer.Send(ctx, out.Message{FromName: sender, To: p.Email, Subject: subject, HTMLBody: body}); err != nil {
		return 0, fmt.Errorf("send password reset notice: %w", err)
	}
	n.log.InfoContext(ctx, "password reset notice sent", "eventId", e.ID, "tenantId", e.TenantID)
	return Sent, nil
}
