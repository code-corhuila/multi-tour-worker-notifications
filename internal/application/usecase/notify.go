// Package usecase holds the job of the worker: an event in, at most one e-mail out.
package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
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
	// ErrContentUnavailable is answered as a failure too: a customer notice cannot be written without its content.
	ErrContentUnavailable = errors.New("the content of the customer notices cannot be read")
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
	// notices reads the content of a customer notice; nil when the worker has no address of commercial-reservation.
	notices out.NoticeReader
}

// NewNotify returns the job over its only dependency, the mailer.
func NewNotify(mailer out.Mailer, log *slog.Logger) *Notify {
	return &Notify{mailer: mailer, log: log, seen: newSeen(recentEvents)}
}

// Handle sends the notice of the event. An error means nothing was recorded as handled, so a retry sends it.
func (n *Notify) Handle(ctx context.Context, e Event) (Outcome, error) {
	var handle func(context.Context, Event) (Outcome, error)
	switch e.Type {
	case "PasswordReset":
		handle = n.passwordReset
	case "ClientNotified":
		handle = n.clientNotice
	case "EmergencyAccessNoticeRequested":
		handle = n.emergencyNotice
	default:
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
	outcome, err := handle(ctx, e)
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

// emergencyNotice sends the notice that an emergency access to a tenant opened or ended to one of its Administrators (HU-SEC-007 part C, notifications
// spec 002). Everything the e-mail shows is checked first, and a value that ends in a header is a bare address or a name with no line break.
func (n *Notify) emergencyNotice(ctx context.Context, e Event) (Outcome, error) {
	var p struct {
		Kind                  string    `json:"kind"`
		RecipientEmail        string    `json:"recipientEmail"`
		TenantName            string    `json:"tenantName"`
		PlatformAdministrator string    `json:"platformAdministrator"`
		Reason                string    `json:"reason"`
		Scope                 []string  `json:"scope"`
		IncludesSensitiveData bool      `json:"includesSensitiveData"`
		StartedAt             time.Time `json:"startedAt"`
		EndedAt               time.Time `json:"endedAt"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if p.Kind != templates.NoticeOpened && p.Kind != templates.NoticeClosed {
		return 0, fmt.Errorf("%w: kind is not OPENED or CLOSED", ErrInvalidPayload)
	}
	if p.RecipientEmail == "" || p.PlatformAdministrator == "" || p.Reason == "" || p.StartedAt.IsZero() || (p.Kind == templates.NoticeClosed && p.EndedAt.IsZero()) {
		return 0, fmt.Errorf("%w: recipientEmail, platformAdministrator, reason, startedAt and, to close, endedAt are required", ErrInvalidPayload)
	}
	if a, err := mail.ParseAddress(p.RecipientEmail); err != nil || a.Address != p.RecipientEmail {
		return 0, fmt.Errorf("%w: recipientEmail is not one bare address", ErrInvalidPayload)
	}
	sender := p.TenantName
	if sender == "" {
		sender = platformSender
	}
	if strings.ContainsAny(sender, "\r\n") {
		return 0, fmt.Errorf("%w: tenantName has a line break", ErrInvalidPayload)
	}
	subject, body, err := templates.EmergencyNotice(templates.EmergencyNoticeData{
		Kind: p.Kind, TenantName: sender, PlatformAdministrator: p.PlatformAdministrator, Reason: p.Reason, Areas: p.Scope,
		IncludesSensitiveData: p.IncludesSensitiveData, StartedAt: p.StartedAt, EndedAt: p.EndedAt,
	})
	if err != nil {
		return 0, err
	}
	if err := n.mailer.Send(ctx, out.Message{FromName: sender, To: p.RecipientEmail, Subject: subject, HTMLBody: body}); err != nil {
		return 0, fmt.Errorf("send emergency access notice: %w", err)
	}
	n.log.InfoContext(ctx, "emergency access notice sent", "eventId", e.ID, "tenantId", e.TenantID, "kind", p.Kind)
	return Sent, nil
}

// WithNotices gives the job the reader of the content of the customer notices (HU-COM-015); without it a ClientNotified event is a failure.
func (n *Notify) WithNotices(reader out.NoticeReader) *Notify {
	n.notices = reader
	return n
}

// clientNotice sends the e-mail of one customer notice (HU-COM-015). The event carries no personal datum (AC-15.8): the recipient and the
// variables of the template are read at send time from the service that owns the notice, with the tenant of the event in the path (AC-15.9).
// Neither the content nor the address reaches a log (AC-15.13); the log holds the event, the tenant and the type.
func (n *Notify) clientNotice(ctx context.Context, e Event) (Outcome, error) {
	if n.notices == nil {
		return 0, fmt.Errorf("%w: COMMERCIAL_RESERVATION_BASE_URL and SERVICE_TOKEN are not set", ErrContentUnavailable)
	}
	if !uuidPattern.MatchString(e.TenantID) || !uuidPattern.MatchString(e.ID) {
		return 0, fmt.Errorf("%w: the tenant and the event must be identifiers", ErrInvalidPayload)
	}
	content, err := n.notices.Read(ctx, e.TenantID, e.ID)
	if err != nil {
		return 0, err
	}
	// The address ends in a header of the message: one bare address and no line break, so nothing can add a header.
	if a, err := mail.ParseAddress(content.RecipientEmail); err != nil || a.Address != content.RecipientEmail {
		return 0, fmt.Errorf("%w: recipientEmail is not one bare address", ErrInvalidPayload)
	}
	subject, body, err := templates.ClientNotice(templates.ClientNoticeData{Type: content.NotificationType, RecipientName: content.RecipientName,
		TourName: content.TourName, DepartureStartsAt: content.DepartureStartsAt, AmountCents: content.AmountCents, PendingBalanceCents: content.PendingBalanceCents})
	if errors.Is(err, templates.ErrUnknownNotice) {
		return 0, fmt.Errorf("%w: %s", ErrUnknownType, content.NotificationType)
	}
	if err != nil {
		return 0, err
	}
	if err := n.mailer.Send(ctx, out.Message{FromName: platformSender, To: content.RecipientEmail, Subject: subject, HTMLBody: body}); err != nil {
		return 0, fmt.Errorf("send customer notice: %w", err)
	}
	n.log.InfoContext(ctx, "customer notice sent", "eventId", e.ID, "tenantId", e.TenantID, "notificationType", content.NotificationType)
	return Sent, nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
