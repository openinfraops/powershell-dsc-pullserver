-- Migration: add original_name and previous_id columns to configuration_model if table already exists
IF EXISTS (SELECT * FROM sysobjects WHERE name='configuration_model' AND xtype='U')
BEGIN
    IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'configuration_model' AND COLUMN_NAME = 'original_name')
        ALTER TABLE configuration_model ADD original_name NVARCHAR(128) NULL;
    IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'configuration_model' AND COLUMN_NAME = 'previous_id')
        ALTER TABLE configuration_model ADD previous_id INT NULL;
    -- Add FK constraint if not exists
    IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.CONSTRAINT_COLUMN_USAGE WHERE TABLE_NAME = 'configuration_model' AND COLUMN_NAME = 'previous_id')
        ALTER TABLE configuration_model ADD CONSTRAINT FK_configuration_model_previous_id FOREIGN KEY (previous_id) REFERENCES configuration_model(id);
END

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='users' AND xtype='U')
CREATE TABLE users (
    id INT IDENTITY(1,1) PRIMARY KEY,
    first_name NVARCHAR(128) NOT NULL,
    last_name NVARCHAR(128) NOT NULL,
    email NVARCHAR(255) NOT NULL UNIQUE,
    password_hash NVARCHAR(255) NOT NULL,
    is_active BIT DEFAULT 1,
    created_at DATETIME DEFAULT GETDATE(),
    last_logon_date DATETIME,
    role NVARCHAR(50) DEFAULT 'user',
    source NVARCHAR(50) DEFAULT 'local'
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='user_api_tokens' AND xtype='U')
CREATE TABLE user_api_tokens (
    id INT IDENTITY(1,1) PRIMARY KEY,
    user_id INT NOT NULL,
    token_hash NVARCHAR(255) NOT NULL,
    label NVARCHAR(255),
    is_active BIT DEFAULT 1,
    created_at DATETIME DEFAULT GETDATE(),
    revoked_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='modules' AND xtype='U')
CREATE TABLE modules (
    id INT IDENTITY(1,1) PRIMARY KEY,
    name NVARCHAR(255) NOT NULL,
    version NVARCHAR(50) NOT NULL,
    checksum NVARCHAR(255) NOT NULL,
    zip_blob VARBINARY(MAX) NOT NULL,
    uploaded_at DATETIME DEFAULT GETDATE()
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='reports' AND xtype='U')
CREATE TABLE reports (
    id INT IDENTITY(1,1) PRIMARY KEY,
    agent_id NVARCHAR(128),
    job_id NVARCHAR(128),
    report_format_version NVARCHAR(50),
    operation_type NVARCHAR(50),
    refresh_mode NVARCHAR(50),
    status NVARCHAR(50),
    start_time NVARCHAR(50),
    end_time NVARCHAR(50),
    reboot_requested NVARCHAR(10),
    errors NVARCHAR(MAX),           -- JSON array
    status_data NVARCHAR(MAX),      -- JSON array
    additional_data NVARCHAR(MAX),  -- JSON array
    mof_applied BIT DEFAULT 0,      -- Indique si un MOF a été appliqué (0 ou 1)
    created_at DATETIME DEFAULT GETDATE(),
    raw_json NVARCHAR(MAX)
);

-- Pour migration manuelle si la colonne n'existe pas déjà :
-- IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'reports' AND COLUMN_NAME = 'mof_applied')
--     ALTER TABLE reports ADD mof_applied BIT DEFAULT 0;
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'idx_reports_agent_id' AND object_id = OBJECT_ID('reports'))
    CREATE INDEX idx_reports_agent_id ON reports(agent_id);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='dsc_infra_info' AND xtype='U')
CREATE TABLE dsc_infra_info (
    id INT PRIMARY KEY CHECK (id = 1),
    web_version NVARCHAR(20) DEFAULT '0.0.1',
    db_version NVARCHAR(20) DEFAULT '1.2.0',
    latest_release NVARCHAR(50) NULL,
    latest_release_url NVARCHAR(255) NULL,
    update_available BIT DEFAULT 0,
    release_check_ok BIT DEFAULT 0,
    release_checked_at DATETIME NULL,
    updated_at DATETIME DEFAULT GETDATE()
);
IF NOT EXISTS (SELECT 1 FROM dsc_infra_info WHERE id = 1)
    INSERT INTO dsc_infra_info (id, web_version, db_version, updated_at) VALUES (1, '0.0.1', '1.2.0', GETDATE());

IF EXISTS (SELECT * FROM sysobjects WHERE name='dsc_infra_info' AND xtype='U')
BEGIN
    IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'dsc_infra_info' AND COLUMN_NAME = 'latest_release')
        ALTER TABLE dsc_infra_info ADD latest_release NVARCHAR(50) NULL;
    IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'dsc_infra_info' AND COLUMN_NAME = 'latest_release_url')
        ALTER TABLE dsc_infra_info ADD latest_release_url NVARCHAR(255) NULL;
    IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'dsc_infra_info' AND COLUMN_NAME = 'update_available')
        ALTER TABLE dsc_infra_info ADD update_available BIT DEFAULT 0;
    IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'dsc_infra_info' AND COLUMN_NAME = 'release_check_ok')
        ALTER TABLE dsc_infra_info ADD release_check_ok BIT DEFAULT 0;
    IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'dsc_infra_info' AND COLUMN_NAME = 'release_checked_at')
        ALTER TABLE dsc_infra_info ADD release_checked_at DATETIME NULL;
