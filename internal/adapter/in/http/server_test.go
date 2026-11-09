package httpin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	httpin "github.com/code-corhuila/multi-tour-worker-notifications/internal/adapter/in/http"
)

// AC-WK4-01.9: /health answers once the process is up.
func TestC_health_answers(t *testing.T) {
	srv := httptest.NewServer(httpin.NewMux())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", res.StatusCode)
	}
}
