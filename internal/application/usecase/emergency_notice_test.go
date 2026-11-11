package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/usecase"
)

// HU-SEC-007 part C, notifications spec 002: one e-mail for each EmergencyAccessNoticeRequested.

const openedNotice = `{"kind":"OPENED","recipientEmail":"ana@acme.test","tenantName":"Acme Tours","platformAdministrator":"root@multitour.test","reason":"Las cajas muestran saldo negativo","scope":["CASH"],"includesSensitiveData":false,"startedAt":"2026-11-10T15:48:51Z","authorizationId":"a1"}`
const closedNotice = `{"kind":"CLOSED","recipientEmail":"ana@acme.test","tenantName":"Acme Tours","platformAdministrator":"root@multitour.test","reason":"Las cajas muestran saldo negativo","scope":["CASH","CUSTOMERS"],"includesSensitiveData":true,"startedAt":"2026-11-10T15:48:51Z","endedAt":"2026-11-10T17:20:00Z","authorizationId":"a1"}`

func noticeEvent(id, payload string) usecase.Event {
	return usecase.Event{ID: id, Type: "EmergencyAccessNoticeRequested", TenantID: "11111111-1111-4111-8111-111111111111", Payload: []byte(payload)}
}

// AC-WK4-02.1, .2, .3
func TestA_an_emergency_notice_sends_one_email_to_the_recipient_with_the_tenant_as_sender(t *testing.T) {
	m := &fakeMailer{}
	got, err := newNotify(m).Handle(context.Background(), noticeEvent("n1", openedNotice))
	if err != nil || got != usecase.Sent {
		t.Fatalf("outcome %v, err %v, want Sent", got, err)
	}
	if len(m.sent) != 1 || m.sent[0].To != "ana@acme.test" || m.sent[0].FromName != "Acme Tours" || !strings.Contains(m.sent[0].Subject, "Acme Tours") ||
		!strings.Contains(m.sent[0].HTMLBody, "root@multitour.test") || !strings.Contains(m.sent[0].HTMLBody, "Caja") || strings.Contains(m.sent[0].HTMLBody, "Fin:") {
		t.Fatalf("message %+v", m.sent)
	}
	if got, err := newNotify(m).Handle(context.Background(), noticeEvent("n2", closedNotice)); err != nil || got != usecase.Sent {
		t.Fatalf("closed: outcome %v, err %v", got, err)
	}
	if len(m.sent) != 2 || !strings.Contains(m.sent[1].HTMLBody, "Fin: 10/11/2026 12:20") || !strings.Contains(m.sent[1].HTMLBody, "Datos sensibles: Sí") {
		t.Fatalf("closed message %+v", m.sent)
	}
}

// A notice with an empty tenant name is the platform's.
func TestA_an_emergency_notice_without_a_tenant_name_is_sent_by_the_platform(t *testing.T) {
	m := &fakeMailer{}
	payload := strings.Replace(openedNotice, `"tenantName":"Acme Tours",`, "", 1)
	if got, err := newNotify(m).Handle(context.Background(), noticeEvent("n3", payload)); err != nil || got != usecase.Sent || m.sent[0].FromName != "Multitour" {
		t.Fatalf("outcome %v, err %v, sent %+v", got, err, m.sent)
	}
}

// AC-WK4-02.6
func TestA_a_repeated_emergency_notice_sends_no_second_email(t *testing.T) {
	m := &fakeMailer{}
	n := newNotify(m)
	if _, err := n.Handle(context.Background(), noticeEvent("n4", openedNotice)); err != nil {
		t.Fatal(err)
	}
	if got, err := n.Handle(context.Background(), noticeEvent("n4", openedNotice)); err != nil || got != usecase.Repeated || len(m.sent) != 1 {
		t.Fatalf("outcome %v, err %v, sent %d", got, err, len(m.sent))
	}
}

