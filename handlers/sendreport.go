package handlers

import (
	"encoding/json"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/utils"
	"log"
	"net/http"
	"strings"
	"time"
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

	// État dérivé du redémarrage : le rapport est traité comme non fiable, toute
	// valeur invalide/malformée est considérée « pas de redémarrage ».
	rebootRequested := bool(report.RebootRequested)
	requester := extractRebootRequester(report.StatusData)

	// Insérer en base, dans une transaction unique avec la mise à jour de l'état
	// dérivé du nœud (reboot). Si une étape échoue, on annule tout pour ne pas
	// laisser un état partiel incohérent entre reports.reboot_requested et
	// agents.reboot_*.
	database := db.Shared()
	if database != nil {
		now := time.Now().UTC()
		thr := 0
		win := 0
		if global.AppConfig != nil {
			thr = global.AppConfig.DSCPullServer.RebootLoopThreshold
			win = global.AppConfig.DSCPullServer.RebootLoopWindowMins
		}

		tx, err := database.Begin()
		if err != nil {
			log.Printf("[SENDREPORT] Error starting transaction: %v", err)
		} else {
			txFailed := false

			// Vérifie si un rapport existe déjà pour ce job_id
			var count int
			err = tx.QueryRow("SELECT COUNT(*) FROM reports WHERE job_id = ?", report.JobId).Scan(&count)
			if err != nil {
				log.Printf("[SENDREPORT] Error SELECT COUNT on reports: %v", err)
			}
			if count > 0 {
				log.Printf("[SENDREPORT] Updating report in database: agent_id=%s, job_id=%s, operation_type=%s", agentId, report.JobId, report.OperationType)
				_, err = tx.Exec(`UPDATE reports SET 
						agent_id=?, report_format_version=?, operation_type=?, refresh_mode=?, status=?, start_time=?, end_time=?, reboot_requested=?, errors=?, status_data=?, additional_data=?, mof_applied=?, raw_json=?
						WHERE job_id=?`,
					agentId,
					report.ReportFormatVersion,
					report.OperationType,
					report.RefreshMode,
					report.Status,
					report.StartTime,
					report.EndTime,
					rebootRequestedString(report.RebootRequested),
					string(errorsJson),
					string(statusDataJson),
					string(additionalDataJson),
					mofApplied,
					string(reportBody),
					report.JobId,
				)
				if err != nil {
					log.Printf("[SENDREPORT] Error updating report in database: %v", err)
					txFailed = true
				}
			} else {
				log.Printf("[SENDREPORT] Inserting report in database: agent_id=%s, job_id=%s, operation_type=%s", agentId, report.JobId, report.OperationType)
				_, err = tx.Exec(`INSERT INTO reports (
						agent_id, job_id, report_format_version, operation_type, refresh_mode, status, start_time, end_time, reboot_requested, errors, status_data, additional_data, mof_applied, raw_json, created_at
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, `+db.NowExpr(global.AppConfig.Database.Driver)+`)`,
					agentId,
					report.JobId,
					report.ReportFormatVersion,
					report.OperationType,
					report.RefreshMode,
					report.Status,
					report.StartTime,
					report.EndTime,
					rebootRequestedString(report.RebootRequested),
					string(errorsJson),
					string(statusDataJson),
					string(additionalDataJson),
					mofApplied,
					string(reportBody),
				)
				if err != nil {
					log.Printf("[SENDREPORT] Error inserting report in database: %v", err)
					txFailed = true
				}
			}
			// Toujours mettre à jour le heartbeat agent. Le front suppose un stockage
			// UTC (parseUtc ajoute 'Z') ; MSSQL CURRENT_TIMESTAMP est local, d'où
			// GETUTCDATE() via db.NowExpr.
			_, err = tx.Exec("UPDATE agents SET last_communication = "+db.NowExpr(global.AppConfig.Database.Driver)+" WHERE agent_id = ?", agentId)
			if err != nil {
				log.Printf("[SENDREPORT] Error updating last_communication: %v", err)
				txFailed = true
			}

			if mofApplied == 1 {
				hasError := 1
				state := "Failure"
				if strings.ToLower(strings.TrimSpace(report.Status)) == "success" {
					hasError = 0
					state = "Success"
				}
				_, err = tx.Exec("UPDATE agents SET has_error_last_report = ?, state = ? WHERE agent_id = ?", hasError, state, agentId)
				if err != nil {
					log.Printf("[SENDREPORT] Error updating has_error_last_report/state: %v", err)
					txFailed = true
				}
			} else {
				log.Printf("[SENDREPORT] Report ignored for web state (mof_applied=0): agent_id=%s, job_id=%s, operation_type=%s", agentId, report.JobId, report.OperationType)
			}

			if txFailed {
				_ = tx.Rollback()
				log.Printf("[SENDREPORT] Transaction rolled back (report not persisted): agent_id=%s, job_id=%s", agentId, report.JobId)
			} else {
				// Mise à jour de l'état dérivé du redémarrage, dans la même transaction.
				if err := db.ApplyRebootReportState(tx, agentId, rebootRequested, requester, now, thr, win); err != nil {
					log.Printf("[SENDREPORT] Error applying reboot report state: %v", err)
					_ = tx.Rollback()
				} else if err := tx.Commit(); err != nil {
					log.Printf("[SENDREPORT] Error committing transaction: %v", err)
				}
			}
		}
	}

	// Signale au nœud, le cas échéant, que son certificat est dans la fenêtre
	// de renouvellement (en-tête DSC-certificateRotation, §2.2.2.8).
	maybeSetCertificateRotationHeader(w, database, agentId)

	w.Header().Set("ProtocolVersion", "2.0")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

// rebootRequestedString normalise la colonne reports.reboot_requested (TEXT) :
// on écrit "1"/"0" pour les nouveaux rapports, ce que ParseBoolFlexible lit
// indifféremment en plus des anciennes valeurs "True"/"False".
func rebootRequestedString(v schema.FlexBool) string {
	if bool(v) {
		return "1"
	}
	return "0"
}

// extractRebootRequester parcourt les entrées StatusData et retourne
// l'identifiant de la ressource qui a demandé un redémarrage, au format
// « <ModuleName|SourceInfo>:<ResourceId> ». En l'absence d'identifiant fiable,
// elle retombe sur le seul champ disponible, puis sur "(unknown)". Retourne ""
// si aucune entrée ne demande de redémarrage.
func extractRebootRequester(statusData []string) string {
	for _, entry := range statusData {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(entry), &m); err != nil {
			continue
		}
		if !rebootFlagFromMap(m) {
			continue
		}
		module := mapString(m, "ModuleName")
		source := mapString(m, "SourceInfo")
		resource := mapString(m, "ResourceId")
		if module == "" {
			module = source
		}
		switch {
		case module != "" && resource != "":
			return module + ":" + resource
		case module != "":
			return module
		case resource != "":
			return resource
		default:
			return "(unknown)"
		}
	}
	return ""
}

// rebootFlagFromMap lit le drapeau RebootRequested (case-insensitive) d'une
// entrée StatusData, de la même manière tolérante que ParseBoolFlexible.
func rebootFlagFromMap(m map[string]interface{}) bool {
	for k, v := range m {
		if strings.EqualFold(k, "RebootRequested") {
			return schema.ParseBoolFlexible(v)
		}
	}
	return false
}

// mapString retourne la valeur string d'une clé de map JSON, ou "" si absente
// ou non-string.
func mapString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
