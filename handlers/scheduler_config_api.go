package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go-dsc-pull/internal/buildinfo"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/service"
	"go-dsc-pull/utils"
)

const (
	schedulerTaskReportCleanup         = "report_cleanup"
	schedulerTaskReleaseCheck          = "release_check"
	schedulerTaskLogCleanup            = "log_cleanup"
	schedulerTaskConfigurationRepoSync = "configuration_repo_sync"
)

func schedulerDisplayName(taskName string) string {
	switch taskName {
	case schedulerTaskReportCleanup:
		return "Report cleanup"
	case schedulerTaskReleaseCheck:
		return "Release check"
	case schedulerTaskLogCleanup:
		return "Log rotate"
	case schedulerTaskConfigurationRepoSync:
		return "Configuration repository sync"
	default:
		return taskName
	}
}

func schedulerNextRunFromConfig(taskName string) *time.Time {
	now := time.Now().UTC()
	switch taskName {
	case schedulerTaskReportCleanup:
		if !global.AppConfig.DSCPullServer.EnableReportAutoCleanup {
			return nil
		}
		mins := global.AppConfig.DSCPullServer.ReportCleanupIntervalMins
		if mins <= 0 {
			mins = 1440
		}
		n := now.Add(time.Duration(mins) * time.Minute)
		return &n
	case schedulerTaskReleaseCheck:
		if !global.AppConfig.DSCPullServer.EnableReleaseCheck {
			return nil
		}
		mins := global.AppConfig.DSCPullServer.ReleaseCheckIntervalMins
		if mins <= 0 {
			mins = 1440
		}
		n := now.Add(time.Duration(mins) * time.Minute)
		return &n
	case schedulerTaskLogCleanup:
		if !global.AppConfig.DSCPullServer.EnableLogRotation {
			return nil
		}
		n := now.Add(time.Hour)
		return &n
	case schedulerTaskConfigurationRepoSync:
		database, err := db.OpenDB(&global.AppConfig.Database)
		if err != nil {
			return nil
		}
		defer database.Close()
		settings, err := db.GetConfigurationRepoSettings(database)
		if err != nil || !settings.Enabled {
			return nil
		}
		mins := settings.SyncIntervalMins
		if mins <= 0 {
			mins = 60
		}
		n := now.Add(time.Duration(mins) * time.Minute)
		return &n
	default:
		return nil
	}
}

type SchedulerConfigDTO struct {
	EnableReportAutoCleanup           bool   `json:"enable_report_auto_cleanup"`
	ReportRetentionDays               int    `json:"report_retention_days"`
	ReportCleanupIntervalMins         int    `json:"report_cleanup_interval_mins"`
	EnableReleaseCheck                bool   `json:"enable_release_check"`
	ReleaseCheckIntervalMins          int    `json:"release_check_interval_mins"`
	EnableLogRotation                 bool   `json:"enable_log_rotation"`
	LogRotateMaxSizeMB                int    `json:"log_rotate_max_size_mb"`
	LogRotateMaxBackups               int    `json:"log_rotate_max_backups"`
	LogRotateMaxAgeDays               int    `json:"log_rotate_max_age_days"`
	EnableConfigurationRepoSync       bool   `json:"enable_configuration_repo_sync"`
	ConfigurationRepoSyncIntervalMins int    `json:"configuration_repo_sync_interval_mins"`
	ConfigurationRepoURL              string `json:"configuration_repo_url"`
	ConfigurationRepoBranch           string `json:"configuration_repo_branch"`
	ConfigurationRepoDirectory        string `json:"configuration_repo_directory"`
	ConfigurationCompilerScript       string `json:"configuration_compiler_script"`
	ConfigurationPowerShellExecutable string `json:"configuration_powershell_executable"`
	CompileOnNodeRegistration         bool   `json:"compile_configuration_on_node_registration"`
}

func resolveConfigPath() (string, error) {
	exeDir, err := utils.ExePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exeDir), "config.json"), nil
}

func schedulerActorEmail(r *http.Request) string {
	if r.Context().Value("userId") != nil {
		if sub, ok := r.Context().Value("userId").(string); ok && sub != "" {
			return sub
		}
	}
	return "?"
}

func SchedulerConfigAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		cfg := global.AppConfig
		if cfg == nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to load config"})
			return
		}
		enableLogRotation := cfg.DSCPullServer.EnableLogRotation
		logRotateMaxSizeMB := cfg.DSCPullServer.LogRotateMaxSizeMB
		logRotateMaxBackups := cfg.DSCPullServer.LogRotateMaxBackups
		logRotateMaxAgeDays := cfg.DSCPullServer.LogRotateMaxAgeDays
		if !enableLogRotation && logRotateMaxSizeMB == 0 && logRotateMaxBackups == 0 && logRotateMaxAgeDays == 0 {
			enableLogRotation = true
			logRotateMaxSizeMB = 10
			logRotateMaxBackups = 5
			logRotateMaxAgeDays = 30
		}
		database, err := db.OpenDB(&cfg.Database)
		if err != nil {
			http.Error(w, "DB open error", http.StatusInternalServerError)
			return
		}
		defer database.Close()
		repoSettings, err := db.GetConfigurationRepoSettings(database)
		if err != nil {
			http.Error(w, "Failed to load configuration repository settings", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(SchedulerConfigDTO{
			EnableReportAutoCleanup:           cfg.DSCPullServer.EnableReportAutoCleanup,
			ReportRetentionDays:               cfg.DSCPullServer.ReportRetentionDays,
			ReportCleanupIntervalMins:         cfg.DSCPullServer.ReportCleanupIntervalMins,
			EnableReleaseCheck:                cfg.DSCPullServer.EnableReleaseCheck,
			ReleaseCheckIntervalMins:          cfg.DSCPullServer.ReleaseCheckIntervalMins,
			EnableLogRotation:                 enableLogRotation,
			LogRotateMaxSizeMB:                logRotateMaxSizeMB,
			LogRotateMaxBackups:               logRotateMaxBackups,
			LogRotateMaxAgeDays:               logRotateMaxAgeDays,
			EnableConfigurationRepoSync:       repoSettings.Enabled,
			ConfigurationRepoSyncIntervalMins: repoSettings.SyncIntervalMins,
			ConfigurationRepoURL:              repoSettings.URL,
			ConfigurationRepoBranch:           repoSettings.Branch,
			ConfigurationRepoDirectory:        repoSettings.Directory,
			ConfigurationCompilerScript:       repoSettings.CompilerScript,
			ConfigurationPowerShellExecutable: repoSettings.PowerShellExecutable,
			CompileOnNodeRegistration:         repoSettings.CompileOnNodeRegistration,
		})

	case http.MethodPut:
		var req SchedulerConfigDTO
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
			return
		}
		if req.ReportRetentionDays <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "report_retention_days must be > 0"})
			return
		}
		if req.ReportCleanupIntervalMins <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "report_cleanup_interval_mins must be > 0"})
			return
		}
		if req.ReleaseCheckIntervalMins <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "release_check_interval_mins must be > 0"})
			return
		}
		if req.LogRotateMaxSizeMB <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "log_rotate_max_size_mb must be > 0"})
			return
		}
		if req.LogRotateMaxBackups <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "log_rotate_max_backups must be > 0"})
			return
		}
		if req.LogRotateMaxAgeDays <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "log_rotate_max_age_days must be > 0"})
			return
		}
		if req.EnableConfigurationRepoSync && req.ConfigurationRepoSyncIntervalMins <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "configuration_repo_sync_interval_mins must be > 0"})
			return
		}
		if (req.EnableConfigurationRepoSync || req.CompileOnNodeRegistration) && (req.ConfigurationRepoURL == "" || req.ConfigurationRepoDirectory == "" || req.ConfigurationCompilerScript == "") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "configuration repository URL, directory, and compiler script are required"})
			return
		}

		configPath, err := resolveConfigPath()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to resolve config path"})
			return
		}

		fileData, err := os.ReadFile(configPath)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read config file"})
			return
		}

		var fullConfig map[string]interface{}
		if err := json.Unmarshal(fileData, &fullConfig); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to decode config"})
			return
		}

		dscPullRaw, ok := fullConfig["dsc_pullserver"].(map[string]interface{})
		if !ok {
			dscPullRaw = map[string]interface{}{}
		}
		dscPullRaw["enable_report_auto_cleanup"] = req.EnableReportAutoCleanup
		dscPullRaw["report_retention_days"] = req.ReportRetentionDays
		dscPullRaw["report_cleanup_interval_mins"] = req.ReportCleanupIntervalMins
		dscPullRaw["enable_release_check"] = req.EnableReleaseCheck
		dscPullRaw["release_check_interval_mins"] = req.ReleaseCheckIntervalMins
		dscPullRaw["enable_log_rotation"] = req.EnableLogRotation
		dscPullRaw["log_rotate_max_size_mb"] = req.LogRotateMaxSizeMB
		dscPullRaw["log_rotate_max_backups"] = req.LogRotateMaxBackups
		dscPullRaw["log_rotate_max_age_days"] = req.LogRotateMaxAgeDays
		fullConfig["dsc_pullserver"] = dscPullRaw

		data, err := json.MarshalIndent(fullConfig, "", "  ")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to marshal config"})
			return
		}
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to write config"})
			return
		}
		database, err := db.OpenDB(&global.AppConfig.Database)
		if err != nil {
			http.Error(w, "DB open error", http.StatusInternalServerError)
			return
		}
		defer database.Close()
		if err := db.SaveConfigurationRepoSettings(database, global.AppConfig.Database.Driver, db.ConfigurationRepoSettings{
			Enabled:                   req.EnableConfigurationRepoSync,
			URL:                       req.ConfigurationRepoURL,
			Branch:                    req.ConfigurationRepoBranch,
			Directory:                 req.ConfigurationRepoDirectory,
			CompilerScript:            req.ConfigurationCompilerScript,
			PowerShellExecutable:      req.ConfigurationPowerShellExecutable,
			SyncIntervalMins:          req.ConfigurationRepoSyncIntervalMins,
			CompileOnNodeRegistration: req.CompileOnNodeRegistration,
			LegacyImported:            true,
		}); err != nil {
			http.Error(w, "Failed to save configuration repository settings", http.StatusInternalServerError)
			return
		}

		// Hot-reload in-memory values for immediate API visibility.
		global.AppConfig.DSCPullServer.EnableReportAutoCleanup = req.EnableReportAutoCleanup
		global.AppConfig.DSCPullServer.ReportRetentionDays = req.ReportRetentionDays
		global.AppConfig.DSCPullServer.ReportCleanupIntervalMins = req.ReportCleanupIntervalMins
		global.AppConfig.DSCPullServer.EnableReleaseCheck = req.EnableReleaseCheck
		global.AppConfig.DSCPullServer.ReleaseCheckIntervalMins = req.ReleaseCheckIntervalMins
		global.AppConfig.DSCPullServer.EnableLogRotation = req.EnableLogRotation
		global.AppConfig.DSCPullServer.LogRotateMaxSizeMB = req.LogRotateMaxSizeMB
		global.AppConfig.DSCPullServer.LogRotateMaxBackups = req.LogRotateMaxBackups
		global.AppConfig.DSCPullServer.LogRotateMaxAgeDays = req.LogRotateMaxAgeDays

		if auditDB, auditErr := db.OpenDB(&global.AppConfig.Database); auditErr == nil {
			driverName := global.AppConfig.Database.Driver
			details := fmt.Sprintf(
				"Updated scheduler config: report_cleanup(enable=%t, retention_days=%d, interval_mins=%d), release_check(enable=%t, interval_mins=%d), log_rotation(enable=%t, max_size_mb=%d, max_backups=%d, max_age_days=%d), configuration_repo_sync(enable=%t, interval_mins=%d)",
				req.EnableReportAutoCleanup,
				req.ReportRetentionDays,
				req.ReportCleanupIntervalMins,
				req.EnableReleaseCheck,
				req.ReleaseCheckIntervalMins,
				req.EnableLogRotation,
				req.LogRotateMaxSizeMB,
				req.LogRotateMaxBackups,
				req.LogRotateMaxAgeDays,
				req.EnableConfigurationRepoSync,
				req.ConfigurationRepoSyncIntervalMins,
			)
			_ = db.InsertAudit(auditDB, driverName, schedulerActorEmail(r), "update", "scheduler_config", details, r.RemoteAddr)
			auditDB.Close()
		}

		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func SchedulerRunCleanupAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	database, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer database.Close()

	retentionDays := global.AppConfig.DSCPullServer.ReportRetentionDays
	if retentionDays <= 0 {
		http.Error(w, "Invalid report retention configuration", http.StatusBadRequest)
		return
	}

	startedAt := time.Now().UTC()
	nextRun := schedulerNextRunFromConfig(schedulerTaskReportCleanup)
	_ = db.BeginSchedulerTaskRun(database, global.AppConfig.Database.Driver, schedulerTaskReportCleanup, schedulerDisplayName(schedulerTaskReportCleanup), "manual", startedAt, nextRun)

	deleted, err := db.CleanupOldReports(database, global.AppConfig.Database.Driver, retentionDays)
	if err != nil {
		_ = db.InsertAudit(database, global.AppConfig.Database.Driver, schedulerActorEmail(r), "run", "scheduler_task", fmt.Sprintf("Manual run failed: %s, error=%s", schedulerTaskReportCleanup, err.Error()), r.RemoteAddr)
		_ = db.CompleteSchedulerTaskRun(database, global.AppConfig.Database.Driver, schedulerTaskReportCleanup, "error", err.Error(), time.Now().UTC(), nextRun)
		http.Error(w, "Cleanup failed", http.StatusInternalServerError)
		return
	}

	logs.WriteLogFile("INFO [REPORT CLEANUP] Manual cleanup triggered from admin")
	_ = db.InsertAudit(database, global.AppConfig.Database.Driver, schedulerActorEmail(r), "run", "scheduler_task", fmt.Sprintf("Manual run: %s, deleted=%d", schedulerTaskReportCleanup, deleted), r.RemoteAddr)
	_ = db.CompleteSchedulerTaskRun(database, global.AppConfig.Database.Driver, schedulerTaskReportCleanup, "success", fmt.Sprintf("Deleted %d old report(s)", deleted), time.Now().UTC(), nextRun)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int64{"deleted": deleted})
}

