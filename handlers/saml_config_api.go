package handlers

import (
	"encoding/json"
	"go-dsc-pull/internal"
	internaldb "go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
	"io/ioutil"
	"net/http"
)

// SAMLConfigAPIHandler handles GET and PUT for SAML config
func SAMLConfigAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	configPath, err := internal.ConfigFilePath("config.json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to resolve config path"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg := global.AppConfig
		if cfg == nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to load config"})
			return
		}
		json.NewEncoder(w).Encode(cfg.SAML)
	case http.MethodPut:
		// Only allow admin (add your own middleware/check here)
		var newSAML schema.SAMLConfig
		if err := json.NewDecoder(r.Body).Decode(&newSAML); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
			return
		}
		// Charger le fichier de config existant
		fileData, err := ioutil.ReadFile(configPath)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read config file"})
			return
		}
		var fullConfig map[string]interface{}
		if err := json.Unmarshal(fileData, &fullConfig); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to decode config"})
			return
		}
		// Overwrite SAML config
		fullConfig["saml"] = newSAML
		// Write back to file
		data, err := json.MarshalIndent(fullConfig, "", "  ")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to marshal config"})
			return
		}
		if err := ioutil.WriteFile(configPath, data, 0644); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to write config"})
			return
		}
		// Met à jour la config globale en mémoire (hot reload)
		global.AppConfig.SAML = newSAML

		// Audit modification SAML config (ouvre une connexion DB temporaire)
		driverName := global.AppConfig.Database.Driver
		user := "?"
		if r.Context().Value("userId") != nil {
			if sub, ok := r.Context().Value("userId").(string); ok {
				user = sub
			}
		}
		dbConn := internaldb.Shared()
		_ = internaldb.InsertAudit(dbConn, driverName, user, "update", "saml_config", "Updated SAML configuration", "")

		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
