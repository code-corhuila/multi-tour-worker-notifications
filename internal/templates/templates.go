// Package templates holds one Spanish template per e-mail type. Each one takes a small struct with only what it shows, so a field
// the struct does not name (a credential, a code, a link) cannot reach a body.
package templates

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"
)

//go:embed *.html
var files embed.FS

var (
	passwordReset       = template.Must(template.ParseFS(files, "password_reset.html"))
	passwordResetFailed = template.Must(template.ParseFS(files, "password_reset_failed.html"))
	emergencyAccess     = template.Must(template.ParseFS(files, "emergency_access.html"))
	clientNotice        = template.Must(template.ParseFS(files, "client_notice.html"))
)

// colombia has no daylight saving, so a fixed offset is exact and needs no tzdata in the image.
var colombia = time.FixedZone("COT", -5*60*60)

// PasswordResetData is all the notice shows.
type PasswordResetData struct {
	TenantName string
	ResetAt    time.Time
}

// PasswordReset renders the notice that the password of an administrative account was reset: its subject and its HTML body.
func PasswordReset(d PasswordResetData) (subject, body string, err error) {
	var b bytes.Buffer
	view := struct{ TenantName, ResetAt string }{d.TenantName, d.ResetAt.In(colombia).Format("02/01/2006 15:04")}
	if err := passwordReset.Execute(&b, view); err != nil {
		return "", "", fmt.Errorf("render password reset notice: %w", err)
	}
	return "Se restableció la contraseña de tu cuenta", b.String(), nil
}

// PasswordResetFailedData is all the notice of a failed reset shows: never the reason of the refusal, the account or a code.
type PasswordResetFailedData struct {
	TenantName string
	FailedAt   time.Time
}

// PasswordResetFailed renders the notice that a reset of the password of an administrative account was tried and did not complete.
func PasswordResetFailed(d PasswordResetFailedData) (subject, body string, err error) {
	var b bytes.Buffer
	view := struct{ TenantName, FailedAt string }{d.TenantName, d.FailedAt.In(colombia).Format("02/01/2006 15:04")}
	if err := passwordResetFailed.Execute(&b, view); err != nil {
		return "", "", fmt.Errorf("render failed password reset notice: %w", err)
	}
	return "Intentaron restablecer la contraseña de tu cuenta", b.String(), nil
}

// The two kinds of the notice of an emergency access (HU-SEC-007 part C, notifications spec 002).
const (
	NoticeOpened = "OPENED"
	NoticeClosed = "CLOSED"
)

// areaLabels are the areas of an access as the Administrator reads them; one that is not here is shown as it came.
var areaLabels = map[string]string{
	"RESERVATIONS": "Reservas", "CATALOG": "Catálogo", "OPERATIONS": "Operación", "CASH": "Caja", "CUSTOMERS": "Clientes",
}

// EmergencyNoticeData is all the notice of an emergency access shows (RN-WK4-06): never the statement of why the authorization was impossible, the
// justification of the sensitive data, a credential or a link, none of which reach this struct.
type EmergencyNoticeData struct {
	Kind                  string
	TenantName            string
	PlatformAdministrator string
	Reason                string
	Areas                 []string
	IncludesSensitiveData bool
	StartedAt             time.Time
	EndedAt               time.Time
}

// EmergencyNotice renders the notice that an emergency access to a tenant opened or ended: its subject and its HTML body.
func EmergencyNotice(d EmergencyNoticeData) (subject, body string, err error) {
	var title string
	switch d.Kind {
	case NoticeOpened:
		title = "Se abrió un acceso de emergencia a " + d.TenantName
	case NoticeClosed:
		title = "Terminó el acceso de emergencia a " + d.TenantName
	default:
		return "", "", fmt.Errorf("no text for the kind %q of an emergency access notice", d.Kind)
	}
	labels := make([]string, len(d.Areas))
	for i, a := range d.Areas {
		if label, ok := areaLabels[a]; ok {
			labels[i] = label
		} else {
			labels[i] = a
		}
	}
	sensitive := "No"
	if d.IncludesSensitiveData {
		sensitive = "Sí"
	}
	view := struct {
		Closed                                                                          bool
		TenantName, PlatformAdministrator, Reason, Areas, Sensitive, StartedAt, EndedAt string
	}{
		Closed: d.Kind == NoticeClosed, TenantName: d.TenantName, PlatformAdministrator: d.PlatformAdministrator, Reason: d.Reason,
		Areas: strings.Join(labels, ", "), Sensitive: sensitive, StartedAt: d.StartedAt.In(colombia).Format("02/01/2006 15:04"),
	}
	if d.Kind == NoticeClosed {
		view.EndedAt = d.EndedAt.In(colombia).Format("02/01/2006 15:04")
	}
	var b bytes.Buffer
	if err := emergencyAccess.Execute(&b, view); err != nil {
		return "", "", fmt.Errorf("render emergency access notice: %w", err)
	}
	return title, b.String(), nil
}

