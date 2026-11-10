package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/port/out"
	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/usecase"
)

// HU-COM-015 (AC-15.8, AC-15.9, AC-15.10, AC-15.13): the event of a notice carries no personal datum; job 4 reads the content at send time and sends one e-mail.

const (
	tenantID = "11111111-1111-4111-8111-111111111111"
	noticeID = "22222222-2222-4222-8222-222222222222"
)

const clientNoticePayload = `{"customerId":"33333333-3333-4333-8333-333333333333","notificationType":"PROOF_APPROVED","triggeringEventType":"PaymentValidated","triggeringEventId":"44444444-4444-4444-8444-444444444444"}`

type fakeReader struct {
	content out.NoticeContent
	err     error
	calls   []string
}

func (f *fakeReader) Read(_ context.Context, tenant, event string) (out.NoticeContent, error) {
	f.calls = append(f.calls, tenant+"/"+event)
	return f.content, f.err
}

func approvedContent() out.NoticeContent {
	amount := int64(50000000)
	return out.NoticeContent{EventID: noticeID, NotificationType: "PROOF_APPROVED", RecipientEmail: "ana@cliente.test", RecipientName: "Ana Pérez",
		TourName: "Cañón del Chicamocha", DepartureStartsAt: time.Date(2026, 12, 5, 13, 0, 0, 0, time.UTC), AmountCents: &amount}
}

func clientNotice(id string) usecase.Event {
	return usecase.Event{ID: id, Type: "ClientNotified", TenantID: tenantID, Payload: []byte(clientNoticePayload)}
}

func newClientNotify(m out.Mailer, r out.NoticeReader, logs io.Writer) *usecase.Notify {
	return usecase.NewNotify(m, slog.New(slog.NewJSONHandler(logs, nil))).WithNotices(r)
}

func TestA_a_client_notice_reads_its_content_and_sends_one_email(t *testing.T) {
	m, r := &fakeMailer{}, &fakeReader{content: approvedContent()}
	got, err := newClientNotify(m, r, io.Discard).Handle(context.Background(), clientNotice(noticeID))
	if err != nil || got != usecase.Sent {
		t.Fatalf("outcome %v, err %v, want Sent", got, err)
	}
	if len(r.calls) != 1 || r.calls[0] != tenantID+"/"+noticeID {
		t.Fatalf("content read %v, want the tenant of the event and the id of the notice", r.calls)
	}
	if len(m.sent) != 1 || m.sent[0].To != "ana@cliente.test" || m.sent[0].FromName != "Multitour" || m.sent[0].Subject != "Aprobamos tu comprobante de pago" ||
		!strings.Contains(m.sent[0].HTMLBody, "Ana Pérez") || !strings.Contains(m.sent[0].HTMLBody, "$500.000") {
		t.Fatalf("message %+v", m.sent)
	}
}

// AC-15.10: an event delivered twice sends one e-mail and does not read the content again.
func TestA_a_repeated_client_notice_sends_no_second_email(t *testing.T) {
	m, r := &fakeMailer{}, &fakeReader{content: approvedContent()}
	n := newClientNotify(m, r, io.Discard)
	if _, err := n.Handle(context.Background(), clientNotice(noticeID)); err != nil {
		t.Fatal(err)
	}
	if got, err := n.Handle(context.Background(), clientNotice(noticeID)); err != nil || got != usecase.Repeated || len(m.sent) != 1 || len(r.calls) != 1 {
		t.Fatalf("outcome %v, err %v, sent %d, reads %d", got, err, len(m.sent), len(r.calls))
	}
}

// AC-15.6, C-05: a failure is an error for job 7 to retry; nothing is recorded as handled, so the retry sends.
func TestA_a_failed_read_or_send_is_an_error_and_the_retry_sends(t *testing.T) {
	m, r := &fakeMailer{}, &fakeReader{err: errors.New("http 503")}
	n := newClientNotify(m, r, io.Discard)
	if _, err := n.Handle(context.Background(), clientNotice(noticeID)); err == nil || errors.Is(err, usecase.ErrInvalidPayload) || len(m.sent) != 0 {
		t.Fatalf("error %v, sent %d, want a plain error and no e-mail", err, len(m.sent))
	}
	r.err, r.content = nil, approvedContent()
	m.err = errors.New("smtp down")
	if _, err := n.Handle(context.Background(), clientNotice(noticeID)); err == nil {
		t.Fatal("a failed send was not an error")
	}
	m.err = nil
	if got, err := n.Handle(context.Background(), clientNotice(noticeID)); err != nil || got != usecase.Sent || len(m.sent) != 1 {
		t.Fatalf("retry: outcome %v, err %v, sent %d", got, err, len(m.sent))
	}
}

