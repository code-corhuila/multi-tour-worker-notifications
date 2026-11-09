package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/port/out"
	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/usecase"
)

type fakeMailer struct {
	sent []out.Message
	err  error
}

func (f *fakeMailer) Send(_ context.Context, m out.Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func newNotify(m out.Mailer) *usecase.Notify {
	return usecase.NewNotify(m, slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func resetEvent(id, payload string) usecase.Event {
	return usecase.Event{ID: id, Type: "PasswordReset", TenantID: "11111111-1111-4111-8111-111111111111", Payload: []byte(payload)}
}

const adminPayload = `{"accountId":"a","email":"owner@acme.test","tenantName":"Acme Tours","roles":["TENANT_ADMINISTRATOR"],"resetAt":"2026-11-09T13:05:00Z"}`

// AC-WK4-01.1
func TestA_admin_reset_sends_one_email(t *testing.T) {
	m := &fakeMailer{}
	got, err := newNotify(m).Handle(context.Background(), resetEvent("e1", adminPayload))
	if err != nil || got != usecase.Sent {
		t.Fatalf("outcome %v, err %v, want Sent", got, err)
	}
	if len(m.sent) != 1 || m.sent[0].To != "owner@acme.test" || m.sent[0].FromName != "Acme Tours" {
		t.Fatalf("message %+v", m.sent)
	}
	if !strings.Contains(m.sent[0].HTMLBody, "09/11/2026 08:05") {
		t.Fatalf("the time of the reset is not in the body:\n%s", m.sent[0].HTMLBody)
	}
}

// The Platform Administrator belongs to no tenant: the sender is the platform.
func TestA_platform_admin_reset_is_sent_by_the_platform(t *testing.T) {
	m := &fakeMailer{}
	payload := `{"accountId":"a","email":"root@multitour.test","roles":["PLATFORM_ADMINISTRATOR"],"resetAt":"2026-11-09T13:05:00Z"}`
	if got, err := newNotify(m).Handle(context.Background(), usecase.Event{ID: "e2", Type: "PasswordReset", Payload: []byte(payload)}); err != nil || got != usecase.Sent {
		t.Fatalf("outcome %v, err %v", got, err)
	}
	if m.sent[0].FromName != "Multitour" {
		t.Fatalf("sender %q, want Multitour", m.sent[0].FromName)
	}
}

// AC-WK4-01.2 and RN-WK4-04
func TestA_customer_reset_sends_nothing(t *testing.T) {
	m := &fakeMailer{}
	payload := `{"accountId":"a","email":"c@x.test","tenantName":"Acme Tours","roles":["CUSTOMER"],"resetAt":"2026-11-09T13:05:00Z"}`
	got, err := newNotify(m).Handle(context.Background(), resetEvent("e3", payload))
	if err != nil || got != usecase.NotNeeded || len(m.sent) != 0 {
		t.Fatalf("outcome %v, err %v, sent %d, want NotNeeded and nothing sent", got, err, len(m.sent))
	}
}

// AC-WK4-01.4
func TestA_a_repeated_event_sends_no_second_email(t *testing.T) {
	m := &fakeMailer{}
	n := newNotify(m)
	_, _ = n.Handle(context.Background(), resetEvent("e4", adminPayload))
	got, err := n.Handle(context.Background(), resetEvent("e4", adminPayload))
	if err != nil || got != usecase.Repeated || len(m.sent) != 1 {
		t.Fatalf("outcome %v, err %v, sent %d, want Repeated and one e-mail", got, err, len(m.sent))
	}
}

// AC-WK4-01.3: a failed send is not remembered, so the retry of job 7 sends it.
func TestA_a_failed_send_is_retried(t *testing.T) {
	m := &fakeMailer{err: errors.New("smtp down")}
	n := newNotify(m)
	if _, err := n.Handle(context.Background(), resetEvent("e5", adminPayload)); err == nil {
		t.Fatal("a failed send must return the error")
	}
	m.err = nil
	got, err := n.Handle(context.Background(), resetEvent("e5", adminPayload))
	if err != nil || got != usecase.Sent || len(m.sent) != 1 {
		t.Fatalf("retry: outcome %v, err %v, sent %d", got, err, len(m.sent))
	}
}

// AC-WK4-01.6
func TestA_unknown_type_or_missing_email_is_an_error_not_a_loss(t *testing.T) {
	cases := []struct {
		name  string
		event usecase.Event
		want  error
	}{
		{"unknown type", usecase.Event{ID: "e6", Type: "ReservationCreated", Payload: []byte(`{}`)}, usecase.ErrUnknownType},
		{"no email", resetEvent("e7", `{"roles":["TENANT_ADMINISTRATOR"],"resetAt":"2026-11-09T13:05:00Z"}`), usecase.ErrInvalidPayload},
		{"malformed json", resetEvent("e8", `{"roles":`), usecase.ErrInvalidPayload},
		{"no reset time", resetEvent("e9", `{"email":"a@b.test","roles":["TENANT_ADMINISTRATOR"]}`), usecase.ErrInvalidPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &fakeMailer{}
			_, err := newNotify(m).Handle(context.Background(), tc.event)
			if !errors.Is(err, tc.want) || len(m.sent) != 0 {
				t.Fatalf("err %v, sent %d, want %v and nothing sent", err, len(m.sent), tc.want)
			}
		})
	}
}

// RN-WK4-02: whatever else the payload carries never reaches the e-mail.
func TestA_extra_fields_of_the_payload_never_reach_the_email(t *testing.T) {
	m := &fakeMailer{}
	payload := `{"email":"o@acme.test","tenantName":"Acme","roles":["TENANT_ADMINISTRATOR"],"resetAt":"2026-11-09T13:05:00Z","password":"Sup3rSecret","code":"918273","token":"abc.def"}`
	if _, err := newNotify(m).Handle(context.Background(), resetEvent("e10", payload)); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Sup3rSecret", "918273", "abc.def"} {
		if strings.Contains(m.sent[0].HTMLBody, leaked) || strings.Contains(m.sent[0].Subject, leaked) {
			t.Fatalf("%q reached the e-mail", leaked)
		}
	}
}
