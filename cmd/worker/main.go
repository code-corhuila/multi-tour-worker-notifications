package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/code-corhuila/multi-tour-worker-runtime/logging"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/config"
)

// The server's limits are declared here, not left to defaults (Annex D).
const (
	readHeaderTimeout = 5 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 20 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "notifications:", err)
		os.Exit(1)
	}
}

// run starts the worker: the configuration is read once and a bad one stops it.
func run() error {
	cfg, err := config.LoadNotifications(os.Getenv)
	if err != nil {
		return err
	}
	log := logging.New(os.Stdout)
	handler, err := newHandler(cfg, log)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout: writeTimeout, IdleTimeout: idleTimeout}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	log.Info("notifications worker started", "addr", cfg.HTTPAddr)

	var failure error
	select {
	case <-ctx.Done():
	case failure = <-serverErr:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && failure == nil {
		failure = err
	}
	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}
	return failure
}