// A worker without the address of the service answers a failure, never a silent success.
func TestA_a_client_notice_without_a_content_reader_is_a_failure(t *testing.T) {
	m := &fakeMailer{}
	n := usecase.NewNotify(m, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if _, err := n.Handle(context.Background(), clientNotice(noticeID)); !errors.Is(err, usecase.ErrContentUnavailable) || len(m.sent) != 0 {
		t.Fatalf("error %v, sent %d, want ErrContentUnavailable", err, len(m.sent))
	}
}

func TestA_a_client_notice_with_a_bad_id_is_an_invalid_payload_and_reads_nothing(t *testing.T) {
	for name, e := range map[string]usecase.Event{
		"tenant is not a uuid": {ID: noticeID, Type: "ClientNotified", TenantID: "../x", Payload: []byte(clientNoticePayload)},
		"no tenant":            {ID: noticeID, Type: "ClientNotified", Payload: []byte(clientNoticePayload)},
		"event is not a uuid":  {ID: "n1", Type: "ClientNotified", TenantID: tenantID, Payload: []byte(clientNoticePayload)},
	} {
		r := &fakeReader{content: approvedContent()}
		if _, err := newClientNotify(&fakeMailer{}, r, io.Discard).Handle(context.Background(), e); !errors.Is(err, usecase.ErrInvalidPayload) || len(r.calls) != 0 {
			t.Errorf("%s: error %v, reads %d, want ErrInvalidPayload and no read", name, err, len(r.calls))
		}
	}
}

// What ends in a header is one bare address; an unknown type has no template and ends failed where a person sees it.
func TestA_a_client_notice_refuses_a_bad_address_and_an_unknown_type(t *testing.T) {
	for name, change := range map[string]func(*out.NoticeContent){
		"address with a line break": func(c *out.NoticeContent) { c.RecipientEmail = "ana@cliente.test\r\nBcc: x@y.test" },
		"address with a name":       func(c *out.NoticeContent) { c.RecipientEmail = "Ana <ana@cliente.test>" },
		"no address":                func(c *out.NoticeContent) { c.RecipientEmail = "" },
	} {
		content := approvedContent()
		change(&content)
		m := &fakeMailer{}
		if _, err := newClientNotify(m, &fakeReader{content: content}, io.Discard).Handle(context.Background(), clientNotice(noticeID)); !errors.Is(err, usecase.ErrInvalidPayload) || len(m.sent) != 0 {
			t.Errorf("%s: error %v, sent %d, want ErrInvalidPayload and no e-mail", name, err, len(m.sent))
		}
	}
	content := approvedContent()
	content.NotificationType = "SOMETHING_ELSE"
	if _, err := newClientNotify(&fakeMailer{}, &fakeReader{content: content}, io.Discard).Handle(context.Background(), clientNotice(noticeID)); !errors.Is(err, usecase.ErrUnknownType) {
		t.Fatalf("error %v, want ErrUnknownType", err)
	}
}

// AC-15.13, RNF-03: no log line carries an address, a name or the content of the mail.
func TestA_no_log_line_of_a_client_notice_carries_the_address_the_name_or_the_content(t *testing.T) {
	var logs bytes.Buffer
	if _, err := newClientNotify(&fakeMailer{}, &fakeReader{content: approvedContent()}, &logs).Handle(context.Background(), clientNotice(noticeID)); err != nil {
		t.Fatal(err)
	}
	if _, err := newClientNotify(&fakeMailer{err: errors.New("smtp")}, &fakeReader{content: approvedContent()}, &logs).Handle(context.Background(), clientNotice("55555555-5555-4555-8555-555555555555")); err == nil {
		t.Fatal("the send did not fail")
	}
	for _, secret := range []string{"ana@cliente.test", "Ana Pérez", "Chicamocha", "500.000"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("the log carries %q:\n%s", secret, logs.String())
		}
	}
	if !strings.Contains(logs.String(), noticeID) || !strings.Contains(logs.String(), "PROOF_APPROVED") {
		t.Fatalf("the log lacks the event id or the type:\n%s", logs.String())
	}
}
