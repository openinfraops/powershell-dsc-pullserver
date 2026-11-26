package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseBoolFlexible(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want bool
	}{
		{"true bool", true, true},
		{"false bool", false, false},
		{"True string", "True", true},
		{"lowercase true", "true", true},
		{"mixed case true", "TrUe", true},
		{"False string", "False", false},
		{"lowercase false", "false", false},
		{"one string", "1", true},
		{"zero string", "0", false},
		{"t", "t", true},
		{"yes", "yes", true},
		{"empty string", "", false},
		{"garbage", "maybe", false},
		{"int64 one", int64(1), true},
		{"int64 zero", int64(0), false},
		{"int one", 1, true},
		{"float64 one", float64(1), true},
		{"float64 zero", float64(0), false},
		{"nil", nil, false},
		{"bytes true", []byte("True"), true},
		{"bytes zero", []byte("0"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseBoolFlexible(c.in); got != c.want {
				t.Fatalf("ParseBoolFlexible(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestFlexBoolUnmarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
		err  bool
	}{
		{"quoted True", `"True"`, true, false},
		{"quoted False", `"False"`, false, false},
		{"json true", `true`, true, false},
		{"json false", `false`, false, false},
		{"lowercase", `"true"`, true, false},
		{"one", `"1"`, true, false},
		{"zero", `"0"`, false, false},
		{"garbage", `"nope"`, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b FlexBool
			if err := json.Unmarshal([]byte(c.in), &b); err != nil {
				if !c.err {
					t.Fatalf("unmarshal %q: %v", c.in, err)
				}
				return
			}
			if bool(b) != c.want {
				t.Fatalf("FlexBool(%q) = %v, want %v", c.in, bool(b), c.want)
			}
		})
	}
}

// TestFlexBoolMarshalJSON vérifie que FlexBool s'encode comme un booléen JSON
// natif (pas comme une chaîne), ce qui garantit que les endpoints /reports
// renvoient `RebootRequested` sous forme de true/false.
func TestFlexBoolMarshalJSON(t *testing.T) {
	if b, err := json.Marshal(FlexBool(true)); err != nil || string(b) != "true" {
		t.Fatalf("marshal true = %s, err=%v; want true", b, err)
	}
	if b, err := json.Marshal(FlexBool(false)); err != nil || string(b) != "false" {
		t.Fatalf("marshal false = %s, err=%v; want false", b, err)
	}
}

// TestDscReportRebootRoundTrip vérifie qu'un rapport DSC avec RebootRequested
// au format chaîne "True"/"False" se normalise puis se ré-encode en booléen.
func TestDscReportRebootRoundTrip(t *testing.T) {
	body := `{"JobId":"j1","ReportFormatVersion":"2.0","RebootRequested":"True","StatusData":["{\"RebootRequested\":\"True\",\"ModuleName\":\"M\",\"ResourceId\":\"R\"}"]}`
	var report DscReport
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if !bool(report.RebootRequested) {
		t.Fatalf("RebootRequested = false, want true (normalized from \"True\")")
	}
	out, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if !strings.Contains(string(out), `"RebootRequested":true`) {
		t.Fatalf("marshaled report does not contain RebootRequested:true\n%s", out)
	}
}
