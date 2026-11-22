package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MaxLoginAttempts is the default number of failed login attempts allowed
// within a single window before the client IP is temporarily blocked.
const MaxLoginAttempts = 5

// LoginRateLimitWindow is the default window (duration) during which failed
// attempts are counted. When the number of failures reaches MaxLoginAttempts,
// further attempts are rejected (HTTP 429) until the oldest failure leaves the
// window.
const LoginRateLimitWindow = 15 * time.Minute

// LoginRateLimiter is an in-memory, per-client-IP limiter for failed login
// attempts. It provides brute-force protection for the /api/v1/login endpoint
// without any external dependency.
//
// It is safe for concurrent use.
type LoginRateLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	// failures maps a client IP to the timestamps of its failed attempts
	// that are still inside the window.
	failures map[string][]time.Time
}

// NewLoginRateLimiter creates a limiter that allows up to maxAttempts failed
// attempts per IP within window. Non-positive values fall back to the defaults.
func NewLoginRateLimiter(maxAttempts int, window time.Duration) *LoginRateLimiter {
	if maxAttempts <= 0 {
		maxAttempts = MaxLoginAttempts
	}
	if window <= 0 {
		window = LoginRateLimitWindow
	}
	return &LoginRateLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		failures:    make(map[string][]time.Time),
	}
}

// purgeLocked removes, for a given IP, the failure timestamps that are older
// than the window. Callers must hold l.mu.
func (l *LoginRateLimiter) purgeLocked(ip string, now time.Time) []time.Time {
	stamps := l.failures[ip]
	cutoff := now.Add(-l.window)
	kept := stamps[:0]
	for _, t := range stamps {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, ip)
		return nil
	}
	l.failures[ip] = kept
	return kept
}

// LoginAllowed reports whether the given client IP may attempt a login.
// It is false when the IP has accumulated maxAttempts failures inside the
// window. Calling it does not record a failure.
func (l *LoginRateLimiter) LoginAllowed(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	stamps := l.purgeLocked(ip, time.Now())
	return len(stamps) < l.maxAttempts
}

// LoginFailed records a failed login attempt for the given client IP.
func (l *LoginRateLimiter) LoginFailed(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	stamps := l.purgeLocked(ip, now)
	l.failures[ip] = append(stamps, now)
}

// LoginSucceeded clears all recorded failures for the given client IP
// (typically after a successful login).
func (l *LoginRateLimiter) LoginSucceeded(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}

// RemainingAttempts returns how many additional failed attempts the IP is
// allowed before being blocked (0 when already blocked at the moment of the
// call).
func (l *LoginRateLimiter) RemainingAttempts(ip string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	stamps := l.purgeLocked(ip, time.Now())
	remaining := l.maxAttempts - len(stamps)
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// ClientIP extracts the client IP address from a request, suitable for use as a
// rate-limiting key. It returns a bare IP (without port).
//
// Les en-têtes X-Forwarded-For / X-Real-IP ne sont honorés que si le client
// direct (RemoteAddr) appartient à la liste des proxies de confiance
// (trustedProxies, adresses exactes ou CIDR). Dans le cas contraire, ces
// en-têtes sont ignorés afin qu'un attaquant ne puisse pas contourner le
// rate-limit en envoyant un en-tête différent à chaque tentative.
func ClientIP(r *http.Request, trustedProxies []string) string {
	if r == nil {
		return ""
	}
	peer := peerIP(r.RemoteAddr)
	if isTrustedProxy(peer, trustedProxies) {
		for _, h := range []string{"X-Forwarded-For", "X-Real-IP"} {
			val := strings.TrimSpace(r.Header.Get(h))
			if val == "" {
				continue
			}
			// X-Forwarded-For may be a comma-separated list; take the first entry.
			if i := strings.IndexByte(val, ','); i >= 0 {
				val = val[:i]
			}
			val = strings.TrimSpace(val)
			if val != "" {
				return val
			}
		}
	}
	return peer
}

// peerIP extrait l'adresse IP brute (sans port) depuis RemoteAddr.
func peerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// isTrustedProxy reporte si ip correspond à l'une des entrées de trusted
// (adresse IP exacte ou réseau CIDR).
func isTrustedProxy(ip string, trusted []string) bool {
	if ip == "" || len(trusted) == 0 {
		return false
	}
	for _, t := range trusted {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.Contains(t, "/") {
			_, ipnet, err := net.ParseCIDR(t)
			if err != nil {
				continue
			}
			if parsed := net.ParseIP(ip); parsed != nil && ipnet.Contains(parsed) {
				return true
			}
			continue
		}
		if strings.EqualFold(ip, t) {
			return true
		}
	}
	return false
}