func SchedulerConfigFromApp(cfg *schema.AppConfig) SchedulerConfigDTO {
	enableLogRotation := cfg.DSCPullServer.EnableLogRotation
	logRotateMaxSizeMB := cfg.DSCPullServer.LogRotateMaxSizeMB
	logRotateMaxBackups := cfg.DSCPullServer.LogRotateMaxBackups
	logRotateMaxAgeDays := cfg.DSCPullServer.LogRotateMaxAgeDays
	if !enableLogRotation && logRotateMaxSizeMB == 0 && logRotateMaxBackups == 0 && logRotateMaxAgeDays == 0 {
		enableLogRotation = true
		logRotateMaxSizeMB = 10
		logRotateMaxBackups = 5
		logRotateMaxAgeDays = 30
	}

	return SchedulerConfigDTO{
		EnableReportAutoCleanup:   cfg.DSCPullServer.EnableReportAutoCleanup,
		ReportRetentionDays:       cfg.DSCPullServer.ReportRetentionDays,
		ReportCleanupIntervalMins: cfg.DSCPullServer.ReportCleanupIntervalMins,
		EnableReleaseCheck:        cfg.DSCPullServer.EnableReleaseCheck,
		ReleaseCheckIntervalMins:  cfg.DSCPullServer.ReleaseCheckIntervalMins,
		EnableLogRotation:         enableLogRotation,
		LogRotateMaxSizeMB:        logRotateMaxSizeMB,
		LogRotateMaxBackups:       logRotateMaxBackups,
		LogRotateMaxAgeDays:       logRotateMaxAgeDays,
	}
}

func SchedulerRunReleaseCheckAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := service.CheckLatestRelease(buildinfo.Version)
	driverName := "sqlite"
	if global.AppConfig != nil && global.AppConfig.Database.Driver != "" {
		driverName = global.AppConfig.Database.Driver
	}
	startedAt := time.Now().UTC()
	nextRun := schedulerNextRunFromConfig(schedulerTaskReleaseCheck)
	database, dbErr := db.OpenDB(&global.AppConfig.Database)
	if dbErr == nil {
		defer database.Close()
		_ = db.BeginSchedulerTaskRun(database, driverName, schedulerTaskReleaseCheck, schedulerDisplayName(schedulerTaskReleaseCheck), "manual", startedAt, nextRun)
	}
	if err != nil {
		http.Error(w, "Release check failed", http.StatusInternalServerError)
		logs.WriteLogFile("WARN [RELEASE CHECK] Manual release check failed: " + err.Error())
		if dbErr == nil {
			_ = db.InsertAudit(database, driverName, schedulerActorEmail(r), "run", "scheduler_task", fmt.Sprintf("Manual run failed: %s, error=%s", schedulerTaskReleaseCheck, err.Error()), r.RemoteAddr)
			_ = db.CompleteSchedulerTaskRun(database, driverName, schedulerTaskReleaseCheck, "error", err.Error(), time.Now().UTC(), nextRun)
		}
		if dbErr == nil {
			_ = db.PersistReleaseCheckFailure(database, driverName)
		}
		return
	}
	if dbErr == nil {
		_ = db.PersistReleaseCheckSuccess(database, driverName, result.LatestRelease, result.LatestReleaseURL, result.UpdateAvailable)
		msg := "Already up to date"
		if result.UpdateAvailable {
			msg = fmt.Sprintf("Update available: %s", result.LatestRelease)
		}
		_ = db.CompleteSchedulerTaskRun(database, driverName, schedulerTaskReleaseCheck, "success", msg, time.Now().UTC(), nextRun)
	}

	if result.UpdateAvailable {
		logs.WriteLogFile("INFO [RELEASE CHECK] Manual check: update available latest=" + result.LatestRelease + ", current=" + result.CurrentVersion)
	} else {
		logs.WriteLogFile("INFO [RELEASE CHECK] Manual check: already up to date current=" + result.CurrentVersion)
	}
	if dbErr == nil {
		_ = db.InsertAudit(database, driverName, schedulerActorEmail(r), "run", "scheduler_task", fmt.Sprintf("Manual run: %s, update_available=%t, latest=%s", schedulerTaskReleaseCheck, result.UpdateAvailable, result.LatestRelease), r.RemoteAddr)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"current_version":    result.CurrentVersion,
		"latest_release":     result.LatestRelease,
		"latest_release_url": result.LatestReleaseURL,
		"update_available":   result.UpdateAvailable,
	})
}

func SchedulerRunConfigurationRepoAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	database, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer database.Close()
	repoSettings, err := db.GetConfigurationRepoSettings(database)
	if err != nil {
		http.Error(w, "Failed to load configuration repository settings", http.StatusInternalServerError)
		return
	}
	if repoSettings.URL == "" || repoSettings.Directory == "" || repoSettings.CompilerScript == "" {
		http.Error(w, "Configuration repository is incomplete", http.StatusBadRequest)
		return
	}
	startedAt := time.Now().UTC()
	nextRun := schedulerNextRunFromConfig(schedulerTaskConfigurationRepoSync)
	driver := global.AppConfig.Database.Driver
	_ = db.BeginSchedulerTaskRun(database, driver, schedulerTaskConfigurationRepoSync, schedulerDisplayName(schedulerTaskConfigurationRepoSync), "manual", startedAt, nextRun)
	config := service.ConfigurationRepoConfig{URL: repoSettings.URL, Branch: repoSettings.Branch, Directory: repoSettings.Directory, CompilerScript: repoSettings.CompilerScript, PowerShellExecutable: repoSettings.PowerShellExecutable}
	contexts, nodeAgentIDs, err := db.ListNodeCompilationContextsWithAgentIDs(database)
	if err != nil {
		http.Error(w, "Failed to load node compilation contexts", http.StatusInternalServerError)
		return
	}
	config.NodeContexts = contexts
	changed, err := service.RunConfigurationRepoSync(config, func(compiled []service.CompiledConfiguration) (int, error) {
		return db.ImportCompiledConfigurationsAndAssign(database, compiled, nodeAgentIDs)
	})
	if err != nil {
		_ = db.InsertAudit(database, driver, schedulerActorEmail(r), "run", "scheduler_task", fmt.Sprintf("Manual run failed: %s, error=%s", schedulerTaskConfigurationRepoSync, err.Error()), r.RemoteAddr)
		_ = db.CompleteSchedulerTaskRun(database, driver, schedulerTaskConfigurationRepoSync, "error", err.Error(), time.Now().UTC(), nextRun)
		http.Error(w, "Configuration repository sync failed", http.StatusInternalServerError)
		return
	}
	message := fmt.Sprintf("Updated configurations: %d", changed)
	_ = db.InsertAudit(database, driver, schedulerActorEmail(r), "run", "scheduler_task", "Manual run: "+schedulerTaskConfigurationRepoSync+", "+message, r.RemoteAddr)
	_ = db.CompleteSchedulerTaskRun(database, driver, schedulerTaskConfigurationRepoSync, "success", message, time.Now().UTC(), nextRun)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"updated": changed})
}

func SchedulerRunLogCleanupAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	startedAt := time.Now().UTC()
	nextRun := schedulerNextRunFromConfig(schedulerTaskLogCleanup)
	driverName := global.AppConfig.Database.Driver
	database, dbErr := db.OpenDB(&global.AppConfig.Database)
	if dbErr == nil {
		_ = db.BeginSchedulerTaskRun(database, driverName, schedulerTaskLogCleanup, schedulerDisplayName(schedulerTaskLogCleanup), "manual", startedAt, nextRun)
	}

	deleted, err := logs.RunLogBackupCleanupNow()
	if err != nil {
		if dbErr == nil {
			_ = db.InsertAudit(database, driverName, schedulerActorEmail(r), "run", "scheduler_task", fmt.Sprintf("Manual run failed: %s, error=%s", schedulerTaskLogCleanup, err.Error()), r.RemoteAddr)
			_ = db.CompleteSchedulerTaskRun(database, driverName, schedulerTaskLogCleanup, "error", err.Error(), time.Now().UTC(), nextRun)
			database.Close()
		}
		http.Error(w, "Log cleanup failed", http.StatusInternalServerError)
		return
	}

	_ = logs.WriteLogFile("INFO [LOG ROTATION] Manual backup cleanup triggered from admin")
	if dbErr == nil {
		_ = db.InsertAudit(database, driverName, schedulerActorEmail(r), "run", "scheduler_task", fmt.Sprintf("Manual run: %s, deleted=%d", schedulerTaskLogCleanup, deleted), r.RemoteAddr)
	}
	if dbErr == nil {
		_ = db.CompleteSchedulerTaskRun(database, driverName, schedulerTaskLogCleanup, "success", fmt.Sprintf("Deleted %d backup(s)", deleted), time.Now().UTC(), nextRun)
		database.Close()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"deleted": deleted})
}

func SchedulerTasksAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	database, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer database.Close()

	driver := global.AppConfig.Database.Driver
	_ = db.UpsertSchedulerTask(database, driver, schedulerTaskReportCleanup, schedulerDisplayName(schedulerTaskReportCleanup), schedulerNextRunFromConfig(schedulerTaskReportCleanup))
	_ = db.UpsertSchedulerTask(database, driver, schedulerTaskReleaseCheck, schedulerDisplayName(schedulerTaskReleaseCheck), schedulerNextRunFromConfig(schedulerTaskReleaseCheck))
	_ = db.UpsertSchedulerTask(database, driver, schedulerTaskLogCleanup, schedulerDisplayName(schedulerTaskLogCleanup), schedulerNextRunFromConfig(schedulerTaskLogCleanup))

	tasks, err := db.ListSchedulerTasks(database)
	if err != nil {
		http.Error(w, "Failed to list tasks", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tasks)
}

func SchedulerTaskHistoryAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	taskName := r.PathValue("task")
	if taskName == "" {
		http.Error(w, "Missing task", http.StatusBadRequest)
		return
	}
	database, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer database.Close()

	limit := 20
	offset := 0
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsed <= 0 {
			http.Error(w, "Invalid limit", http.StatusBadRequest)
			return
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	}
	if rawOffset := r.URL.Query().Get("offset"); rawOffset != "" {
		parsed, parseErr := strconv.Atoi(rawOffset)
		if parseErr != nil || parsed < 0 {
			http.Error(w, "Invalid offset", http.StatusBadRequest)
			return
		}
		offset = parsed
	}

	historyWithProbe, err := db.ListSchedulerTaskRunsPage(database, global.AppConfig.Database.Driver, taskName, offset, limit+1)
	if err != nil {
		http.Error(w, "Failed to load history", http.StatusInternalServerError)
		return
	}
	hasMore := len(historyWithProbe) > limit
	history := historyWithProbe
	if hasMore {
		history = historyWithProbe[:limit]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"items":    history,
		"limit":    limit,
		"offset":   offset,
		"has_more": hasMore,
	})
}
