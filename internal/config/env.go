package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
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
	// JWTPublicKey (PEM, as the value of the variable) or JWTPublicKeyFile (a path) is the public key of the security service;
	// the worker checks the token of every call itself (ADR-004 D5). Issuer and audience are the platform's unless set.
	JWTPublicKey     string
	JWTPublicKeyFile string
	JWTIssuer        string
	JWTAudience      string
	// CommercialReservationURL and ServiceToken let job 4 read the content of a customer notice (HU-COM-015); both empty leave that notice off.
	CommercialReservationURL string
	ServiceToken             runtimeconfig.Secret
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
	// An environment file holds the key on one line with the line breaks written as backslash-n.
	c.JWTPublicKey = strings.ReplaceAll(getenv("JWT_PUBLIC_KEY"), `\n`, "\n")
	c.JWTPublicKeyFile = getenv("JWT_PUBLIC_KEY_FILE")
	if c.JWTPublicKey == "" && c.JWTPublicKeyFile == "" {
		return Notifications{}, fmt.Errorf("JWT_PUBLIC_KEY or JWT_PUBLIC_KEY_FILE is required")
	}
	c.JWTIssuer, c.JWTAudience = valueOr(getenv("JWT_ISSUER"), "multi-tour-security"), valueOr(getenv("JWT_AUDIENCE"), "multi-tour")
	if err := loadNoticeReader(getenv, &c); err != nil {
		return Notifications{}, err
	}
	return c, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// loadNoticeReader reads where job 4 asks for the content of a customer notice and the service token it asks with (HU-COM-015). Both come
// together, or neither: the notices of a reset and of an emergency access need neither. A customer notice without them ends failed, visible.
func loadNoticeReader(getenv func(string) string, c *Notifications) error {
	raw := strings.TrimRight(getenv("COMMERCIAL_RESERVATION_BASE_URL"), "/")
	c.ServiceToken = runtimeconfig.Secret(getenv("SERVICE_TOKEN"))
	switch {
	case raw == "" && c.ServiceToken == "":
		return nil
	case raw == "":
		return fmt.Errorf("COMMERCIAL_RESERVATION_BASE_URL is required when SERVICE_TOKEN is set")
	case c.ServiceToken == "":
		return fmt.Errorf("SERVICE_TOKEN is required when COMMERCIAL_RESERVATION_BASE_URL is set")
	}
	// Only scheme://host[:port]: credentials, a query, a fragment or a path would reach every request.
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return fmt.Errorf("COMMERCIAL_RESERVATION_BASE_URL must be an http(s) url with only scheme, host and port")
	}
	c.CommercialReservationURL = raw
	return nil
}
