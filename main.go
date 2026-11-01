package main

import (
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"go-dsc-pull/internal"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/buildinfo"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/routes"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/service"
	"go-dsc-pull/utils"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {

	runApp := func() {
		logs.WriteLogFile(fmt.Sprintf("INFO [BUILD] %s", buildinfo.Short()))
		// Résout dynamiquement les chemins de cert/key si nécessaire
		resolveCertKeyPath := func(certFile, keyFile string) (string, string) {
			if filepath.IsAbs(certFile) && filepath.IsAbs(keyFile) {
				return certFile, keyFile
			}
			exePath, err := utils.ExePath()
			baseDir := ""
			if err == nil {
				baseDir = filepath.Dir(exePath)
			}
			if !filepath.IsAbs(certFile) {
				certFile = filepath.Join(baseDir, certFile)
			}
			if !filepath.IsAbs(keyFile) {
				keyFile = filepath.Join(baseDir, keyFile)
			}
			return certFile, keyFile
		}

		// Chargement de la configuration globale (incluant SAML)
		appCfg, err := internal.LoadAppConfig("config.json")
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [INIT] Failed to load global config: %v", err))
			os.Exit(1)
		}
		global.AppConfig = appCfg

		// Initialisation SAML Service Provider si activé
		samlMiddleware, err := auth.InitSamlMiddleware(global.AppConfig)
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [SAML] %v", err))
			os.Exit(1)
		}

		// Initialisation automatique de la base (CREATE IF NOT EXISTS)
		dbCfg := &schema.DBConfig{
			Driver:   global.AppConfig.Database.Driver,
			Server:   global.AppConfig.Database.Server,
			Port:     global.AppConfig.Database.Port,
			User:     global.AppConfig.Database.User,
			Password: global.AppConfig.Database.Password,
			Database: global.AppConfig.Database.Database,
		}
		dbPath := dbCfg.Database
		if dbCfg.Driver == "sqlite" && !filepath.IsAbs(dbPath) {
			exePath, err := utils.ExePath()
			baseDir := ""
			if err == nil {
				baseDir = filepath.Dir(exePath)
				dbCfg.Database = filepath.Join(baseDir, dbPath)
			}
		}
		db.InitDB(dbCfg)
		dbConn, err := db.OpenDB(dbCfg)
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [INITDB] Failed to open DB: %v", err))
			os.Exit(1)
		}
		if cancelledRuns, cancelErr := db.CancelRunningSchedulerTasks(dbConn, global.AppConfig.Database.Driver, time.Now().UTC(), "Cancelled on application startup"); cancelErr != nil {
			logs.WriteLogFile(fmt.Sprintf("WARN [SCHEDULER] Failed to cancel stale running tasks on startup: %v", cancelErr))
		} else if cancelledRuns > 0 {
			logs.WriteLogFile(fmt.Sprintf("INFO [SCHEDULER] Cancelled %d stale running task execution(s) on startup", cancelledRuns))
		}
		repoSettings, err := db.InitializeConfigurationRepoSettings(dbConn, global.AppConfig.Database.Driver, db.ConfigurationRepoSettings{
			Enabled:              global.AppConfig.DSCPullServer.LegacyConfigurationRepoEnabled,
			URL:                  global.AppConfig.DSCPullServer.LegacyConfigurationRepoURL,
			Branch:               global.AppConfig.DSCPullServer.LegacyConfigurationRepoBranch,
			Directory:            global.AppConfig.DSCPullServer.LegacyConfigurationRepoDirectory,
			CompilerScript:       global.AppConfig.DSCPullServer.LegacyConfigurationCompilerScript,
			PowerShellExecutable: global.AppConfig.DSCPullServer.LegacyConfigurationPowerShellExecutable,
			SyncIntervalMins:     global.AppConfig.DSCPullServer.LegacyConfigurationRepoSyncIntervalMins,
		})
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIGURATION REPO] Failed to load database settings: %v", err))
			repoSettings = db.ConfigurationRepoSettings{Branch: "main", Directory: "data/configuration-repository", CompilerScript: "scripts/Compile-Configurations.ps1", PowerShellExecutable: "pwsh", SyncIntervalMins: 60}
		}

		const (
			taskReportCleanup         = "report_cleanup"
			taskReleaseCheck          = "release_check"
			taskLogCleanup            = "log_cleanup"
			taskConfigurationRepoSync = "configuration_repo_sync"
		)

		const logMaintenanceInterval = time.Hour

		reportDisplay := "Report cleanup"
		releaseDisplay := "Release check"
		logDisplay := "Log rotate"
		configurationRepoDisplay := "Configuration repository sync"

		var reportNext *time.Time
		if global.AppConfig.DSCPullServer.EnableReportAutoCleanup {
			intervalMins := global.AppConfig.DSCPullServer.ReportCleanupIntervalMins
			if intervalMins <= 0 {
				intervalMins = 1440
			}
			n := time.Now().UTC().Add(time.Duration(intervalMins) * time.Minute)
			reportNext = &n
		}
		_ = db.UpsertSchedulerTask(dbConn, global.AppConfig.Database.Driver, taskReportCleanup, reportDisplay, reportNext)

		var releaseNext *time.Time
		if global.AppConfig.DSCPullServer.EnableReleaseCheck {
			intervalMins := global.AppConfig.DSCPullServer.ReleaseCheckIntervalMins
			if intervalMins <= 0 {
				intervalMins = 1440
			}
			n := time.Now().UTC().Add(time.Duration(intervalMins) * time.Minute)
			releaseNext = &n
		}
		_ = db.UpsertSchedulerTask(dbConn, global.AppConfig.Database.Driver, taskReleaseCheck, releaseDisplay, releaseNext)

		var logNext *time.Time
		if global.AppConfig.DSCPullServer.EnableLogRotation {
			n := time.Now().UTC().Add(logMaintenanceInterval)
			logNext = &n
		}
		_ = db.UpsertSchedulerTask(dbConn, global.AppConfig.Database.Driver, taskLogCleanup, logDisplay, logNext)
		repoCfg := service.ConfigurationRepoConfig{URL: repoSettings.URL, Branch: repoSettings.Branch, Directory: repoSettings.Directory, CompilerScript: repoSettings.CompilerScript, PowerShellExecutable: repoSettings.PowerShellExecutable}
		repoSyncEnabled := repoSettings.Enabled
		repoIntervalMins := repoSettings.SyncIntervalMins
		if repoIntervalMins <= 0 {
			repoIntervalMins = 60
		}
		var repoNext *time.Time
		if repoSyncEnabled {
			n := time.Now().UTC().Add(time.Duration(repoIntervalMins) * time.Minute)
			repoNext = &n
		}
		_ = db.UpsertSchedulerTask(dbConn, global.AppConfig.Database.Driver, taskConfigurationRepoSync, configurationRepoDisplay, repoNext)

		if global.AppConfig.DSCPullServer.EnableReportAutoCleanup {
			retentionDays := global.AppConfig.DSCPullServer.ReportRetentionDays
			intervalMins := global.AppConfig.DSCPullServer.ReportCleanupIntervalMins
			if retentionDays <= 0 {
				logs.WriteLogFile("WARN [REPORT CLEANUP] Disabled: report_retention_days must be > 0")
			} else {
				if intervalMins <= 0 {
					intervalMins = 1440
				}
				logs.WriteLogFile(fmt.Sprintf("INFO [REPORT CLEANUP] Enabled: retention=%d day(s), interval=%d minute(s)", retentionDays, intervalMins))
				db.StartReportCleanupWorker(
					dbConn,
					global.AppConfig.Database.Driver,
					retentionDays,
					time.Duration(intervalMins)*time.Minute,
					func(startedAt time.Time, nextRunAt *time.Time) {
						_ = db.BeginSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskReportCleanup, reportDisplay, "automatic", startedAt, nextRunAt)
					},
					func(startedAt time.Time, deleted int64, cleanupErr error) {
						if cleanupErr != nil {
							logs.WriteLogFile(fmt.Sprintf("ERROR [REPORT CLEANUP] %v", cleanupErr))
							msg := cleanupErr.Error()
							_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskReportCleanup, "error", msg, time.Now().UTC(), nil)
							return
						}
						logs.WriteLogFile(fmt.Sprintf("INFO [REPORT CLEANUP] Deleted %d old report(s)", deleted))
						next := startedAt.Add(time.Duration(intervalMins) * time.Minute)
						msg := fmt.Sprintf("Deleted %d old report(s)", deleted)
						_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskReportCleanup, "success", msg, time.Now().UTC(), &next)
					},
				)
			}
		}

		if global.AppConfig.DSCPullServer.EnableReleaseCheck {
			intervalMins := global.AppConfig.DSCPullServer.ReleaseCheckIntervalMins
			if intervalMins <= 0 {
				intervalMins = 1440
			}
			logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Enabled: interval=%d minute(s)", intervalMins))
			service.StartReleaseCheckWorker(
				buildinfo.Version,
				time.Duration(intervalMins)*time.Minute,
				func(startedAt time.Time, nextRunAt *time.Time) {
					_ = db.BeginSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskReleaseCheck, releaseDisplay, "automatic", startedAt, nextRunAt)
				},
				func(startedAt time.Time, result service.ReleaseCheckResult, checkErr error) {
					if checkErr != nil {
						logs.WriteLogFile(fmt.Sprintf("WARN [RELEASE CHECK] %v", checkErr))
						_ = db.PersistReleaseCheckFailure(dbConn, global.AppConfig.Database.Driver)
						next := startedAt.Add(time.Duration(intervalMins) * time.Minute)
						_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskReleaseCheck, "error", checkErr.Error(), time.Now().UTC(), &next)
						return
					}
					_ = db.PersistReleaseCheckSuccess(dbConn, global.AppConfig.Database.Driver, result.LatestRelease, result.LatestReleaseURL, result.UpdateAvailable)
					if result.UpdateAvailable {
						logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Update available: current=%s latest=%s", result.CurrentVersion, result.LatestRelease))
						next := startedAt.Add(time.Duration(intervalMins) * time.Minute)
						_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskReleaseCheck, "success", fmt.Sprintf("Update available: %s", result.LatestRelease), time.Now().UTC(), &next)
					} else {
						logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Up to date: current=%s", result.CurrentVersion))
						next := startedAt.Add(time.Duration(intervalMins) * time.Minute)
						_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskReleaseCheck, "success", "Already up to date", time.Now().UTC(), &next)
					}
				},
			)
		}

		if global.AppConfig.DSCPullServer.EnableLogRotation {
			logs.WriteLogFile(fmt.Sprintf("INFO [LOG ROTATION] Enabled: maintenance interval=%s", logMaintenanceInterval))
			logs.StartLogMaintenanceWorker(
				logMaintenanceInterval,
				func(startedAt time.Time, nextRunAt *time.Time) {
					_ = db.BeginSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskLogCleanup, logDisplay, "automatic", startedAt, nextRunAt)
				},
				func(startedAt time.Time, deleted int, runErr error) {
					next := startedAt.Add(logMaintenanceInterval)
					if runErr != nil {
						logs.WriteLogFile(fmt.Sprintf("ERROR [LOG ROTATION] %v", runErr))
						_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskLogCleanup, "error", runErr.Error(), time.Now().UTC(), &next)
						return
					}
					msg := fmt.Sprintf("Deleted %d backup(s)", deleted)
					_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskLogCleanup, "success", msg, time.Now().UTC(), &next)
				},
			)
		}

		if repoSyncEnabled {
			logs.WriteLogFile(fmt.Sprintf("INFO [CONFIGURATION REPO] Enabled: interval=%d minute(s)", repoIntervalMins))
			service.StartConfigurationRepoWorkerWithConfig(
				func() (service.ConfigurationRepoConfig, error) {
					contexts, _, contextErr := db.ListNodeCompilationContextsWithAgentIDs(dbConn)
					if contextErr != nil {
						return service.ConfigurationRepoConfig{}, contextErr
					}
					runConfig := repoCfg
					runConfig.NodeContexts = contexts
					return runConfig, nil
				},
				time.Duration(repoIntervalMins)*time.Minute,
				func(compiled []service.CompiledConfiguration) (int, error) {
					_, nodeAgentIDs, contextErr := db.ListNodeCompilationContextsWithAgentIDs(dbConn)
					if contextErr != nil {
						return 0, contextErr
					}
					return db.ImportCompiledConfigurationsAndAssign(dbConn, compiled, nodeAgentIDs)
				},
				func(startedAt time.Time, nextRunAt *time.Time) {
					_ = db.BeginSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskConfigurationRepoSync, configurationRepoDisplay, "automatic", startedAt, nextRunAt)
				},
				func(startedAt time.Time, changed int, syncErr error) {
					next := startedAt.Add(time.Duration(repoIntervalMins) * time.Minute)
					if syncErr != nil {
						logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIGURATION REPO] %v", syncErr))
						_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskConfigurationRepoSync, "error", syncErr.Error(), time.Now().UTC(), &next)
						return
					}
					message := fmt.Sprintf("Updated configurations: %d", changed)
					_ = db.CompleteSchedulerTaskRun(dbConn, global.AppConfig.Database.Driver, taskConfigurationRepoSync, "success", message, time.Now().UTC(), &next)
				},
			)
		} else {
			logs.WriteLogFile("INFO [CONFIGURATION REPO] Synchronization disabled")
		}

		if global.AppConfig.SAML.Enabled {
			logs.WriteLogFile(fmt.Sprintf("INFO [SAML] SAML Authentication activated (EntityID: %s)", global.AppConfig.SAML.EntityID))
		} else {
			logs.WriteLogFile("INFO [SAML] SAML Authentication deactivated (local auth)")
		}

		// Vérifie si la table users est vide et insère le compte admin si besoin
		{
			var count int
			err := dbConn.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
			if err == nil && count == 0 {
				hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
				_, err := dbConn.Exec("INSERT INTO users (first_name, last_name, email, password_hash, is_active, role, source) VALUES (?, ?, ?, ?, ?, ?, ?)", "Admin", "User", "admin@localhost", string(hash), true, "admin", "local")
				if err != nil {
					logs.WriteLogFile(fmt.Sprintf("ERROR [INITDB] Failed to insert admin: %v", err))
				} else {
					logs.WriteLogFile("INFO [INITDB] Admin account created: admin@localhost / password")
				}
			}
		}

		// --- Mux DSC (endpoints MS-DSCPM) ---
		dscMux := http.NewServeMux()
		routes.RegisterDSCRoutes(dscMux, dbConn)
		// Middleware pour logguer le certificat client DSC
		dscCertLogMiddleware := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
					clientCert := r.TLS.PeerCertificates[0]
					h := sha1.New()
					h.Write(clientCert.Raw)
					sha1sum := hex.EncodeToString(h.Sum(nil))
					sha1sumLower := strings.ToLower(sha1sum)
					logs.WriteLogFile(fmt.Sprintf("[DSC][TLS] Certificat client reçu: Subject=%s, Thumbprint=%s", clientCert.Subject, sha1sumLower))
					// Vérification de l'empreinte si la validation du certificat client est activée
					// On ne fait PAS ce contrôle pour la route de registration DSC (PUT /PSDSCPullServer.svc/{node})
					isRegister := r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/PSDSCPullServer.svc/") && len(strings.Split(r.URL.Path, "/")) == 3
					if global.AppConfig.DSCPullServer.EnableClientCertValidation && !isRegister {
						// Vérifier la présence de l'empreinte dans la base de données (en minuscules)
						var count int
						err := dbConn.QueryRow("SELECT COUNT(*) FROM agents WHERE LOWER(certificate_thumbprint) = ?", sha1sumLower).Scan(&count)
						if err != nil {
							logs.WriteLogFile(fmt.Sprintf("[DSC][TLS] Error verifying thumbprint: %v", err))
							http.Error(w, "Internal error verifying certificate", http.StatusInternalServerError)
							return
						}
						if count == 0 {
							logs.WriteLogFile(fmt.Sprintf("[DSC][TLS] Certificate thumbprint not recognized: %s", sha1sumLower))
							http.Error(w, "Certificate unauthorized", http.StatusUnauthorized)
							return
						}
					}
				}
				next.ServeHTTP(w, r)
			})
		}
		// --- Mux Web/API/GUI ---
		webMux := http.NewServeMux()
		routes.RegisterWebRoutes(webMux, dbConn, auth.JwtOrAPITokenAuthMiddleware(dbConn), samlMiddleware)

		// Ajout du logging middleware
		dscHandler := logs.LoggingMiddleware(dscCertLogMiddleware(dscMux))
		webHandler := logs.LoggingMiddleware(webMux)

		// Lancer les deux serveurs sur des ports différents avec HTTPS optionnel
		go func() {
			if global.AppConfig.DSCPullServer.EnableHTTPS {
				logs.WriteLogFile(fmt.Sprintf("INFO [DSC Core Server] DSC Server (HTTPS) on :%d ...", global.AppConfig.DSCPullServer.Port))
				certFile, keyFile := resolveCertKeyPath(global.AppConfig.DSCPullServer.CertFile, global.AppConfig.DSCPullServer.KeyFile)
				// Bypass CA verification: accepter tout certificat client présenté
				tlsConfig := &tls.Config{
					MinVersion: tls.VersionTLS12,
				}
				if global.AppConfig.DSCPullServer.EnableClientCertValidation {
					if global.AppConfig.DSCPullServer.BypassCAValidation {
						tlsConfig.ClientAuth = tls.RequireAnyClientCert
					}
				} else {
					logs.WriteLogFile(fmt.Sprintf("WARN [DSC Core Server] Client certificate validation is disabled."))
				}
				server := &http.Server{
					Addr:      fmt.Sprintf(":%d", global.AppConfig.DSCPullServer.Port),
					Handler:   dscHandler,
					TLSConfig: tlsConfig,
				}
				err := server.ListenAndServeTLS(certFile, keyFile)
				if err != nil {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC Core Server] Error starting HTTPS server: %v", err))
					os.Exit(1)
				}
			} else {
				logs.WriteLogFile(fmt.Sprintf("INFO [DSC WebUi] DSC Server (HTTP) on :%d ...", global.AppConfig.DSCPullServer.Port))
				err := http.ListenAndServe(fmt.Sprintf(":%d", global.AppConfig.DSCPullServer.Port), dscHandler)
				if err != nil {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUi] Error starting HTTP server: %v", err))
					os.Exit(1)
				}
			}
		}()

		if global.AppConfig.WebUI.EnableHTTPS {
			logs.WriteLogFile(fmt.Sprintf("INFO [DSC WebUI] IHM/API (HTTPS) on :%d ... (IHM on /web/)", global.AppConfig.WebUI.Port))
			certFile, keyFile := resolveCertKeyPath(global.AppConfig.WebUI.CertFile, global.AppConfig.WebUI.KeyFile)
			err := http.ListenAndServeTLS(
				fmt.Sprintf(":%d", global.AppConfig.WebUI.Port),
				certFile,
				keyFile,
				webHandler,
			)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUI] Error starting HTTPS server: %v", err))
				os.Exit(1)
			}
		} else {
			logs.WriteLogFile(fmt.Sprintf("INFO [DSC WebUI] IHM/API (HTTP) on :%d ... (IHM on /web/)", global.AppConfig.WebUI.Port))
			err := http.ListenAndServe(fmt.Sprintf(":%d", global.AppConfig.WebUI.Port), webHandler)
			if err != nil {
				logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUI] Error starting HTTP server: %v", err))
				os.Exit(1)
			}
		}
	}

	if runtime.GOOS == "windows" {
		err := service.StartWindowsService("DSCPullServer", runApp)
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [SERVICE] %v", err))
			os.Exit(1)
		}
	} else {
		runApp()
	}
}
