package db

import "database/sql"

type ConfigurationRepoSettings struct {
	Enabled                   bool   `json:"enable_configuration_repo_sync"`
	URL                       string `json:"configuration_repo_url"`
	Branch                    string `json:"configuration_repo_branch"`
	Directory                 string `json:"configuration_repo_directory"`
	CompilerScript            string `json:"configuration_compiler_script"`
	PowerShellExecutable      string `json:"configuration_powershell_executable"`
	SyncIntervalMins          int    `json:"configuration_repo_sync_interval_mins"`
	CompileOnNodeRegistration bool   `json:"compile_configuration_on_node_registration"`
	LegacyImported            bool   `json:"-"`
}

func GetConfigurationRepoSettings(database *sql.DB) (ConfigurationRepoSettings, error) {
	var settings ConfigurationRepoSettings
	err := database.QueryRow(`SELECT enabled, repository_url, branch, checkout_directory, compiler_script, powershell_executable, sync_interval_mins, compile_on_node_registration, legacy_imported FROM configuration_repo_settings WHERE id = 1`).Scan(
		&settings.Enabled,
		&settings.URL,
		&settings.Branch,
		&settings.Directory,
		&settings.CompilerScript,
		&settings.PowerShellExecutable,
		&settings.SyncIntervalMins,
		&settings.CompileOnNodeRegistration,
		&settings.LegacyImported,
	)
	return settings, err
}

func SaveConfigurationRepoSettings(database *sql.DB, driver string, settings ConfigurationRepoSettings) error {
	if driver == "mssql" || driver == "sqlserver" {
		_, err := database.Exec(`IF EXISTS (SELECT 1 FROM configuration_repo_settings WHERE id = 1)
UPDATE configuration_repo_settings SET enabled = ?, repository_url = ?, branch = ?, checkout_directory = ?, compiler_script = ?, powershell_executable = ?, sync_interval_mins = ?, compile_on_node_registration = ?, legacy_imported = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1
ELSE
INSERT INTO configuration_repo_settings (id, enabled, repository_url, branch, checkout_directory, compiler_script, powershell_executable, sync_interval_mins, compile_on_node_registration, legacy_imported) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			settings.Enabled, settings.URL, settings.Branch, settings.Directory, settings.CompilerScript, settings.PowerShellExecutable, settings.SyncIntervalMins, settings.CompileOnNodeRegistration, settings.LegacyImported,
			settings.Enabled, settings.URL, settings.Branch, settings.Directory, settings.CompilerScript, settings.PowerShellExecutable, settings.SyncIntervalMins, settings.CompileOnNodeRegistration, settings.LegacyImported,
		)
		return err
	}
	_, err := database.Exec(`INSERT INTO configuration_repo_settings (id, enabled, repository_url, branch, checkout_directory, compiler_script, powershell_executable, sync_interval_mins, compile_on_node_registration, legacy_imported, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(id) DO UPDATE SET enabled = excluded.enabled, repository_url = excluded.repository_url, branch = excluded.branch, checkout_directory = excluded.checkout_directory, compiler_script = excluded.compiler_script, powershell_executable = excluded.powershell_executable, sync_interval_mins = excluded.sync_interval_mins, compile_on_node_registration = excluded.compile_on_node_registration, legacy_imported = excluded.legacy_imported, updated_at = CURRENT_TIMESTAMP`,
		settings.Enabled, settings.URL, settings.Branch, settings.Directory, settings.CompilerScript, settings.PowerShellExecutable, settings.SyncIntervalMins, settings.CompileOnNodeRegistration, settings.LegacyImported,
	)
	return err
}

func InitializeConfigurationRepoSettings(database *sql.DB, driver string, legacy ConfigurationRepoSettings) (ConfigurationRepoSettings, error) {
	settings, err := GetConfigurationRepoSettings(database)
	if err != nil {
		return ConfigurationRepoSettings{}, err
	}
	if !settings.LegacyImported {
		if settings.URL == "" && legacy.URL != "" {
			settings = legacy
		}
		settings.LegacyImported = true
		if settings.Branch == "" {
			settings.Branch = "main"
		}
		if settings.Directory == "" {
			settings.Directory = "data/configuration-repository"
		}
		if settings.CompilerScript == "" {
			settings.CompilerScript = "scripts/Compile-Configurations.ps1"
		}
		if settings.PowerShellExecutable == "" {
			settings.PowerShellExecutable = "pwsh"
		}
		if settings.SyncIntervalMins <= 0 {
			settings.SyncIntervalMins = 60
		}
		if err := SaveConfigurationRepoSettings(database, driver, settings); err != nil {
			return ConfigurationRepoSettings{}, err
		}
	}
	if settings.Branch == "" {
		settings.Branch = "main"
	}
	if settings.Directory == "" {
		settings.Directory = "data/configuration-repository"
	}
	if settings.CompilerScript == "" {
		settings.CompilerScript = "scripts/Compile-Configurations.ps1"
	}
	if settings.PowerShellExecutable == "" {
		settings.PowerShellExecutable = "pwsh"
	}
	if settings.SyncIntervalMins <= 0 {
		settings.SyncIntervalMins = 60
	}
	return settings, nil
}
