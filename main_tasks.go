package main

import (
	"database/sql"
	"fmt"
	"time"

	"go-dsc-pull/internal/buildinfo"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/service"
)

// Clés canoniques des tâches planifiées (identifient les lignes scheduler_tasks).
const (
	taskReportCleanup               = "report_cleanup"
	taskReleaseCheck                = "release_check"
	taskLogCleanup                  = "log_cleanup"
	taskConfigurationRepoSync       = "configuration_repo_sync"
	taskAgentPurge                  = "agent_purge"
	taskConfigurationVersionCleanup = "configuration_version_cleanup"
	taskErrorDetection              = "error_detection"
	taskIndexMaintenance            = "index_maintenance"
)

// logMaintenanceInterval est l'intervalle de la rotation des logs (non
// configurable dans config.json).
const logMaintenanceInterval = time.Hour

// intervalFromMins convertit un intervalle configuré en minutes en duration,
// en retombant sur def lorsque la valeur est <= 0.
func intervalFromMins(mins, def int) time.Duration {
	if mins <= 0 {
		mins = def
	}
	return time.Duration(mins) * time.Minute
}

// schedulerRecorder adapte les fonctions d'historique du scheduler (internal/db)
// à l'interface service.TaskRecorder utilisée par le registre de tâches, sans
// créer de cycle d'import entre internal/service et internal/db.
type schedulerRecorder struct {
	dbConn *sql.DB
	driver string
}

func (r schedulerRecorder) UpsertTask(taskName, displayName string, nextRunAt *time.Time) error {
	return db.UpsertSchedulerTask(r.dbConn, r.driver, taskName, displayName, nextRunAt)
}

func (r schedulerRecorder) BeginRun(taskName, displayName, triggerSource string, startedAt time.Time, nextRunAt *time.Time) error {
	return db.BeginSchedulerTaskRun(r.dbConn, r.driver, taskName, displayName, triggerSource, startedAt, nextRunAt)
}

func (r schedulerRecorder) CompleteRun(taskName, status, message string, finishedAt time.Time, nextRunAt *time.Time) error {
	return db.CompleteSchedulerTaskRun(r.dbConn, r.driver, taskName, status, message, finishedAt, nextRunAt)
}

// buildScheduledTasks construit la liste des tâches planifiées à partir de la
// configuration courante. Les tâches désactivées (ou mal configurées) sont
// incluses mais marquées désactivées : elles restent enregistrées dans
// scheduler_tasks (la page Scheduler les affiche comme inactives) mais ne
// tournent pas.
func buildScheduledTasks(dbConn *sql.DB, repoSettings db.ConfigurationRepoSettings) []service.ScheduledTask {
	cfg := global.AppConfig.DSCPullServer
	return []service.ScheduledTask{
		reportCleanupTask(cfg),
		agentPurgeTask(cfg),
		configurationVersionCleanupTask(cfg),
		errorDetectionTask(cfg),
		releaseCheckTask(cfg),
		logCleanupTask(cfg),
		indexMaintenanceTask(cfg),
		configurationRepoSyncTask(repoSettings),
	}
}

// reportCleanupTask supprime les rapports plus vieux que la rétention et purge
// les occurrences d'erreur correspondantes.
func reportCleanupTask(cfg schema.HTTPConfig) service.ScheduledTask {
	enabled := cfg.EnableReportAutoCleanup
	interval := intervalFromMins(cfg.ReportCleanupIntervalMins, 1440)
	retentionDays := cfg.ReportRetentionDays
	if enabled && retentionDays <= 0 {
		logs.WriteLogFile("WARN [REPORT CLEANUP] Disabled: report_retention_days must be > 0")
		enabled = false
	} else if enabled {
		logs.WriteLogFile(fmt.Sprintf("INFO [REPORT CLEANUP] Enabled: retention=%d day(s), interval=%d minute(s)", retentionDays, interval/time.Minute))
	}
	return service.ScheduledTask{
		Key:         taskReportCleanup,
		DisplayName: "Report cleanup",
		Enabled:     enabled,
		Interval:    interval,
		Run: func(dbConn *sql.DB, driver string) (string, error) {
			deleted, err := db.CleanupOldReports(dbConn, driver, retentionDays)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [REPORT CLEANUP] %v", err))
				return "", err
			}
			occPurged, occErr := db.PurgeErrorOccurrences(dbConn, driver, retentionDays)
			if occErr != nil {
				logs.WriteLogFile(fmt.Sprintf("WARN [REPORT CLEANUP] Error purging error occurrences: %v", occErr))
			}
			msg := fmt.Sprintf("Deleted %d old report(s), %d stale error occurrence(s)", deleted, occPurged)
			logs.WriteLogFile(fmt.Sprintf("INFO [REPORT CLEANUP] %s", msg))
			return msg, nil
		},
	}
}

