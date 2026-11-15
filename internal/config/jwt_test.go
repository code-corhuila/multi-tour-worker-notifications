package config_test

import (
	"strings"
	"testing"
)

const pemOneLine = "-----BEGIN PUBLIC KEY-----\\nMIIBIjANBgkq\\n-----END PUBLIC KEY-----"

// RN-WK4-04 and ADR-004 D5: the worker checks the token itself, so it does not start without the public key of the security service.
func TestU_config_names_the_jwt_variable_that_is_missing(t *testing.T) {
	env := valid()
	delete(env, "JWT_PUBLIC_KEY")
	_, err := load(env)
	if err == nil || !strings.Contains(err.Error(), "JWT_PUBLIC_KEY") {
		t.Fatalf("error %v, want it to name JWT_PUBLIC_KEY", err)
	}
}

func TestU_the_key_may_come_inline_or_as_a_file_and_the_issuer_and_audience_default_to_the_platforms(t *testing.T) {
	c, err := load(valid())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.JWTPublicKey, "\nMIIBIjANBgkq\n") || strings.Contains(c.JWTPublicKey, "\\n") {
		t.Fatalf("the escaped line breaks were not restored: %q", c.JWTPublicKey)
	}
	if c.JWTIssuer != "multi-tour-security" || c.JWTAudience != "multi-tour" {
		t.Fatalf("issuer %q, audience %q", c.JWTIssuer, c.JWTAudience)
	}
	env := valid()
	delete(env, "JWT_PUBLIC_KEY")
	env["JWT_PUBLIC_KEY_FILE"] = "/run/keys/public.pem"
	c, err = load(env)
	if err != nil || c.JWTPublicKeyFile != "/run/keys/public.pem" || c.JWTPublicKey != "" {
		t.Fatalf("err %v, config %+v", err, c)
	}
}

func TestU_issuer_and_audience_can_be_set(t *testing.T) {
	env := valid()
	env["JWT_ISSUER"], env["JWT_AUDIENCE"] = "other-issuer", "other-audience"
	c, err := load(env)
	if err != nil || c.JWTIssuer != "other-issuer" || c.JWTAudience != "other-audience" {
		t.Fatalf("err %v, config %+v", err, c)
	}
}
