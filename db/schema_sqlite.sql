-- Table pour les tags clé/valeur par agent
CREATE TABLE IF NOT EXISTS agent_tags (
    agent_id TEXT,
    tag_key TEXT NOT NULL,
    tag_value TEXT NOT NULL,
    PRIMARY KEY (agent_id, tag_key, tag_value),
    FOREIGN KEY (agent_id) REFERENCES agents(agent_id)
);

-- Table pour les tokens API utilisateurs
CREATE TABLE IF NOT EXISTS user_api_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    token_hash TEXT NOT NULL,
    label TEXT,
    is_active BOOLEAN DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
-- Table pour les modules DSC uploadés
CREATE TABLE IF NOT EXISTS modules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    checksum TEXT NOT NULL,
    zip_blob BLOB NOT NULL,
    uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
-- Table pour l'historique des rapports DSC
CREATE TABLE IF NOT EXISTS reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id TEXT,
    job_id TEXT,
    report_format_version TEXT,
    operation_type TEXT,
    refresh_mode TEXT,
    status TEXT,
    start_time TEXT,
    end_time TEXT,
    reboot_requested TEXT,
    errors TEXT,           -- JSON array
    status_data TEXT,      -- JSON array
    additional_data TEXT,  -- JSON array
    mof_applied INTEGER DEFAULT 0, -- Indique si un MOF a été appliqué (0 ou 1)
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    raw_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_reports_agent_id ON reports(agent_id);
-- Table pour suivre les informations d'infrastructure DSC (version web, version db, date MAJ)
CREATE TABLE IF NOT EXISTS dsc_infra_info (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    web_version TEXT DEFAULT '0.0.1',
    db_version TEXT DEFAULT '1.2.0',
    latest_release TEXT,
    latest_release_url TEXT,
    update_available INTEGER DEFAULT 0,
    release_check_ok INTEGER DEFAULT 0,
    release_checked_at TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
INSERT OR IGNORE INTO dsc_infra_info (id, web_version, db_version, updated_at) VALUES (1, '0.0.1', '1.2.0', CURRENT_TIMESTAMP);
UPDATE dsc_infra_info SET db_version = '1.2.0', updated_at = CURRENT_TIMESTAMP WHERE id = 1;

CREATE TABLE IF NOT EXISTS scheduler_tasks (
    task_name TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    next_run_at TIMESTAMP NULL,
    last_run_at TIMESTAMP NULL,
    last_status TEXT NOT NULL DEFAULT 'idle',
    last_message TEXT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS configuration_repo_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled INTEGER NOT NULL DEFAULT 0,
    repository_url TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT 'main',
    checkout_directory TEXT NOT NULL DEFAULT 'data/configuration-repository',
    compiler_script TEXT NOT NULL DEFAULT 'scripts/Compile-Configurations.ps1',
    powershell_executable TEXT NOT NULL DEFAULT 'pwsh',
    sync_interval_mins INTEGER NOT NULL DEFAULT 60,
    compile_on_node_registration INTEGER NOT NULL DEFAULT 0,
    legacy_imported INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT OR IGNORE INTO configuration_repo_settings (id) VALUES (1);

CREATE TABLE IF NOT EXISTS scheduler_task_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_name TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP NULL,
    status TEXT NOT NULL,
    message TEXT NULL,
    trigger_source TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_scheduler_runs_task_started ON scheduler_task_runs(task_name, started_at DESC);

-- Schéma pour la table agents

CREATE TABLE IF NOT EXISTS agents (
    agent_id TEXT PRIMARY KEY,
    node_name TEXT,
    lcm_version TEXT,
    registration_type TEXT,
    certificate_thumbprint TEXT,
    certificate_subject TEXT,
    certificate_issuer TEXT,
    certificate_notbefore TEXT,
    certificate_notafter TEXT,
    registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_communication TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    has_error_last_report BOOLEAN DEFAULT 0,
    state TEXT
);
-- Migration : ajout de la colonne has_error_last_report si besoin
-- N'exécuter que si la version de la db est antérieure à 0.0.3
-- Si la version était < 0.0.3, alors on ajoute la colonne
-- (Attention : SQLite ne supporte pas IF NOT EXISTS sur ALTER TABLE, donc il faut gérer l'erreur côté code ou script)
-- À exécuter manuellement ou via un script Go de migration :
-- ALTER TABLE agents ADD COLUMN has_error_last_report BOOLEAN DEFAULT 0;


-- Table de relation 1-n pour les noms de configuration
CREATE TABLE IF NOT EXISTS agent_configurations (
    agent_id TEXT,
    configuration_name TEXT,
    PRIMARY KEY (agent_id, configuration_name),
    FOREIGN KEY (agent_id) REFERENCES agents(agent_id)
);

CREATE TABLE IF NOT EXISTS agent_ips (
    agent_id TEXT,
    ip_address TEXT,
    PRIMARY KEY (agent_id, ip_address),
    FOREIGN KEY (agent_id) REFERENCES agents(agent_id)
);

-- Table for customizable properties
CREATE TABLE IF NOT EXISTS properties (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    priority INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS node_properties (
    node_id TEXT NOT NULL,
    property_id INTEGER NOT NULL,
    value TEXT,
    PRIMARY KEY (node_id, property_id),
    FOREIGN KEY (property_id) REFERENCES properties(id)
);

CREATE TABLE IF NOT EXISTS configuration_model (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    original_name TEXT,
    previous_id INTEGER,
    upload_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    uploaded_by TEXT NOT NULL,
    mof_file BLOB NOT NULL,
    last_usage TIMESTAMP,
    FOREIGN KEY (previous_id) REFERENCES configuration_model(id)
);


-- Table pour les utilisateurs
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_active BOOLEAN DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_logon_date TIMESTAMP,
    role TEXT DEFAULT 'user',
    source TEXT DEFAULT 'local'
);
