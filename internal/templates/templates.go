// Package templates holds one Spanish template per e-mail type. Each one takes a small struct with only what it shows, so a field
// the struct does not name (a credential, a code, a link) cannot reach a body.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strings"
	"time"
)

//go:embed *.html
var files embed.FS

var (
	passwordReset   = template.Must(template.ParseFS(files, "password_reset.html"))
	emergencyAccess = template.Must(template.ParseFS(files, "emergency_access.html"))
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
