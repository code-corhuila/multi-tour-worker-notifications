// Package smtpout is the outbound adapter that sends a message over SMTP with the standard library (plan D-05).
package smtpout

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/port/out"
)

// Config is what the adapter needs; it knows nothing of the environment.
type Config struct {
	Host, From, User, Password string
	Port                       int
	// RequireTLS makes STARTTLS mandatory: a server that does not offer it, or whose certificate does not check out, gets
	// nothing. It is true outside development; only Mailpit in development turns it off (ADR-004 D3).
	RequireTLS bool
	// Timeout bounds the whole send, connection and conversation included. It must be positive (the configuration guarantees it).
	Timeout time.Duration
	// TLSConfig replaces the default (the system roots, the host name checked, TLS 1.2 at least); a test gives its own roots.
	TLSConfig *tls.Config
}

// Mailer sends each message on its own connection: notices are few and a connection kept open would only go stale.
type Mailer struct{ cfg Config }

var _ out.Mailer = (*Mailer)(nil)

// New returns a mailer over cfg.
func New(cfg Config) *Mailer { return &Mailer{cfg: cfg} }

// Send delivers m or returns the reason it could not; it never retries (job 7 does).
func (m *Mailer) Send(ctx context.Context, msg out.Message) error {
	raw, err := m.build(msg, time.Now())
	if err != nil {
		return err
	}
	// The configuration guarantees a positive timeout (SMTP_TIMEOUT); without one every send would be cut at once.
	if m.cfg.Timeout <= 0 {
		return errors.New("the smtp timeout must be positive")
	}
	deadline := time.Now().Add(m.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port)))
	if err != nil {
		return fmt.Errorf("connect to the smtp server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return fmt.Errorf("start the smtp conversation: %w", err)
	}
	defer c.Close()
	if m.cfg.RequireTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the smtp server does not offer STARTTLS and TLS is required")
		}
		if err := c.StartTLS(m.tlsConfig()); err != nil {
			return fmt.Errorf("start TLS: %w", err)
		}
	}
	if m.cfg.User != "" {
		if err := c.Auth(smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("authenticate to the smtp server: %w", err)
		}
	}
	if err := c.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("smtp MAIL: %w", err)
	}
	if err := c.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp RCPT: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp end of data: %w", err)
	}
	// The server has accepted the message: a failed QUIT must not make job 7 retry and send it twice.
	_ = c.Quit()
	return nil
}

func (m *Mailer) tlsConfig() *tls.Config {
	if m.cfg.TLSConfig == nil {
		return &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
	}
	c := m.cfg.TLSConfig.Clone()
	if c.ServerName == "" {
		c.ServerName = m.cfg.Host
	}
	return c
}

// build writes the message: names and subject encoded as RFC 2047 words (they carry accents), the body in base64. The
// addresses go through net/mail and the subject through the Q encoder, which encodes any control character, so a value cannot
// add a header.
func (m *Mailer) build(msg out.Message, now time.Time) ([]byte, error) {
	// Checked here and not only by RCPT, so the header is safe whatever the order of the calls.
	if a, err := mail.ParseAddress(msg.To); err != nil || a.Address != msg.To {
		return nil, errors.New("the recipient is not one bare e-mail address")
	}
	from := mail.Address{Name: msg.FromName, Address: m.cfg.From}
	to := mail.Address{Address: msg.To}
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	domain := "localhost"
	if at := strings.LastIndex(m.cfg.From, "@"); at >= 0 {
		domain = m.cfg.From[at+1:]
	}
	var b strings.Builder
	header := func(name, value string) { b.WriteString(name + ": " + value + "\r\n") }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%x@%s>", id, domain))
	header("MIME-Version", "1.0")
	header("Content-Type", "text/html; charset=UTF-8")
	header("Content-Transfer-Encoding", "base64")
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(msg.HTMLBody))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return []byte(b.String()), nil
}
