package handlers

import (
	"encoding/json"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/utils"
	"log"
	"net/http"
	"strings"
)

// SendReportHandler gère POST /PSDSCPullServer.svc/Nodes(AgentId='...')/SendReport
func SendReportHandler(w http.ResponseWriter, r *http.Request) {
	reportBody, err := readBodyLimited(w, r, maxDSCRequestBody)
	if err != nil {
		log.Printf("[SENDREPORT] Error reading body: %v", err)
		return
	}
	rawId := r.PathValue("node")
	agentId := utils.ExtractAgentId(rawId)
	log.Printf("[SENDREPORT] AgentId=%s (raw=%s), ReportSize=%d", agentId, rawId, len(reportBody))

	// Liaison identité : le certificat client présenté doit correspondre au nœud
	// qui envoie le rapport. Sans effet si la validation de certificat client
	// est désactivée.
	if ok, cause := verifyNodeCertBinding(r, db.Shared(), agentId); !ok {
		log.Printf("[SENDREPORT] Certificate-to-node binding refused for agentId=%q: %s", agentId, cause)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Désérialiser le rapport
	var report schema.DscReport
	if err := json.Unmarshal(reportBody, &report); err != nil {
		log.Printf("[SENDREPORT] Error parsing report JSON: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Préparer les champs JSON pour la base
	errorsJson, _ := json.Marshal(report.Errors)
	statusDataJson, _ := json.Marshal(report.StatusData)
	additionalDataJson, _ := json.Marshal(report.AdditionalData)

	// Contrôle de la présence de MetaData dans StatusData
	mofApplied := 0
	for _, statusEntry := range report.StatusData {
		var entryMap map[string]interface{}
		if err := json.Unmarshal([]byte(statusEntry), &entryMap); err == nil {
			if _, ok := entryMap["MetaData"]; ok {
				mofApplied = 1
				break
			}
		}
	}

	// Insérer en base
	database := db.Shared()
	if database != nil {
		// Vérifie si un rapport existe déjà pour ce job_id
		var count int
		err = database.QueryRow("SELECT COUNT(*) FROM reports WHERE job_id = ?", report.JobId).Scan(&count)
		if err != nil {
			log.Printf("[SENDREPORT] Error SELECT COUNT on reports: %v", err)
		}
		if count > 0 {
			log.Printf("[SENDREPORT] Updating report in database: agent_id=%s, job_id=%s, operation_type=%s", agentId, report.JobId, report.OperationType)
			_, err := database.Exec(`UPDATE reports SET 
					agent_id=?, report_format_version=?, operation_type=?, refresh_mode=?, status=?, start_time=?, end_time=?, reboot_requested=?, errors=?, status_data=?, additional_data=?, mof_applied=?, raw_json=?
					WHERE job_id=?`,
				agentId,
				report.ReportFormatVersion,
				report.OperationType,
				report.RefreshMode,
				report.Status,
				report.StartTime,
				report.EndTime,
				report.RebootRequested,
				string(errorsJson),
				string(statusDataJson),
				string(additionalDataJson),
				mofApplied,
				string(reportBody),
				report.JobId,
			)
			if err != nil {
				log.Printf("[SENDREPORT] Error updating report in database: %v", err)
			}
		} else {
			log.Printf("[SENDREPORT] Inserting report in database: agent_id=%s, job_id=%s, operation_type=%s", agentId, report.JobId, report.OperationType)
			_, err := database.Exec(`INSERT INTO reports (
					agent_id, job_id, report_format_version, operation_type, refresh_mode, status, start_time, end_time, reboot_requested, errors, status_data, additional_data, mof_applied, raw_json
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				agentId,
				report.JobId,
				report.ReportFormatVersion,
				report.OperationType,
				report.RefreshMode,
				report.Status,
				report.StartTime,
				report.EndTime,
				report.RebootRequested,
				string(errorsJson),
				string(statusDataJson),
				string(additionalDataJson),
				mofApplied,
				string(reportBody),
			)
			if err != nil {
				log.Printf("[SENDREPORT] Error inserting report in database: %v", err)
			}
		}
		// Toujours mettre a jour le heartbeat agent.
		_, err = database.Exec("UPDATE agents SET last_communication = CURRENT_TIMESTAMP WHERE agent_id = ?", agentId)
		if err != nil {
			log.Printf("[SENDREPORT] Error updating last_communication: %v", err)
		}

		if mofApplied == 1 {
			hasError := 1
			state := "Failure"
			if strings.ToLower(strings.TrimSpace(report.Status)) == "success" {
				hasError = 0
				state = "Success"
			}
			_, err = database.Exec("UPDATE agents SET has_error_last_report = ?, state = ? WHERE agent_id = ?", hasError, state, agentId)
			if err != nil {
				log.Printf("[SENDREPORT] Error updating has_error_last_report/state: %v", err)
			}
		} else {
			log.Printf("[SENDREPORT] Report ignored for web state (mof_applied=0): agent_id=%s, job_id=%s, operation_type=%s", agentId, report.JobId, report.OperationType)
		}
	}

	w.Header().Set("ProtocolVersion", "2.0")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}
