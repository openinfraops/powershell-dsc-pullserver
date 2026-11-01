package routes

import (
	"database/sql"
	samlsp "github.com/crewjam/saml/samlsp"
	"go-dsc-pull/handlers"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/middleware"
	"go-dsc-pull/utils"
	"html/template"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

// RegisterWebRoutes sets up all web/API endpoints on the provided mux
func RegisterWebRoutes(mux *http.ServeMux, dbConn *sql.DB, jwtAuthMiddleware func(http.Handler) http.Handler, samlMiddleware http.Handler) {
	// Handler pour la page d'audit admin
	// Admin-only middleware for user management
	adminOnly := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !auth.IsAdmin(r, dbConn) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// Render access denied page (must be defined before use)
	renderDenied := func(w http.ResponseWriter) {
		// Load and render access_denied.tmpl with layout, head, menu, footer
		exeDir, err := utils.ExePath()
		if err != nil {
			http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		baseDir := filepath.Dir(exeDir)
		tmpl, err := template.New("layout.tmpl").
			ParseFiles(
				filepath.Join(baseDir, "templates/layout.tmpl"),
				filepath.Join(baseDir, "templates/head.tmpl"),
				filepath.Join(baseDir, "templates/menu.tmpl"),
				filepath.Join(baseDir, "templates/footer.tmpl"),
				filepath.Join(baseDir, "templates/access_denied.tmpl"),
			)
		if err != nil {
			http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		data := map[string]interface{}{"Title": "Access Denied"}
		err = tmpl.ExecuteTemplate(w, "layout", data)
		if err != nil {
			http.Error(w, "Render error: "+err.Error(), http.StatusInternalServerError)
		}
	}
	// Endpoint pour la liste des profils utilisateurs disponibles
	mux.Handle("GET /api/v1/user_roles", jwtAuthMiddleware(http.HandlerFunc(handlers.UserRolesHandler())))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" {
			http.Redirect(w, r, "/web", http.StatusFound)
			return
		}
		handlers.NotFoundHandler(w, r)
	})
	exeDir, err := utils.ExePath()
	if err != nil {
		panic("Failed to get executable path: " + err.Error())
	}
	baseDir := filepath.Dir(exeDir)
	var samlSPMiddleware *samlsp.Middleware
	if smw, ok := samlMiddleware.(*samlsp.Middleware); ok {
		samlSPMiddleware = smw
	}
	mux.Handle("GET /api/v1/about", jwtAuthMiddleware(http.HandlerFunc(handlers.AboutAPIHandler(dbConn))))
	mux.Handle("GET /api/v1/my", jwtAuthMiddleware(http.HandlerFunc(handlers.MyUserInfoHandler(dbConn))))
	mux.Handle("/web/profile", handlers.WebJWTAuthMiddleware(http.HandlerFunc(handlers.ProfileHandler)))
	// API tokens utilisateur
	mux.Handle("GET /api/v1/users/{id}/tokens", jwtAuthMiddleware(http.HandlerFunc(handlers.ListUserAPITokensHandler(dbConn))))
	mux.Handle("POST /api/v1/users/{id}/tokens", jwtAuthMiddleware(http.HandlerFunc(handlers.CreateUserAPITokenHandler(dbConn))))
	mux.Handle("POST /api/v1/users/{id}/tokens/{tokenid}/revoke", jwtAuthMiddleware(http.HandlerFunc(handlers.RevokeUserAPITokenHandler(dbConn))))
	mux.Handle("DELETE /api/v1/users/{id}/tokens/{tokenid}", jwtAuthMiddleware(http.HandlerFunc(handlers.DeleteUserAPITokenHandler(dbConn))))
	// SAML endpoints
	mux.Handle("GET /api/v1/saml/userinfo", http.HandlerFunc(handlers.SAMLUserInfoHandler))
	mux.Handle("GET /api/v1/saml/enabled", http.HandlerFunc(handlers.SAMLStatusHandler))
	// SAML config management (admin only)
	mux.Handle("GET /api/v1/saml/config", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SAMLConfigAPIHandler))))
	mux.Handle("PUT /api/v1/saml/config", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SAMLConfigAPIHandler))))
	mux.Handle("POST /api/v1/saml/upload_sp_keycert", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SAMLUploadSPKeyCertHandler))))
	mux.Handle("GET /api/v1/scheduler/config", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SchedulerConfigAPIHandler))))
	mux.Handle("PUT /api/v1/scheduler/config", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SchedulerConfigAPIHandler))))
	mux.Handle("GET /api/v1/scheduler/tasks", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SchedulerTasksAPIHandler))))
	mux.Handle("GET /api/v1/scheduler/tasks/{task}/history", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SchedulerTaskHistoryAPIHandler))))
	mux.Handle("POST /api/v1/scheduler/cleanup/run", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SchedulerRunCleanupAPIHandler))))
	mux.Handle("POST /api/v1/scheduler/release-check/run", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SchedulerRunReleaseCheckAPIHandler))))
	mux.Handle("POST /api/v1/scheduler/configuration-repo/run", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SchedulerRunConfigurationRepoAPIHandler))))
	mux.Handle("POST /api/v1/scheduler/logs/cleanup/run", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.SchedulerRunLogCleanupAPIHandler))))
	// Web UI for SAML config (admin only)
	mux.Handle("GET /web/saml_config", handlers.WebJWTAuthMiddleware(middleware.WebAdminOnly(dbConn, renderDenied)(http.HandlerFunc(handlers.WebSAMLConfigHandler))))
	mux.Handle("GET /web/admin/scheduler", handlers.WebJWTAuthMiddleware(middleware.WebAdminOnly(dbConn, renderDenied)(http.HandlerFunc(handlers.WebSchedulerConfigHandler))))
	if samlMiddleware != nil {
		mux.Handle("/saml/", samlMiddleware)
	}
	// API REST endpoints (agents, configs, reports, modules, properties, configuration_models, users, login)
	mux.Handle("GET /api/v1/agents", jwtAuthMiddleware(http.HandlerFunc(handlers.GetAgentsHandler)))
	mux.Handle("POST /api/v1/agents/preenroll", jwtAuthMiddleware(http.HandlerFunc(handlers.PreEnrollAgentHandler)))
	mux.Handle("GET /api/v1/agents/{id}/configs", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentConfigsAPIHandler)))
	mux.Handle("POST /api/v1/agents/{id}/configs", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentConfigsAPIHandlerPostDelete)))
	mux.Handle("DELETE /api/v1/agents/{id}/configs", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentConfigsAPIHandlerPostDelete)))
	mux.Handle("GET /api/v1/agents/{id}/reports", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentReportsListHandler)))
	mux.Handle("GET /api/v1/agents/{id}/reports/latest", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentReportsLatestHandler)))
	mux.Handle("GET /api/v1/agents/{id}/reports/{jobid}", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentReportsByJobIdHandler)))
	mux.Handle("GET /api/v1/agents/{id}", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentByIdAPIHandler)))
	mux.Handle("POST /api/v1/modules/upload", jwtAuthMiddleware(http.HandlerFunc(handlers.ModuleUploadHandler(dbConn))))
	mux.Handle("GET /api/v1/modules", jwtAuthMiddleware(http.HandlerFunc(handlers.ModuleListHandler(dbConn))))
	mux.Handle("DELETE /api/v1/modules/delete", jwtAuthMiddleware(http.HandlerFunc(handlers.ModuleDeleteHandler(dbConn))))
	mux.Handle("GET /api/v1/properties", jwtAuthMiddleware(http.HandlerFunc(handlers.PropertiesListHandler)))
	mux.Handle("POST /api/v1/properties", jwtAuthMiddleware(http.HandlerFunc(handlers.PropertiesCreateHandler)))
	mux.Handle("GET /api/v1/properties/{id}", jwtAuthMiddleware(http.HandlerFunc(handlers.PropertiesGetHandler)))
	mux.Handle("PUT /api/v1/properties/{id}", jwtAuthMiddleware(http.HandlerFunc(handlers.PropertiesUpdateHandler)))
	mux.Handle("DELETE /api/v1/properties/{id}", jwtAuthMiddleware(http.HandlerFunc(handlers.PropertiesDeleteHandler)))
	mux.Handle("POST /api/v1/configuration_models", jwtAuthMiddleware(http.HandlerFunc(handlers.CreateConfigurationModelHandler)))
	mux.Handle("GET /api/v1/configuration_models", jwtAuthMiddleware(http.HandlerFunc(handlers.ListConfigurationModelsHandler)))
	mux.Handle("GET /api/v1/configuration_models/{id}", jwtAuthMiddleware(http.HandlerFunc(handlers.GetConfigurationModelHandler)))
	mux.Handle("PUT /api/v1/configuration_models/{id}", jwtAuthMiddleware(http.HandlerFunc(handlers.UpdateConfigurationModelHandler)))
	mux.Handle("DELETE /api/v1/configuration_models", jwtAuthMiddleware(http.HandlerFunc(handlers.DeleteConfigurationModelHandler)))
	mux.Handle("POST /api/v1/login", handlers.LoginHandler(dbConn))
	mux.Handle("POST /api/v1/logout", handlers.LogoutHandler(samlSPMiddleware))
	mux.HandleFunc("/web/login", WebLoginHandler)
	if smw, ok := samlMiddleware.(*samlsp.Middleware); ok {
		mux.Handle("/web/login/saml", smw.RequireAccount(handlers.SAMLLoginHandler(dbConn)))
		mux.HandleFunc("/saml/login", func(w http.ResponseWriter, r *http.Request) { smw.ServeHTTP(w, r) })
		mux.HandleFunc("/saml/acs", func(w http.ResponseWriter, r *http.Request) { smw.ServeHTTP(w, r) })
		mux.HandleFunc("/saml/metadata", func(w http.ResponseWriter, r *http.Request) { smw.ServeHTTP(w, r) })
	}

	webStatic := http.FileServer(http.Dir(filepath.Join(baseDir, "web")))

	// Web GUI endpoints (login, index, static, node, modules, configuration_model, properties, users, user_edit, user_password)
	mux.Handle("/web", handlers.WebJWTAuthMiddleware(http.HandlerFunc(handlers.WebIndexHandler)))
	// Custom static file handler to prevent directory listing under /web/
	mux.Handle("/web/", http.StripPrefix("/web/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath := strings.TrimSpace(r.URL.Path)
		if requestedPath == "" || requestedPath == "/" {
			handlers.WebJWTAuthMiddleware(http.HandlerFunc(handlers.WebIndexHandler)).ServeHTTP(w, r)
			return
		}

		cleanPath := path.Clean("/" + requestedPath)
		relativePath := strings.TrimPrefix(cleanPath, "/")
		if relativePath == "" || relativePath == "." || strings.Contains(relativePath, "..") || strings.HasSuffix(relativePath, "/") {
			handlers.NotFoundHandler(w, r)
			return
		}
		// Static assets are file resources. Reject extension-less paths to avoid directory probing.
		if path.Ext(relativePath) == "" {
			handlers.NotFoundHandler(w, r)
			return
		}

		r.URL.Path = "/" + relativePath
		webStatic.ServeHTTP(w, r)
	})))
	mux.Handle("/web/node/", handlers.WebJWTAuthMiddleware(http.HandlerFunc(handlers.WebNodeHandler)))
	mux.Handle("/web/modules", handlers.WebJWTAuthMiddleware(http.HandlerFunc(handlers.WebModulesHandler)))
	mux.Handle("/web/configuration_model", handlers.WebJWTAuthMiddleware(http.HandlerFunc(handlers.WebConfigurationModelHandler)))
	mux.Handle("/templates/properties.tmpl", handlers.WebJWTAuthMiddleware(http.HandlerFunc(handlers.WebPropertiesHandler)))
	mux.Handle("/web/users", handlers.WebJWTAuthMiddleware(middleware.WebAdminOnly(dbConn, renderDenied)(http.HandlerFunc(WebUsersHandler))))
	mux.Handle("/web/user_edit", handlers.WebJWTAuthMiddleware(middleware.WebAdminOnly(dbConn, renderDenied)(http.HandlerFunc(WebUserEditHandler))))
	mux.Handle("/web/user_password", handlers.WebJWTAuthMiddleware(middleware.WebAdminOnly(dbConn, renderDenied)(http.HandlerFunc(WebUserPasswordHandler))))
	mux.Handle("POST /api/v1/users/{id}/password", jwtAuthMiddleware(http.HandlerFunc(handlers.ChangeUserPasswordHandler(dbConn))))
	mux.Handle("GET /api/v1/users", jwtAuthMiddleware(http.HandlerFunc(handlers.ListUsersHandler(dbConn))))
	mux.Handle("GET /api/v1/users/{id}", jwtAuthMiddleware(http.HandlerFunc(handlers.GetUserHandler(dbConn))))
	mux.Handle("POST /api/v1/users", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.CreateUserHandler(dbConn)))))
	mux.Handle("PUT /api/v1/users/{id}", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.UpdateUserHandler(dbConn)))))
	mux.Handle("DELETE /api/v1/users/{id}", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.DeleteUserHandler(dbConn)))))
	mux.Handle("POST /api/v1/users/{id}/active", jwtAuthMiddleware(http.HandlerFunc(handlers.SetUserActiveHandler(dbConn))))
	// API tags agents
	mux.Handle("GET /api/v1/agents/{id}/tags", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentTagsListHandler)))
	mux.Handle("PUT /api/v1/agents/{id}/tags", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentTagsSetHandler)))
	mux.Handle("DELETE /api/v1/agents/{id}/tags", jwtAuthMiddleware(http.HandlerFunc(handlers.AgentTagsDeleteHandler)))
	// Détail configuration (API et Web)
	mux.Handle("GET /api/v1/configuration_models/{id}/detail", jwtAuthMiddleware(http.HandlerFunc(handlers.GetConfigurationModelDetailHandler)))
	mux.Handle("GET /api/v1/configuration_models/{id}/download", jwtAuthMiddleware(http.HandlerFunc(handlers.DownloadConfigurationModelHandler)))
	mux.Handle("/web/configuration_model/", handlers.WebJWTAuthMiddleware(http.HandlerFunc(handlers.WebConfigurationModelDetailHandler)))
	mux.Handle("GET /api/v1/modules/{name}", jwtAuthMiddleware(http.HandlerFunc(handlers.GetModuleVersionHandler)))
	mux.Handle("DELETE /api/v1/agents/{id}", jwtAuthMiddleware(http.HandlerFunc(handlers.DeleteNodeHandler)))
	mux.Handle("/web/admin/audit", handlers.WebJWTAuthMiddleware(middleware.WebAdminOnly(dbConn, renderDenied)(http.HandlerFunc(handlers.WebAuditHandler))))
	mux.Handle("GET /api/v1/audit", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.AuditListHandler))))
	mux.Handle("GET /api/v1/audit/filters", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.AuditFilterOptionsHandler))))
	mux.Handle("GET /api/v1/audit/export", jwtAuthMiddleware(adminOnly(http.HandlerFunc(handlers.AuditExportCSVHandler))))
}
