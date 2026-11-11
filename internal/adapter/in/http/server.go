package httpin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/code-corhuila/multi-tour-worker-runtime/correlation"
	"github.com/code-corhuila/multi-tour-worker-runtime/health"

	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/usecase"
)

const (
	tenantRoute   = "POST /api/v1/notifications/internal/tenants/{tenantId}/events"
	noTenantRoute = "POST /api/v1/notifications/internal/events"
	maxBodyBytes  = 64 << 10
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// addressPattern finds an e-mail address in the text of a server: a reply such as "550 no such user a@b.test" repeats it.
	addressPattern = regexp.MustCompile(`[^\s<>"',;]+@[^\s<>"',;]+`)
)

// withoutAddresses keeps the cause for the log but not the person it was about.
func withoutAddresses(text string) string { return addressPattern.ReplaceAllString(text, "<address>") }

// Notifier is what the routes need from the use case, so a test can replace it.
type Notifier interface {
	Handle(ctx context.Context, e usecase.Event) (usecase.Outcome, error)
}

// NewMux returns the routes with no business purpose: /health.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /health", health.Handler())
	return mux
}

type handler struct {
	notifier Notifier
	verifier *Verifier
	log      *slog.Logger
}

// New returns the whole inbound side: /health and the two internal routes job 7 delivers events to. The gateway never
// routes /internal/ (ADR-004 D5); each call is still checked here.
func New(n Notifier, v *Verifier, log *slog.Logger) http.Handler {
	h := &handler{notifier: n, verifier: v, log: log}
	mux := NewMux()
	mux.HandleFunc(tenantRoute, h.deliver)
	mux.HandleFunc(noTenantRoute, h.deliver)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "route not found")
	})
	return withCorrelation(mux)
}

// withCorrelation reuses a well-formed X-Correlation-Id or generates one, returns it and puts it in the context.
func withCorrelation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(correlation.Header)
		if !uuidPattern.MatchString(id) {
			id = correlation.New()
		}
		w.Header().Set(correlation.Header, id)
		next.ServeHTTP(w, r.WithContext(correlation.WithID(r.Context(), id)))
	})
}

// envelope is the event as ADR-004 D4 writes it; the fields the notices do not use are ignored.
type envelope struct {
	ID       string          `json:"eventId"`
	Type     string          `json:"eventType"`
	TenantID string          `json:"tenantId"`
	Payload  json.RawMessage `json:"payload"`
}

func (h *handler) deliver(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	e, ok := decode(w, r)
	if !ok {
		return
	}
	outcome, err := h.notifier.Handle(r.Context(), e)
	switch {
	case err == nil && outcome == usecase.Repeated:
		writeJSON(w, http.StatusOK, struct{}{})
	case err == nil:
		writeJSON(w, http.StatusCreated, struct{}{})
	case errors.Is(err, usecase.ErrUnknownType), errors.Is(err, usecase.ErrInvalidPayload):
		h.fail(w, r, e, http.StatusInternalServerError, "INTERNAL_ERROR", "the event cannot be turned into an e-mail", err)
	default:
		h.fail(w, r, e, http.StatusBadGateway, "SERVICE_UNAVAILABLE", "the e-mail could not be sent", err)
	}
}

// fail answers with a message that names no driver, address or server; the cause goes to the log under the trace id.
func (h *handler) fail(w http.ResponseWriter, r *http.Request, e usecase.Event, status int, code, message string, cause error) {
	h.log.ErrorContext(r.Context(), "event not delivered", "traceId", correlation.From(r.Context()), "eventId", e.ID, "error", withoutAddresses(cause.Error()))
	writeError(w, r, status, code, message)
}

// authorize answers 401 or 403 itself and reports whether the caller is a service.
func (h *handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	scheme, raw, found := strings.Cut(r.Header.Get("Authorization"), " ")
	raw = strings.TrimSpace(raw)
	if !found || !strings.EqualFold(scheme, "Bearer") || raw == "" {
		return unauthorized(w, r)
	}
	claims, err := h.verifier.Verify(raw)
	if err != nil {
		h.log.DebugContext(r.Context(), "token refused", "traceId", correlation.From(r.Context()), "reason", err.Error())
		return unauthorized(w, r)
	}
	if !claims.isService() {
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "only a service may deliver events")
		return false
	}
	return true
}

func unauthorized(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
	return false
}

// decode reads one JSON object and checks what the route needs; it answers 400 itself.
func decode(w http.ResponseWriter, r *http.Request) (usecase.Event, bool) {
	var env envelope
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := decoder.Decode(&env); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "the body must be one JSON event")
		return usecase.Event{}, false
	}
	switch {
	case env.ID == "":
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "eventId is required")
	case env.Type == "":
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "eventType is required")
	case r.PathValue("tenantId") != "" && env.TenantID != r.PathValue("tenantId"):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "tenantId does not match the route")
	default:
		return usecase.Event{ID: env.ID, Type: env.Type, TenantID: env.TenantID, Payload: env.Payload}, true
	}
	return usecase.Event{}, false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError is the single envelope {error, message, traceId} of every answer that is not a success (Annex C).
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message, "traceId": correlation.From(r.Context())})
}
