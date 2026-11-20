package db

import (
	"database/sql"
	"testing"
)

func TestPruneOldConfigurationVersionsKeepsActiveAndRecentHistory(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	for _, statement := range []string{
		`CREATE TABLE configuration_model (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, original_name TEXT, previous_id INTEGER, upload_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP, uploaded_by TEXT NOT NULL, mof_file BLOB NOT NULL, last_usage TIMESTAMP NULL)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	// Base 'app' : version active + 5 historiques.
	// Base 'web' : version active + 2 historiques.
	rows := []struct {
		name, original, uploadDate string
	}{
		{"app", "app", "2026-01-01 00:00:00"}, // active
		{"app-1", "app", "2026-01-02 00:00:00"},
		{"app-2", "app", "2026-01-03 00:00:00"},
		{"app-3", "app", "2026-01-04 00:00:00"},
		{"app-4", "app", "2026-01-05 00:00:00"},
		{"app-5", "app", "2026-01-06 00:00:00"},
		{"web", "web", "2026-02-01 00:00:00"}, // active
		{"web-1", "web", "2026-02-02 00:00:00"},
		{"web-2", "web", "2026-02-03 00:00:00"},
	}
	for _, r := range rows {
		if _, err := database.Exec(`INSERT INTO configuration_model (name, original_name, upload_date, uploaded_by, mof_file) VALUES (?, ?, ?, 'tester', ?)`, r.name, r.original, r.uploadDate, []byte("mof")); err != nil {
			t.Fatal(err)
		}
	}

	// maxKeep=3 => on conserve l'active + les 2 historiques les plus récentes par base.
	pruned, err := PruneOldConfigurationVersions(database, 3)
	if err != nil {
		t.Fatalf("PruneOldConfigurationVersions: %v", err)
	}
	// 'app' : app-3, app-2, app-1 supprimées (3). 'web' : aucune (seulement 2 histoires).
	if pruned != 3 {
		t.Fatalf("pruned = %d, want 3", pruned)
	}

	var remaining int
	if err := database.QueryRow(`SELECT COUNT(*) FROM configuration_model`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	// 9 insertées - 3 supprimées = 6 restantes.
	if remaining != 6 {
		t.Fatalf("remaining = %d, want 6", remaining)
	}

	// L'active doit toujours être présente.
	var activeCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM configuration_model WHERE original_name = name`).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 2 {
		t.Fatalf("active count = %d, want 2", activeCount)
	}

	// Les historiques conservées pour 'app' doivent être app-5 et app-4.
	var kept []string
	rows2, err := database.Query(`SELECT name FROM configuration_model WHERE original_name = 'app' AND name <> original_name ORDER BY upload_date DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var n string
		if err := rows2.Scan(&n); err != nil {
			t.Fatal(err)
		}
		kept = append(kept, n)
	}
	if len(kept) != 2 || kept[0] != "app-5" || kept[1] != "app-4" {
		t.Fatalf("kept histories = %v, want [app-5 app-4]", kept)
	}
}

func TestPruneOldConfigurationVersionsMaxKeepOneKeepsOnlyActive(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	if _, err := database.Exec(`CREATE TABLE configuration_model (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, original_name TEXT, previous_id INTEGER, upload_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP, uploaded_by TEXT NOT NULL, mof_file BLOB NOT NULL, last_usage TIMESTAMP NULL)`); err != nil {
		t.Fatal(err)
	}

	for _, r := range []struct{ name, original, uploadDate string }{
		{"app", "app", "2026-01-01 00:00:00"},
		{"app-1", "app", "2026-01-02 00:00:00"},
		{"app-2", "app", "2026-01-03 00:00:00"},
	} {
		if _, err := database.Exec(`INSERT INTO configuration_model (name, original_name, upload_date, uploaded_by, mof_file) VALUES (?, ?, ?, 'tester', ?)`, r.name, r.original, r.uploadDate, []byte("mof")); err != nil {
			t.Fatal(err)
		}
	}

	pruned, err := PruneOldConfigurationVersions(database, 1)
	if err != nil {
		t.Fatalf("PruneOldConfigurationVersions: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("pruned = %d, want 2", pruned)
	}

	var remaining int
	if err := database.QueryRow(`SELECT COUNT(*) FROM configuration_model`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining = %d, want 1 (only active)", remaining)
	}
}