END

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='scheduler_tasks' AND xtype='U')
CREATE TABLE scheduler_tasks (
    task_name NVARCHAR(64) PRIMARY KEY,
    display_name NVARCHAR(128) NOT NULL,
    next_run_at DATETIME NULL,
    last_run_at DATETIME NULL,
    last_status NVARCHAR(32) NOT NULL DEFAULT 'idle',
    last_message NVARCHAR(512) NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

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

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='scheduler_task_runs' AND xtype='U')
CREATE TABLE scheduler_task_runs (
    id INT IDENTITY(1,1) PRIMARY KEY,
    task_name NVARCHAR(64) NOT NULL,
    started_at DATETIME NOT NULL,
    finished_at DATETIME NULL,
    status NVARCHAR(32) NOT NULL,
    message NVARCHAR(512) NULL,
    trigger_source NVARCHAR(32) NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'idx_scheduler_runs_task_started' AND object_id = OBJECT_ID('scheduler_task_runs'))
    CREATE INDEX idx_scheduler_runs_task_started ON scheduler_task_runs(task_name, started_at DESC);

-- To update version:
-- UPDATE dsc_infra_info SET db_version = '1.1.3p2', updated_at = GETDATE() WHERE id = 1;

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='agents' AND xtype='U')
CREATE TABLE agents (
    agent_id NVARCHAR(128) PRIMARY KEY,
    node_name NVARCHAR(128),
    lcm_version NVARCHAR(50) NULL,
    registration_type NVARCHAR(50) NULL,
    certificate_thumbprint NVARCHAR(128) NULL,
    certificate_subject NVARCHAR(255) NULL,
    certificate_issuer NVARCHAR(255) NULL,
    certificate_notbefore NVARCHAR(50) NULL,
    certificate_notafter NVARCHAR(50) NULL,
    registered_at DATETIME DEFAULT GETDATE(),
    last_communication DATETIME DEFAULT GETDATE(),
    state NVARCHAR(50),
    has_error_last_report BIT DEFAULT 0
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='agent_configurations' AND xtype='U')
CREATE TABLE agent_configurations (
    agent_id NVARCHAR(128),
    configuration_name NVARCHAR(128),
    PRIMARY KEY (agent_id, configuration_name),
    FOREIGN KEY (agent_id) REFERENCES agents(agent_id)
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='agent_ips' AND xtype='U')
CREATE TABLE agent_ips (
    agent_id NVARCHAR(128),
    ip_address NVARCHAR(45),
    PRIMARY KEY (agent_id, ip_address),
    FOREIGN KEY (agent_id) REFERENCES agents(agent_id)
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='properties' AND xtype='U')
CREATE TABLE properties (
    id INT IDENTITY(1,1) PRIMARY KEY,
    name NVARCHAR(128) NOT NULL UNIQUE,
    description NVARCHAR(255),
    priority INT DEFAULT 0
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='node_properties' AND xtype='U')
CREATE TABLE node_properties (
    node_id NVARCHAR(128) NOT NULL,
    property_id INT NOT NULL,
    value NVARCHAR(255),
    PRIMARY KEY (node_id, property_id),
    FOREIGN KEY (property_id) REFERENCES properties(id)
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='configuration_model' AND xtype='U')
CREATE TABLE configuration_model (
    id INT IDENTITY(1,1) PRIMARY KEY,
    name NVARCHAR(128) NOT NULL,
    original_name NVARCHAR(128) NULL,
    previous_id INT NULL,
    upload_date DATETIME DEFAULT GETDATE(),
    uploaded_by NVARCHAR(128) NOT NULL,
    mof_file VARBINARY(MAX) NOT NULL,
    last_usage DATETIME,
    FOREIGN KEY (previous_id) REFERENCES configuration_model(id)
);

IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='agent_tags' AND xtype='U')
CREATE TABLE agent_tags (
    agent_id NVARCHAR(128) NOT NULL,
    tag_key NVARCHAR(128) NOT NULL,
    tag_value NVARCHAR(255) NOT NULL,
    PRIMARY KEY (agent_id, tag_key, tag_value),
    FOREIGN KEY (agent_id) REFERENCES agents(agent_id)
);


-- Recommended indexes for performance
IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'idx_configuration_model_name' AND object_id = OBJECT_ID('configuration_model'))
    CREATE INDEX idx_configuration_model_name ON configuration_model(name);

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'idx_configuration_model_original_name' AND object_id = OBJECT_ID('configuration_model'))
    CREATE INDEX idx_configuration_model_original_name ON configuration_model(original_name);

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'idx_agent_configurations_configuration_name' AND object_id = OBJECT_ID('agent_configurations'))
    CREATE INDEX idx_agent_configurations_configuration_name ON agent_configurations(configuration_name);

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'idx_agents_state' AND object_id = OBJECT_ID('agents'))
    CREATE INDEX idx_agents_state ON agents(state);

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'idx_reports_job_id' AND object_id = OBJECT_ID('reports'))
    CREATE INDEX idx_reports_job_id ON reports(job_id);

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'idx_modules_name' AND object_id = OBJECT_ID('modules'))
    CREATE INDEX idx_modules_name ON modules(name);