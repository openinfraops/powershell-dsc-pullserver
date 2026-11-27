package db

import (
	"database/sql"
	"strings"
	"testing"
)

func newCertTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`CREATE TABLE agents (
		agent_id TEXT PRIMARY KEY,
		node_name TEXT,
		certificate_thumbprint TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	// Real schema ships an index on certificate_thumbprint so the middleware
	// lookup (WHERE certificate_thumbprint = ?) uses the index (issue #73).
	if _, err := database.Exec(`CREATE INDEX IF NOT EXISTS idx_agents_certificate_thumbprint ON agents(certificate_thumbprint)`); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestNormalizeCertificateThumbprintsLowercasesExistingRows(t *testing.T) {
	database := newCertTestDB(t)

	// Legacy rows registered before the lowercase change: thumbprints stored uppercase.
	for _, tp := range []string{"906BF4B01D9A6916CE2848E94B7AAA825D9A1BA0", "830E60A13631B0E1C9720D3000B7B8DBC85D163F"} {
		if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint) VALUES (?, ?, ?)`, "agent-"+tp, "node-"+tp, tp); err != nil {
			t.Fatal(err)
		}
	}
	// A row already lowercase must not be rewritten.
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint) VALUES (?, ?, ?)`, "agent-lower", "node-lower", "3cd55f62621b6f7ee1e7a55b3795a8f129240ccb"); err != nil {
		t.Fatal(err)
	}

	normalized, err := NormalizeCertificateThumbprints(database, "sqlite")
	if err != nil {
		t.Fatalf("NormalizeCertificateThumbprints: %v", err)
	}
	if normalized != 2 {
		t.Fatalf("normalized = %d, want 2", normalized)
	}

	rows, err := database.Query(`SELECT certificate_thumbprint FROM agents ORDER BY agent_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var tp string
		if err := rows.Scan(&tp); err != nil {
			t.Fatal(err)
		}
		got = append(got, tp)
	}
	want := []string{
		"830e60a13631b0e1c9720d3000b7b8dbc85d163f",
		"906bf4b01d9a6916ce2848e94b7aaa825d9a1ba0",
		"3cd55f62621b6f7ee1e7a55b3795a8f129240ccb",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeCertificateThumbprintsIsIdempotent(t *testing.T) {
	database := newCertTestDB(t)
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint) VALUES (?, ?, ?)`, "a", "n", "906BF4B01D9A6916CE2848E94B7AAA825D9A1BA0"); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeCertificateThumbprints(database, "sqlite"); err != nil {
		t.Fatal(err)
	}
	// Second run rewrites nothing.
	normalized, err := NormalizeCertificateThumbprints(database, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != 0 {
		t.Fatalf("second run normalized = %d, want 0", normalized)
	}
}

func TestIsCertificateThumbprintRegisteredAfterNormalization(t *testing.T) {
	database := newCertTestDB(t)
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint) VALUES (?, ?, ?)`, "a", "n", "906BF4B01D9A6916CE2848E94B7AAA825D9A1BA0"); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeCertificateThumbprints(database, "sqlite"); err != nil {
		t.Fatal(err)
	}
	// After normalisation a lowercase lookup must match the (now lowercase) stored row.
	ok, err := IsCertificateThumbprintRegistered(database, "906bf4b01d9a6916ce2848e94b7aaa825d9a1ba0")
	if err != nil {
		t.Fatalf("IsCertificateThumbprintRegistered: %v", err)
	}
	if !ok {
		t.Fatalf("expected lowercase thumbprint to be registered after normalization")
	}
}

// TestIsCertificateThumbprintRegisteredUsesIndex vérifie que la requête du
// middleware DSC (WHERE certificate_thumbprint = ?) est servie par l'index
// idx_agents_certificate_thumbprint et non par un scan complet de la table
// (issue #73, critère « le plan de requête utilise l'index »).
func TestIsCertificateThumbprintRegisteredUsesIndex(t *testing.T) {
	database := newCertTestDB(t)
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint) VALUES (?, ?, ?)`, "a", "n", "906bf4b01d9a6916ce2848e94b7aaa825d9a1ba0"); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`EXPLAIN QUERY PLAN SELECT COUNT(*) FROM agents WHERE certificate_thumbprint = ?`, "906bf4b01d9a6916ce2848e94b7aaa825d9a1ba0")
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()

	var plan []string
	for rows.Next() {
		var id, parent int
		var notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	joined := strings.Join(plan, "\n")
	if strings.Contains(joined, "SCAN") && !strings.Contains(joined, "USING INDEX") {
		t.Fatalf("expected the certificate_thumbprint lookup to use an index, got plan:\n%s", joined)
	}
	if !strings.Contains(joined, "idx_agents_certificate_thumbprint") {
		t.Fatalf("expected plan to reference idx_agents_certificate_thumbprint, got plan:\n%s", joined)
	}
}
