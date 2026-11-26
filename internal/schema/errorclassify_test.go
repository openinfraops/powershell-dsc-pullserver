package schema

import (
	"strings"
	"testing"
)

func TestNormalizeErrorHostname(t *testing.T) {
	got := NormalizeError("Resource failed on node SRV01 with access denied", "SRV01")
	if !strings.Contains(got, "<HOST>") {
		t.Fatalf("normalized %q does not contain <HOST>", got)
	}
	if strings.Contains(got, "SRV01") {
		t.Fatalf("normalized %q still contains the node name", got)
	}
}

func TestNormalizeErrorGUID(t *testing.T) {
	msg := "Failed connecting to 12345678-1234-1234-1234-123456789012 and bare a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"
	got := NormalizeError(msg, "")
	if strings.Count(got, "<GUID>") != 2 {
		t.Fatalf("normalized %q should contain exactly 2 <GUID> markers, got %d", got, strings.Count(got, "<GUID>"))
	}
}

func TestNormalizeErrorPaths(t *testing.T) {
	msg := `Cannot find C:\Program Files\foo\bar.txt or \\server\share\baz.txt`
	got := NormalizeError(msg, "")
	if strings.Count(got, "<PATH>") != 2 {
		t.Fatalf("normalized %q should contain exactly 2 <PATH> markers", got)
	}
}

func TestNormalizeErrorIPs(t *testing.T) {
	msg := "Connection to 192.168.1.100 failed, retry 2001:db8::1"
	got := NormalizeError(msg, "")
	if strings.Count(got, "<IP>") != 2 {
		t.Fatalf("normalized %q should contain exactly 2 <IP> markers, got %d", got, strings.Count(got, "<IP>"))
	}
}

func TestNormalizeErrorPreservesHexAndCodeNumbers(t *testing.T) {
	cases := []struct {
		name, msg, want string
	}{
		{"hresult", "Access denied (0x80070005)", "0x80070005"},
		{"exit code", "Process exited with exit code 101", "exit code 101"},
		{"error code", "Failed (error code 2060)", "error code 2060"},
		{"win32 code", "win32 code 32", "win32 code 32"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeError(c.msg, "")
			if !strings.Contains(got, c.want) {
				t.Fatalf("normalized %q does not preserve %q (got %q)", c.msg, c.want, got)
			}
		})
	}
}

func TestNormalizeErrorNeutralizesLargeNumbers(t *testing.T) {
	got := NormalizeError("Timeout after 5000 ms at 1712345678901", "")
	if strings.Count(got, "<N>") != 2 {
		t.Fatalf("normalized %q should contain exactly 2 <N> markers, got %d", got, strings.Count(got, "<N>"))
	}
}

func TestFingerprintInvariance(t *testing.T) {
	fp1 := Fingerprint("MSFT_x", "SRV01", `Failed on SRV01 accessing C:\x\y, 192.168.1.1, guid 12345678-1234-1234-1234-123456789012, timeout 5000`)
	fp2 := Fingerprint("MSFT_x", "SRV02", `Failed on SRV02 accessing D:\a\b, 10.0.0.9, guid abcdef01-2345-6789-abcd-ef0123456789, timeout 9000`)
	if fp1 != fp2 {
		t.Fatalf("fingerprints differ across varying GUID/path/IP/host: %q vs %q", fp1, fp2)
	}
}

func TestFingerprintDifferentErrorsNeverMerged(t *testing.T) {
	cases := []struct {
		name       string
		msg1, msg2 string
	}{
		{"hresult", "Access denied 0x80070005", "Access denied 0x80070006"},
		{"exit code", "Exited with exit code 1", "Exited with exit code 2"},
		{"error code", "error code 101", "error code 202"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fp1 := Fingerprint("", "SRV01", c.msg1)
			fp2 := Fingerprint("", "SRV01", c.msg2)
			if fp1 == fp2 {
				t.Fatalf("distinct errors merged into same fingerprint %q", fp1)
			}
		})
	}
}

func TestFingerprintResourceTypeDistinguishes(t *testing.T) {
	fp1 := Fingerprint("MSFT_x", "SRV01", "same message")
	fp2 := Fingerprint("MSFT_y", "SRV01", "same message")
	if fp1 == fp2 {
		t.Fatalf("different resource types merged into same fingerprint %q", fp1)
	}
}

