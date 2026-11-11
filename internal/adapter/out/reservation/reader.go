// Package reservation is the output adapter that reads the content of a customer notice from commercial-reservation.
package reservation

import (
	"context"
	"fmt"
	"net/url"
	"time"

	runtimeconfig "github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/port/out"
)

// Reader asks the internal route of the notice with the service token of this worker (ADR-004 D5); the tenant is in the path.
type Reader struct {
	client *httpclient.Client
}

var _ out.NoticeReader = (*Reader)(nil)

// New returns a reader over the base URL of commercial-reservation (scheme, host and port only).
func New(baseURL string, token runtimeconfig.Secret) *Reader {
	return &Reader{client: httpclient.New(baseURL, token)}
}

// content is ClientNoticeContent of the contract of commercial-reservation.
type content struct {
	EventID             string    `json:"eventId"`
	NotificationType    string    `json:"notificationType"`
	RecipientEmail      string    `json:"recipientEmail"`
	RecipientName       string    `json:"recipientName"`
	ReservationID       string    `json:"reservationId"`
	TourName            string    `json:"tourName"`
	DepartureStartsAt   time.Time `json:"departureStartsAt"`
	AmountCents         *int64    `json:"amountCents"`
	PendingBalanceCents *int64    `json:"pendingBalanceCents"`
	CauseCode           string    `json:"causeCode"`
}

// Read answers the content of the notice. The error names the status the service answered and never the content, the address or the token.
func (r *Reader) Read(ctx context.Context, tenantID, eventID string) (out.NoticeContent, error) {
	var c content
	path := "/api/v1/commercial-reservation/internal/tenants/" + url.PathEscape(tenantID) + "/client-notices/" + url.PathEscape(eventID)
	if err := r.client.Get(ctx, path, &c); err != nil {
		return out.NoticeContent{}, fmt.Errorf("read the content of the notice: %w", err)
	}
	return out.NoticeContent(c), nil
}
