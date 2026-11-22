// GET /api/v1/modules/{name}
// GET /api/v1/modules/{name}?latest=1
package handlers

import (
	"encoding/json"
	"database/sql"
	"net/http"
	"log"
	"strings"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

func GetModuleVersionHandler(w http.ResponseWriter, r *http.Request) {
	dbConn := db.Shared()

	// Récupère le nom du module depuis l'URL
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	name := parts[len(parts)-1]
	// Nettoie le nom du module pour éviter les soucis de caractères parasites
	name = strings.Trim(name, " \"'{}[];:")
	latest := r.URL.Query().Get("latest") == "1"

	       // Si latest=1, retourne la version la plus récente
	       var version string
	       var found bool
	       query := ""
	       if strings.Contains(strings.ToLower(global.AppConfig.Database.Driver), "sqlserver") || strings.Contains(strings.ToLower(global.AppConfig.Database.Driver), "mssql") {
		       query = "SELECT TOP 1 version FROM modules WHERE name = ? ORDER BY version DESC"
	       } else {
		       query = "SELECT version FROM modules WHERE name = ? ORDER BY version DESC LIMIT 1"
	       }
	       if latest {
		       row := dbConn.QueryRow(query, name)
		       err := row.Scan(&version)
		       found = (err == nil)
	       } else if v := r.URL.Query().Get("version"); v != "" {
		       row := dbConn.QueryRow("SELECT version FROM modules WHERE name = ? AND version = ?", name, v)
		       err := row.Scan(&version)
		       found = (err == nil)
	       } else {
		       row := dbConn.QueryRow(query, name)
		       err := row.Scan(&version)
		       found = (err == nil)
	       }
	log.Printf("[GetModuleVersionHandler] name=%s latest=%v found=%v version=%s", name, latest, found, version);
	resp := map[string]interface{}{
		"available": found,
		"version": version,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}


type ModuleInfo struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Checksum   string `json:"checksum"`
	UploadedAt string `json:"uploaded_at"`
}

func ModuleListHandler(db *sql.DB) http.HandlerFunc {
	       return func(w http.ResponseWriter, r *http.Request) {
		       // Support de l'option ?count=1
		       if r.URL.Query().Get("count") == "1" {
			       var count int
			       err := db.QueryRow("SELECT COUNT(*) FROM modules").Scan(&count)
			       if err != nil {
				       w.WriteHeader(http.StatusInternalServerError)
				       w.Write([]byte("DB error"))
				       return
			       }
			       w.Header().Set("Content-Type", "application/json")
			       json.NewEncoder(w).Encode(map[string]int{"count": count})
			       return
		       }
		       rows, err := db.Query(`SELECT id, name, version, checksum, uploaded_at FROM modules ORDER BY uploaded_at DESC`)
		       if err != nil {
			       w.WriteHeader(http.StatusInternalServerError)
			       w.Write([]byte("DB error"))
			       return
		       }
		       defer rows.Close()
		       var modules []ModuleInfo
		       for rows.Next() {
			       var m ModuleInfo
			       if err := rows.Scan(&m.ID, &m.Name, &m.Version, &m.Checksum, &m.UploadedAt); err != nil {
				       continue
			       }
			       modules = append(modules, m)
		       }
		       w.Header().Set("Content-Type", "application/json")
		       json.NewEncoder(w).Encode(modules)
	       }
}
