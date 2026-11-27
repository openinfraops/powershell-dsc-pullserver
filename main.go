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
	"go-dsc-pull/internal/monitor"
	"go-dsc-pull/internal/routes"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/service"
	"go-dsc-pull/utils"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// httpServerTimeouts returns the HTTP server timeout durations for a config
// block, applying the sensible defaults when a value is 0 or absent.
//
// Defaults (kept generous for large MOF/module downloads; read_header fixed at
// 10s to guard against slowloris): read=120s, write=180s, idle=120s.
func httpServerTimeouts(cfg schema.HTTPConfig) (readHeader, read, write, idle time.Duration) {
	readHeader = time.Duration(cfg.ReadHeaderTimeoutSeconds) * time.Second
	if readHeader <= 0 {
		readHeader = 10 * time.Second
	}
	read = time.Duration(cfg.ReadTimeoutSeconds) * time.Second
	if read <= 0 {
		read = 120 * time.Second
	}
	write = time.Duration(cfg.WriteTimeoutSeconds) * time.Second
	if write <= 0 {
		write = 180 * time.Second
	}
	idle = time.Duration(cfg.IdleTimeoutSeconds) * time.Second
	if idle <= 0 {
		idle = 120 * time.Second
	}
	return readHeader, read, write, idle
}

