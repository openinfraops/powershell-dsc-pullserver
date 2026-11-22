package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/schema"
	internalutils "go-dsc-pull/internal/utils"
	utils "go-dsc-pull/utils"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"
)

// RegisterHandler gère l'enregistrement initial (POST /PSDSCPullServer.svc/Nodes)
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	// --- Contrôle de la signature Authorization DSC ---
	authHeader := r.Header.Get("Authorization")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to read request body", http.StatusInternalServerError)
		return
	}
	// Charger la config pour récupérer la clé d'enregistrement
	registrationKeyPlain := global.AppConfig.DSCPullServer.RegistrationKey
	if registrationKeyPlain == "" {
		logs.WriteLogFile(fmt.Sprintf("[REGISTER][CONFIG] registrationKey missing in config file, server stopped."))
		http.Error(w, "Server configuration error: registrationKey missing", http.StatusInternalServerError)
		return
	}
	xmsDate := r.Header.Get("x-ms-date")
	valid, _ := internalutils.ValidateDSCRegistrationKey(body, xmsDate, authHeader, registrationKeyPlain)
	if !valid {
		log.Print("[REGISTER][AUTH] invalid DSC registration signature")
		http.Error(w, "Unauthorized: invalid signature", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req map[string]interface{}
	_ = json.Unmarshal(body, &req) // Body peut être vide en PUT
	// Extraire l'AgentId du segment d'URL si présent
	raw := r.PathValue("node")
	agentId := utils.ExtractAgentId(raw)
	if agentId == "" {
		agentId = generateAgentId()
	}

	// --- Insertion en base ---
	// Charger la config DB
	database, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		log.Printf("[REGISTER][DB] Erreur ouverture DB: %v", err)
		// On continue, mais pas d'insertion DB
	} else {
		if err != nil {
			logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Erreur ouverture DB: %v", err))
		} else {
			defer database.Close()
			driver := global.AppConfig.Database.Driver
			var existingAgentCount int
			_ = database.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = ?`, agentId).Scan(&existingAgentCount)
			isNewNode := existingAgentCount == 0
			// Insertion ou mise à jour agent principal
			agentInfo := req["AgentInformation"].(map[string]interface{})
			nodeName := agentInfo["NodeName"].(string)
			lcmVersion := agentInfo["LCMVersion"].(string)
			regInfo := req["RegistrationInformation"].(map[string]interface{})
			certInfo := regInfo["CertificateInformation"].(map[string]interface{})
			registrationType := regInfo["RegistrationMessageType"].(string)
			thumbprint := certInfo["Thumbprint"].(string)
			subject := certInfo["Subject"].(string)
			issuer := certInfo["Issuer"].(string)
			notbefore := certInfo["NotBefore"].(string)
			notafter := certInfo["NotAfter"].(string)

			// Vérifie si NodeName existe avec un AgentId TEMP
			var tempAgentId string
			err = database.QueryRow(`SELECT agent_id FROM agents WHERE node_name = ? AND agent_id LIKE 'TEMP-%' AND deleted_at IS NULL`, nodeName).Scan(&tempAgentId)
			if err == nil && tempAgentId != "" {
				// Migration TEMP → réel.
				// SQL Server interdit de modifier la PK agents.agent_id lorsqu'une table
				// enfant (agent_tags, agent_configurations, agent_ips) la référence via une
				// FOREIGN KEY : un `UPDATE agents SET agent_id = ...` échoue avec une erreur
				// de contrainte (constaté en production sur MSSQL). On insère donc une
				// nouvelle ligne avec l'ID réel, on ré-attache les lignes enfants, puis on
				// supprime la ligne TEMP, le tout dans une transaction.
				if migrateErr := migrateTempAgentToRealID(database, driver, tempAgentId, agentId, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter); migrateErr != nil {
					logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Migration agent TEMP %s vers %s impossible: %v", tempAgentId, agentId, migrateErr))
				}
			} else {
				// Upsert de la ligne principale de l'agent (enrôlement ou
				// ré-enregistrement / renouvellement de certificat).
				err = upsertAgent(database, driver, agentId, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter, isNewNode)
				if err != nil {
					logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Erreur upsert agent: %v", err))
				}
			}

			// Only replace config links when registration payload actually contains ConfigurationNames.
			extractedConfigNames := []string{}
			if err == nil {
				if configNames, ok := req["ConfigurationNames"]; ok {
					switch vv := configNames.(type) {
					case []interface{}:
						for _, n := range vv {
							if s, ok := n.(string); ok && s != "" {
								extractedConfigNames = append(extractedConfigNames, s)
							}
						}
					case string:
						if vv != "" {
							extractedConfigNames = append(extractedConfigNames, vv)
						}
					}
				}

				if len(extractedConfigNames) == 0 {
					logs.WriteLogFile(fmt.Sprintf("[INFO][REGISTER][DB] Aucun ConfigurationNames fourni, conservation des liens existants pour agentId=%s", agentId))
				} else {
					_, err := database.Exec(`DELETE FROM agent_configurations WHERE agent_id = ?`, agentId)
					if err != nil {
						logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Erreur suppression configs existantes: %v", err))
					}
					for _, configName := range extractedConfigNames {
						if driver == "sqlite" {
							logs.WriteLogFile(fmt.Sprintf("[INFO][REGISTER][DB] Insertion config (SQLite): agentId=%s, config=%s", agentId, configName))
							_, err := database.Exec(`INSERT OR REPLACE INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, agentId, configName)
							if err != nil {
								logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Erreur insertion config: %v (agentId=%s, config=%s)", err, agentId, configName))
							}
						} else {
							logs.WriteLogFile(fmt.Sprintf("[INFO][REGISTER][DB] Insertion config (MSSQL): agentId=%s, config=%s", agentId, configName))
							_, err := database.Exec(`IF NOT EXISTS (SELECT 1 FROM agent_configurations WHERE agent_id = ? AND configuration_name = ?) INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, agentId, configName, agentId, configName)
							if err != nil {
								logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Erreur insertion config: %v (agentId=%s, config=%s)", err, agentId, configName))
							}
						}
					}
				}
			}
			if isNewNode && len(extractedConfigNames) == 0 {
				if driver == "sqlite" {
					_, err = database.Exec(`INSERT OR REPLACE INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, agentId, nodeName)
				} else {
					_, err = database.Exec(`IF NOT EXISTS (SELECT 1 FROM agent_configurations WHERE agent_id = ? AND configuration_name = ?) INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, agentId, nodeName, agentId, nodeName)
				}
				if err != nil {
					logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][CONFIG] Failed to assign node configuration %s: %v", nodeName, err))
				}
			}
			if isNewNode && tempAgentId == "" {
				triggerNodeRecompileAsync(agentId, nodeName)
			}
		}
	}

	resp := schema.RegisterResponse{AgentId: agentId}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ProtocolVersion", "2.0")
	_ = json.NewEncoder(w).Encode(resp)
}

// upsertAgent insère ou met à jour la ligne principale d'un agent selon qu'il
// s'agit d'un nouvel enregistrement (enrôlement) ou d'un ré-enregistrement
// (ex. renouvellement du certificat du nœud).
//
//   - Nouvel agent : insertion, avec state='pending_apply'.
//   - Agent déjà enregistré : mise à jour des champs du nœud et du certificat
//     (thumbprint, subject, issuer, notbefore, notafter) SANS écraser
//     registered_at, last_communication, has_error_last_report, needs_recompile,
//     state ni deleted_at. C'est ce qui permet le renouvellement de certificat.
func upsertAgent(database *sql.DB, driver, agentId, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter string, isNewNode bool) error {
	if isNewNode {
		if driver == "sqlite" {
			_, err := database.Exec(`INSERT OR REPLACE INTO agents (agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				agentId, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter, "pending_apply")
			return err
		}
		_, err := database.Exec(`IF NOT EXISTS (SELECT 1 FROM agents WHERE agent_id = ?) INSERT INTO agents (agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, registered_at, last_communication, has_error_last_report) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			agentId, agentId, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter, nil, nil, 0)
		return err
	}
	// Ré-enregistrement : mise à jour (le renouvellement de certificat aboutit ici).
	_, err := database.Exec(`UPDATE agents SET node_name=?, lcm_version=?, registration_type=?, certificate_thumbprint=?, certificate_subject=?, certificate_issuer=?, certificate_notbefore=?, certificate_notafter=? WHERE agent_id=?`,
		nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter, agentId)
	return err
}

// migrateTempAgentToRealID remplace une pré-inscription TEMP-* par l'ID réel d'un
// agent après enregistrement authentifié. MSSQL refuse de modifier la PK agents.agent_id
// référencée par les tables enfants, d'où l'insertion d'une nouvelle ligne puis la
// ré-attribution des enfants et la suppression de la ligne TEMP, dans une transaction.
func migrateTempAgentToRealID(database *sql.DB, driver, tempAgentID, realAgentID, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter string) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if driver == "sqlite" {
		_, err = tx.Exec(`INSERT OR REPLACE INTO agents (agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			realAgentID, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter, "pending_apply")
	} else {
		_, err = tx.Exec(`IF NOT EXISTS (SELECT 1 FROM agents WHERE agent_id = ?) INSERT INTO agents (agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, registered_at, last_communication, state, has_error_last_report) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, GETDATE(), GETDATE(), 'pending_apply', 0)`,
			realAgentID, realAgentID, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter)
	}
	if err != nil {
		return fmt.Errorf("insert real agent %s: %w", realAgentID, err)
	}

	for _, q := range []string{
		`UPDATE agent_tags SET agent_id = ? WHERE agent_id = ?`,
		`UPDATE agent_configurations SET agent_id = ? WHERE agent_id = ?`,
		`UPDATE agent_ips SET agent_id = ? WHERE agent_id = ?`,
		`UPDATE node_properties SET node_id = ? WHERE node_id = ?`,
	} {
		if _, err := tx.Exec(q, realAgentID, tempAgentID); err != nil {
			return fmt.Errorf("migrate children from %s: %w", tempAgentID, err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM agents WHERE agent_id = ?`, tempAgentID); err != nil {
		return fmt.Errorf("delete temp agent %s: %w", tempAgentID, err)
	}
	return tx.Commit()
}

func generateAgentId() string {
	rand.Seed(time.Now().UnixNano())
	return randomHex(8) + "-" + randomHex(4) + "-" + randomHex(4) + "-" + randomHex(4) + "-" + randomHex(12)
}

func randomHex(n int) string {
	const letters = "0123456789ABCDEF"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
