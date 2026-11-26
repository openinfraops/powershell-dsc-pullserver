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

func TestPruneOldConfigurationVersionsUnlinksPreviousIDChain(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	// Active la contrainte de clé étrangère auto-référente (comme sur MSSQL) pour
	// vérifier que la déliaison de previous_id rend le DELETE possible.
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE configuration_model (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		original_name TEXT,
		previous_id INTEGER,
		upload_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		uploaded_by TEXT NOT NULL,
		mof_file BLOB NOT NULL,
		last_usage TIMESTAMP NULL,
		FOREIGN KEY (previous_id) REFERENCES configuration_model(id)
	)`); err != nil {
		t.Fatal(err)
	}

	// Chaîne de versionnement pour la base 'app' : l'active (name==original_name)
	// et 5 historiques, chacune pointant vers la version précédente via previous_id.
	rows := []struct{ id int; name, original, uploadDate string }{
		{1, "app", "app", "2026-01-01 00:00:00"},   // active, previous_id NULL
		{2, "app-1", "app", "2026-01-02 00:00:00"}, // previous_id = 1
		{3, "app-2", "app", "2026-01-03 00:00:00"}, // previous_id = 2
		{4, "app-3", "app", "2026-01-04 00:00:00"}, // previous_id = 3
		{5, "app-4", "app", "2026-01-05 00:00:00"}, // previous_id = 4
		{6, "app-5", "app", "2026-01-06 00:00:00"}, // previous_id = 5
	}
	for _, r := range rows {
		var prev interface{}
		if r.id > 1 {
			prev = r.id - 1
		}
		if _, err := database.Exec(`INSERT INTO configuration_model (id, name, original_name, previous_id, upload_date, uploaded_by, mof_file) VALUES (?, ?, ?, ?, ?, 'tester', ?)`, r.id, r.name, r.original, prev, r.uploadDate, []byte("mof")); err != nil {
			t.Fatal(err)
		}
	}

	// maxKeep=3 => garde l'active + app-5 + app-4 ; supprime app-3, app-2, app-1.
	pruned, err := PruneOldConfigurationVersions(database, 3)
	if err != nil {
		t.Fatalf("PruneOldConfigurationVersions: %v", err)
	}
	if pruned != 3 {
		t.Fatalf("pruned = %d, want 3", pruned)
	}

	// La version conservée qui pointait vers un nœud supprimé (app-4 -> app-3)
	// doit avoir son previous_id remis à NULL ; la chaîne restante doit être intacte.
	var keptPreviousIDs map[int]*int = map[int]*int{}
	rows2, err := database.Query(`SELECT id, previous_id FROM configuration_model WHERE original_name = 'app' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var id int
		var prev sql.NullInt64
		if err := rows2.Scan(&id, &prev); err != nil {
			t.Fatal(err)
		}
		var p *int
		if prev.Valid {
			v := int(prev.Int64)
			p = &v
		}
		keptPreviousIDs[id] = p
	}

	// Seules l'active, app-5 et app-4 restent.
	if len(keptPreviousIDs) != 3 {
		t.Fatalf("kept %d rows, want 3: %v", len(keptPreviousIDs), keptPreviousIDs)
	}
	if _, ok := keptPreviousIDs[1]; !ok {
		t.Fatalf("active (id=1) missing: %v", keptPreviousIDs)
	}
	if _, ok := keptPreviousIDs[6]; !ok {
		t.Fatalf("app-5 (id=6) missing: %v", keptPreviousIDs)
	}
	// app-4 (id=5) est conservée mais ne doit plus pointer vers app-3 (supprimé).
	if v := keptPreviousIDs[5]; v != nil {
		t.Fatalf("app-4 previous_id = %d, want NULL (unlinked)", *v)
	}
	// app-5 (id=6) doit toujours pointer vers app-4 (id=5) : chaîne restante intacte.
	if v := keptPreviousIDs[6]; v == nil || *v != 5 {
		t.Fatalf("app-5 previous_id = %v, want 5", keptPreviousIDs[6])
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
