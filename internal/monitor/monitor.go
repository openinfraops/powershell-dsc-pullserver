// Package monitor exposes lightweight process-level runtime metrics for the
// /health and /metrics endpoints (HTTP status counters and a shutdown flag).
// It is intentionally dependency-free beyond the standard library and the
// shared schema.StatusRecorder, so it can be wired around any HTTP handler.
package monitor

import (
	"net/http"
	"sync"
	"sync/atomic"

	"go-dsc-pull/internal/schema"
)

var (
	// shutdown is 1 once the process has been asked to stop. /health uses it to
	// report workers as not alive during the graceful-shutdown window.
	shutdown int32

	// counters tracks, per HTTP status code, how many responses were emitted
	// since the process started (across both the web and the DSC servers).
	counters = &statusCounters{counts: make(map[int]uint64)}
)

// SetShutdown marks the process as shutting down. It is idempotent and safe to
// call more than once.
func SetShutdown() {
	atomic.StoreInt32(&shutdown, 1)
}

// ShuttingDown reports whether the process has been asked to stop.
func ShuttingDown() bool {
	return atomic.LoadInt32(&shutdown) == 1
}

// statusCounters is a mutex-protected map of status code -> response count.
type statusCounters struct {
	mu     sync.Mutex
	counts map[int]uint64
}

func (c *statusCounters) inc(code int) {
	c.mu.Lock()
	c.counts[code]++
	c.mu.Unlock()
}

func (c *statusCounters) get(code int) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[code]
}

// StatusCount records how many responses of the given status code have been
// emitted since the process started.
func StatusCount(code int) uint64 {
	return counters.get(code)
}

// StatusCodeMiddleware records the HTTP status code of every request into the
// global counters. It wraps the provided handler and passes through a
// schema.StatusRecorder so downstream middleware that writes a status code is
// still observed correctly.
func StatusCodeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &schema.StatusRecorder{ResponseWriter: w, Status: http.StatusOK}
		next.ServeHTTP(rec, r)
		counters.inc(rec.Status)
	})
}
