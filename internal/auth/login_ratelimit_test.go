package auth

import (
	"testing"
	"time"
)

func TestLoginRateLimiterAllowsUpToMax(t *testing.T) {
	l := NewLoginRateLimiter(3, time.Minute)
	ip := "10.0.0.1"

	if !l.LoginAllowed(ip) {
		t.Fatalf("expected the first attempt to be allowed")
	}

	l.LoginFailed(ip)
	l.LoginFailed(ip)
	if !l.LoginAllowed(ip) {
		t.Fatalf("expected the 3rd attempt to be allowed (limit of 3)")
	}

	l.LoginFailed(ip)
	if l.LoginAllowed(ip) {
		t.Fatalf("expected the IP to be blocked after reaching the limit")
	}
	if got := l.RemainingAttempts(ip); got != 0 {
		t.Fatalf("expected 0 remaining attempts, got %d", got)
	}
}

func TestLoginSucceededClearsFailures(t *testing.T) {
	l := NewLoginRateLimiter(2, time.Minute)
	ip := "10.0.0.2"

	l.LoginFailed(ip)
	l.LoginFailed(ip)
	if l.LoginAllowed(ip) {
		t.Fatalf("expected the IP to be blocked")
	}

	l.LoginSucceeded(ip)
	if !l.LoginAllowed(ip) {
		t.Fatalf("expected the IP to be unblocked after a successful login")
	}
}

func TestLoginRateLimiterWindowExpiry(t *testing.T) {
	l := NewLoginRateLimiter(1, 50*time.Millisecond)
	ip := "10.0.0.3"

	l.LoginFailed(ip)
	if l.LoginAllowed(ip) {
		t.Fatalf("expected the IP to be blocked")
	}

	// Wait for the window to expire.
	time.Sleep(70 * time.Millisecond)
	if !l.LoginAllowed(ip) {
		t.Fatalf("expected the IP to be allowed again after the window expired")
	}
}

func TestLoginRateLimiterIsPerIP(t *testing.T) {
	l := NewLoginRateLimiter(1, time.Minute)
	ip1 := "10.0.0.4"
	ip2 := "10.0.0.5"

	l.LoginFailed(ip1)
	if l.LoginAllowed(ip1) {
		t.Fatalf("expected ip1 to be blocked")
	}
	if !l.LoginAllowed(ip2) {
		t.Fatalf("expected ip2 to be unaffected by ip1")
	}
}
