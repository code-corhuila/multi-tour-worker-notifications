package httpin_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	httpin "github.com/code-corhuila/multi-tour-worker-notifications/internal/adapter/in/http"
	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/usecase"
)

const (
	issuer   = "multi-tour-security"
	audience = "multi-tour"
	tenantID = "11111111-1111-4111-8111-111111111111"
)

var (
	signer   = mustKey()
	otherKey = mustKey()
)

func mustKey() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}

// fakeNotifier records what the route hands to the use case and answers as told.
type fakeNotifier struct {
	got     []usecase.Event
	outcome usecase.Outcome
	err     error
}

func (f *fakeNotifier) Handle(_ context.Context, e usecase.Event) (usecase.Outcome, error) {
	f.got = append(f.got, e)
	return f.outcome, f.err
}

func newServer(t *testing.T, n *fakeNotifier) *httptest.Server {
	t.Helper()
	return newServerLogging(t, n, io.Discard)
}

func newServerLogging(t *testing.T, n *fakeNotifier, log io.Writer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpin.New(n, httpin.NewVerifier(&signer.PublicKey, issuer, audience), slog.New(slog.NewJSONHandler(log, nil))))
	t.Cleanup(srv.Close)
	return srv
}

func claims(mut func(jwt.MapClaims)) jwt.MapClaims {
	c := jwt.MapClaims{"sub": "worker-event-relay", "iss": issuer, "aud": []string{audience}, "roles": []string{"SERVICE"},
		"exp": time.Now().Add(time.Hour).Unix()}
	if mut != nil {
		mut(c)
	}
	return c
}

func sign(t *testing.T, method jwt.SigningMethod, key any, c jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, c).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func serviceToken(t *testing.T) string { return sign(t, jwt.SigningMethodRS256, signer, claims(nil)) }

const envelope = `{"eventId":"e1","eventType":"PasswordReset","eventVersion":1,"tenantId":"` + tenantID + `","aggregateId":"a1",` +
	`"payload":{"email":"o@acme.test","roles":["TENANT_ADMINISTRATOR"]},"occurredAt":"2026-11-09T13:05:00Z"}`

func post(t *testing.T, url, token, body string, headers ...string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func tenantRoute(srv *httptest.Server) string {
	return srv.URL + "/api/v1/notifications/internal/tenants/" + tenantID + "/events"
}

func noTenantRoute(srv *httptest.Server) string {
	return srv.URL + "/api/v1/notifications/internal/events"
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	TraceID string `json:"traceId"`
}

func decodeError(t *testing.T, res *http.Response) errorBody {
	t.Helper()
	var e errorBody
	if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
		t.Fatalf("the answer is not the common envelope: %v", err)
	}
	return e
}

// AC-WK4-01.9: /health answers once the process is up, without a token.
func TestC_health_answers(t *testing.T) {
	srv := newServer(t, &fakeNotifier{})
	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
}

// AC-WK4-01.1
func TestC_admin_reset_sends_one_email_and_answers_201(t *testing.T) {
	n := &fakeNotifier{outcome: usecase.Sent}
	srv := newServer(t, n)
	res := post(t, tenantRoute(srv), serviceToken(t), envelope)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", res.StatusCode)
	}
	if len(n.got) != 1 || n.got[0].ID != "e1" || n.got[0].Type != "PasswordReset" || n.got[0].TenantID != tenantID ||
		!strings.Contains(string(n.got[0].Payload), "o@acme.test") {
		t.Fatalf("the use case got %+v", n.got)
	}
}

// A customer's reset needs no e-mail and job 7 must still see the event as delivered.
func TestC_an_event_that_needs_no_email_answers_201(t *testing.T) {
	srv := newServer(t, &fakeNotifier{outcome: usecase.NotNeeded})
	if res := post(t, tenantRoute(srv), serviceToken(t), envelope); res.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", res.StatusCode)
	}
}

// AC-WK4-01.4
func TestC_repeat_event_answers_200_without_a_second_email(t *testing.T) {
	srv := newServer(t, &fakeNotifier{outcome: usecase.Repeated})
	if res := post(t, tenantRoute(srv), serviceToken(t), envelope); res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
}

