package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
)

func TestExtractRebootRequester(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{
			name: "module and resource",
			in:   []string{`{"RebootRequested":"True","ModuleName":"xPSDesiredStateConfiguration","ResourceId":"[xPackage]MyPkg"}`},
			want: "xPSDesiredStateConfiguration:[xPackage]MyPkg",
		},
		{
			name: "source fallback when module missing",
			in:   []string{`{"RebootRequested":"true","SourceInfo":"C:\\x.ps1::1","ResourceId":"[xPackage]P"}`},
			want: "C:\\x.ps1::1:[xPackage]P",
		},
		{
			name: "no reboot requested",
			in:   []string{`{"RebootRequested":"False","ModuleName":"M","ResourceId":"R"}`},
			want: "",
		},
		{
			name: "unknown when no identifier",
			in:   []string{`{"RebootRequested":"True"}`},
			want: "(unknown)",
		},
		{
			name: "malformed json skipped",
			in:   []string{`{not-json`},
			want: "",
		},
		{
			name: "case insensitive key",
			in:   []string{`{"rebootrequested":"True","ModuleName":"M","ResourceId":"R"}`},
			want: "M:R",
		},
		{
			name: "second entry used",
			in:   []string{`{"RebootRequested":"False","ResourceId":"A"}`, `{"RebootRequested":"True","ResourceId":"B"}`},
			want: "B",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractRebootRequester(c.in); got != c.want {
				t.Fatalf("extractRebootRequester(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestRebootRequestedString(t *testing.T) {
	if got := rebootRequestedString(schema.FlexBool(true)); got != "1" {
		t.Fatalf("true -> %q, want 1", got)
	}
	if got := rebootRequestedString(schema.FlexBool(false)); got != "0" {
		t.Fatalf("false -> %q, want 0", got)
	}
}

// TestGetAgents_RebootFilters vérifie que ?reboot_pending=true|false et
// ?reboot_loop=true filtrent correctement la liste des nœuds.
func TestGetAgents_RebootFilters(t *testing.T) {
	database := setupAgentsDB(t)

	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, reboot_pending, reboot_loop, reboot_request_count, reboot_pending_since, reboot_requested_by) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"pending", "PN", 1, 0, 2, "2026-01-01 00:00:00", "Mod:Res")
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, reboot_pending, reboot_loop) VALUES (?, ?, ?, ?)`,
		"normal", "NN", 0, 0)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, reboot_pending, reboot_loop) VALUES (?, ?, ?, ?)`,
		"loop", "LN", 1, 1)

	oldShared := db.Shared()
	oldAppConfig := global.AppConfig
	defer func() {
		db.SetShared(oldShared)
		global.AppConfig = oldAppConfig
	}()
	db.SetShared(database)
	global.AppConfig = &schema.AppConfig{Database: schema.DBConfig{Driver: "sqlite"}}

	// ?reboot_pending=true -> pending + loop (les deux ont reboot_pending=1).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?reboot_pending=true", nil)
	rec := httptest.NewRecorder()
	GetAgentsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reboot_pending=true status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	ids := decodeAgentIDs(t, rec.Body.Bytes())
	assertContains(t, ids, "pending")
	assertContains(t, ids, "loop")
	assertNotContains(t, ids, "normal")

	// ?reboot_pending=false -> normal seul.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agents?reboot_pending=false", nil)
	rec = httptest.NewRecorder()
	GetAgentsHandler(rec, req)
	ids = decodeAgentIDs(t, rec.Body.Bytes())
	assertNotContains(t, ids, "pending")
	assertNotContains(t, ids, "loop")
	assertContains(t, ids, "normal")

	// ?reboot_loop=true -> loop seul.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/agents?reboot_loop=true", nil)
	rec = httptest.NewRecorder()
	GetAgentsHandler(rec, req)
	ids = decodeAgentIDs(t, rec.Body.Bytes())
	assertNotContains(t, ids, "pending")
	assertContains(t, ids, "loop")
	assertNotContains(t, ids, "normal")
}

// TestGetAgents_RebootStats vérifie que ?stats expose reboot_pending et
// reboot_loop.
func TestGetAgents_RebootStats(t *testing.T) {
	database := setupAgentsDB(t)

	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, reboot_pending, reboot_loop) VALUES (?, ?, ?, ?)`, "p1", "P1", 1, 0)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, reboot_pending, reboot_loop) VALUES (?, ?, ?, ?)`, "p2", "P2", 1, 1)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, reboot_pending, reboot_loop) VALUES (?, ?, ?, ?)`, "n1", "N1", 0, 0)

	oldShared := db.Shared()
	oldAppConfig := global.AppConfig
	defer func() {
		db.SetShared(oldShared)
		global.AppConfig = oldAppConfig
	}()
	db.SetShared(database)
	global.AppConfig = &schema.AppConfig{Database: schema.DBConfig{Driver: "sqlite"}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?stats", nil)
	rec := httptest.NewRecorder()
	GetAgentsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var stats map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats["reboot_pending"] != 2 {
		t.Fatalf("reboot_pending = %d, want 2", stats["reboot_pending"])
	}
	if stats["reboot_loop"] != 1 {
		t.Fatalf("reboot_loop = %d, want 1", stats["reboot_loop"])
	}
}

func decodeAgentIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var agents []struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(body, &agents); err != nil {
		t.Fatalf("decode agents: %v (body: %s)", err, body)
	}
	ids := make([]string, 0, len(agents))
	for _, a := range agents {
		ids = append(ids, a.AgentID)
	}
	return ids
}

func assertContains(t *testing.T, ids []string, want string) {
	t.Helper()
	for _, id := range ids {
		if id == want {
			return
		}
	}
	t.Fatalf("expected %q to be present, got %v", want, ids)
}

func assertNotContains(t *testing.T, ids []string, want string) {
	t.Helper()
	for _, id := range ids {
		if id == want {
			t.Fatalf("expected %q to be absent, got %v", want, ids)
		}
	}
}
