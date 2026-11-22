package handlers

import (
	"encoding/json"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/schema"
	"net/http"
)

// AgentByIdAPIHandler retourne les infos d'un agent donné
func AgentByIdAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}
	database := db.Shared()

	row := database.QueryRow(`SELECT agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, registered_at, last_communication, has_error_last_report, needs_recompile, state, deleted_at, deleted_by FROM agents WHERE agent_id = ?`, agentId)
	var a schema.Agent
	var hasErrorBool bool
	var needsRecompileBool bool
	var state, deletedAt, deletedBy *string
	err := row.Scan(&a.AgentId, &a.NodeName, &a.LCMVersion, &a.RegistrationType, &a.CertificateThumbprint, &a.CertificateSubject, &a.CertificateIssuer, &a.CertificateNotBefore, &a.CertificateNotAfter, &a.RegisteredAt, &a.LastCommunication, &hasErrorBool, &needsRecompileBool, &state, &deletedAt, &deletedBy)
	a.HasErrorLastReport = hasErrorBool
	a.NeedsRecompile = needsRecompileBool
	a.State = state
	a.DeletedAt = deletedAt
	a.DeletedBy = deletedBy
	if err != nil {
		http.Error(w, "Agent non trouvé", http.StatusNotFound)
		return
	}
	// Ajoute les configurations associées
	configs, err := db.GetAgentConfigurations(database, agentId)
	if err == nil {
		a.Configurations = configs
	}
	tags, err := db.GetAgentTags(database, agentId)
	if err == nil && len(tags) > 0 {
		a.Tags = tags
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a)
}
