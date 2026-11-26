package monitor

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// atomic_store resets the shutdown flag to a known value (tests are in the
// same package, so they can reach the unexported var).
func atomic_store(t *testing.T, v int32) {
	t.Helper()
	shutdown = v
}

// resetCounters clears the status counters so each test starts from zero.
func resetCounters() {
	counters.mu.Lock()
	counters.counts = make(map[int]uint64)
	counters.mu.Unlock()
}

func TestShutdownFlag(t *testing.T) {
	// Reset to a known state.
	atomic_store(t, 0)
	if ShuttingDown() {
		t.Fatalf("expected ShuttingDown()=false initially")
	}
	SetShutdown()
	if !ShuttingDown() {
		t.Fatalf("expected ShuttingDown()=true after SetShutdown")
	}
	// Idempotent.
	SetShutdown()
	if !ShuttingDown() {
		t.Fatalf("expected ShuttingDown()=true after second SetShutdown")
	}
	// Reset for other tests.
	atomic_store(t, 0)
}

func TestStatusCodeMiddlewareCounts(t *testing.T) {
	atomic_store(t, 0)
	// Reset counters.
	resetCounters()

	h := StatusCodeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := StatusCount(http.StatusUnauthorized); got != 1 {
		t.Fatalf("StatusCount(401) = %d, want 1", got)
	}
	if got := StatusCount(http.StatusOK); got != 0 {
		t.Fatalf("StatusCount(200) = %d, want 0", got)
	}
}

func TestStatusCodeMiddlewareDefaultsTo200(t *testing.T) {
	atomic_store(t, 0)
	resetCounters()

	h := StatusCodeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No explicit WriteHeader -> status defaults to 200.
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := StatusCount(http.StatusOK); got != 1 {
		t.Fatalf("StatusCount(200) = %d, want 1", got)
	}
}