// agentPurgeTask supprime définitivement les agents en corbeille depuis plus que
// la rétention, ainsi que leurs occurrences d'erreur éphémères.
func agentPurgeTask(cfg schema.HTTPConfig) service.ScheduledTask {
	enabled := cfg.EnableAgentPurge
	interval := intervalFromMins(cfg.AgentPurgeIntervalMins, 1440)
	retentionDays := cfg.AgentPurgeRetentionDays
	if enabled && retentionDays <= 0 {
		logs.WriteLogFile("WARN [AGENT PURGE] Disabled: agent_purge_retention_days must be > 0")
		enabled = false
	} else if enabled {
		logs.WriteLogFile(fmt.Sprintf("INFO [AGENT PURGE] Enabled: retention=%d day(s), interval=%d minute(s)", retentionDays, interval/time.Minute))
	}
	return service.ScheduledTask{
		Key:         taskAgentPurge,
		DisplayName: "Agent trash purge",
		Enabled:     enabled,
		Interval:    interval,
		Run: func(dbConn *sql.DB, driver string) (string, error) {
			purged, err := db.PurgeDeletedAgents(dbConn, driver, retentionDays)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [AGENT PURGE] %v", err))
				return "", err
			}
			occPurged, occErr := db.PurgeEphemeralErrorOccurrences(dbConn)
			if occErr != nil {
				logs.WriteLogFile(fmt.Sprintf("WARN [AGENT PURGE] Error purging ephemeral error occurrences: %v", occErr))
			}
			msg := fmt.Sprintf("Purged %d trashed agent(s), %d ephemeral error occurrence(s)", purged, occPurged)
			logs.WriteLogFile(fmt.Sprintf("INFO [AGENT PURGE] %s", msg))
			return msg, nil
		},
	}
}

// configurationVersionCleanupTask supprime les versions historiques de
// configuration au-delà du plafond configuré.
func configurationVersionCleanupTask(cfg schema.HTTPConfig) service.ScheduledTask {
	enabled := cfg.EnableConfigurationVersionCleanup
	interval := intervalFromMins(cfg.ConfigurationVersionCleanupIntervalMins, 1440)
	maxKeep := cfg.ConfigurationVersionMaxKeep
	if enabled && maxKeep < 1 {
		logs.WriteLogFile("WARN [CONFIG VERSION CLEANUP] Disabled: configuration_version_max_keep must be >= 1")
		enabled = false
	} else if enabled {
		logs.WriteLogFile(fmt.Sprintf("INFO [CONFIG VERSION CLEANUP] Enabled: max_keep=%d version(s), interval=%d minute(s)", maxKeep, interval/time.Minute))
	}
	return service.ScheduledTask{
		Key:         taskConfigurationVersionCleanup,
		DisplayName: "Configuration version retention",
		Enabled:     enabled,
		Interval:    interval,
		Run: func(dbConn *sql.DB, driver string) (string, error) {
			pruned, err := db.PruneOldConfigurationVersions(dbConn, maxKeep)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIG VERSION CLEANUP] %v", err))
				return "", err
			}
			msg := fmt.Sprintf("Pruned %d old configuration version(s)", pruned)
			logs.WriteLogFile(fmt.Sprintf("INFO [CONFIG VERSION CLEANUP] %s", msg))
			return msg, nil
		},
	}
}

