package main

import (
	"context"
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
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {

	// Mode CLI : création du compte administrateur initial en local, avec un
	// mot de passe aléatoire. Intercepté avant le démarrage du serveur / service.
	if len(os.Args) > 1 {
		switch strings.TrimLeft(os.Args[1], "-") {
		case "init-bootstrap-admin":
			os.Exit(runInitBootstrapAdmin())
		}
	}

	runApp := func(ctx context.Context) {
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

		// --- Vérifications de sécurité au démarrage (S4) ---
		// Un secret JWT absent rendrait l'authentification web/API inopérante
		// (erreur 500 à la volée). On prévient dès le boot.
		if auth.ResolveJWTSecret(global.AppConfig) == "" {
			logs.WriteLogFile("WARN [SECURITY] shared_secret is not set (neither web_ui.shared_secret nor dsc_pullserver.shared_secret). Web/API authentication will fail until a value is configured.")
		}
		// Une clé d'enregistrement vide empêche tout enrôlement de nœud DSC.
		if strings.TrimSpace(global.AppConfig.DSCPullServer.RegistrationKey) == "" {
			logs.WriteLogFile("WARN [SECURITY] dsc_pullserver.registrationKey is empty. DSC nodes will not be able to register.")
		}
		// Secrets présents mais trop faibles (placeholders, trop courts, faible
		// diversité de caractères) : on prévient dès le boot.
		for _, w := range auth.SecurityWarnings(global.AppConfig) {
			logs.WriteLogFile(w)
		}

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
		// Register the process-wide pool so handlers reuse it instead of opening a
		// new pool per request (Q2). Handlers must never Close() this handle.
		db.SetShared(dbConn)
		// One-time normalisation: nodes registered before the lowercase-thumbprint
		// change store their certificate_thumbprint in uppercase. Lowercase them so
		// the index-friendly, LOWER()-free lookup matches existing nodes (SQLite is
		// case-sensitive; MSSQL is a harmless no-op given its CI collation).
		if normalized, normErr := db.NormalizeCertificateThumbprints(dbConn, global.AppConfig.Database.Driver); normErr != nil {
			logs.WriteLogFile(fmt.Sprintf("WARN [INITDB] Failed to normalize certificate thumbprints: %v", normErr))
		} else if normalized > 0 {
			logs.WriteLogFile(fmt.Sprintf("INFO [INITDB] Normalized %d certificate thumbprint(s) to lowercase", normalized))
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
			RepositoryUsername:   global.AppConfig.DSCPullServer.LegacyConfigurationRepoUsername,
			RepositoryPassword:   global.AppConfig.DSCPullServer.LegacyConfigurationRepoPassword,
			SyncIntervalMins:     global.AppConfig.DSCPullServer.LegacyConfigurationRepoSyncIntervalMins,
		})
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIGURATION REPO] Failed to load database settings: %v", err))
			repoSettings = db.ConfigurationRepoSettings{Branch: "main", Directory: "data/configuration-repository", CompilerScript: "scripts/Compile-Configurations.ps1", PowerShellExecutable: "pwsh", SyncIntervalMins: 60}
		}

		const (
			taskReportCleanup               = "report_cleanup"
			taskReleaseCheck                = "release_check"
			taskLogCleanup                  = "log_cleanup"
			taskConfigurationRepoSync       = "configuration_repo_sync"
			taskAgentPurge                  = "agent_purge"
			taskConfigurationVersionCleanup = "configuration_version_cleanup"
		)

		const logMaintenanceInterval = time.Hour

		// Déclaration des tâches planifiées. La planification (première exécution,
		// enregistrement dans scheduler_tasks) et l'enregistrement des runs
		// (begin/complete) sont factorisés dans taskMeta.
		reportCleanup := taskMeta{
			key:      taskReportCleanup,
			display:  "Report cleanup",
			enabled:  global.AppConfig.DSCPullServer.EnableReportAutoCleanup,
			interval: intervalFromMins(global.AppConfig.DSCPullServer.ReportCleanupIntervalMins, 1440),
		}
		reportCleanup.register(dbConn, global.AppConfig.Database.Driver)

		releaseCheck := taskMeta{
			key:      taskReleaseCheck,
			display:  "Release check",
			enabled:  global.AppConfig.DSCPullServer.EnableReleaseCheck,
			interval: intervalFromMins(global.AppConfig.DSCPullServer.ReleaseCheckIntervalMins, 1440),
		}
		releaseCheck.register(dbConn, global.AppConfig.Database.Driver)

		logCleanup := taskMeta{
			key:      taskLogCleanup,
			display:  "Log rotate",
			enabled:  global.AppConfig.DSCPullServer.EnableLogRotation,
			interval: logMaintenanceInterval,
		}
		logCleanup.register(dbConn, global.AppConfig.Database.Driver)

		agentPurge := taskMeta{
			key:      taskAgentPurge,
			display:  "Agent trash purge",
			enabled:  global.AppConfig.DSCPullServer.EnableAgentPurge,
			interval: intervalFromMins(global.AppConfig.DSCPullServer.AgentPurgeIntervalMins, 1440),
		}
		agentPurge.register(dbConn, global.AppConfig.Database.Driver)

		configVersionCleanup := taskMeta{
			key:      taskConfigurationVersionCleanup,
			display:  "Configuration version retention",
			enabled:  global.AppConfig.DSCPullServer.EnableConfigurationVersionCleanup,
			interval: intervalFromMins(global.AppConfig.DSCPullServer.ConfigurationVersionCleanupIntervalMins, 1440),
		}
		configVersionCleanup.register(dbConn, global.AppConfig.Database.Driver)

		repoCfg := service.ConfigurationRepoConfig{URL: repoSettings.URL, Branch: repoSettings.Branch, Directory: repoSettings.Directory, CompilerScript: repoSettings.CompilerScript, PowerShellExecutable: repoSettings.PowerShellExecutable, RepositoryUsername: repoSettings.RepositoryUsername, RepositoryPassword: repoSettings.RepositoryPassword}
		repoSync := taskMeta{
			key:      taskConfigurationRepoSync,
			display:  "Configuration repository sync",
			enabled:  repoSettings.Enabled,
			interval: intervalFromMins(repoSettings.SyncIntervalMins, 60),
		}
		repoSync.register(dbConn, global.AppConfig.Database.Driver)

		if global.AppConfig.DSCPullServer.EnableReportAutoCleanup {
			retentionDays := global.AppConfig.DSCPullServer.ReportRetentionDays
			if retentionDays <= 0 {
				logs.WriteLogFile("WARN [REPORT CLEANUP] Disabled: report_retention_days must be > 0")
			} else {
				logs.WriteLogFile(fmt.Sprintf("INFO [REPORT CLEANUP] Enabled: retention=%d day(s), interval=%d minute(s)", retentionDays, reportCleanup.interval/time.Minute))
				db.StartReportCleanupWorker(
					ctx,
					dbConn,
					global.AppConfig.Database.Driver,
					retentionDays,
					reportCleanup.interval,
					reportCleanup.beginRecord(dbConn, global.AppConfig.Database.Driver),
					func(startedAt time.Time, deleted int64, cleanupErr error) {
						if cleanupErr != nil {
							logs.WriteLogFile(fmt.Sprintf("ERROR [REPORT CLEANUP] %v", cleanupErr))
							reportCleanup.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, cleanupErr.Error(), cleanupErr)
							return
						}
						logs.WriteLogFile(fmt.Sprintf("INFO [REPORT CLEANUP] Deleted %d old report(s)", deleted))
						reportCleanup.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, fmt.Sprintf("Deleted %d old report(s)", deleted), nil)
					},
				)
			}
		}

		if global.AppConfig.DSCPullServer.EnableAgentPurge {
			retentionDays := global.AppConfig.DSCPullServer.AgentPurgeRetentionDays
			if retentionDays <= 0 {
				logs.WriteLogFile("WARN [AGENT PURGE] Disabled: agent_purge_retention_days must be > 0")
			} else {
				logs.WriteLogFile(fmt.Sprintf("INFO [AGENT PURGE] Enabled: retention=%d day(s), interval=%d minute(s)", retentionDays, agentPurge.interval/time.Minute))
				db.StartAgentPurgeWorker(
					ctx,
					dbConn,
					global.AppConfig.Database.Driver,
					retentionDays,
					agentPurge.interval,
					agentPurge.beginRecord(dbConn, global.AppConfig.Database.Driver),
					func(startedAt time.Time, purged int64, cleanupErr error) {
						if cleanupErr != nil {
							logs.WriteLogFile(fmt.Sprintf("ERROR [AGENT PURGE] %v", cleanupErr))
							agentPurge.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, cleanupErr.Error(), cleanupErr)
							return
						}
						logs.WriteLogFile(fmt.Sprintf("INFO [AGENT PURGE] Purged %d trashed agent(s)", purged))
						agentPurge.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, fmt.Sprintf("Purged %d trashed agent(s)", purged), nil)
					},
				)
			}
		}

		if global.AppConfig.DSCPullServer.EnableConfigurationVersionCleanup {
			maxKeep := global.AppConfig.DSCPullServer.ConfigurationVersionMaxKeep
			if maxKeep < 1 {
				logs.WriteLogFile("WARN [CONFIG VERSION CLEANUP] Disabled: configuration_version_max_keep must be >= 1")
			} else {
				logs.WriteLogFile(fmt.Sprintf("INFO [CONFIG VERSION CLEANUP] Enabled: max_keep=%d version(s), interval=%d minute(s)", maxKeep, configVersionCleanup.interval/time.Minute))
				db.StartConfigurationVersionCleanupWorker(
					ctx,
					dbConn,
					maxKeep,
					configVersionCleanup.interval,
					configVersionCleanup.beginRecord(dbConn, global.AppConfig.Database.Driver),
					func(startedAt time.Time, pruned int64, cleanupErr error) {
						if cleanupErr != nil {
							logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIG VERSION CLEANUP] %v", cleanupErr))
							configVersionCleanup.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, cleanupErr.Error(), cleanupErr)
							return
						}
						logs.WriteLogFile(fmt.Sprintf("INFO [CONFIG VERSION CLEANUP] Pruned %d old configuration version(s)", pruned))
						configVersionCleanup.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, fmt.Sprintf("Pruned %d old configuration version(s)", pruned), nil)
					},
				)
			}
		}

		if global.AppConfig.DSCPullServer.EnableReleaseCheck {
			logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Enabled: interval=%d minute(s)", releaseCheck.interval/time.Minute))
			service.StartReleaseCheckWorker(
				ctx,
				buildinfo.Version,
				releaseCheck.interval,
				releaseCheck.beginRecord(dbConn, global.AppConfig.Database.Driver),
				func(startedAt time.Time, result service.ReleaseCheckResult, checkErr error) {
					if checkErr != nil {
						logs.WriteLogFile(fmt.Sprintf("WARN [RELEASE CHECK] %v", checkErr))
						_ = db.PersistReleaseCheckFailure(dbConn, global.AppConfig.Database.Driver)
						releaseCheck.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, checkErr.Error(), checkErr)
						return
					}
					_ = db.PersistReleaseCheckSuccess(dbConn, global.AppConfig.Database.Driver, result.LatestRelease, result.LatestReleaseURL, result.UpdateAvailable)
					if result.UpdateAvailable {
						logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Update available: current=%s latest=%s", result.CurrentVersion, result.LatestRelease))
						releaseCheck.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, fmt.Sprintf("Update available: %s", result.LatestRelease), nil)
					} else {
						logs.WriteLogFile(fmt.Sprintf("INFO [RELEASE CHECK] Up to date: current=%s", result.CurrentVersion))
						releaseCheck.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, "Already up to date", nil)
					}
				},
			)
		}

		if global.AppConfig.DSCPullServer.EnableLogRotation {
			logs.WriteLogFile(fmt.Sprintf("INFO [LOG ROTATION] Enabled: maintenance interval=%s", logCleanup.interval))
			logs.StartLogMaintenanceWorker(
				ctx,
				logCleanup.interval,
				logCleanup.beginRecord(dbConn, global.AppConfig.Database.Driver),
				func(startedAt time.Time, deleted int, runErr error) {
					if runErr != nil {
						logs.WriteLogFile(fmt.Sprintf("ERROR [LOG ROTATION] %v", runErr))
						logCleanup.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, runErr.Error(), runErr)
						return
					}
					msg := fmt.Sprintf("Deleted %d backup(s)", deleted)
					logCleanup.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, msg, nil)
				},
			)
		}

		if repoSync.enabled {
			logs.WriteLogFile(fmt.Sprintf("INFO [CONFIGURATION REPO] Enabled: interval=%d minute(s)", repoSync.interval/time.Minute))
			service.StartConfigurationRepoWorkerWithConfig(
				ctx,
				func() (service.ConfigurationRepoConfig, error) {
					contexts, _, contextErr := db.ListNodeCompilationContextsWithAgentIDs(dbConn)
					if contextErr != nil {
						return service.ConfigurationRepoConfig{}, contextErr
					}
					runConfig := repoCfg
					runConfig.NodeContexts = contexts
					return runConfig, nil
				},
				repoSync.interval,
				func(compiled []service.CompiledConfiguration) (int, error) {
					_, nodeAgentIDs, contextErr := db.ListNodeCompilationContextsWithAgentIDs(dbConn)
					if contextErr != nil {
						return 0, contextErr
					}
					return db.ImportCompiledConfigurationsAndAssign(dbConn, compiled, nodeAgentIDs)
				},
				repoSync.beginRecord(dbConn, global.AppConfig.Database.Driver),
				func(startedAt time.Time, changed int, syncErr error) {
					if syncErr != nil {
						logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIGURATION REPO] %v", syncErr))
						repoSync.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, syncErr.Error(), syncErr)
						return
					}
					message := fmt.Sprintf("Updated configurations: %d", changed)
					repoSync.completeRecord(dbConn, global.AppConfig.Database.Driver)(startedAt, message, nil)
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

		// Vérifie si la table users est vide et guide l'opérateur. Le compte
		// administrateur initial n'est plus créé avec un mot de passe par défaut
		// (vulnérabilité C1) : il doit être généré via "init-bootstrap-admin".
		{
			var count int
			err := dbConn.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
			if err == nil && count == 0 {
				logs.WriteLogFile("WARN [INITDB] No user in the database: create the initial admin account via \"DSC-Admin-Console.exe init-bootstrap-admin\" (random password). The server no longer creates a default admin account.")
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
					logs.WriteLogFile(fmt.Sprintf("[DSC][TLS] Client certificate received: Subject=%s, Thumbprint=%s", clientCert.Subject, sha1sumLower))
					// Vérification de l'empreinte si la validation du certificat client est activée
					// On ne fait PAS ce contrôle pour la route de registration DSC (PUT /PSDSCPullServer.svc/{node})
					isRegister := r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/PSDSCPullServer.svc/") && len(strings.Split(r.URL.Path, "/")) == 3
					if global.AppConfig.DSCPullServer.EnableClientCertValidation && !isRegister {
						// Vérifie la présence de l'empreinte en base (les empreintes
						// sont stockées en minuscules, la comparaison reste index-friendly).
						registered, err := db.IsCertificateThumbprintRegistered(dbConn, sha1sumLower)
						if err != nil {
							logs.WriteLogFile(fmt.Sprintf("[DSC][TLS] Error verifying thumbprint: %v", err))
							http.Error(w, "Internal error verifying certificate", http.StatusInternalServerError)
							return
						}
						if !registered {
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

		// --- Serveurs HTTP avec arrêt propre (drivé par ctx.Done()) ---
		// Chaque serveur est démarré dans une goroutine. À la réception d'un
		// Stop/Shutdown du service Windows (annulation du contexte), runApp
		// déclenche un Shutdown gracieux des deux serveurs puis retourne, ce qui
		// permet à la couche service de finaliser l'arrêt sans tuer les requêtes.

		// Serveur DSC (endpoints MS-DSCPM)
		dscServer := &http.Server{
			Addr:              fmt.Sprintf(":%d", global.AppConfig.DSCPullServer.Port),
			Handler:           dscHandler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       120 * time.Second,
			WriteTimeout:      180 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
		if global.AppConfig.DSCPullServer.EnableHTTPS {
			certFile, keyFile := resolveCertKeyPath(global.AppConfig.DSCPullServer.CertFile, global.AppConfig.DSCPullServer.KeyFile)
			// Bypass CA verification: accepter tout certificat client présenté
			tlsConfig := &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
			// Vérifie si la validation du certificat client est activée.
			if global.AppConfig.DSCPullServer.EnableClientCertValidation {
				// Toujours exiger un certificat client : le middleware d'empreinte
				// (enrôlement du nœud) ne s'exécute que si un certificat est présenté.
				// bypass_ca_validation ne conditionne que la validation de la chaîne CA,
				// pas l'exigence d'un certificat.
				if global.AppConfig.DSCPullServer.BypassCAValidation {
					// Aucune validation de la signature/CA : seuls l'empreinte du
					// certificat (enrôlement du nœud) compte. Modèle DSC avec des
					// certificats potentiellement auto-signés.
					tlsConfig.ClientAuth = tls.RequireAnyClientCert
				} else {
					// Validation de la chaîne/signature contre le magasin d'autorités
					// du système, en plus du contrôle d'empreinte (enrôlement).
					tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
				}
			} else {
				logs.WriteLogFile(fmt.Sprintf("WARN [DSC Core Server] Client certificate validation is disabled."))
			}
			dscServer.TLSConfig = tlsConfig
			go func() {
				logs.WriteLogFile(fmt.Sprintf("INFO [DSC Core Server] DSC Server (HTTPS) on :%d ...", global.AppConfig.DSCPullServer.Port))
				if err := dscServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC Core Server] Error starting HTTPS server: %v", err))
					os.Exit(1)
				}
			}()
		} else {
			logs.WriteLogFile(fmt.Sprintf("WARN [DSC Core Server] DSC Server is listening in HTTP (no TLS) on :%d ...", global.AppConfig.DSCPullServer.Port))
			if global.AppConfig.DSCPullServer.EnableClientCertValidation {
				logs.WriteLogFile(fmt.Sprintf("WARN [SECURITY] enable_client_cert_validation is true but the DSC endpoint is served over plain HTTP. No client certificate is exchanged, so the pull endpoints are NOT protected by mutual TLS."))
			}
			go func() {
				if err := dscServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUi] Error starting HTTP server: %v", err))
					os.Exit(1)
				}
			}()
		}

		// Serveur WebUI / API
		webServer := &http.Server{
			Addr:              fmt.Sprintf(":%d", global.AppConfig.WebUI.Port),
			Handler:           webHandler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       120 * time.Second,
			WriteTimeout:      180 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
		if global.AppConfig.WebUI.EnableHTTPS {
			certFile, keyFile := resolveCertKeyPath(global.AppConfig.WebUI.CertFile, global.AppConfig.WebUI.KeyFile)
			go func() {
				logs.WriteLogFile(fmt.Sprintf("INFO [DSC WebUI] UI/API (HTTPS) on :%d ... (UI on /web/)", global.AppConfig.WebUI.Port))
				if err := webServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUI] Error starting HTTPS server: %v", err))
					os.Exit(1)
				}
			}()
		} else {
			logs.WriteLogFile(fmt.Sprintf("WARN [DSC WebUI] UI/API is listening in HTTP (no TLS) on :%d ... (UI on /web/)", global.AppConfig.WebUI.Port))
			logs.WriteLogFile("WARN [SECURITY] The Web UI / API is served over plain HTTP. Credentials and JWT tokens transit unencrypted. Use HTTPS (enable_https: true) in production.")
			go func() {
				if err := webServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUI] Error starting HTTP server: %v", err))
					os.Exit(1)
				}
			}()
		}

		// Bloque jusqu'à l'arrêt, puis ferme gracieusement les deux serveurs.
		<-ctx.Done()
		logs.WriteLogFile("INFO [SHUTDOWN] Shutdown requested, closing HTTP servers ...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = dscServer.Shutdown(shutdownCtx)
		_ = webServer.Shutdown(shutdownCtx)
		logs.WriteLogFile("INFO [SHUTDOWN] HTTP servers stopped cleanly.")
	}

	if runtime.GOOS == "windows" {
		err := service.StartWindowsService("DSCPullServer", runApp)
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [SERVICE] %v", err))
			os.Exit(1)
		}
	} else {
		// Le contexte n'est jamais annulé en mode hors-service : le processus
		// tourne jusqu'à son arrêt (kill). La closure runApp bloque sur
		// ctx.Done() comme le faisait auparavant le ListenAndServe du serveur.
		runApp(context.Background())
	}
}
