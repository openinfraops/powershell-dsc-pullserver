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
	LegacyConfigurationRepoUsername         string `json:"configuration_repo_username"`
	LegacyConfigurationRepoPassword         string `json:"configuration_repo_password"`
	LegacyConfigurationRepoSyncIntervalMins int    `json:"configuration_repo_sync_interval_mins"`
	// MetricsToken est un jeton optionnel qui protège le endpoint GET /metrics
	// (format texte Prometheus). S'il est défini, une requête doit présenter ce
	// jeton via un en-tête "Authorization: Bearer <token>" ou un paramètre
	// "?token=<token>". Le endpoint /metrics accepte aussi un token API
	// utilisateur valide (le même que /api/v1/*, "Authorization: Token
	// <token>"), même lorsque MetricsToken est vide. Le endpoint /health reste,
	// lui, non authentifié.
	MetricsToken string `json:"metrics_token"`
	// TrustedProxies liste les adresses IP ou CIDR des reverse-proxies de
	// confiance. Si le client direct appartient à cette liste, les en-têtes
	// X-Forwarded-For / X-Real-IP sont honorés pour déduire l'IP réelle d'un
	// client ; sinon ces en-têtes sont ignorés (anti-contournement du
	// rate-limit par spoofing).
	TrustedProxies                          []string `json:"trusted_proxies"`
	EnableAgentPurge                        bool     `json:"enable_agent_purge"`
	AgentPurgeRetentionDays                 int      `json:"agent_purge_retention_days"`
	AgentPurgeIntervalMins                  int      `json:"agent_purge_interval_mins"`
	EnableConfigurationVersionCleanup       bool     `json:"enable_configuration_version_cleanup"`
	ConfigurationVersionMaxKeep             int      `json:"configuration_version_max_keep"`
	ConfigurationVersionCleanupIntervalMins int      `json:"configuration_version_cleanup_interval_mins"`
	// RebootLoopThreshold est le nombre N de rapports consécutifs demandant un
	// redémarrage au-delà duquel un nœud est marqué en "boucle de redémarrage"
	// (reboot_loop). 0/absent -> défaut 3.
	RebootLoopThreshold int `json:"reboot_loop_threshold"`
	// RebootLoopWindowMins est la fenêtre minimale (en minutes) sur laquelle les
	// N rapports doivent se produire pour déclencher la détection de boucle.
	// 0/absent -> défaut 60.
	RebootLoopWindowMins int `json:"reboot_loop_window_mins"`
	// EnableErrorDetection active la tâche périodique de détection d'erreurs
	// (spike, nouvelle erreur, erreur après déploiement).
	EnableErrorDetection bool `json:"enable_error_detection"`
	// ErrorDetectionIntervalMins est la fréquence (en minutes) de la tâche de
	// détection. 0/absent -> défaut 5.
	ErrorDetectionIntervalMins int `json:"error_detection_interval_mins"`
	// ErrorSpikeMinNodes est le nombre N minimal de nœuds touchés sur la dernière
	// heure pour déclencher un « spike ». 0/absent -> défaut 5.
	ErrorSpikeMinNodes int `json:"error_spike_min_nodes"`
	// ErrorSpikePercent est le pourcentage X des nœuds actifs qui, s'il donne un
	// seuil supérieur à N, remplace N. 0/absent -> défaut 10.
	ErrorSpikePercent int `json:"error_spike_percent"`
	// ErrorNewWindowHours est la fenêtre (en heures) sous laquelle une première
	// apparition est considérée comme une « nouvelle erreur ». 0 -> défaut 24.
	ErrorNewWindowHours int `json:"error_new_window_hours"`
	// ErrorDeploymentWindowMins est la fenêtre (en minutes) après la publication
	// de la dernière configuration dans laquelle une erreur est considérée comme
	// « apparue après déploiement ». 0 -> défaut 60.
	ErrorDeploymentWindowMins int `json:"error_deployment_window_mins"`
	// ErrorFingerprintDailyLimit borne le nombre d'empreintes distinctes qu'un
	// nœud peut créer par jour. 0/absent -> défaut 1000.
	ErrorFingerprintDailyLimit int `json:"error_fingerprint_daily_limit"`
	// ErrorCategories est la liste ordonnée (premier match gagnant) des règles de
	// catégorisation. Absente -> règles par défaut embarquées.
	ErrorCategories []CategoryRule `json:"error_categories"`
	// ErrorDetectionRepeatMinutes est le délai minimal entre deux alertes pour une
	// même empreinte. 0 -> aucune déduplication temporelle.
	ErrorDetectionRepeatMinutes int `json:"error_detection_repeat_minutes"`
	// ErrorDetectionGraceMinutes est la période de grâce après le démarrage d'un
	// nœud pendant laquelle une nouvelle erreur n'est pas alertée. 0 -> défaut 5.
	ErrorDetectionGraceMinutes int `json:"error_detection_grace_minutes"`
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
