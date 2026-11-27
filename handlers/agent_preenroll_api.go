package handlers

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/schema"
	"log"
	"net/http"
	"strings"
)

// ptr retourne un pointeur sur une chaîne (utilitaire)
func ptr(s string) *string { return &s }

// PreEnrollAgentHandler allows pre-registration of an agent with only NodeName
func PreEnrollAgentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	// Pre-enrollment can supply tags (incl. environment), so it is a
	// privileged operation reserved to admins. Enforcement lives in the API
	// itself (not only the UI): the route is wrapped by adminOnly and we also
	// check auth.IsAdmin here as a defensive backstop.
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	var req struct {
		NodeName string `json:"node_name"`
		Tags     []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NodeName == "" {
		http.Error(w, "Invalid or missing node_name", http.StatusBadRequest)
		return
	}
	// Generate a temporary AgentId (SHA1 of NodeName + 'TEMP')
	h := sha1.New()
	h.Write([]byte(req.NodeName + "TEMP"))
	tempAgentId := fmt.Sprintf("TEMP-%x", h.Sum(nil)[:8])

	var err error

	// Insert agent (ignore if already exists), compatible SQLite/MSSQL
	driver := global.AppConfig.Database.Driver
	if driver == "sqlite" {
		_, err = database.Exec(`INSERT OR IGNORE INTO agents (agent_id, node_name, last_communication, state) VALUES (?, ?, ?, ?)`, tempAgentId, req.NodeName, "0000-00-01 00:00:00", "waiting_for_registration")
	} else if driver == "mssql" {
		// MSSQL : insérer avec la date minimale valide
		_, err = database.Exec(`IF NOT EXISTS (SELECT 1 FROM agents WHERE agent_id = ?) INSERT INTO agents (agent_id, node_name, last_communication, state) VALUES (?, ?, ?, ?)`, tempAgentId, tempAgentId, req.NodeName, "1753-01-01 00:00:00", "waiting_for_registration")
	} else {
		// fallback générique
		_, err = database.Exec(`INSERT INTO agents (agent_id, node_name) VALUES (?, ?)`, tempAgentId, req.NodeName)
	}
	if err != nil {
		log.Printf("[API][DB] Error inserting agent: %v", err)
		_ = logs.WriteLogFile(fmt.Sprintf("ERROR [API][PRE-ENROLL] Error inserting agent: %v (NodeName=%s)", err, req.NodeName))
		http.Error(w, "DB insert error", http.StatusInternalServerError)
		return
	}

	_ = logs.WriteLogFile(fmt.Sprintf("INFO [API][PRE-ENROLL] Agent pre-enrolled: NodeName=%s, AgentId=%s", req.NodeName, tempAgentId))

	// Pose les tags fournis au pré-enregistrement (la compilation initiale en a besoin).
	for _, tag := range req.Tags {
		if tag.Key == "" || tag.Value == "" {
			continue
		}
		if err := db.SetAgentTag(database, global.AppConfig.Database.Driver, tempAgentId, tag.Key, tag.Value); err != nil {
			_ = logs.WriteLogFile(fmt.Sprintf("ERROR [API][PRE-ENROLL] Error applying tag %s=%s: %v", tag.Key, tag.Value, err))
		}
	}

	// Audit pre-enroll (actor + source IP) and list the tags that were
	// applied at enrollment, with emphasis on the environment tag.
	driverName := global.AppConfig.Database.Driver
	actor := currentUserEmail(r, database)
	ip := auth.ClientIP(r, global.AppConfig.WebUI.TrustedProxies)
	var tagSummary []string
	for _, tag := range req.Tags {
		if tag.Key == "" || tag.Value == "" {
			continue
		}
		tagSummary = append(tagSummary, tag.Key+"="+tag.Value)
	}
	details := "Pre-enrolled agent: " + tempAgentId + " (" + req.NodeName + ")"
	if len(tagSummary) > 0 {
		details += " tags=[" + strings.Join(tagSummary, ", ") + "]"
	}
	_ = db.InsertAudit(database, driverName, actor, "preenroll", "agent", details, ip)

	// Compilation initiale : en async (le nœud ne registrera que bien plus tard),
	// la config sera prête avant le premier pull de l'agent.
	triggerNodeRecompileAsync(tempAgentId, req.NodeName)

	agent := schema.Agent{
		AgentId:           tempAgentId,
		NodeName:          req.NodeName,
		LastCommunication: "0000-00-01 00:00:00",
		State:             ptr("waiting_for_registration"),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agent)
}
