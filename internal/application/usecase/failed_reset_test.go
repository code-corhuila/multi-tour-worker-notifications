package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/usecase"
)

func failedEvent(id, payload string) usecase.Event {
	return usecase.Event{ID: id, Type: "PasswordResetFailed", TenantID: "11111111-1111-4111-8111-111111111111", Payload: []byte(payload)}
}

const failedPayload = `{"accountId":"a","reason":"WRONG_CODE","email":"owner@acme.test","tenantName":"Acme Tours","roles":["TENANT_ADMINISTRATOR"],"failedAt":"2026-11-12T18:05:00Z"}`

// AC-WK4-03.1 and .2: one e-mail to the owner, from the tenant, with the time of the attempt in Colombia.
func TestA_failed_reset_sends_one_email(t *testing.T) {
	m := &fakeMailer{}
	got, err := newNotify(m).Handle(context.Background(), failedEvent("f1", failedPayload))
	if err != nil || got != usecase.Sent {
		t.Fatalf("outcome %v, err %v, want Sent", got, err)
	}
	if len(m.sent) != 1 || m.sent[0].To != "owner@acme.test" || m.sent[0].FromName != "Acme Tours" ||
		m.sent[0].Subject != "Intentaron restablecer la contraseña de tu cuenta" {
		t.Fatalf("message %+v", m.sent)
	}
	if !strings.Contains(m.sent[0].HTMLBody, "12/11/2026 13:05") {
		t.Fatalf("the time of the attempt is not in the body:\n%s", m.sent[0].HTMLBody)
	}
}

// The Platform Administrator belongs to no tenant: the sender is the platform.
func TestA_failed_reset_of_the_platform_administrator_is_sent_by_the_platform(t *testing.T) {
	m := &fakeMailer{}
	payload := `{"accountId":"a","reason":"POLICY","email":"root@multitour.test","roles":["PLATFORM_ADMINISTRATOR"],"failedAt":"2026-11-12T18:05:00Z"}`
	if got, err := newNotify(m).Handle(context.Background(), usecase.Event{ID: "f2", Type: "PasswordResetFailed", Payload: []byte(payload)}); err != nil || got != usecase.Sent {
		t.Fatalf("outcome %v, err %v", got, err)
	}
	if m.sent[0].FromName != "Multitour" {
		t.Fatalf("sender %q, want Multitour", m.sent[0].FromName)
	}
}

// AC-WK4-03.3: the reason and the rest of the payload never reach the e-mail.
func TestA_failed_reset_email_never_carries_the_reason_or_the_account(t *testing.T) {
	m := &fakeMailer{}
	if _, err := newNotify(m).Handle(context.Background(), failedEvent("f3", failedPayload)); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"WRONG_CODE", "accountId", "TENANT_ADMINISTRATOR", "owner@acme.test"} {
		if strings.Contains(m.sent[0].HTMLBody, leaked) || strings.Contains(m.sent[0].Subject, leaked) {
			t.Fatalf("the e-mail leaks %q:\n%s", leaked, m.sent[0].HTMLBody)
		}
	}
}

// AC-WK4-03.4: a non-administrative account sends nothing and the event counts as delivered.
func TestA_failed_reset_of_a_non_administrative_account_sends_nothing(t *testing.T) {
	m := &fakeMailer{}
	payload := `{"accountId":"a","reason":"WRONG_CODE","email":"c@x.test","tenantName":"Acme Tours","roles":["CUSTOMER"],"failedAt":"2026-11-12T18:05:00Z"}`
	got, err := newNotify(m).Handle(context.Background(), failedEvent("f4", payload))
	if err != nil || got != usecase.NotNeeded || len(m.sent) != 0 {
		t.Fatalf("outcome %v, err %v, sent %d, want NotNeeded and nothing sent", got, err, len(m.sent))
	}
}

// AC-WK4-03.4: a payload without what the notice needs is a failure, so it is never lost in silence.
func TestA_failed_reset_with_a_bad_payload_is_an_error_not_a_loss(t *testing.T) {
	cases := map[string]string{
		"no email":          `{"roles":["TENANT_ADMINISTRATOR"],"tenantName":"Acme","failedAt":"2026-11-12T18:05:00Z"}`,
		"no time":           `{"roles":["TENANT_ADMINISTRATOR"],"tenantName":"Acme","email":"o@acme.test"}`,
		"two addresses":     `{"roles":["TENANT_ADMINISTRATOR"],"tenantName":"Acme","email":"a@x.test, b@x.test","failedAt":"2026-11-12T18:05:00Z"}`,
		"a line break":      `{"roles":["TENANT_ADMINISTRATOR"],"tenantName":"Ac\nme","email":"o@acme.test","failedAt":"2026-11-12T18:05:00Z"}`,
		"malformed payload": `{"roles":`,
	}
	for name, payload := range cases {
		m := &fakeMailer{}
		if _, err := newNotify(m).Handle(context.Background(), failedEvent("f5", payload)); !errors.Is(err, usecase.ErrInvalidPayload) || len(m.sent) != 0 {
			t.Errorf("%s: err %v, sent %d, want ErrInvalidPayload and nothing sent", name, err, len(m.sent))
		}
	}
}

// AC-WK4-03.6
func TestA_a_repeated_failed_reset_sends_no_second_email(t *testing.T) {
	m := &fakeMailer{}
	n := newNotify(m)
	_, _ = n.Handle(context.Background(), failedEvent("f6", failedPayload))
	got, err := n.Handle(context.Background(), failedEvent("f6", failedPayload))
	if err != nil || got != usecase.Repeated || len(m.sent) != 1 {
		t.Fatalf("outcome %v, err %v, sent %d, want Repeated and one e-mail", got, err, len(m.sent))
	}
}

// AC-WK4-03.5: a failed send is not remembered, so the retry of job 7 sends it.
func TestA_a_failed_send_of_the_failed_reset_notice_is_retried(t *testing.T) {
	m := &fakeMailer{err: errors.New("smtp down")}
	n := newNotify(m)
	if _, err := n.Handle(context.Background(), failedEvent("f7", failedPayload)); err == nil {
		t.Fatal("a failed send must return the error")
	}
	m.err = nil
	if got, err := n.Handle(context.Background(), failedEvent("f7", failedPayload)); err != nil || got != usecase.Sent || len(m.sent) != 1 {
		t.Fatalf("retry: outcome %v, err %v, sent %d", got, err, len(m.sent))
	}
}
