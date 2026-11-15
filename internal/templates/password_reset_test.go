package templates_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/templates"
)

var reset = time.Date(2026, 11, 9, 13, 5, 0, 0, time.UTC)

// AC-WK4-01.5: Spanish, the tenant name, the time in Colombia (UTC-5, no daylight saving) and what to do if it was not the owner.
func TestA_notice_is_spanish_with_tenant_sender_and_time(t *testing.T) {
	subject, body, err := templates.PasswordReset(templates.PasswordResetData{TenantName: "Travesía Natural", ResetAt: reset})
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Se restableció la contraseña de tu cuenta" {
		t.Fatalf("subject %q", subject)
	}
	for _, want := range []string{"Travesía Natural", "09/11/2026 08:05", "hora de Colombia", "Si no fuiste tú"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%s", want, body)
		}
	}
}

// RN-WK4-02: the template receives two fields and nothing else, so a credential, a code or a link cannot reach the body.
func TestA_notice_carries_no_forbidden_field(t *testing.T) {
	typ := reflect.TypeOf(templates.PasswordResetData{})
	if typ.NumField() != 2 {
		t.Fatalf("the template data has %d fields, want exactly TenantName and ResetAt", typ.NumField())
	}
	_, body, err := templates.PasswordReset(templates.PasswordResetData{TenantName: "Acme", ResetAt: reset})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"http://", "https://", "código", "token", "Bearer"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("body contains %q, a notice carries no link, code or token:\n%s", forbidden, body)
		}
	}
}

func TestA_a_tenant_name_cannot_inject_markup(t *testing.T) {
	_, body, err := templates.PasswordReset(templates.PasswordResetData{TenantName: "<script>x</script>", ResetAt: reset})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<script>") {
		t.Fatalf("the name was not escaped:\n%s", body)
	}
}
