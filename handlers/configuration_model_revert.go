package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// ConfigurationModelRevertHandler replace la configuration active par une
// version antérieure, en s'appuyant sur le modèle de versioning existant :
//   - la version active a name == original_name (base) ;
//   - les versions antérieures ont original_name == base mais un name versionné
//     (base-<horodatage>).
//
// Revenir à une version consiste à :
//  1. renommer la version active actuelle en version antérieure (base-<ts>) ;
//  2. promouvoir la version cible en version active (name = base, original_name
//     = base), avec previous_id pointant sur l'ancienne active.
//
// Les nœuds liés à la configuration (via agent_configurations.configuration_name
// = base) sont marqués needs_recompile=1 / state='pending_apply' pour être
// réappliqués au prochain cycle.
//
//	POST /api/v1/configuration_models/{id}/revert
func ConfigurationModelRevertHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}

	dbConn, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	target, err := db.GetConfigurationModel(dbConn, id)
	if err != nil || target == nil || target.OriginalName == nil {
		http.Error(w, "Configuration not found", http.StatusNotFound)
		return
	}

	base := *target.OriginalName
	if target.Name == base {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "already_active", "name": base})
		return
	}

	// Identifier la version active actuelle (name == base, original_name == base).
	var activeID int64
	var lookupErr error
	if isMSSQLDriver(global.AppConfig.Database.Driver) {
		lookupErr = dbConn.QueryRow("SELECT TOP 1 id FROM configuration_model WHERE original_name = ? AND name = ? ORDER BY upload_date DESC", base, base).Scan(&activeID)
	} else {
		lookupErr = dbConn.QueryRow("SELECT id FROM configuration_model WHERE original_name = ? AND name = ? ORDER BY upload_date DESC LIMIT 1", base, base).Scan(&activeID)
	}
	if lookupErr != nil {
		http.Error(w, "No active configuration found", http.StatusNotFound)
		return
	}
	if activeID == id {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "already_active", "name": base})
		return
	}

	// 1. Renommer l'active actuelle en version antérieure.
	versionName := base + "-" + time.Now().UTC().Format("20060102150405")
	if _, err := dbConn.Exec(`UPDATE configuration_model SET name = ? WHERE id = ?`, versionName, activeID); err != nil {
		http.Error(w, "DB rename error", http.StatusInternalServerError)
		return
	}

	// 2. Promouvoir la version cible en version active.
	if _, err := dbConn.Exec(`UPDATE configuration_model SET name = ?, original_name = ?, previous_id = ? WHERE id = ?`, base, base, activeID, id); err != nil {
		http.Error(w, "DB revert error", http.StatusInternalServerError)
		return
	}

	// 3. Marquer les nœuds liés pour réapplication.
	_, _ = dbConn.Exec(`UPDATE agents SET needs_recompile = 1, state = 'pending_apply' WHERE agent_id IN (SELECT agent_id FROM agent_configurations WHERE configuration_name = ?)`, base)

	// 4. Audit.
	uploadedBy := "?"
	if r.Context().Value("userId") != nil {
		if sub, ok := r.Context().Value("userId").(string); ok && sub != "" {
			uploadedBy = sub
		}
	}
	_ = db.InsertAudit(dbConn, global.AppConfig.Database.Driver, uploadedBy, "revert", "configuration_model", "Reverted configuration to version: "+base, strconv.FormatInt(id, 10))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "reverted",
		"id":     id,
		"name":   base,
	})
}