// The Platform Administrator belongs to no tenant: job 7 uses the route without tenant.
func TestC_the_route_without_tenant_takes_an_event_of_no_tenant(t *testing.T) {
	n := &fakeNotifier{outcome: usecase.Sent}
	srv := newServer(t, n)
	body := strings.Replace(envelope, `"tenantId":"`+tenantID+`",`, "", 1)
	if res := post(t, noTenantRoute(srv), serviceToken(t), body); res.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", res.StatusCode)
	}
	if len(n.got) != 1 || n.got[0].TenantID != "" {
		t.Fatalf("the use case got %+v", n.got)
	}
}

// AC-WK4-01.3, RN-WK4-01: the answer is a 5xx, so job 7 retries; the text of the failure never leaves the worker.
func TestC_smtp_failure_answers_5xx(t *testing.T) {
	srv := newServer(t, &fakeNotifier{err: fmt.Errorf("send: %w", errors.New("dial tcp 10.1.2.3:25: refused"))})
	res := post(t, tenantRoute(srv), serviceToken(t), envelope, "X-Correlation-Id", "c0ffee00-0000-4000-8000-000000000001")
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", res.StatusCode)
	}
	e := decodeError(t, res)
	if strings.Contains(e.Message, "10.1.2.3") || e.TraceID != "c0ffee00-0000-4000-8000-000000000001" {
		t.Fatalf("envelope %+v leaks the failure or lost the trace id", e)
	}
}

// A server may repeat the recipient in its reply ("550 no such user owner@acme.test"): the log keeps the cause but not the address.
func TestC_the_log_of_a_failed_send_keeps_no_e_mail_address(t *testing.T) {
	var logged bytes.Buffer
	srv := newServerLogging(t, &fakeNotifier{err: errors.New("smtp RCPT: 550 no such user owner@acme.test")}, &logged)
	post(t, tenantRoute(srv), serviceToken(t), envelope)
	if strings.Contains(logged.String(), "owner@acme.test") || !strings.Contains(logged.String(), "no such user") {
		t.Fatalf("the log should keep the cause without the address: %s", logged.String())
	}
}

// AC-WK4-01.6
func TestC_unknown_type_or_missing_email_answers_5xx(t *testing.T) {
	for _, cause := range []error{usecase.ErrUnknownType, usecase.ErrInvalidPayload} {
		srv := newServer(t, &fakeNotifier{err: fmt.Errorf("wrapped: %w", cause)})
		if res := post(t, tenantRoute(srv), serviceToken(t), envelope); res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%v: status %d, want 500", cause, res.StatusCode)
		}
	}
}

func TestC_a_bad_request_is_a_400_with_the_envelope(t *testing.T) {
	cases := map[string]string{
		"malformed json":    `{"eventId":`,
		"no event type":     strings.Replace(envelope, `"eventType":"PasswordReset",`, "", 1),
		"no event id":       strings.Replace(envelope, `"eventId":"e1",`, "", 1),
		"another tenant":    strings.Replace(envelope, tenantID, "22222222-2222-4222-8222-222222222222", 1),
		"two json values":   envelope + envelope,
		"payload too large": `{"eventId":"e1","eventType":"PasswordReset","payload":"` + strings.Repeat("x", 80<<10) + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			n := &fakeNotifier{outcome: usecase.Sent}
			srv := newServer(t, n)
			res := post(t, tenantRoute(srv), serviceToken(t), body)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", res.StatusCode)
			}
			if e := decodeError(t, res); e.Error != "VALIDATION_ERROR" || len(n.got) != 0 {
				t.Fatalf("envelope %+v, use case calls %d", e, len(n.got))
			}
		})
	}
}

// RN-WK4-03, AC-WK4-01.7: the route trusts nothing it cannot verify.
func TestC_no_service_token_is_refused(t *testing.T) {
	expired := sign(t, jwt.SigningMethodRS256, signer, claims(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }))
	noExp := sign(t, jwt.SigningMethodRS256, signer, claims(func(c jwt.MapClaims) { delete(c, "exp") }))
	noSub := sign(t, jwt.SigningMethodRS256, signer, claims(func(c jwt.MapClaims) { delete(c, "sub") }))
	hs256 := sign(t, jwt.SigningMethodHS256, []byte("a secret that is long enough for hmac"), claims(nil))
	none := sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims(nil))
	forged := sign(t, jwt.SigningMethodRS256, otherKey, claims(nil))
	badIssuer := sign(t, jwt.SigningMethodRS256, signer, claims(func(c jwt.MapClaims) { c["iss"] = "someone-else" }))
	badAudience := sign(t, jwt.SigningMethodRS256, signer, claims(func(c jwt.MapClaims) { c["aud"] = []string{"another-app"} }))
	cases := map[string]string{"no token": "", "garbage": "not.a.token", "expired": expired, "no exp": noExp, "no sub": noSub,
		"hs256": hs256, "alg none": none, "other key": forged, "other issuer": badIssuer, "other audience": badAudience}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			n := &fakeNotifier{outcome: usecase.Sent}
			srv := newServer(t, n)
			res := post(t, tenantRoute(srv), token, envelope)
			if res.StatusCode != http.StatusUnauthorized || len(n.got) != 0 {
				t.Fatalf("status %d, use case calls %d, want 401 and none", res.StatusCode, len(n.got))
			}
			if e := decodeError(t, res); e.Error != "UNAUTHORIZED" {
				t.Fatalf("envelope %+v", e)
			}
		})
	}
}

