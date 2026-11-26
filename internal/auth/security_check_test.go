package auth

import (
	"strings"
	"testing"

	"go-dsc-pull/internal/schema"
)

func TestSecurityWarningsNilConfig(t *testing.T) {
	if w := SecurityWarnings(nil); len(w) != 0 {
		t.Fatalf("expected no warnings for nil config, got %v", w)
	}
}

func TestSecurityWarningsEmptySecrets(t *testing.T) {
	cfg := &schema.AppConfig{}
	if w := SecurityWarnings(cfg); len(w) != 0 {
		t.Fatalf("expected no warnings for empty secrets (handled elsewhere), got %v", w)
	}
}

func TestSecurityWarningsStrongSecrets(t *testing.T) {
	strong := "pQw8vZ3rT6sJkL2xQ9eBfG7hN1uVtX4yC5zR8aW0sD3mPqE6bH0jKfL9nS2tU7vY1"
	cfg := &schema.AppConfig{}
	cfg.WebUI.SharedAccessSecret = strong
	cfg.DSCPullServer.RegistrationKey = "uJc5QxW7vZ2rT8sK4mP9eBfG6hN1dVtX3yC5zR8aW0sD3mPqE6bH0jKfL9nS2tU7vY1"
	if w := SecurityWarnings(cfg); len(w) != 0 {
		t.Fatalf("expected no warnings for strong secrets, got %v", w)
	}
}

func TestSecurityWarningsPlaceholderSharedSecret(t *testing.T) {
	cfg := &schema.AppConfig{}
	cfg.WebUI.SharedAccessSecret = "example-shared-secret"
	cfg.DSCPullServer.RegistrationKey = "uJc5QxW7vZ2rT8sK4mP9eBfG6hN1dVtX3yC5zR8aW0sD3mPqE6bH0jKfL9nS2tU7vY1"
	w := SecurityWarnings(cfg)
	if len(w) != 1 || !strings.Contains(w[0], "shared_secret") {
		t.Fatalf("expected one warning for shared_secret placeholder, got %v", w)
	}
}

func TestSecurityWarningsShortSecrets(t *testing.T) {
	cfg := &schema.AppConfig{}
	cfg.WebUI.SharedAccessSecret = "abc"     // too short
	cfg.DSCPullServer.RegistrationKey = "x"  // too short
	w := SecurityWarnings(cfg)
	if len(w) != 2 {
		t.Fatalf("expected two warnings for short secrets, got %v", w)
	}
	for _, msg := range w {
		if !strings.Contains(msg, "only") {
			t.Fatalf("expected a length warning, got %q", msg)
		}
	}
}

func TestSecurityWarningsLowDiversity(t *testing.T) {
	// Long enough but nearly all the same character -> low entropy.
	cfg := &schema.AppConfig{}
	cfg.WebUI.SharedAccessSecret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	w := SecurityWarnings(cfg)
	if len(w) != 1 || !strings.Contains(w[0], "diversity") {
		t.Fatalf("expected a low-diversity warning, got %v", w)
	}
}

func TestFatalSecurityErrorsNilConfig(t *testing.T) {
	if errs := FatalSecurityErrors(nil); len(errs) != 0 {
		t.Fatalf("expected no fatal errors for nil config, got %v", errs)
	}
}

func TestFatalSecurityErrorsCertValidationOverHTTP(t *testing.T) {
	// enable_client_cert_validation=true with enable_https=false is a
	// fail-open configuration: no client certificate is negotiated, so the
	// pull endpoints are NOT protected by mTLS. The server must refuse to start.
	cfg := &schema.AppConfig{}
	cfg.DSCPullServer.EnableClientCertValidation = true
	cfg.DSCPullServer.EnableHTTPS = false

	errs := FatalSecurityErrors(cfg)
	if len(errs) != 1 {
		t.Fatalf("expected exactly one fatal error, got %v", errs)
	}
	for _, e := range errs {
		if !strings.Contains(e, "enable_client_cert_validation") || !strings.Contains(e, "enable_https") {
			t.Fatalf("error must name both settings, got %q", e)
		}
		if !strings.Contains(e, "ERROR [SECURITY]") {
			t.Fatalf("error must be tagged ERROR [SECURITY], got %q", e)
		}
	}
}

func TestFatalSecurityErrorsCertValidationOverHTTPS(t *testing.T) {
	// The valid production combination must not be rejected.
	cfg := &schema.AppConfig{}
	cfg.DSCPullServer.EnableClientCertValidation = true
	cfg.DSCPullServer.EnableHTTPS = true
	if errs := FatalSecurityErrors(cfg); len(errs) != 0 {
		t.Fatalf("expected no fatal errors for mTLS over HTTPS, got %v", errs)
	}
}

func TestFatalSecurityErrorsNoCertValidationOverHTTP(t *testing.T) {
	// HTTP without client-certificate validation is an explicit opt-out (no
	// mTLS expected), so it is allowed (only a startup WARN is emitted).
	cfg := &schema.AppConfig{}
	cfg.DSCPullServer.EnableClientCertValidation = false
	cfg.DSCPullServer.EnableHTTPS = false
	if errs := FatalSecurityErrors(cfg); len(errs) != 0 {
		t.Fatalf("expected no fatal errors for HTTP without client-cert validation, got %v", errs)
	}
}