// errorDetectionTask évalue les groupes d'erreurs DSC (spike / nouvelle erreur /
// erreur après déploiement) et alerte sur les groupes détectés.
func errorDetectionTask(cfg schema.HTTPConfig) service.ScheduledTask {
	enabled := cfg.EnableErrorDetection
	interval := intervalFromMins(cfg.ErrorDetectionIntervalMins, 5)
	detParams := db.ErrorDetectionParams{
		Now:              time.Now().UTC(),
		SpikeMinNodes:    5,
		SpikePercent:     10,
		NewWindow:        24 * time.Hour,
		DeploymentWindow: 60 * time.Minute,
		GracePeriod:      5 * time.Minute,
	}
	if cfg.ErrorSpikeMinNodes > 0 {
		detParams.SpikeMinNodes = cfg.ErrorSpikeMinNodes
	}
	if cfg.ErrorSpikePercent > 0 {
		detParams.SpikePercent = float64(cfg.ErrorSpikePercent)
	}
	if cfg.ErrorNewWindowHours > 0 {
		detParams.NewWindow = time.Duration(cfg.ErrorNewWindowHours) * time.Hour
	}
	if cfg.ErrorDeploymentWindowMins > 0 {
		detParams.DeploymentWindow = time.Duration(cfg.ErrorDeploymentWindowMins) * time.Minute
	}
	if cfg.ErrorDetectionRepeatMinutes > 0 {
		detParams.RepeatDelay = time.Duration(cfg.ErrorDetectionRepeatMinutes) * time.Minute
	}
	if cfg.ErrorDetectionGraceMinutes > 0 {
		detParams.GracePeriod = time.Duration(cfg.ErrorDetectionGraceMinutes) * time.Minute
	}
	if enabled {
		logs.WriteLogFile(fmt.Sprintf("INFO [ERROR DETECTION] Enabled: interval=%d minute(s), spike=%d node(s)/%d%%, new=%dh, deploy=%dm", interval/time.Minute, detParams.SpikeMinNodes, int(detParams.SpikePercent), cfg.ErrorNewWindowHours, cfg.ErrorDeploymentWindowMins))
	}
	return service.ScheduledTask{
		Key:         taskErrorDetection,
		DisplayName: "Error detection",
		Enabled:     enabled,
		Interval:    interval,
		Run: func(dbConn *sql.DB, driver string) (string, error) {
			results, err := db.RunErrorDetection(dbConn, detParams)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [ERROR DETECTION] %v", err))
				return "", err
			}
			alerted := 0
			for _, r := range results {
				if r.Alerted {
					alerted++
					logs.WriteLogFile("ALERT [ERROR DETECTION] " + r.Message)
				}
			}
			msg := fmt.Sprintf("Evaluated error groups: %d alert(s)", alerted)
			logs.WriteLogFile(fmt.Sprintf("INFO [ERROR DETECTION] %s", msg))
			return msg, nil
		},
	}
}

// releaseCheckTask interroge GitHub pour détecter une nouvelle version.
func releaseCheckTask(cfg schema.HTTPConfig) service.ScheduledTask {
	enabled := cfg.EnableReleaseCheck
	interval := intervalFromMins(cfg.ReleaseCheckIntervalMins, 1440)
	if enabled {
		logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Enabled: interval=%d minute(s)", interval/time.Minute))
	}
	return service.ScheduledTask{
		Key:         taskReleaseCheck,
		DisplayName: "Release check",
		Enabled:     enabled,
		Interval:    interval,
		Run: func(dbConn *sql.DB, driver string) (string, error) {
			result, err := service.CheckLatestRelease(buildinfo.Version)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("WARN [RELEASE CHECK] %v", err))
				if persistErr := db.PersistReleaseCheckFailure(dbConn, driver); persistErr != nil {
					logs.WriteLogFile(fmt.Sprintf("WARN [RELEASE CHECK] Failed to persist failure: %v", persistErr))
				}
				return "", err
			}
			if persistErr := db.PersistReleaseCheckSuccess(dbConn, driver, result.LatestRelease, result.LatestReleaseURL, result.UpdateAvailable); persistErr != nil {
				logs.WriteLogFile(fmt.Sprintf("WARN [RELEASE CHECK] Failed to persist result: %v", persistErr))
			}
			if result.UpdateAvailable {
				logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Update available: current=%s latest=%s", result.CurrentVersion, result.LatestRelease))
				return fmt.Sprintf("Update available: %s", result.LatestRelease), nil
			}
			logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Up to date: current=%s", result.CurrentVersion))
			return "Already up to date", nil
		},
	}
}

