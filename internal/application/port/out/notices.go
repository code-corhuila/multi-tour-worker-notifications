package out

import (
	"context"
	"time"
)

// NoticeContent is what the template of one customer notice needs, read at send time from the service that owns the notice
// (HU-COM-015 AC-15.9). It holds personal data, so it is never logged and never stored.
type NoticeContent struct {
	EventID             string
	NotificationType    string
	RecipientEmail      string
	RecipientName       string
	ReservationID       string
	TourName            string
	DepartureStartsAt   time.Time
	AmountCents         *int64
	PendingBalanceCents *int64
	CauseCode           string
}

// NoticeReader reads the content of the notice of a tenant by its event id. An error means it could not be read; the event is retried.
type NoticeReader interface {
	Read(ctx context.Context, tenantID, eventID string) (NoticeContent, error)
}