// ErrUnknownNotice is the answer for a type of customer notice that has no text.
var ErrUnknownNotice = errors.New("no text for this type of customer notice")

// noticeTexts are the eight types of notice of RF-019 (HU-COM-015): the subject, the sentence that says what happened and the one that says what
// follows. None of them shows the free-text reason of a rejection (DP-15-03), a credential or a link.
var noticeTexts = map[string]struct{ subject, headline, next string }{
	"PROOF_APPROVED":                    {"Aprobamos tu comprobante de pago", "Tu comprobante de pago fue aprobado.", "No tienes que hacer nada más por ahora."},
	"PROOF_REJECTED":                    {"No pudimos aprobar tu comprobante de pago", "Revisamos tu comprobante de pago y no pudimos aprobarlo.", "Si el plazo de pago sigue vigente, puedes presentar un nuevo comprobante."},
	"RESERVATION_CONFIRMED":             {"Tu reserva está confirmada", "Tu reserva está confirmada.", "Gracias por tu pago. Te esperamos en la salida."},
	"RESERVATION_CANCELLED_NON_PAYMENT": {"Cancelamos tu reserva por falta de pago", "Cancelamos tu reserva por falta de pago.", "Si todavía quieres viajar, puedes hacer una nueva reserva."},
	"REFUND_PROCESSED":                  {"Procesamos tu reembolso", "Procesamos el reembolso de tu reserva.", "El tiempo en que lo ves reflejado depende de tu medio de pago."},
	"CREDIT_BALANCE_RECORDED":           {"Registramos un saldo a tu favor", "Registramos un saldo a tu favor por tu reserva.", "Puedes usarlo en una próxima reserva."},
	"DEPARTURE_CANCELLED":               {"Se canceló la salida de tu reserva", "Se canceló la salida de tu reserva por una causa ajena a ti.", "Nos comunicaremos contigo para ofrecerte una alternativa."},
	"RESCHEDULING_PROPOSED":             {"Te proponemos reprogramar tu salida", "Te proponemos reprogramar tu salida.", "Cuéntanos si aceptas la nueva fecha."},
}

// ClientNoticeData is all a customer notice shows; an absent amount, balance, tour or departure is left out of the body.
type ClientNoticeData struct {
	Type                string
	RecipientName       string
	TourName            string
	DepartureStartsAt   time.Time
	AmountCents         *int64
	PendingBalanceCents *int64
}

// ClientNotice renders the notice of a customer: its subject and its HTML body.
func ClientNotice(d ClientNoticeData) (subject, body string, err error) {
	text, ok := noticeTexts[d.Type]
	if !ok {
		return "", "", fmt.Errorf("%w: %q", ErrUnknownNotice, d.Type)
	}
	view := struct{ Name, Headline, Next, Tour, Departure, Amount, Balance string }{
		Name: d.RecipientName, Headline: text.headline, Next: text.next, Tour: d.TourName, Amount: pesos(d.AmountCents), Balance: pesos(d.PendingBalanceCents),
	}
	if !d.DepartureStartsAt.IsZero() {
		view.Departure = d.DepartureStartsAt.In(colombia).Format("02/01/2006 15:04")
	}
	var b bytes.Buffer
	if err := clientNotice.Execute(&b, view); err != nil {
		return "", "", fmt.Errorf("render customer notice: %w", err)
	}
	return text.subject, b.String(), nil
}

// pesos writes an amount in cents as $1.250.000 (the dot of the thousands, the comma of the cents only when there are cents); empty when absent.
func pesos(cents *int64) string {
	if cents == nil {
		return ""
	}
	whole, digits := strconv.FormatInt(*cents/100, 10), ""
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			digits += "."
		}
		digits += string(c)
	}
	if rest := *cents % 100; rest != 0 {
		return fmt.Sprintf("$%s,%02d", digits, rest)
	}
	return "$" + digits
}
