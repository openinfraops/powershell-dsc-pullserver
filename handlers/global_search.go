package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// searchAgentItem est un résultat de recherche côté nœuds.
type searchAgentItem struct {
	AgentID               string `json:"agent_id"`
	NodeName              string `json:"node_name"`
	State                 string `json:"state"`
	CertificateThumbprint string `json:"certificate_thumbprint"`
}

// searchConfigItem est un résultat de recherche côté configurations (version active uniquement).
type searchConfigItem struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	UploadedBy   string `json:"uploaded_by"`
}

// searchModuleItem est un résultat de recherche côté modules.
type searchModuleItem struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// searchResponse regroupe les résultats de la recherche globale.
type searchResponse struct {
	Agents         []searchAgentItem  `json:"agents"`
	Configurations []searchConfigItem `json:"configurations"`
	Modules        []searchModuleItem `json:"modules"`
}

// maxSearchResults limite le nombre de résultats renvoyés par catégorie.
const maxSearchResults = 10

// GlobalSearchHandler effectue une recherche transversale dans les nœuds, les
// configurations et les modules.
//
//	GET /api/v1/search?q=x
func GlobalSearchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(searchResponse{
			Agents:         []searchAgentItem{},
			Configurations: []searchConfigItem{},
			Modules:        []searchModuleItem{},
		})
		return
	}

	database, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer database.Close()

	pattern := "%" + q + "%"
	resp := searchResponse{
		Agents:         make([]searchAgentItem, 0),
		Configurations: make([]searchConfigItem, 0),
		Modules:        make([]searchModuleItem, 0),
	}

	// Nœuds (actifs uniquement) : nom, id, ou empreinte du certificat.
	agentRows, err := database.Query(
		`SELECT agent_id, COALESCE(node_name, ''), COALESCE(state, ''), COALESCE(certificate_thumbprint, '')
		 FROM agents
		 WHERE deleted_at IS NULL
		   AND (COALESCE(node_name, '') LIKE ? OR agent_id LIKE ? OR COALESCE(certificate_thumbprint, '') LIKE ?)
		 ORDER BY COALESCE(node_name, '')
		 LIMIT ?`, pattern, pattern, pattern, maxSearchResults)
	if err == nil {
		defer agentRows.Close()
		for agentRows.Next() {
			var a searchAgentItem
			if agentRows.Scan(&a.AgentID, &a.NodeName, &a.State, &a.CertificateThumbprint) == nil {
				resp.Agents = append(resp.Agents, a)
			}
		}
	}

	// Configurations : versions actives uniquement (original_name = name).
	configRows, err := database.Query(
		`SELECT id, name, COALESCE(original_name, ''), COALESCE(uploaded_by, '')
		 FROM configuration_model
		 WHERE original_name = name
		   AND (name LIKE ? OR COALESCE(original_name, '') LIKE ?)
		 ORDER BY name
		 LIMIT ?`, pattern, pattern, maxSearchResults)
	if err == nil {
		defer configRows.Close()
		for configRows.Next() {
			var c searchConfigItem
			if configRows.Scan(&c.ID, &c.Name, &c.OriginalName, &c.UploadedBy) == nil {
				resp.Configurations = append(resp.Configurations, c)
			}
		}
	}

	// Modules : nom.
	moduleRows, err := database.Query(
		`SELECT id, name, COALESCE(version, '') FROM modules WHERE name LIKE ? ORDER BY name LIMIT ?`,
		pattern, maxSearchResults)
	if err == nil {
		defer moduleRows.Close()
		for moduleRows.Next() {
			var m searchModuleItem
			if moduleRows.Scan(&m.ID, &m.Name, &m.Version) == nil {
				resp.Modules = append(resp.Modules, m)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
