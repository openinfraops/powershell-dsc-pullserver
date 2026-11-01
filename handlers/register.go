package handlers

import (
	"encoding/json"
	"fmt"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/service"
	internalutils "go-dsc-pull/internal/utils"
	utils "go-dsc-pull/utils"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
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
			err = database.QueryRow(`SELECT agent_id FROM agents WHERE node_name = ? AND agent_id LIKE 'TEMP-%'`, nodeName).Scan(&tempAgentId)
			if err == nil && tempAgentId != "" {
				// Mise à jour : change l'agent_id et les infos
				_, err = database.Exec(`UPDATE agents SET agent_id = ?, lcm_version = ?, registration_type = ?, certificate_thumbprint = ?, certificate_subject = ?, certificate_issuer = ?, certificate_notbefore = ?, certificate_notafter = ? WHERE agent_id = ?`,
					agentId, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter, tempAgentId)
				if err != nil {
					logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Erreur update agent TEMP: %v", err))
				}
				// Met à jour l'agent_id dans la table agent_tags
				_, err = database.Exec(`UPDATE agent_tags SET agent_id = ? WHERE agent_id = ?`, agentId, tempAgentId)
				if err != nil {
					logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Erreur update agent_tags TEMP: %v", err))
				}
			} else {
				// Insertion normale, compatible SQLite/MSSQL
				if driver == "sqlite" {
					_, err = database.Exec(`INSERT OR REPLACE INTO agents (agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
						agentId, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter, "pending_apply")
				} else {
					_, err = database.Exec(`IF NOT EXISTS (SELECT 1 FROM agents WHERE agent_id = ?) INSERT INTO agents (agent_id, node_name, lcm_version, registration_type, certificate_thumbprint, certificate_subject, certificate_issuer, certificate_notbefore, certificate_notafter, registered_at, last_communication, has_error_last_report) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
						agentId, agentId, nodeName, lcmVersion, registrationType, thumbprint, subject, issuer, notbefore, notafter, nil, nil, 0)
				}
				if err != nil {
					logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][DB] Erreur insertion agent: %v", err))
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
			if isNewNode {
				repoSettings, settingsErr := db.GetConfigurationRepoSettings(database)
				if settingsErr == nil && repoSettings.CompileOnNodeRegistration {
					nodeTags, tagsErr := db.GetAgentTags(database, agentId)
					if tagsErr != nil {
						logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][CONFIG] Failed to load tags for node %s: %v", nodeName, tagsErr))
						nodeTags = map[string][]string{}
					}
					rows, queryErr := database.Query(`SELECT configuration_name FROM agent_configurations WHERE agent_id = ?`, agentId)
					configNames := make([]string, 0)
					if queryErr == nil {
						for rows.Next() {
							var name string
							if rows.Scan(&name) == nil && name != "" {
								configNames = append(configNames, name)
							}
						}
						rows.Close()
					} else {
						logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][CONFIG] Failed to load assigned configurations: %v", queryErr))
					}
					if len(configNames) == 0 {
						configNames = append(configNames, nodeName)
					}
					repoConfig := service.ConfigurationRepoConfig{
						URL:                  repoSettings.URL,
						Branch:               repoSettings.Branch,
						Directory:            repoSettings.Directory,
						CompilerScript:       repoSettings.CompilerScript,
						PowerShellExecutable: repoSettings.PowerShellExecutable,
					}
					go func() {
						_, compileErr := service.RunConfigurationRepoSyncForNodeContext(repoConfig, configNames, nodeName, nodeTags, func(compiled []service.CompiledConfiguration) (int, error) {
							compileDB, openErr := db.OpenDB(&global.AppConfig.Database)
							if openErr != nil {
								return 0, openErr
							}
							defer compileDB.Close()
							assignments := make(map[string]string, len(compiled)+1)
							assignments[strings.ToLower(strings.TrimSpace(nodeName))] = agentId
							for _, configuration := range compiled {
								assignments[strings.ToLower(strings.TrimSpace(configuration.Name))] = agentId
							}
							return db.ImportCompiledConfigurationsAndAssign(compileDB, compiled, assignments)
						})
						if compileErr != nil {
							logs.WriteLogFile(fmt.Sprintf("[ERROR][REGISTER][CONFIG] Compilation failed for node %s: %v", nodeName, compileErr))
						} else {
							logs.WriteLogFile(fmt.Sprintf("[INFO][REGISTER][CONFIG] Compiled assigned configurations for node %s", nodeName))
						}
					}()
				}
			}
		}
	}

	resp := schema.RegisterResponse{AgentId: agentId}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ProtocolVersion", "2.0")
	_ = json.NewEncoder(w).Encode(resp)
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
