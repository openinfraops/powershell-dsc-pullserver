package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDscSettingsSaveAndGet(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`CREATE TABLE dsc_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		certificate_rotation_enabled INTEGER NOT NULL DEFAULT 0,
		certificate_renewal_days INTEGER NOT NULL DEFAULT 30,
		legacy_imported INTEGER NOT NULL DEFAULT 0,
		reregistration_mode TEXT NOT NULL DEFAULT 'deny',
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO dsc_settings (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}

	// Valeurs par défaut.
	settings, err := GetDscSettings(database)
	if err != nil {
		t.Fatal(err)
	}
	if settings.CertificateRotationEnabled || settings.CertificateRenewalDays != 30 {
		t.Fatalf("unexpected defaults: %+v", settings)
	}

	// Initialisation : applique le défaut de la fenêtre de renouvellement.
	settings, err = InitializeDscSettings(database, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if settings.CertificateRenewalDays != 30 || !settings.LegacyImported {
		t.Fatalf("unexpected initialized settings: %+v", settings)
	}

	// Écriture d'une configuration non-défaut.
	settings.CertificateRotationEnabled = true
	settings.CertificateRenewalDays = 7
	if err := SaveDscSettings(database, "sqlite", settings); err != nil {
		t.Fatal(err)
	}
	got, err := GetDscSettings(database)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CertificateRotationEnabled || got.CertificateRenewalDays != 7 {
		t.Fatalf("settings were not persisted: %+v", got)
	}
}
