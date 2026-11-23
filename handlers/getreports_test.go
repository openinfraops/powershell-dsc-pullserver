package handlers

import (
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractJobId(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "Reports(JobId='11111111-2222-3333-4444-555555555555')", want: "11111111-2222-3333-4444-555555555555"},
		{in: `Reports(JobId="11111111-2222-3333-4444-555555555555")`, want: "11111111-2222-3333-4444-555555555555"},
		{in: "11111111-2222-3333-4444-555555555555", want: "11111111-2222-3333-4444-555555555555"},
		{in: "Reports(", want: ""},
		{in: "", want: ""},
	}
	for _, c := range cases {
		if got := extractJobId(c.in); got != c.want {
			t.Errorf("extractJobId(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGetReportsHandler_RawJSON(t *testing.T) {
	database := setupAgentsDB(t)
	injectShared(t, database, true)
	if _, err := database.Exec(`CREATE TABLE reports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		agent_id TEXT,
		job_id TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		raw_json TEXT)`); err != nil {
		t.Fatal(err)
	}

	cert := genDSCClientCert(t)
	sum := sha1.Sum(cert.Raw)
	thumb := strings.ToLower(hex.EncodeToString(sum[:]))
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		agentID, "NODE", thumb, "compliant"); err != nil {
		t.Fatal(err)
	}

	const rawJSON = `{"JobId":"11111111-2222-3333-4444-555555555555","Status":"Success"}`
	if _, err := database.Exec(`INSERT INTO reports (agent_id, job_id, raw_json) VALUES (?, ?, ?)`, agentID, "11111111-2222-3333-4444-555555555555", rawJSON); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+agentID+"')/Reports(JobId='11111111-2222-3333-4444-555555555555')", nil)
	req.SetPathValue("node", "Nodes(AgentId='"+agentID+"')")
	req.SetPathValue("reports", "Reports(JobId='11111111-2222-3333-4444-555555555555')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	GetReportsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != rawJSON {
		t.Fatalf("expected raw report JSON %q, got %q", rawJSON, got)
	}
	if got := rec.Header().Get("ProtocolVersion"); got != "2.0" {
		t.Fatalf("expected ProtocolVersion 2.0, got %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", got)
	}
}
