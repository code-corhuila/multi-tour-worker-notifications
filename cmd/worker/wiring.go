package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	httpin "github.com/code-corhuila/multi-tour-worker-notifications/internal/adapter/in/http"
	smtpout "github.com/code-corhuila/multi-tour-worker-notifications/internal/adapter/out/smtp"
	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/usecase"
	"github.com/code-corhuila/multi-tour-worker-notifications/internal/config"
)

// newHandler builds the worker from its configuration. A public key it cannot read stops the start, naming the variable, so
// the worker never starts only to refuse every call.
func newHandler(cfg config.Notifications, log *slog.Logger) (http.Handler, error) {
	// The inline key wins when both are set.
	pem, variable := []byte(cfg.JWTPublicKey), "JWT_PUBLIC_KEY"
	if cfg.JWTPublicKey == "" {
		var err error
		variable = "JWT_PUBLIC_KEY_FILE"
		if pem, err = os.ReadFile(cfg.JWTPublicKeyFile); err != nil {
			return nil, fmt.Errorf("%s cannot be read", variable)
		}
	}
	key, err := httpin.ParsePublicKey(pem)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", variable, err)
	}
	mailer := smtpout.New(smtpout.Config{Host: cfg.SMTPHost, Port: cfg.SMTPPort, From: cfg.SMTPFrom, User: cfg.SMTPUser,
		Password: cfg.SMTPPassword.Reveal(), RequireTLS: cfg.RequireTLS, Timeout: cfg.SMTPTimeout})
	notify := usecase.NewNotify(mailer, log)
	return httpin.New(notify, httpin.NewVerifier(key, cfg.JWTIssuer, cfg.JWTAudience), log), nil
}