func TestRedactSecret(t *testing.T) {
	cases := []string{
		"login failed password=secret123",
		"auth failed key=abc123def",
		"token=abcdef123456",
		"api_key=12345",
		`password "hunter2"`,
	}
	for _, c := range cases {
		got := RedactSecret(c)
		if strings.Contains(got, "secret123") || strings.Contains(got, "abc123def") || strings.Contains(got, "hunter2") || strings.Contains(got, "12345") {
			t.Fatalf("RedactSecret(%q) = %q did not redact the secret", c, got)
		}
		if !strings.Contains(got, "<REDACTED>") {
			t.Fatalf("RedactSecret(%q) = %q does not contain <REDACTED>", c, got)
		}
	}
}

func TestClassifyErrorDefaults(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"The module MSFT_xSqlPackage is not found", "module_missing"},
		{"Cannot find resource MSFT_x", "module_missing"},
		{"Access is denied", "access_denied"},
		{"Permission denied opening file", "access_denied"},
		{"Cannot decrypt the credential", "credential_decryption"},
		{"No private key found", "credential_decryption"},
		{"A reboot is pending", "reboot_required"},
		{"Restart required", "reboot_required"},
		{"The operation timed out", "timeout_network"},
		{"WinRM connection refused", "timeout_network"},
		{"Checksum mismatch detected", "checksum_download"},
		{"The downloaded MOF is corrupt", "checksum_download"},
		{"Something completely unrelated", "unknown"},
	}
	for _, c := range cases {
		if got := ClassifyError(nil, c.msg); got != c.want {
			t.Errorf("ClassifyError(nil, %q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

func TestClassifyErrorCustomRules(t *testing.T) {
	rules := []CategoryRule{
		{Pattern: "extremely specific", Category: "custom"},
	}
	if got := ClassifyError(rules, "this is an extremely specific thing"); got != "custom" {
		t.Fatalf("ClassifyError custom = %q, want custom", got)
	}
	if got := ClassifyError(rules, "not special"); got != DefaultErrorCategory {
		t.Fatalf("ClassifyError custom fallback = %q, want %q", got, DefaultErrorCategory)
	}
}

func TestExtractReportErrorParts(t *testing.T) {
	errors := []string{"Report-level failure", `{"Message":"nested message"}`}
	statusData := []string{
		`{"InDesiredState":false,"ModuleName":"MSFT_x","ResourceId":"R1","Message":"Resource failed"}`,
		`{"InDesiredState":true,"ModuleName":"MSFT_y","ResourceId":"R2","Message":"ok"}`,
		`{"ModuleName":"MSFT_z","ResourceId":"R3"}`,
	}
	parts := ExtractReportErrorParts(errors, statusData)
	if len(parts) != 3 {
		t.Fatalf("ExtractReportErrorParts returned %d parts, want 3: %+v", len(parts), parts)
	}
	if parts[0].ResourceType != "" || parts[0].Message != "Report-level failure" {
		t.Fatalf("parts[0] = %+v, want report-level", parts[0])
	}
	if parts[1].ResourceType != "" || parts[1].Message != "nested message" {
		t.Fatalf("parts[1] = %+v, want nested message from JSON object", parts[1])
	}
	if parts[2].ResourceType != "MSFT_x" || parts[2].Message != "Resource failed" {
		t.Fatalf("parts[2] = %+v, want MSFT_x failed resource", parts[2])
	}
}

func TestExtractReportErrorPartsSkipsMalformed(t *testing.T) {
	parts := ExtractReportErrorParts([]string{"ok"}, []string{`{not json`, `{"InDesiredState":false,"Message":"good"}`})
	if len(parts) != 2 {
		t.Fatalf("ExtractReportErrorParts returned %d parts, want 2: %+v", len(parts), parts)
	}
	if parts[1].ResourceType != "" {
		t.Fatalf("parts[1] ResourceType = %q, want empty when no module/resource", parts[1].ResourceType)
	}
}

func TestTruncateMessage(t *testing.T) {
	short := strings.Repeat("a", 100)
	if got := TruncateMessage(short); len(got) != 100 {
		t.Fatalf("TruncateMessage(short) len = %d, want 100", len(got))
	}
	long := strings.Repeat("b", MaxErrorMessageLength+50)
	got := TruncateMessage(long)
	if len(got) != MaxErrorMessageLength {
		t.Fatalf("TruncateMessage(long) len = %d, want %d", len(got), MaxErrorMessageLength)
	}
}
