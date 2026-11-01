package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestConfigurationRepoSettingsLegacyImportAndSave(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`CREATE TABLE configuration_repo_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1), enabled INTEGER NOT NULL DEFAULT 0,
		repository_url TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL DEFAULT 'main',
		checkout_directory TEXT NOT NULL DEFAULT 'data/configuration-repository',
		compiler_script TEXT NOT NULL DEFAULT 'scripts/Compile-Configurations.ps1',
		powershell_executable TEXT NOT NULL DEFAULT 'pwsh', sync_interval_mins INTEGER NOT NULL DEFAULT 60,
		compile_on_node_registration INTEGER NOT NULL DEFAULT 0, legacy_imported INTEGER NOT NULL DEFAULT 0,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO configuration_repo_settings (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}

	legacy := ConfigurationRepoSettings{
		Enabled: true, URL: "https://example.test/configs.git", Branch: "release",
		Directory: "checkout", CompilerScript: "compile.ps1", PowerShellExecutable: "pwsh-preview",
		SyncIntervalMins: 15, CompileOnNodeRegistration: true,
	}
	settings, err := InitializeConfigurationRepoSettings(database, "sqlite", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.LegacyImported || settings.URL != legacy.URL || !settings.CompileOnNodeRegistration {
		t.Fatalf("legacy settings were not imported: %+v", settings)
	}

	settings.URL = ""
	settings.Enabled = false
	if err := SaveConfigurationRepoSettings(database, "sqlite", settings); err != nil {
		t.Fatal(err)
	}
	settings, err = InitializeConfigurationRepoSettings(database, "sqlite", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if settings.URL != "" || settings.Enabled {
		t.Fatalf("cleared settings were restored from legacy config: %+v", settings)
	}
}
