package templates_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/templates"
)

// HU-SEC-007 part C, notifications spec 002: the notice that an emergency access to a tenant opened or ended.

var (
	started = time.Date(2026, 11, 10, 15, 48, 51, 0, time.UTC)
	ended   = time.Date(2026, 11, 10, 17, 20, 0, 0, time.UTC)
)

func openedData() templates.EmergencyNoticeData {
	return templates.EmergencyNoticeData{
		Kind: templates.NoticeOpened, TenantName: "Travesía Natural", PlatformAdministrator: "root@multitour.test",
		Reason: "Todas las cajas de hoy muestran saldo negativo", Areas: []string{"CASH", "CUSTOMERS"}, IncludesSensitiveData: true, StartedAt: started,
	}
}

// AC-WK4-02.2 and .4: Spanish, the tenant, who, why, the areas as Spanish labels, whether sensitive data are reached, and the start in Colombia time.
func TestA_opened_notice_shows_the_listed_fields(t *testing.T) {
	subject, body, err := templates.EmergencyNotice(openedData())
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Se abrió un acceso de emergencia a Travesía Natural" {
		t.Fatalf("subject %q", subject)
	}
	for _, want := range []string{"Travesía Natural", "root@multitour.test", "Todas las cajas de hoy muestran saldo negativo", "Caja", "Clientes",
		"Datos sensibles: Sí", "10/11/2026 10:48", "hora de Colombia", "no incluye contraseñas ni enlaces"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Fin:") {
		t.Errorf("an opening has no end:\n%s", body)
	}
}

// AC-WK4-02.3
func TestA_closed_notice_adds_the_end_time(t *testing.T) {
	d := openedData()
	d.Kind, d.EndedAt, d.IncludesSensitiveData = templates.NoticeClosed, ended, false
	subject, body, err := templates.EmergencyNotice(d)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Terminó el acceso de emergencia a Travesía Natural" {
		t.Fatalf("subject %q", subject)
	}
	for _, want := range []string{"Inicio: 10/11/2026 10:48", "Fin: 10/11/2026 12:20", "Datos sensibles: No"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%s", want, body)
		}
	}
}

// RN-WK4-06: the template receives the listed fields and nothing else, and what a person typed is escaped.
func TestA_emergency_notice_carries_only_its_listed_fields(t *testing.T) {
	if n := reflect.TypeOf(templates.EmergencyNoticeData{}).NumField(); n != 8 {
		t.Fatalf("the template data has %d fields, want the eight listed (kind, tenant, administrator, reason, areas, sensitive, start, end)", n)
	}
	d := openedData()
	d.Reason = `<script>alert(1)</script>`
	_, body, err := templates.EmergencyNotice(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<script>") {
		t.Errorf("the reason was not escaped:\n%s", body)
	}
	for _, forbidden := range []string{"http://", "https://", "código", "token", "Bearer", "declaración"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the notice contains %q:\n%s", forbidden, body)
		}
	}
}

func TestA_an_unknown_kind_is_an_error_not_an_empty_notice(t *testing.T) {
	d := openedData()
	d.Kind = "OTHER"
	if _, _, err := templates.EmergencyNotice(d); err == nil {
		t.Fatal("a kind with no text must fail")
	}
}
