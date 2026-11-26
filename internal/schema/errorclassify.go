package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// MaxErrorMessageLength bounds the size of a single error message that is
// stored (the DSC report content is treated as untrusted).
const MaxErrorMessageLength = 2048

// DefaultErrorCategory is the fallback category when no rule matches.
const DefaultErrorCategory = "unknown"

// CategoryRule maps a message pattern (regular expression) to a category.
// Rules are evaluated in order and the first match wins.
type CategoryRule struct {
	Pattern  string `json:"pattern"`
	Category string `json:"category"`
}

// ErrorPart is a single error extracted from a DSC report. ResourceType
// distinguishes different problems (e.g. the module/resource that failed);
// it is empty for report-level errors.
type ErrorPart struct {
	ResourceType string `json:"resource_type"`
	Message      string `json:"message"`
}

// DefaultCategoryRules is the ordered default rule set (first match wins).
// The wording of DSC resources varies with the OS language; these cover the
// most common English signals. Rules can be overridden/extended through the
// dsc_pullserver.error_categories configuration key.
var DefaultCategoryRules = []CategoryRule{
	{Pattern: `(?i)\b(module|resource)\b.{0,40}\bnot\s+found|cannot\s+find\s+(module|resource)|could\s+not\s+find\s+(module|resource)|wrong\s+module\s+version|invalid\s+module\s+version|module\s+version\s+mismatch`, Category: "module_missing"},
	{Pattern: `(?i)access\s+is\s+denied|access\s+denied|permission\s+denied|unauthorized|0x80070005|error_privilege_not_held`, Category: "access_denied"},
	{Pattern: `(?i)(credential|certificate|private\s+key|thumbprint)\s+(decryption|decrypt|not\s+found|missing|unable\s+to\s+decrypt)|cannot\s+decrypt|failed\s+to\s+decrypt|no\s+private\s+key`, Category: "credential_decryption"},
	{Pattern: `(?i)pending\s+reboot|reboot\s+required|requires?\s+a\s+reboot|restart\s+required|\b(reboot|restart)\s+is\s+pending`, Category: "reboot_required"},
	{Pattern: `(?i)timed?\s+out|timeout|winrm|unable\s+to\s+connect|connection\s+(refused|timed out|failed)|network\s+(unreachable|error)|download\s+failed|socket|tcp\s+error`, Category: "timeout_network"},
	{Pattern: `(?i)checksum|hash\s+mismatch|corrupt|corrupted|integrity|invalid\s+(module|mof)|bad\s+checksum|mount\s+failed`, Category: "checksum_download"},
}

// normalizeRules are applied in order after replacing the node name. They
// neutralise message parts that vary between nodes so the same problem on many
// nodes yields the same fingerprint.
var normalizeRules = []struct {
	re *regexp.Regexp
	to string
}{
	// GUIDs (standard and bare 32-hex).
	{regexp.MustCompile(`(?i)[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}`), "<GUID>"},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{32}\b`), "<GUID>"},
	// Windows and UNC paths.
	{regexp.MustCompile(`(?i)[a-z]:\\[^\s"']+|\\\\[^\s"']+`), "<PATH>"},
	// IPv4 addresses.
	{regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`), "<IP>"},
	// IPv6 addresses (best effort).
	{regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){2,}[0-9a-f:.]+\b`), "<IP>"},
}