func TestC_other_tokens_get_403(t *testing.T) {
	user := sign(t, jwt.SigningMethodRS256, signer, claims(func(c jwt.MapClaims) { c["roles"] = []string{"TENANT_ADMINISTRATOR"} }))
	noRoles := sign(t, jwt.SigningMethodRS256, signer, claims(func(c jwt.MapClaims) { delete(c, "roles") }))
	restricted := sign(t, jwt.SigningMethodRS256, signer, claims(func(c jwt.MapClaims) { c["access"] = "restricted" }))
	for name, token := range map[string]string{"a person": user, "no roles": noRoles, "a restricted token": restricted} {
		t.Run(name, func(t *testing.T) {
			n := &fakeNotifier{outcome: usecase.Sent}
			srv := newServer(t, n)
			res := post(t, tenantRoute(srv), token, envelope)
			if res.StatusCode != http.StatusForbidden || len(n.got) != 0 {
				t.Fatalf("status %d, use case calls %d, want 403 and none", res.StatusCode, len(n.got))
			}
		})
	}
}

func TestC_a_correlation_id_is_reused_or_generated_and_returned(t *testing.T) {
	srv := newServer(t, &fakeNotifier{outcome: usecase.Sent})
	reused := post(t, tenantRoute(srv), serviceToken(t), envelope, "X-Correlation-Id", "c0ffee00-0000-4000-8000-000000000002")
	if got := reused.Header.Get("X-Correlation-Id"); got != "c0ffee00-0000-4000-8000-000000000002" {
		t.Fatalf("a valid id was not reused: %q", got)
	}
	generated := post(t, tenantRoute(srv), serviceToken(t), envelope)
	if got := generated.Header.Get("X-Correlation-Id"); len(got) != 36 {
		t.Fatalf("no id generated: %q", got)
	}
}

func TestC_an_unknown_route_is_a_404_with_the_envelope(t *testing.T) {
	srv := newServer(t, &fakeNotifier{})
	res := post(t, srv.URL+"/api/v1/notifications/other", serviceToken(t), envelope)
	if res.StatusCode != http.StatusNotFound || decodeError(t, res).Error != "NOT_FOUND" {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestU_the_public_key_is_read_from_pem_and_weak_keys_are_refused(t *testing.T) {
	der, _ := x509.MarshalPKIXPublicKey(&signer.PublicKey)
	if _, err := httpin.ParsePublicKey(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})); err != nil {
		t.Fatalf("a valid key was refused: %v", err)
	}
	if _, err := httpin.ParsePublicKey([]byte("not pem")); err == nil {
		t.Fatal("text that is not PEM was accepted")
	}
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	smallDER, _ := x509.MarshalPKIXPublicKey(&small.PublicKey)
	if _, err := httpin.ParsePublicKey(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: smallDER})); err == nil {
		t.Fatal("a 1024-bit key was accepted")
	}
}

// HU-SEC-007 part C, notifications spec 002, AC-WK4-02.1: the route takes the notice of an emergency access like any other event.
func TestC_emergency_notice_sends_one_email_and_answers_201(t *testing.T) {
	n := &fakeNotifier{outcome: usecase.Sent}
	srv := newServer(t, n)
	body := strings.Replace(envelope, `"eventType":"PasswordReset"`, `"eventType":"EmergencyAccessNoticeRequested"`, 1)
	if res := post(t, tenantRoute(srv), serviceToken(t), body); res.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", res.StatusCode)
	}
	if len(n.got) != 1 || n.got[0].Type != "EmergencyAccessNoticeRequested" || n.got[0].TenantID != tenantID {
		t.Fatalf("the use case got %+v", n.got)
	}
}
