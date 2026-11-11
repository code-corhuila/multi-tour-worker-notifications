// Package templates holds one Spanish template per e-mail type. Each one takes a small struct with only what it shows, so a field
// the struct does not name (a credential, a code, a link) cannot reach a body.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"time"
)

//go:embed *.html
var files embed.FS

var passwordReset = template.Must(template.ParseFS(files, "password_reset.html"))

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
