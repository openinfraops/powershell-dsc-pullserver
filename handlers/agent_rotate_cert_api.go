package handlers

import (
	"encoding/json"
	"net/http"

	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
)

// AgentRotateCertificateHandler force le renouvellement du certificat d'un nœud
// « ASAP » : il lève le drapeau agents.force_cert_rotation, ce qui fait émettre
// l'en-tête DSC-certificateRotation: True au prochain pull du nœud (même hors
// fenêtre de renouvellement et même si l'activation globale en base est off —
// la demande explicite d'un admin prime).
//
// POST  -> lève le drapeau (demander le renouvellement).
// DELETE -> retire le drapeau (annuler la demande).
//
// Réservé aux admins. Le drapeau est levé automatiquement quand le nœud a
// effectivement renouvelé (CertificateRotation ou ré-enregistrement).
func AgentRotateCertificateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	agentId := r.PathValue("id")
	if agentId == "" {
		http.Error(w, "AgentId manquant", http.StatusBadRequest)
		return
	}
	database := db.Shared()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}

	value := 0
	if r.Method == http.MethodPost {
		value = 1
	}
	result, err := database.Exec(`UPDATE agents SET force_cert_rotation = ? WHERE agent_id = ?`, value, agentId)
	if err != nil {
		http.Error(w, "Failed to update certificate rotation flag", http.StatusInternalServerError)
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		http.Error(w, "Agent non trouvé", http.StatusNotFound)
		return
	}

	status := "renewal_requested"
	if value == 0 {
		status = "renewal_cancelled"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": status, "agent_id": agentId, "force_cert_rotation": value == 1})
}