func main() {

	// Mode CLI : crÃ©ation du compte administrateur initial en local, avec un
	// mot de passe alÃ©atoire. InterceptÃ© avant le dÃ©marrage du serveur / service.
	if len(os.Args) > 1 {
		switch strings.TrimLeft(os.Args[1], "-") {
		case "init-bootstrap-admin":
			os.Exit(runInitBootstrapAdmin())
		}
	}

	runApp := func(ctx context.Context) error {
		logs.WriteLogFile(fmt.Sprintf("INFO [BUILD] %s", buildinfo.Short()))
		// RÃ©sout dynamiquement les chemins de cert/key si nÃ©cessaire
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

		// --- VÃ©rifications de sÃ©curitÃ© au dÃ©marrage (S4) ---
		// Fail-closed : certaines combinaisons de configuration sont refusÃ©es
		// au dÃ©marrage (exit non-zÃ©ro) plutÃ´t que de dÃ©marrer avec une
		// protection qui n'est en rÃ©alitÃ© pas active. On les traite avant les
		// simples avertissements.
		if fatal := auth.FatalSecurityErrors(global.AppConfig); len(fatal) > 0 {
			for _, f := range fatal {
				logs.WriteLogFile(f)
			}
			os.Exit(1)
		}
		// Un secret JWT absent rendrait l'authentification web/API inopÃ©rante
		// (erreur 500 Ã  la volÃ©e). On prÃ©vient dÃ¨s le boot.
		if auth.ResolveJWTSecret(global.AppConfig) == "" {
			logs.WriteLogFile("WARN [SECURITY] shared_secret is not set (neither web_ui.shared_secret nor dsc_pullserver.shared_secret). Web/API authentication will fail until a value is configured.")
		}
		// Une clÃ© d'enregistrement vide empÃªche tout enrÃ´lement de nÅ“ud DSC.
		if strings.TrimSpace(global.AppConfig.DSCPullServer.RegistrationKey) == "" {
			logs.WriteLogFile("WARN [SECURITY] dsc_pullserver.registrationKey is empty. DSC nodes will not be able to register.")
		}
		// Secrets prÃ©sents mais trop faibles (placeholders, trop courts, faible
		// diversitÃ© de caractÃ¨res) : on prÃ©vient dÃ¨s le boot.
		for _, w := range auth.SecurityWarnings(global.AppConfig) {
			logs.WriteLogFile(w)
		}

		// Initialisation SAML Service Provider si activÃ©
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

		// Enregistre puis démarre les tâches planifiées via le registre.
		driver := global.AppConfig.Database.Driver
		service.StartScheduledTasks(ctx, dbConn, driver, schedulerRecorder{dbConn: dbConn, driver: driver}, buildScheduledTasks(dbConn, repoSettings))

		if global.AppConfig.SAML.Enabled {
			logs.WriteLogFile(fmt.Sprintf("INFO [SAML] SAML Authentication activated (EntityID: %s)", global.AppConfig.SAML.EntityID))
		} else {
			logs.WriteLogFile("INFO [SAML] SAML Authentication deactivated (local auth)")
		}

		// VÃ©rifie si la table users est vide et guide l'opÃ©rateur. Le compte
		// administrateur initial n'est plus crÃ©Ã© avec un mot de passe par dÃ©faut
		// (vulnÃ©rabilitÃ© C1) : il doit Ãªtre gÃ©nÃ©rÃ© via "init-bootstrap-admin".
		{
			var count int
			err := dbConn.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
			if err == nil && count == 0 {
				exeName := "DSC-Admin-Console.exe"
				if p, pathErr := utils.ExePath(); pathErr == nil {
					exeName = filepath.Base(p)
				}
				logs.WriteLogFile(fmt.Sprintf("WARN [INITDB] No user in the database: create the initial admin account via \"%s init-bootstrap-admin\" (random password). The server no longer creates a default admin account.", exeName))
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
					// Neutralise les caractÃ¨res de contrÃ´le (retours Ã  la ligne,
					// tabulationsâ€¦) du Subject du certificat client avant de
					// l'Ã©crire dans le log, pour empÃªcher toute injection de ligne.
					subject := logs.SanitizeLogMessage(clientCert.Subject.String())
					logs.WriteLogFile(fmt.Sprintf("[DSC][TLS] Client certificate received: Subject=%s, Thumbprint=%s", subject, sha1sumLower))
					// VÃ©rification de l'empreinte si la validation du certificat client est activÃ©e
					// On ne fait PAS ce contrÃ´le pour la route de registration DSC (PUT /PSDSCPullServer.svc/{node})
					isRegister := r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/PSDSCPullServer.svc/") && len(strings.Split(r.URL.Path, "/")) == 3
					if global.AppConfig.DSCPullServer.EnableClientCertValidation && !isRegister {
						// VÃ©rifie la prÃ©sence de l'empreinte en base (les empreintes
						// sont stockÃ©es en minuscules, la comparaison reste index-friendly).
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
		dscHandler := logs.LoggingMiddleware(monitor.StatusCodeMiddleware(dscCertLogMiddleware(dscMux)))
		webHandler := logs.LoggingMiddleware(monitor.StatusCodeMiddleware(webMux))

		// Les goroutines de listener reportent leurs erreurs ici au lieu
		// d'appeler os.Exit, afin qu'elles puissent être propagées et qu'un
		// arrêt propre (serveurs, pool DB, flush logs) soit exécuté. Capacité 2
		// pour que chaque serveur puisse émettre son erreur sans bloquer.
		srvErr := make(chan error, 2)

		// --- Serveurs HTTP avec arrÃªt propre (drivÃ© par ctx.Done()) ---
		// Chaque serveur est dÃ©marrÃ© dans une goroutine. Ã€ la rÃ©ception d'un
		// Stop/Shutdown du service Windows (annulation du contexte), runApp
		// dÃ©clenche un Shutdown gracieux des deux serveurs puis retourne, ce qui
		// permet Ã  la couche service de finaliser l'arrÃªt sans tuer les requÃªtes.

		// Serveur DSC (endpoints MS-DSCPM)
		dscReadHeader, dscRead, dscWrite, dscIdle := httpServerTimeouts(global.AppConfig.DSCPullServer)
		dscServer := &http.Server{
			Addr:              fmt.Sprintf(":%d", global.AppConfig.DSCPullServer.Port),
			Handler:           dscHandler,
			ReadHeaderTimeout: dscReadHeader,
			ReadTimeout:       dscRead,
			WriteTimeout:      dscWrite,
			IdleTimeout:       dscIdle,
		}
		if global.AppConfig.DSCPullServer.EnableHTTPS {
			certFile, keyFile := resolveCertKeyPath(global.AppConfig.DSCPullServer.CertFile, global.AppConfig.DSCPullServer.KeyFile)
			// Bypass CA verification: accepter tout certificat client prÃ©sentÃ©
			tlsConfig := &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
			// VÃ©rifie si la validation du certificat client est activÃ©e.
			if global.AppConfig.DSCPullServer.EnableClientCertValidation {
				// Toujours exiger un certificat client : le middleware d'empreinte
				// (enrÃ´lement du nÅ“ud) ne s'exÃ©cute que si un certificat est prÃ©sentÃ©.
				// bypass_ca_validation ne conditionne que la validation de la chaÃ®ne CA,
				// pas l'exigence d'un certificat.
				if global.AppConfig.DSCPullServer.BypassCAValidation {
					// Aucune validation de la signature/CA : seuls l'empreinte du
					// certificat (enrÃ´lement du nÅ“ud) compte. ModÃ¨le DSC avec des
					// certificats potentiellement auto-signÃ©s.
					tlsConfig.ClientAuth = tls.RequireAnyClientCert
				} else {
					// Validation de la chaÃ®ne/signature contre le magasin d'autoritÃ©s
					// du systÃ¨me, en plus du contrÃ´le d'empreinte (enrÃ´lement).
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
					srvErr <- err
				}
			}()
		} else {
			logs.WriteLogFile(fmt.Sprintf("WARN [DSC Core Server] DSC Server is listening in HTTP (no TLS) on :%d ...", global.AppConfig.DSCPullServer.Port))
			go func() {
				if err := dscServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUi] Error starting HTTP server: %v", err))
					srvErr <- err
				}
			}()
		}

		// Serveur WebUI / API
		webReadHeader, webRead, webWrite, webIdle := httpServerTimeouts(global.AppConfig.WebUI)
		webServer := &http.Server{
			Addr:              fmt.Sprintf(":%d", global.AppConfig.WebUI.Port),
			Handler:           webHandler,
			ReadHeaderTimeout: webReadHeader,
			ReadTimeout:       webRead,
			WriteTimeout:      webWrite,
			IdleTimeout:       webIdle,
		}
		if global.AppConfig.WebUI.EnableHTTPS {
			certFile, keyFile := resolveCertKeyPath(global.AppConfig.WebUI.CertFile, global.AppConfig.WebUI.KeyFile)
			go func() {
				logs.WriteLogFile(fmt.Sprintf("INFO [DSC WebUI] UI/API (HTTPS) on :%d ... (UI on /web/)", global.AppConfig.WebUI.Port))
				if err := webServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUI] Error starting HTTPS server: %v", err))
					srvErr <- err
				}
			}()
		} else {
			logs.WriteLogFile(fmt.Sprintf("WARN [DSC WebUI] UI/API is listening in HTTP (no TLS) on :%d ... (UI on /web/)", global.AppConfig.WebUI.Port))
			logs.WriteLogFile("WARN [SECURITY] The Web UI / API is served over plain HTTP. Credentials and JWT tokens transit unencrypted. Use HTTPS (enable_https: true) in production.")
			go func() {
				if err := webServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logs.WriteLogFile(fmt.Sprintf("ERROR [DSC WebUI] Error starting HTTP server: %v", err))
					srvErr <- err
				}
			}()
		}

		// Bloque jusqu'Ã  l'arrÃªt, puis ferme gracieusement les deux serveurs.
		var runErr error
		select {
		case <-ctx.Done():
			runErr = nil
		case err := <-srvErr:
			logs.WriteLogFile(fmt.Sprintf("ERROR [SERVER] A listener failed, shutting down: %v", err))
			runErr = err
		}
		// Signale aux consommateurs (GET /health) que le processus s'arrÃªte :
		// pendant la fenÃªtre de shutdown gracieux, workers.alive passe Ã  false.
		monitor.SetShutdown()
		logs.WriteLogFile("INFO [SHUTDOWN] Shutdown requested, closing HTTP servers ...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = dscServer.Shutdown(shutdownCtx)
		_ = webServer.Shutdown(shutdownCtx)
		logs.WriteLogFile("INFO [SHUTDOWN] HTTP servers stopped cleanly.")
		return runErr
	}

	if runtime.GOOS == "windows" {
		err := service.StartWindowsService(runApp)
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [SERVICE] %v", err))
			os.Exit(1)
		}
	} else {
		// ArrÃªt gracieux sur SIGINT/SIGTERM (Docker, systemd, Ctrl-C) : le
		// contexte est annulÃ© Ã  la rÃ©ception du signal, ce qui dÃ©clenche le
		// chemin de shutdown propre (http.Server.Shutdown + arrÃªt des workers
		// planifiÃ©s). Sans cela, le processus est tuÃ© sans fermer proprement
		// les serveurs HTTP ni laisser les requÃªtes en vol se terminer.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runApp(ctx); err != nil {
			logs.WriteLogFile(fmt.Sprintf("ERROR [APP] %v", err))
			os.Exit(1)
		}
	}
}