// AC-WK4-02.5: a failed send is a failure and nothing is remembered, so the retry of job 7 sends it.
func TestA_a_failed_send_of_the_notice_is_retried(t *testing.T) {
	m := &fakeMailer{err: errors.New("smtp down")}
	n := newNotify(m)
	if _, err := n.Handle(context.Background(), noticeEvent("n5", openedNotice)); err == nil {
		t.Fatal("a failed send must be a failure")
	}
	m.err = nil
	if got, err := n.Handle(context.Background(), noticeEvent("n5", openedNotice)); err != nil || got != usecase.Sent || len(m.sent) != 1 {
		t.Fatalf("retry: outcome %v, err %v, sent %d", got, err, len(m.sent))
	}
}

// AC-WK4-02.7: what the notice needs is checked, and a header is never built from a line break.
func TestA_an_invalid_emergency_notice_is_a_failure_and_sends_nothing(t *testing.T) {
	cases := map[string]string{
		"unknown kind":        strings.Replace(openedNotice, `"OPENED"`, `"OTHER"`, 1),
		"no kind":             strings.Replace(openedNotice, `"kind":"OPENED",`, "", 1),
		"no recipient":        strings.Replace(openedNotice, `"recipientEmail":"ana@acme.test",`, "", 1),
		"recipient not bare":  strings.Replace(openedNotice, "ana@acme.test", "Ana <ana@acme.test>", 1),
		"no administrator":    strings.Replace(openedNotice, `"platformAdministrator":"root@multitour.test",`, "", 1),
		"no reason":           strings.Replace(openedNotice, `"reason":"Las cajas muestran saldo negativo",`, "", 1),
		"no start":            strings.Replace(openedNotice, `"startedAt":"2026-11-10T15:48:51Z",`, "", 1),
		"closed without end":  strings.Replace(closedNotice, `"endedAt":"2026-11-10T17:20:00Z",`, "", 1),
		"tenant with a break": strings.Replace(openedNotice, "Acme Tours", `Acme\r\nBcc: x@y.test`, 1),
		"not json":            `{`,
	}
	for name, payload := range cases {
		m := &fakeMailer{}
		_, err := newNotify(m).Handle(context.Background(), noticeEvent("n6", payload))
		if !errors.Is(err, usecase.ErrInvalidPayload) || len(m.sent) != 0 {
			t.Errorf("%s: err %v, sent %d, want ErrInvalidPayload and no e-mail", name, err, len(m.sent))
		}
	}
}

// The facts of security carry the instants with microseconds (`2026-11-10T15:48:51.123456Z`): that is the format the notice really gets.
func TestA_an_emergency_notice_with_the_microseconds_security_writes_is_sent(t *testing.T) {
	m := &fakeMailer{}
	payload := strings.Replace(strings.Replace(closedNotice, "15:48:51Z", "15:48:51.123456Z", 1), "17:20:00Z", "17:20:00.654321Z", 1)
	if got, err := newNotify(m).Handle(context.Background(), noticeEvent("n7", payload)); err != nil || got != usecase.Sent {
		t.Fatalf("outcome %v, err %v", got, err)
	}
	if !strings.Contains(m.sent[0].HTMLBody, "Inicio: 10/11/2026 10:48") || !strings.Contains(m.sent[0].HTMLBody, "Fin: 10/11/2026 12:20") {
		t.Fatalf("the instants are not in the body:\n%s", m.sent[0].HTMLBody)
	}
}

// AC-WK4-02.4: even if a statement or a justification of the sensitive data reached the event, nothing of it reaches the body.
func TestA_what_the_notice_does_not_list_never_reaches_the_body_even_if_the_event_carries_it(t *testing.T) {
	m := &fakeMailer{}
	payload := strings.Replace(openedNotice, `"authorizationId":"a1"`,
		`"statement":"declaracion-secreta-xyz","sensitiveReason":"justificacion-sensible-xyz","password":"clave-xyz","link":"https://example.test/x"`, 1)
	if _, err := newNotify(m).Handle(context.Background(), noticeEvent("n8", payload)); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"declaracion-secreta-xyz", "justificacion-sensible-xyz", "clave-xyz", "https://example.test"} {
		if strings.Contains(m.sent[0].HTMLBody, leaked) || strings.Contains(m.sent[0].Subject, leaked) {
			t.Errorf("the notice carries %q", leaked)
		}
	}
}
