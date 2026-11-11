package templates_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/templates"
)

// HU-COM-015 AC-15.1 to AC-15.5: one Spanish template for each of the eight types of notice of RF-019.

func cents(v int64) *int64 { return &v }

var departure = time.Date(2026, 12, 5, 13, 0, 0, 0, time.UTC)

func TestA_each_type_of_client_notice_has_a_spanish_subject_and_body(t *testing.T) {
	cases := map[string]struct{ subject, text string }{
		"PROOF_APPROVED":                    {"Aprobamos tu comprobante de pago", "fue aprobado"},
		"PROOF_REJECTED":                    {"No pudimos aprobar tu comprobante de pago", "nuevo comprobante"},
		"RESERVATION_CONFIRMED":             {"Tu reserva está confirmada", "confirmada"},
		"RESERVATION_CANCELLED_NON_PAYMENT": {"Cancelamos tu reserva por falta de pago", "falta de pago"},
		"REFUND_PROCESSED":                  {"Procesamos tu reembolso", "reembolso"},
		"CREDIT_BALANCE_RECORDED":           {"Registramos un saldo a tu favor", "saldo a tu favor"},
		"DEPARTURE_CANCELLED":               {"Se canceló la salida de tu reserva", "canceló la salida"},
		"RESCHEDULING_PROPOSED":             {"Te proponemos reprogramar tu salida", "reprogramar"},
	}
	for noticeType, want := range cases {
		subject, body, err := templates.ClientNotice(templates.ClientNoticeData{Type: noticeType, RecipientName: "Ana Pérez", TourName: "Cañón del Chicamocha"})
		if err != nil {
			t.Fatalf("%s: %v", noticeType, err)
		}
		if subject != want.subject {
			t.Errorf("%s: subject %q, want %q", noticeType, subject, want.subject)
		}
		for _, text := range []string{"Hola Ana Pérez", "Cañón del Chicamocha", want.text} {
			if !strings.Contains(body, text) {
				t.Errorf("%s: body lacks %q:\n%s", noticeType, text, body)
			}
		}
	}
}

// The amount and the balance are shown in pesos with the dot of the thousands, the departure in the hour of Colombia.
func TestA_the_notice_shows_the_amount_the_balance_and_the_departure(t *testing.T) {
	_, body, err := templates.ClientNotice(templates.ClientNoticeData{Type: "PROOF_APPROVED", RecipientName: "Ana", TourName: "Tour",
		DepartureStartsAt: departure, AmountCents: cents(125000050), PendingBalanceCents: cents(30000000)})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"$1.250.000,50", "$300.000", "05/12/2026 08:00"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%s", want, body)
		}
	}
}

// What the content does not carry is not shown: no empty line, no zero.
func TestA_the_notice_omits_what_the_content_lacks(t *testing.T) {
	_, body, err := templates.ClientNotice(templates.ClientNoticeData{Type: "RESERVATION_CONFIRMED", RecipientName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"Monto", "Saldo pendiente", "Salida:", "Tour:", "$0"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("body shows %q with no value:\n%s", unwanted, body)
		}
	}
}

// RN-WK4-02, DP-15-03: the name is escaped and no link, credential or reason can reach a body.
func TestA_the_name_is_escaped_and_the_body_has_no_link(t *testing.T) {
	_, body, err := templates.ClientNotice(templates.ClientNoticeData{Type: "PROOF_REJECTED", RecipientName: "<script>x</script>", TourName: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("name not escaped:\n%s", body)
	}
	if strings.Contains(body, "http") || strings.Contains(body, "<a ") {
		t.Fatalf("body has a link:\n%s", body)
	}
}

func TestA_an_unknown_type_of_notice_has_no_template(t *testing.T) {
	if _, _, err := templates.ClientNotice(templates.ClientNoticeData{Type: "SOMETHING_ELSE"}); !errors.Is(err, templates.ErrUnknownNotice) {
		t.Fatalf("error %v, want ErrUnknownNotice", err)
	}
}
