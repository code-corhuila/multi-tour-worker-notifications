package templates_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/templates"
)

var failedAt = time.Date(2026, 11, 12, 18, 5, 0, 0, time.UTC)

// AC-WK4-03.2: Spanish, the tenant name, the time of the attempt in Colombia (UTC-5) and what to do if it was not the owner.
func TestA_failed_reset_notice_is_spanish_with_tenant_sender_and_bogota_time(t *testing.T) {
	subject, body, err := templates.PasswordResetFailed(templates.PasswordResetFailedData{TenantName: "Travesía Natural", FailedAt: failedAt})
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Intentaron restablecer la contraseña de tu cuenta" {
		t.Fatalf("subject %q", subject)
	}
	for _, want := range []string{"Travesía Natural", "12/11/2026 13:05", "hora de Colombia", "no se completó", "Si no fuiste tú"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%s", want, body)
		}
	}
}

// AC-WK4-03.3 and RN-WK4-09: the template receives two fields and nothing else, so no credential, code, link or reason of the refusal reaches the body.
func TestA_failed_reset_notice_carries_only_its_listed_fields(t *testing.T) {
	if n := reflect.TypeOf(templates.PasswordResetFailedData{}).NumField(); n != 2 {
		t.Fatalf("the template data has %d fields, want exactly TenantName and FailedAt", n)
	}
	_, body, err := templates.PasswordResetFailed(templates.PasswordResetFailedData{TenantName: "Acme", FailedAt: failedAt})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"http://", "https://", "código", "token", "Bearer", "WRONG_CODE", "RETIRED_CODE", "POLICY"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("body contains %q:\n%s", forbidden, body)
		}
	}
}

func TestA_failed_reset_notice_does_not_let_a_tenant_name_inject_markup(t *testing.T) {
	_, body, err := templates.PasswordResetFailed(templates.PasswordResetFailedData{TenantName: "<script>x</script>", FailedAt: failedAt})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<script>") {
		t.Fatalf("the name was not escaped:\n%s", body)
	}
}
