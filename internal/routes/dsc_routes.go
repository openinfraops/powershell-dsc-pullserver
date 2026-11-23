package routes

import (
	"net/http"
	"go-dsc-pull/handlers"
	"go-dsc-pull/utils"
	"database/sql"
	"strings"
	"log"
)

// RegisterDSCRoutes sets up all DSC (.svc) endpoints on the provided mux
func RegisterDSCRoutes(mux *http.ServeMux, dbConn *sql.DB) {
	// Strict MS-DSCPM registration route: PUT only.
	mux.HandleFunc("PUT /PSDSCPullServer.svc/{node}", handlers.RegisterHandler)
	mux.HandleFunc("GET /PSDSCPullServer.svc/{node}/{config}/ConfigurationContent", handlers.ConfigurationContentHandler)
	mux.HandleFunc("POST /PSDSCPullServer.svc/{node}/SendReport", handlers.SendReportHandler)
	mux.HandleFunc("POST /PSDSCPullServer.svc/{node}/CertificateRotation", handlers.CertificateRotationHandler)
	mux.HandleFunc("GET /PSDSCPullServer.svc/{node}/{reports}", handlers.GetReportsHandler)
	mux.HandleFunc("POST /PSDSCPullServer.svc/{id}/GetDscAction", func(w http.ResponseWriter, r *http.Request) {
		raw := r.PathValue("id")
		agentId := utils.ExtractAgentId(raw)
		log.Printf("[GETDSCACTION-ROUTE] rawId=%q extractedAgentId=%q", raw, agentId)
		handlers.GetDscActionNodeHandlerWithId(w, r, agentId)
	})
	mux.HandleFunc("GET /PSDSCPullServer.svc/{module}/ModuleContent", func(w http.ResponseWriter, r *http.Request) {
		moduleSeg := r.PathValue("module")
		var name, version string
		if len(moduleSeg) > 9 && moduleSeg[:8] == "Modules(" && moduleSeg[len(moduleSeg)-1:] == ")" {
			inner := moduleSeg[8 : len(moduleSeg)-1]
			parts := strings.Split(inner, ",")
			for _, part := range parts {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 {
					k := strings.TrimSpace(kv[0])
					v := strings.Trim(kv[1], "'\"")
					if k == "ModuleName" {
						name = v
					} else if k == "ModuleVersion" {
						version = v
					}
				}
			}
		}
		log.Printf("[MODULECONTENT] Agent request ModuleName=%s, ModuleVersion=%s", name, version)
		checksum := r.URL.Query().Get("Checksum")
		q := r.URL.Query()
		q.Set("ModuleName", name)
		q.Set("ModuleVersion", version)
		if checksum != "" { q.Set("Checksum", checksum) }
		r.URL.RawQuery = q.Encode()
		handlers.ModuleDownloadHandler(dbConn)(w, r)
	})
}
