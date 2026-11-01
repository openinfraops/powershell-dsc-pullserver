IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='configuration_repo_settings' AND xtype='U')
CREATE TABLE configuration_repo_settings (
    id INT PRIMARY KEY CHECK (id = 1),
    enabled BIT NOT NULL DEFAULT 0,
    repository_url NVARCHAR(2048) NOT NULL DEFAULT '',
    branch NVARCHAR(255) NOT NULL DEFAULT 'main',
    checkout_directory NVARCHAR(1024) NOT NULL DEFAULT 'data/configuration-repository',
    compiler_script NVARCHAR(1024) NOT NULL DEFAULT 'scripts/Compile-Configurations.ps1',
    powershell_executable NVARCHAR(255) NOT NULL DEFAULT 'pwsh',
    sync_interval_mins INT NOT NULL DEFAULT 60,
    compile_on_node_registration BIT NOT NULL DEFAULT 0,
    legacy_imported BIT NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
IF NOT EXISTS (SELECT 1 FROM configuration_repo_settings WHERE id = 1)
    INSERT INTO configuration_repo_settings (id) VALUES (1);
IF EXISTS (SELECT 1 FROM dsc_infra_info WHERE id = 1)
    UPDATE dsc_infra_info SET db_version = '1.2.0', updated_at = GETDATE() WHERE id = 1;
ELSE
    INSERT INTO dsc_infra_info (id, web_version, db_version, updated_at) VALUES (1, '1.2.0', '1.2.0', GETDATE());
