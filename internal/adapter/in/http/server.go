package httpin

import (
	"net/http"

	"github.com/code-corhuila/multi-tour-worker-runtime/health"
)

// NewMux returns the routes of the worker. /health is the only one a worker exposes without a business purpose.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /health", health.Handler())
	return mux
}
