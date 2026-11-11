package config

import (
	"fmt"
	"net/mail"
	"strconv"
	"time"

	runtimeconfig "github.com/code-corhuila/multi-tour-worker-runtime/config"
)

const defaultSMTPTimeout = 10 * time.Second

// Notifications is everything the worker reads from the environment.
type Notifications struct {
	SMTPHost     string
	SMTPPort     int
	SMTPFrom     string
	SMTPUser     string
	SMTPPassword runtimeconfig.Secret
	// RequireTLS is true unless SMTP_REQUIRE_TLS=false is set on purpose, as in development with Mailpit (ADR-004 D3).
	RequireTLS  bool
	SMTPTimeout time.Duration
	HTTPAddr    string
}

// LoadNotifications reads the variables through getenv. Anything missing or malformed stops the worker at start; an error
// names the variable and never prints a value.
func LoadNotifications(getenv func(string) string) (Notifications, error) {
	c := Notifications{SMTPHost: getenv("SMTP_HOST"), SMTPFrom: getenv("SMTP_FROM"), SMTPUser: getenv("SMTP_USER"),
		SMTPPassword: runtimeconfig.Secret(getenv("SMTP_PASSWORD")), RequireTLS: true, SMTPTimeout: defaultSMTPTimeout, HTTPAddr: ":8080"}
	if c.SMTPHost == "" {
		return Notifications{}, fmt.Errorf("SMTP_HOST is required")
	}
	port, err := strconv.Atoi(getenv("SMTP_PORT"))
	if err != nil || port < 1 || port > 65535 {
		return Notifications{}, fmt.Errorf("SMTP_PORT must be a port from 1 to 65535")
	}
	c.SMTPPort = port
	// A bare address: the visible name is the tenant's and is added when the message is built (plan D-12).
	if a, err := mail.ParseAddress(c.SMTPFrom); err != nil || a.Name != "" || a.Address != c.SMTPFrom {
		return Notifications{}, fmt.Errorf("SMTP_FROM must be one bare e-mail address")
	}
	// The credentials come together: one of them alone could only fail at the first send.
	if (c.SMTPUser == "") != (c.SMTPPassword == "") {
		missing := "SMTP_PASSWORD"
		if c.SMTPUser == "" {
			missing = "SMTP_USER"
		}
		return Notifications{}, fmt.Errorf("%s is required when the other SMTP credential is set", missing)
	}
	if s := getenv("SMTP_REQUIRE_TLS"); s != "" {
		if c.RequireTLS, err = strconv.ParseBool(s); err != nil {
			return Notifications{}, fmt.Errorf("SMTP_REQUIRE_TLS must be true or false")
		}
	}
	if s := getenv("SMTP_TIMEOUT"); s != "" {
		if c.SMTPTimeout, err = time.ParseDuration(s); err != nil || c.SMTPTimeout <= 0 {
			return Notifications{}, fmt.Errorf("SMTP_TIMEOUT must be a positive duration")
		}
	}
	if s := getenv("HTTP_ADDR"); s != "" {
		c.HTTPAddr = s
	}
	return c, nil
}
