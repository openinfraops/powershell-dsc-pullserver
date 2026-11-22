package handlers

import (
	"log"
	"net/http"
	"fmt"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"go-dsc-pull/internal/db"
)

// ConfigurationContentHandler gère GET /PSDSCPullServer.svc/{node}/{config}/ConfigurationContent
func ConfigurationContentHandler(w http.ResponseWriter, r *http.Request) {
	nodeRaw := r.PathValue("node")
	configRaw := r.PathValue("config")
	// Extraction de l'agentId et extraction robuste du nom de configuration
	agentId := nodeRaw
	configName := configRaw
	// Si le format est Configurations(ConfigurationName='test'), extraire le nom
	if strings.HasPrefix(configName, "Configurations(") && strings.HasSuffix(configName, ")") {
		inner := configName[len("Configurations(") : len(configName)-1]
		parts := strings.Split(inner, ",")
		for _, part := range parts {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				k := strings.TrimSpace(kv[0])
				v := strings.Trim(kv[1], "'\"")
				if k == "ConfigurationName" {
					configName = v
				}
			}
		}
	}
	log.Printf("[CONFIGURATIONCONTENT] AgentId=%s ConfigName=%s", agentId, configName)
	dbConn := db.Shared()
	// Liaison identité : le certificat client présenté doit correspondre au nœud
	// (agentId) qui tire la configuration. Sans effet si la validation de
	// certificat client est désactivée (déploiement ayant renoncé à l'identité mTLS).
	if ok, cause := verifyNodeCertBinding(r, dbConn, agentId); !ok {
		log.Printf("[CONFIGURATIONCONTENT] Liaison cert→nœud refusée pour agentId=%q: %s", agentId, cause)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	// La configuration demandée doit être rattachée à ce nœud : on empêche le
	// téléchargement de n'importe quel MOF par nom.
	var linked int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM agent_configurations WHERE agent_id = ? AND LOWER(configuration_name) = LOWER(?)", agentId, configName).Scan(&linked); err != nil {
		log.Printf("[CONFIGURATIONCONTENT] Erreur vérification rattachement config pour agentId=%q: %v", agentId, err)
		http.Error(w, "Configuration not found", http.StatusNotFound)
		return
	}
	if linked == 0 {
		log.Printf("[CONFIGURATIONCONTENT] Config '%s' non rattachée au nœud agentId=%q", configName, agentId)
		http.Error(w, "Configuration not found", http.StatusNotFound)
		return
	}
	// Recherche par nom (sans extension)
	log.Printf("[CONFIGURATIONCONTENT] Recherche MOF en base pour configName='%s'", configName)
	// Met à jour last_usage à CURRENT_TIMESTAMP pour cette configuration (insensible à la casse)
	_, err := dbConn.Exec("UPDATE configuration_model SET last_usage = CURRENT_TIMESTAMP WHERE LOWER(name) = LOWER(?)", configName)
	if err != nil {
		log.Printf("[CONFIGURATIONCONTENT] Erreur lors de la mise à jour de last_usage: %v", err)
	}
	row := dbConn.QueryRow("SELECT mof_file FROM configuration_model WHERE LOWER(name) = LOWER(?)", configName)
	var data []byte
	if err := row.Scan(&data); err != nil {
		http.Error(w, "Configuration not found", http.StatusNotFound)
		return
	}
	// Calcul du checksum SHA256
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Checksum", strings.ToUpper(checksum))
	w.Header().Set("ChecksumAlgorithm", "SHA-256")
	w.Header().Set("ProtocolVersion", "2.0")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
