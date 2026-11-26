package logs

import "testing"

func TestSanitizeLogMessage(t *testing.T) {
	// Control characters (newline, CR, tab) are replaced by a space and other
	// control characters (e.g. NUL, DEL) by '?', preventing log-line injection
	// via fields a client controls (URL, RemoteAddr, author, certificate Subject).
	in := "CN=foo\nDC=bar\tOU=baz\r\nO=qux\x00admin\x7fend"
	want := "CN=foo DC=bar OU=baz  O=qux?admin?end"
	if got := SanitizeLogMessage(in); got != want {
		t.Fatalf("SanitizeLogMessage(%q) = %q, want %q", in, got, want)
	}
}

func TestSanitizeLogMessageKeepsPrintable(t *testing.T) {
	// Printable ASCII and non-ASCII (UTF-8) content is preserved unchanged.
	in := `CN=dsc.example.com,O=Team,OU=Infra,CN=éléphant`
	if got := SanitizeLogMessage(in); got != in {
		t.Fatalf("SanitizeLogMessage(%q) = %q, want unchanged", in, got)
	}
}
