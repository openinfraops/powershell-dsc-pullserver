package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-dsc-pull/internal/db"
)

// ConfigurationModelDiffHandler returns the decoded MOF text of two versions of
// a configuration so the client can render a side-by-side diff.
//
//	GET /api/v1/configuration_models/{id}/diff?other={otherID}
//
// Both identifiers are configuration_model ids. The MOF blobs are decoded to
// UTF-8 text (handling UTF-8/UTF-16 BOM and inline encodings) via
// decodeMOFText, then returned as `left` and `right` so the comparison can be
// computed client-side without exposing encoding concerns to the UI.
func ConfigurationModelDiffHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.PathValue("id")
	otherStr := r.URL.Query().Get("other")
	if idStr == "" || otherStr == "" {
		http.Error(w, "id and other parameters are required", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return
	}
	otherID, err := strconv.ParseInt(otherStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid other parameter", http.StatusBadRequest)
		return
	}

	dbConn := db.Shared()

	cm, err := db.GetConfigurationModel(dbConn, id)
	if err != nil {
		http.Error(w, "Configuration not found", http.StatusNotFound)
		return
	}
	other, err := db.GetConfigurationModel(dbConn, otherID)
	if err != nil {
		http.Error(w, "Other configuration not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         cm.ID,
		"name":       cm.Name,
		"other_id":   other.ID,
		"other_name": other.Name,
		"left":       decodeMOFText(cm.MofFile),
		"right":      decodeMOFText(other.MofFile),
	})
}
