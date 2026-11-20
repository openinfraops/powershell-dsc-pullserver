package handlers

import (
	"encoding/json"
	"net/http"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// AgentTagsOptionsHandler retourne les clés de tag distinctes présentes sur les
// nœuds, pour alimenter le filtre/sélecteur de tags de la page Nodes.
//
//	GET /api/v1/agents/tags/options
func AgentTagsOptionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	database, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer database.Close()

	rows, err := database.Query(`SELECT DISTINCT tag_key FROM agent_tags WHERE tag_key IS NOT NULL AND tag_key <> '' ORDER BY tag_key`)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	keys := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err == nil {
			keys = append(keys, k)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": keys})
}
