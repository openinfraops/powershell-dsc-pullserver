package schema

import (
	"encoding/json"
	"strings"
)

// FlexBool is a boolean that tolerates the DSC protocol's string encodings of
// booleans ("True"/"False", case-insensitive) as well as real JSON booleans.
// Missing or invalid values decode to false, so an untrusted report cannot make
// the server misinterpret a reboot request.
type FlexBool bool

// UnmarshalJSON accepts "True"/"False"/"true"/"false" (with or without quotes),
// "1"/"0", and JSON true/false. Anything else is treated as false.
func (b *FlexBool) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		*b = false
		return nil
	}
	// A JSON string literal ("True", "1", ...) is decoded first so the value is
	// passed without its surrounding quotes.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*b = FlexBool(parseBoolString(s))
		return nil
	}
	// Otherwise treat the raw bytes as a JSON boolean/number (true, 1, ...).
	*b = FlexBool(ParseBoolFlexible(data))
	return nil
}

// ParseBoolFlexible interprets a value (from JSON, a database column, or a
// map[string]interface{}) as a boolean, accepting the DSC protocol's
// "True"/"False" (case-insensitive), "1"/"0", and real booleans. Invalid or
// missing values are false — the report content is treated as untrusted.
func ParseBoolFlexible(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int32:
		return t != 0
	case int:
		return t != 0
	case float64:
		return t != 0
	case []byte:
		return parseBoolString(string(t))
	case string:
		return parseBoolString(t)
	default:
		return false
	}
}

// parseBoolString interprets a trimmed, lower-cased string as a boolean.
func parseBoolString(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "t", "yes", "y":
		return true
	default:
		return false
	}
}
