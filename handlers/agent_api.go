package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"

)

// GET /api/v1/agents
func GetAgentsHandler(w http.ResponseWriter, r *http.Request) {
	database, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		log.Printf("[API][DB] Erreur ouverture DB: %v", err)
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer database.Close()

	// Support de l'option ?stats (avec ou sans valeur), ex: /api/v1/agents?stats
	if _, statsRequested := r.URL.Query()["stats"]; statsRequested {
		var compliant, failed, pendingEnroll, pendingApply int
		err := database.QueryRow(`
			SELECT
				SUM(CASE WHEN LOWER(COALESCE(state, '')) IN ('success', 'ok') THEN 1 ELSE 0 END) AS compliant,
				SUM(CASE WHEN LOWER(COALESCE(state, '')) = 'failure' THEN 1 ELSE 0 END) AS failed,
				SUM(CASE WHEN LOWER(COALESCE(state, '')) = 'waiting_for_registration' THEN 1 ELSE 0 END) AS pending_enroll,
				SUM(CASE WHEN LOWER(COALESCE(state, '')) = 'pending_apply' THEN 1 ELSE 0 END) AS pending_apply
			FROM agents`).Scan(&compliant, &failed, &pendingEnroll, &pendingApply)
		if err != nil {
			http.Error(w, "DB stats error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{
			"compliant":      compliant,
			"failed":         failed,
			"pending_enroll": pendingEnroll,
			"pending_apply":  pendingApply,
		})
		return
	}

	       // Support de l'option ?count=1
	       if r.URL.Query().Get("count") == "1" {
		       var count int
		       err := database.QueryRow("SELECT COUNT(*) FROM agents").Scan(&count)
		       if err != nil {
			       http.Error(w, "DB count error", http.StatusInternalServerError)
			       return
		       }
		       w.Header().Set("Content-Type", "application/json")
		       _ = json.NewEncoder(w).Encode(map[string]int{"count": count})
		       return
	       }

		       // Filtrage dynamique
		       q := `SELECT agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, registered_at, last_communication, has_error_last_report, needs_recompile, state FROM agents WHERE 1=1`
		       args := []interface{}{}
		       nodeName := r.URL.Query().Get("node_name")
		       if nodeName != "" {
			       q += " AND node_name = ?"
			       args = append(args, nodeName)
		       }
		       hasError := r.URL.Query().Get("has_error_last_report")
		       if hasError == "true" {
			       q += " AND has_error_last_report = 1"
		       } else if hasError == "false" {
			       q += " AND has_error_last_report = 0"
		       }
		       for _, rawTag := range r.URL.Query()["tag"] {
			       tagKey, tagValue, found := strings.Cut(rawTag, ":")
			       if !found || tagKey == "" || tagValue == "" {
				       http.Error(w, "Invalid tag filter, expected tag=key:value", http.StatusBadRequest)
				       return
			       }
			       q += " AND EXISTS (SELECT 1 FROM agent_tags at WHERE at.agent_id = agents.agent_id AND at.tag_key = ? AND at.tag_value = ?)"
			       args = append(args, tagKey, tagValue)
		       }
		       rows, err := database.Query(q, args...)
		       if err != nil {
			       http.Error(w, "DB query error", http.StatusInternalServerError)
			       return
		       }
		       defer rows.Close()

			   agents := []schema.Agent{}
			   for rows.Next() {
				   var a schema.Agent
				   var lcmVersion, registrationType, certificateThumbprint, certificateSubject, certificateIssuer, certificateNotBefore, certificateNotAfter, registeredAt, lastCommunication, state *string
				   var hasErrorBool bool
				   var needsRecompileBool bool
				   if err := rows.Scan(&a.AgentId, &a.NodeName, &lcmVersion, &registrationType, &certificateThumbprint, &certificateSubject, &certificateIssuer, &certificateNotBefore, &certificateNotAfter, &registeredAt, &lastCommunication, &hasErrorBool, &needsRecompileBool, &state); err == nil {
					   // Pour DataTables, renvoyer les champs attendus même vides
					   empty := ""
					   a.LCMVersion = lcmVersion
					   if a.LCMVersion == nil { a.LCMVersion = &empty }
					   a.RegistrationType = registrationType
					   if a.RegistrationType == nil { a.RegistrationType = &empty }
					   a.CertificateThumbprint = certificateThumbprint
					   if a.CertificateThumbprint == nil { a.CertificateThumbprint = &empty }
					   a.CertificateSubject = certificateSubject
					   if a.CertificateSubject == nil { a.CertificateSubject = &empty }
					   a.CertificateIssuer = certificateIssuer
					   if a.CertificateIssuer == nil { a.CertificateIssuer = &empty }
					   a.CertificateNotBefore = certificateNotBefore
					   if a.CertificateNotBefore == nil { a.CertificateNotBefore = &empty }
					   a.CertificateNotAfter = certificateNotAfter
					   if a.CertificateNotAfter == nil { a.CertificateNotAfter = &empty }
					   a.RegisteredAt = registeredAt
					   if a.RegisteredAt == nil { a.RegisteredAt = &empty }
					   // Correction pour last_communication
					   if lastCommunication != nil {
						   a.LastCommunication = *lastCommunication
					   } else {
						   a.LastCommunication = empty
					   }
					   a.HasErrorLastReport = hasErrorBool
					   a.NeedsRecompile = needsRecompileBool
					   tags, tagErr := db.GetAgentTags(database, a.AgentId)
					   if tagErr == nil && len(tags) > 0 {
						   a.Tags = tags
					   }
					   a.State = state
					   agents = append(agents, a)
				   } else {
					   log.Printf("[API][DB] Agent ignoré, erreur scan: %v", err)
				   }
			   }
		       w.Header().Set("Content-Type", "application/json")
		       _ = json.NewEncoder(w).Encode(agents)
}

// DELETE /api/v1/agents/{id}
func DeleteNodeHandler(w http.ResponseWriter, r *http.Request) {
	dbConn, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	id := r.PathValue("id")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tx, err := dbConn.Begin()
	if err != nil {
		log.Printf("[API][DB] Cannot start transaction for agent delete %s: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	deleteQueries := []string{
		"DELETE FROM agent_tags WHERE agent_id = ?",
		"DELETE FROM reports WHERE agent_id = ?",
		"DELETE FROM agent_configurations WHERE agent_id = ?",
		"DELETE FROM agent_ips WHERE agent_id = ?",
	}
	for _, query := range deleteQueries {
		if _, err := tx.Exec(query, id); err != nil {
			_ = tx.Rollback()
			log.Printf("[API][DB] Failed to delete dependencies for agent %s with query %q: %v", id, query, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	result, err := tx.Exec("DELETE FROM agents WHERE agent_id = ?", id)
	if err != nil {
		_ = tx.Rollback()
		log.Printf("[API][DB] Failed to delete agent %s: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		_ = tx.Rollback()
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[API][DB] Failed to commit agent delete %s: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Audit suppression
	driverName := global.AppConfig.Database.Driver
	user := "?"
	if r.Context().Value("userId") != nil {
		if sub, ok := r.Context().Value("userId").(string); ok {
			user = sub
		}
	}
	_ = db.InsertAudit(dbConn, driverName, user, "delete", "agent", "Deleted agent: "+id, "")
	w.WriteHeader(http.StatusOK)
}