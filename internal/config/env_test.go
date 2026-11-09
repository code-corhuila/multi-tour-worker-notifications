package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/config"
)

// valid is the smallest environment that starts the worker.
func valid() map[string]string {
	return map[string]string{"SMTP_HOST": "mailpit", "SMTP_PORT": "1025", "SMTP_FROM": "no-reply@multitour.local"}
}

func load(env map[string]string) (config.Notifications, error) {
	return config.LoadNotifications(func(k string) string { return env[k] })
}

func TestU_the_declared_defaults_apply(t *testing.T) {
	c, err := load(valid())
	if err != nil {
		t.Fatal(err)
	}
	if c.SMTPHost != "mailpit" || c.SMTPPort != 1025 || c.SMTPFrom != "no-reply@multitour.local" {
		t.Fatalf("smtp settings not read: %+v", c)
	}
	if !c.RequireTLS || c.HTTPAddr != ":8080" || c.SMTPTimeout != 10*time.Second {
		t.Fatalf("defaults wrong: tls=%v addr=%q timeout=%v", c.RequireTLS, c.HTTPAddr, c.SMTPTimeout)
	}
}

// RN-WK4-04 of the piece spec (RN-WRK4-04): the worker does not start without its SMTP settings.
func TestU_config_names_the_bad_variable(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]string)
		want   string
	}{
		{"no host", func(e map[string]string) { delete(e, "SMTP_HOST") }, "SMTP_HOST"},
		{"no port", func(e map[string]string) { delete(e, "SMTP_PORT") }, "SMTP_PORT"},
		{"port text", func(e map[string]string) { e["SMTP_PORT"] = "mail" }, "SMTP_PORT"},
		{"port zero", func(e map[string]string) { e["SMTP_PORT"] = "0" }, "SMTP_PORT"},
		{"port too big", func(e map[string]string) { e["SMTP_PORT"] = "70000" }, "SMTP_PORT"},
		{"no sender", func(e map[string]string) { delete(e, "SMTP_FROM") }, "SMTP_FROM"},
		{"sender not an address", func(e map[string]string) { e["SMTP_FROM"] = "not an address" }, "SMTP_FROM"},
		{"user without password", func(e map[string]string) { e["SMTP_USER"] = "u" }, "SMTP_PASSWORD"},
		{"password without user", func(e map[string]string) { e["SMTP_PASSWORD"] = "p" }, "SMTP_USER"},
		{"tls flag text", func(e map[string]string) { e["SMTP_REQUIRE_TLS"] = "maybe" }, "SMTP_REQUIRE_TLS"},
		{"timeout text", func(e map[string]string) { e["SMTP_TIMEOUT"] = "soon" }, "SMTP_TIMEOUT"},
		{"timeout negative", func(e map[string]string) { e["SMTP_TIMEOUT"] = "-1s" }, "SMTP_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := valid()
			tc.change(env)
			_, err := load(env)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want it to name %s", err, tc.want)
			}
		})
	}
}

func TestU_the_password_never_prints(t *testing.T) {
	env := valid()
	env["SMTP_USER"], env["SMTP_PASSWORD"] = "mailer", "s3cret-value"
	c, err := load(env)
	if err != nil {
		t.Fatal(err)
	}
	if c.SMTPPassword.Reveal() != "s3cret-value" {
		t.Fatal("password not read")
	}
	if got := strings.Join([]string{c.SMTPPassword.String()}, ""); strings.Contains(got, "s3cret") {
		t.Fatalf("password printed: %q", got)
	}
}

// AC-WK4-01.8: outside development the connection must be TLS, so the flag can only be turned off on purpose.
func TestU_tls_is_turned_off_only_on_purpose(t *testing.T) {
	env := valid()
	env["SMTP_REQUIRE_TLS"] = "false"
	c, err := load(env)
	if err != nil || c.RequireTLS {
		t.Fatalf("err=%v tls=%v, want tls off", err, c.RequireTLS)
	}
}
