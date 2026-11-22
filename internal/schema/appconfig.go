package schema

type HTTPConfig struct {
	Port                                    int    `json:"port"`
	EnableHTTPS                             bool   `json:"enable_https"`
	CertFile                                string `json:"cert_file"`
	KeyFile                                 string `json:"key_file"`
	EnableClientCertValidation              bool   `json:"enable_client_cert_validation"`
	BypassCAValidation                      bool   `json:"bypass_ca_validation"`
	RegistrationKey                         string `json:"registrationKey"`
	SharedAccessSecret                      string `json:"shared_secret"`
	EnableReportAutoCleanup                 bool   `json:"enable_report_auto_cleanup"`
	ReportRetentionDays                     int    `json:"report_retention_days"`
	ReportCleanupIntervalMins               int    `json:"report_cleanup_interval_mins"`
	EnableReleaseCheck                      bool   `json:"enable_release_check"`
	ReleaseCheckIntervalMins                int    `json:"release_check_interval_mins"`
	EnableLogRotation                       bool   `json:"enable_log_rotation"`
	LogRotateMaxSizeMB                      int    `json:"log_rotate_max_size_mb"`
	LogRotateMaxBackups                     int    `json:"log_rotate_max_backups"`
	LogRotateMaxAgeDays                     int    `json:"log_rotate_max_age_days"`
	LegacyConfigurationRepoEnabled          bool   `json:"enable_configuration_repo_sync"`
	LegacyConfigurationRepoURL              string `json:"configuration_repo_url"`
	LegacyConfigurationRepoBranch           string `json:"configuration_repo_branch"`
	LegacyConfigurationRepoDirectory        string `json:"configuration_repo_directory"`
	LegacyConfigurationCompilerScript       string `json:"configuration_compiler_script"`
	LegacyConfigurationPowerShellExecutable string `json:"configuration_powershell_executable"`
	LegacyConfigurationRepoUsername         string   `json:"configuration_repo_username"`
	LegacyConfigurationRepoPassword         string   `json:"configuration_repo_password"`
	LegacyConfigurationRepoSyncIntervalMins int      `json:"configuration_repo_sync_interval_mins"`
	// TrustedProxies liste les adresses IP ou CIDR des reverse-proxies de
	// confiance. Si le client direct appartient à cette liste, les en-têtes
	// X-Forwarded-For / X-Real-IP sont honorés pour déduire l'IP réelle d'un
	// client ; sinon ces en-têtes sont ignorés (anti-contournement du
	// rate-limit par spoofing).
	TrustedProxies []string `json:"trusted_proxies"`
	EnableAgentPurge                        bool   `json:"enable_agent_purge"`
	AgentPurgeRetentionDays                 int    `json:"agent_purge_retention_days"`
	AgentPurgeIntervalMins                  int    `json:"agent_purge_interval_mins"`
	EnableConfigurationVersionCleanup       bool   `json:"enable_configuration_version_cleanup"`
	ConfigurationVersionMaxKeep             int    `json:"configuration_version_max_keep"`
	ConfigurationVersionCleanupIntervalMins int    `json:"configuration_version_cleanup_interval_mins"`
}

type AppConfig struct {
	Driver        string     `json:"driver"`
	Server        string     `json:"server"`
	Port          int        `json:"port"`
	User          string     `json:"user"`
	Password      string     `json:"password"`
	Database      DBConfig   `json:"database"`
	DSCPullServer HTTPConfig `json:"dsc_pullserver"`
	WebUI         HTTPConfig `json:"web_ui"`
	SAML          SAMLConfig `json:"saml"`
}

type DBConfig struct {
	Driver   string `json:"driver"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"name"`
}

type SAMLConfig struct {
	Enabled        bool                 `json:"enabled"`
	EntityID       string               `json:"entity_id"`
	IdpMetadataURL string               `json:"idp_metadata_url"`
	SPKeyFile      string               `json:"sp_key_file"`
	SPCertFile     string               `json:"sp_cert_file"`
	UserMapping    SAMLAttributeMapping `json:"user_mapping"`
	GroupMapping   SAMLGroupMapping     `json:"group_mapping"`
}

type SAMLAttributeMapping struct {
	Email     string `json:"email"`
	Sn        string `json:"sn"`
	GivenName string `json:"givenname"`
}

type SAMLGroupMapping struct {
	Attribute  string `json:"attribute"`
	AdminValue string `json:"admin_value"`
	UserValue  string `json:"user_value"`
}

type SAMLUserMapping map[string]string