// indexMaintenanceTask optimise les index (et, pour SQLite, compacte
// optionnellement le fichier via VACUUM). Le comportement est adapté au driver :
// SQLite utilise PRAGMA optimize + REINDEX (VACUUM optionnel), MSSQL un
// ALTER INDEX ... REORGANIZE en ligne. Opt-in : désactivée par défaut.
func indexMaintenanceTask(cfg schema.HTTPConfig) service.ScheduledTask {
	enabled := cfg.EnableIndexMaintenance
	interval := intervalFromMins(cfg.IndexMaintenanceIntervalMins, 1440)
	vacuum := cfg.IndexMaintenanceVacuum
	if enabled {
		mode := "optimize+reindex"
		if vacuum {
			mode = "optimize+reindex+vacuum"
		}
		logs.WriteLogFile(fmt.Sprintf("INFO [INDEX MAINTENANCE] Enabled: interval=%d minute(s), mode=%s", interval/time.Minute, mode))
	} else {
		logs.WriteLogFile("INFO [INDEX MAINTENANCE] Disabled (opt-in, use index_maintenance_vacuum for SQLite VACUUM)")
	}
	return service.ScheduledTask{
		Key:         taskIndexMaintenance,
		DisplayName: "Index maintenance",
		Enabled:     enabled,
		Interval:    interval,
		Run: func(dbConn *sql.DB, driver string) (string, error) {
			message, err := db.RunIndexMaintenance(dbConn, driver, vacuum)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [INDEX MAINTENANCE] %v", err))
				return "", err
			}
			logs.WriteLogFile(fmt.Sprintf("INFO [INDEX MAINTENANCE] %s", message))
			return message, nil
		},
	}
}

// logCleanupTask supprime les backups de log excédentaires/expirés.
func logCleanupTask(cfg schema.HTTPConfig) service.ScheduledTask {
	enabled := cfg.EnableLogRotation
	if enabled {
		logs.WriteLogFile(fmt.Sprintf("INFO [LOG ROTATION] Enabled: maintenance interval=%s", logMaintenanceInterval))
	}
	return service.ScheduledTask{
		Key:         taskLogCleanup,
		DisplayName: "Log rotate",
		Enabled:     enabled,
		Interval:    logMaintenanceInterval,
		Run: func(dbConn *sql.DB, driver string) (string, error) {
			deleted, err := logs.RunLogBackupCleanupNow()
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [LOG ROTATION] %v", err))
				return "", err
			}
			msg := fmt.Sprintf("Deleted %d backup(s)", deleted)
			logs.WriteLogFile(fmt.Sprintf("INFO [LOG ROTATION] %s", msg))
			return msg, nil
		},
	}
}

// configurationRepoSyncTask synchronise le dépôt Git et compile puis importe les
// MOF, sans résoudre les contextes de nœuds tant que le dépôt n'a pas changé.
func configurationRepoSyncTask(repoSettings db.ConfigurationRepoSettings) service.ScheduledTask {
	interval := intervalFromMins(repoSettings.SyncIntervalMins, 60)
	repoCfg := service.ConfigurationRepoConfig{
		URL:                  repoSettings.URL,
		Branch:               repoSettings.Branch,
		Directory:            repoSettings.Directory,
		CompilerScript:       repoSettings.CompilerScript,
		PowerShellExecutable: repoSettings.PowerShellExecutable,
		RepositoryUsername:   repoSettings.RepositoryUsername,
		RepositoryPassword:   repoSettings.RepositoryPassword,
	}
	if repoSettings.Enabled {
		logs.WriteLogFile(fmt.Sprintf("INFO [CONFIGURATION REPO] Enabled: interval=%d minute(s)", interval/time.Minute))
	} else {
		logs.WriteLogFile("INFO [CONFIGURATION REPO] Synchronization disabled")
	}
	return service.ScheduledTask{
		Key:         taskConfigurationRepoSync,
		DisplayName: "Configuration repository sync",
		Enabled:     repoSettings.Enabled,
		Interval:    interval,
		Run: func(dbConn *sql.DB, driver string) (string, error) {
			runConfig := repoCfg
			// La résolution des contextes de compilation est paresseuse : le
			// dépôt est synchronisé en premier, et on ne résout les propriétés
			// des nœuds que si le dépôt a changé (compilation réelle).
			runConfig.ResolveNodeContexts = func() ([]service.NodeCompilationContext, error) {
				contexts, _, contextErr := db.ListNodeCompilationContextsWithAgentIDs(dbConn)
				return contexts, contextErr
			}
			process := func(compiled []service.CompiledConfiguration) (int, error) {
				if len(compiled) == 0 {
					return 0, nil
				}
				_, nodeAgentIDs, contextErr := db.ListNodeCompilationContextsWithAgentIDs(dbConn)
				if contextErr != nil {
					return 0, contextErr
				}
				return db.ImportCompiledConfigurationsAndAssign(dbConn, compiled, nodeAgentIDs)
			}
			changed, err := service.RunConfigurationRepoSync(runConfig, process)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIGURATION REPO] %v", err))
				return "", err
			}
			return fmt.Sprintf("Updated configurations: %d", changed), nil
		},
	}
}
