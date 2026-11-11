package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/config"
)

func publicKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func baseConfig() config.Notifications {
	return config.Notifications{SMTPHost: "mailpit", SMTPPort: 1025, SMTPFrom: "no-reply@multitour.local",
		JWTIssuer: "multi-tour-security", JWTAudience: "multi-tour"}
}

var quiet = slog.New(slog.NewJSONHandler(io.Discard, nil))

// The worker stops at start with a key it cannot read, instead of refusing every call later.
func TestC_start_fails_with_a_public_key_that_cannot_be_read(t *testing.T) {
	cfg := baseConfig()
	cfg.JWTPublicKey = "ci-placeholder"
	if _, err := newHandler(cfg, quiet); err == nil || !strings.Contains(err.Error(), "JWT_PUBLIC_KEY") {
		t.Fatalf("error %v, want it to name JWT_PUBLIC_KEY", err)
	}
	cfg = baseConfig()
	cfg.JWTPublicKeyFile = filepath.Join(t.TempDir(), "missing.pem")
	if _, err := newHandler(cfg, quiet); err == nil || !strings.Contains(err.Error(), "JWT_PUBLIC_KEY_FILE") {
		t.Fatalf("error %v, want it to name JWT_PUBLIC_KEY_FILE", err)
	}
}

func TestC_the_key_comes_inline_or_from_a_file_and_the_handler_serves_health_and_refuses_unsigned_calls(t *testing.T) {
	file := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(file, []byte(publicKeyPEM(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	inline, fromFile := baseConfig(), baseConfig()
	inline.JWTPublicKey = publicKeyPEM(t)
	fromFile.JWTPublicKeyFile = file
	for name, cfg := range map[string]config.Notifications{"inline": inline, "file": fromFile} {
		t.Run(name, func(t *testing.T) {
			h, err := newHandler(cfg, quiet)
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(h)
			defer srv.Close()
			health, err := http.Get(srv.URL + "/health")
			if err != nil {
				t.Fatal(err)
			}
			defer health.Body.Close()
			unsigned, err := http.Post(srv.URL+"/api/v1/notifications/internal/events", "application/json", strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			defer unsigned.Body.Close()
			if health.StatusCode != http.StatusOK || unsigned.StatusCode != http.StatusUnauthorized {
				t.Fatalf("health %d, unsigned call %d, want 200 and 401", health.StatusCode, unsigned.StatusCode)
			}
		})
	}
}