// redactRules neutralise obvious secret-bearing patterns before storage. The
// report content is untrusted and may carry property values.
var redactRules = []struct {
	re *regexp.Regexp
	to string
}{
	{regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|key|api[_-]?key|apikey|access[_-]?key)\s*[:=]\s*["']?[^\s"',;]+`), "$1=<REDACTED>"},
	{regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|key|api[_-]?key|apikey|access[_-]?key)\s+["'][^"']+["']`), "$1=<REDACTED>"},
}

var (
	// reNumber matches large decimal numbers (3+ digits) that are candidates to
	// be neutralised (counters, timestamps, sizes).
	reNumber = regexp.MustCompile(`\b\d{3,}\b`)
	// reCodeNumber matches a code-context number following an error/exit/return/
	// hresult/win32 "code" keyword. Such numbers distinguish different problems
	// and must be preserved.
	reCodeNumber = regexp.MustCompile(`(?i)(?:exit|error|return|hresult|win32)\s*code\s*[:=]?\s*\d+`)
	// reHexCode matches HRESULT/hex codes, which must also be preserved.
	reHexCode = regexp.MustCompile(`(?i)0x[0-9a-f]{2,}`)
)

// TruncateMessage bounds an error message to MaxErrorMessageLength bytes.
func TruncateMessage(msg string) string {
	if len(msg) > MaxErrorMessageLength {
		return msg[:MaxErrorMessageLength]
	}
	return msg
}

// NormalizeError neutralises the variable parts of an error message: the node
// name, GUIDs, paths, IPs and large numbers, so that identical problems on
// different nodes produce the same fingerprint. HRESULT/hex codes, exit codes
// and well-known error names are left intact.
func NormalizeError(msg, nodeName string) string {
	msg = TruncateMessage(msg)
	if nodeName != "" {
		msg = strings.ReplaceAll(msg, nodeName, "<HOST>")
	}
	for _, r := range normalizeRules {
		msg = r.re.ReplaceAllString(msg, r.to)
	}
	msg = normalizeNumbers(msg)
	return msg
}

// Fingerprint returns the 8-byte (hex) SHA-256 of resourceType + "|" + the
// normalized message. First 8 bytes are used to keep the key short.
func Fingerprint(resourceType, nodeName, msg string) string {
	m := NormalizeError(msg, nodeName)
	sum := sha256.Sum256([]byte(resourceType + "|" + m))
	return hex.EncodeToString(sum[:8])
}

// RedactSecret redacts obvious sensitive patterns (password=..., key=...) from
// a message before it is stored or displayed.
func RedactSecret(msg string) string {
	for _, r := range redactRules {
		msg = r.re.ReplaceAllString(msg, r.to)
	}
	return msg
}

// ClassifyError returns the category of a message, using the provided rules in
// order (first match wins). It falls back to DefaultErrorCategory when no rule
// matches or when the rules list is empty.
func ClassifyError(rules []CategoryRule, msg string) string {
	if len(rules) == 0 {
		rules = DefaultCategoryRules
	}
	for _, rule := range rules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue
		}
		if re.MatchString(msg) {
			return rule.Category
		}
	}
	return DefaultErrorCategory
}

// genericErrorMessages are exact (normalised) placeholder messages that carry no
// actionable detail. When a DSC report fails without a usable message (e.g.
// Event 4131 with ErrorCode=1 and ErrorMessage=NULL), the client often emits a
// bare "Unknown"/"Error"/"NULL". These are treated as noise so they do not
// create empty "unknown" error groups.
var genericErrorMessages = map[string]bool{
	"unknown":           true,
	"unknown error":     true,
	"unknown failure":   true,
	"error":             true,
	"error unknown":     true,
	"null":              true,
	"none":              true,
	"nil":               true,
	"n/a":               true,
	"na":                true,
	"-":                 true,
	"failed":            true,
	"failure":           true,
	"generic":           true,
	"no error":          true,
	"not available":     true,
	"not specified":     true,
	"unavailable":       true,
	"empty":             true,
}

// genericErrorWords are tokens that, when a message is composed solely of them,
// still yield a generic placeholder (e.g. "unknown error", "error null",
// "not available").
var genericErrorWords = map[string]bool{
	"unknown":     true,
	"error":       true,
	"failed":      true,
	"failure":     true,
	"null":        true,
	"none":        true,
	"nil":         true,
	"no":          true,
	"not":         true,
	"available":   true,
	"unavailable": true,
	"specified":   true,
	"generic":     true,
	"empty":       true,
}

// isGenericErrorMessage reports whether a message is a generic placeholder with
// no actionable detail. Empty/whitespace messages are generic by definition.
func isGenericErrorMessage(msg string) bool {
	m := strings.TrimSpace(msg)
	if m == "" {
		return true
	}
	// Normalise: lowercase, collapse whitespace, drop a trailing period.
	norm := strings.TrimSuffix(strings.Join(strings.Fields(strings.ToLower(m)), " "), ".")
	if genericErrorMessages[norm] {
		return true
	}
	// A message made only of generic tokens (e.g. "error null") is still noise.
	fields := strings.FieldsFunc(norm, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		if !genericErrorWords[f] {
			return false
		}
	}
	return true
}

// ExtractReportErrorParts extracts the error messages from a DSC report: the
// report-level Errors array plus the per-resource failed StatusData entries
// (InDesiredState = false). Malformed entries and generic placeholder messages
// (e.g. "Unknown", "NULL") are skipped.
func ExtractReportErrorParts(errors []string, statusData []string) []ErrorPart {
	parts := make([]ErrorPart, 0, len(errors)+len(statusData))
	for _, e := range errors {
		msg := errorMessageFromValue(e)
		if msg == "" || isGenericErrorMessage(msg) {
			continue
		}
		parts = append(parts, ErrorPart{ResourceType: "", Message: msg})
	}
	for _, sd := range statusData {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(sd), &m); err != nil {
			continue
		}
		if !isFailedResource(m) {
			continue
		}
		msg := resourceErrorMessage(m)
		if msg == "" || isGenericErrorMessage(msg) {
			continue
		}
		parts = append(parts, ErrorPart{ResourceType: resourceType(m), Message: msg})
	}
	return parts
}

// normalizeNumbers replaces large decimal numbers (3+ digits) with "<N>",
// except when the number is part of an HRESULT/hex code or follows an error /
// exit / return / hresult code keyword. This keeps distinct error codes
// distinct while neutralising counters and timestamps.
func normalizeNumbers(s string) string {
	// Collect the ranges that must be preserved (code-context numbers and
	// hex codes) so the generic number rule does not touch them.
	protected := make([][2]int, 0)
	addRange := func(start, end int) {
		protected = append(protected, [2]int{start, end})
	}
	for _, loc := range reCodeNumber.FindAllStringIndex(s, -1) {
		addRange(loc[0], loc[1])
	}
	for _, loc := range reHexCode.FindAllStringIndex(s, -1) {
		addRange(loc[0], loc[1])
	}
	sort.Slice(protected, func(i, j int) bool { return protected[i][0] < protected[j][0] })

	var b strings.Builder
	last := 0
	for _, loc := range reNumber.FindAllStringIndex(s, -1) {
		if inRanges(protected, loc[0]) {
			continue
		}
		b.WriteString(s[last:loc[0]])
		b.WriteString("<N>")
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// inRanges reports whether pos falls within any of the sorted [start,end)
// ranges (end exclusive).
func inRanges(ranges [][2]int, pos int) bool {
	for _, r := range ranges {
		if pos < r[0] {
			return false
		}
		if pos < r[1] {
			return true
		}
	}
	return false
}

// errorMessageFromValue returns the message from an error value that may be a
// plain string or a JSON object with a Message/ErrorDetails field.
func errorMessageFromValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	// Try to parse as a JSON object (some DSC errors are objects).
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(v), &m); err == nil {
		for _, k := range []string{"Message", "ErrorMessage", "ErrorDetails", "FullyQualifiedErrorId"} {
			if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		// Fall back to the raw JSON text.
		return v
	}
	return v
}

// isFailedResource reports whether a StatusData entry represents a failed
// resource (InDesiredState explicitly false). Entries without the field are
// not treated as failures.
func isFailedResource(m map[string]interface{}) bool {
	for k, v := range m {
		if strings.EqualFold(k, "InDesiredState") {
			return !ParseBoolFlexible(v)
		}
	}
	return false
}

// resourceErrorMessage extracts an error message from a failed resource entry.
func resourceErrorMessage(m map[string]interface{}) string {
	for _, key := range []string{"Error", "ErrorMessage", "Message", "ErrorRecord"} {
		for k, v := range m {
			if !strings.EqualFold(k, key) {
				continue
			}
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return strings.TrimSpace(t)
				}
			case map[string]interface{}:
				if s, ok := t["Message"].(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
	}
	return ""
}

// resourceType returns the module/resource context of a StatusData entry.
func resourceType(m map[string]interface{}) string {
	for k, v := range m {
		if strings.EqualFold(k, "ModuleName") {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	for k, v := range m {
		if strings.EqualFold(k, "ResourceId") {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	for k, v := range m {
		if strings.EqualFold(k, "ConfigurationName") {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
