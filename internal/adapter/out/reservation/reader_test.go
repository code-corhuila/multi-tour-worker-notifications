package reservation_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/adapter/out/reservation"
)

// HU-COM-015 AC-15.9: job 4 asks for the content with its SERVICE token and the tenant in the path.

const (
	tenant = "11111111-1111-4111-8111-111111111111"
	notice = "22222222-2222-4222-8222-222222222222"
)

func TestC_the_reader_asks_the_content_route_with_the_service_token_and_maps_the_answer(t *testing.T) {
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"eventId":"` + notice + `","notificationType":"REFUND_PROCESSED","recipientEmail":"ana@cliente.test","recipientName":"Ana","reservationId":"33333333-3333-4333-8333-333333333333","tourName":"Tour","departureStartsAt":"2026-12-05T13:00:00Z","amountCents":120000,"pendingBalanceCents":0}`))
	}))
	defer srv.Close()
	got, err := reservation.New(srv.URL, "svc-token").Read(context.Background(), tenant, notice)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/commercial-reservation/internal/tenants/"+tenant+"/client-notices/"+notice || auth != "Bearer svc-token" {
		t.Fatalf("path %q, authorization %q", path, auth)
	}
	if got.NotificationType != "REFUND_PROCESSED" || got.RecipientEmail != "ana@cliente.test" || got.RecipientName != "Ana" || got.TourName != "Tour" ||
		!got.DepartureStartsAt.Equal(time.Date(2026, 12, 5, 13, 0, 0, 0, time.UTC)) || got.AmountCents == nil || *got.AmountCents != 120000 ||
		got.PendingBalanceCents == nil || *got.PendingBalanceCents != 0 || got.CauseCode != "" {
		t.Fatalf("content %+v", got)
	}
}

// A refusal or a missing notice is an error that names the status and never the token.
func TestC_the_reader_turns_a_refusal_into_an_error_without_the_token(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusBadGateway} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"X","message":"no svc-token here","traceId":"t"}`))
		}))
		_, err := reservation.New(srv.URL, "svc-token").Read(context.Background(), tenant, notice)
		srv.Close()
		if err == nil || strings.Contains(err.Error(), "svc-token") {
			t.Fatalf("status %d: error %v, want an error without the token", status, err)
		}
	}
}
