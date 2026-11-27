package handlers

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/logs"
)

// collisionDetectionWindowMinutes est la fenêtre (en minutes) pendant laquelle
// deux identités distinctes qui se présentent sous le même node_name sont
// considérées comme « vivantes » en même temps, donc en collision. 0 désactive
// la détection par fenêtre (aucune collision n'est signalée).
//
// Ce seuil est volontairement un constant pour cette passe (focalisée sur
// l'audit) ; il pourra être promu en réglage runtime (table dsc_settings) pour
// le rendre ajustable à chaud.
const collisionDetectionWindowMinutes = 5

// nodeActor identifie l'acteur d'un enregistrement DSC : le nœud lui-même. Il
// n'y a pas d'utilisateur humain dans ce flux, on utilise donc son agent_id.
func nodeActor(agentId string) string {
	if agentId == "" {
		return "node"
	}
	return "node:" + agentId
}

// auditNodeIdentityOverwrite trace un écrasement d'identité : un agent déjà
// enregistré (même agent_id) voit son empreinte de certificat remplacée par une
// autre (ré-enregistrement, renouvellement, ou prise de contrôle). On renseigne
// l'ancienne et la nouvelle empreinte, l'adresse IP, le type d'enregistrement
// (politique utilisée) et la date (créée par InsertAudit).
func auditNodeIdentityOverwrite(database *sql.DB, driver, agentId, nodeName, oldThumb, newThumb, regType, ip string) {
	if oldThumb == "" || strings.EqualFold(oldThumb, newThumb) {
		return
	}
	details := fmt.Sprintf(
		"Identity overwrite for node_name=%s agent_id=%s: certificate thumbprint changed %s -> %s (registration_type=%s)",
		nodeName, agentId, oldThumb, newThumb, regType,
	)
	_ = db.InsertAudit(database, driver, nodeActor(agentId), "agent.overwrite", "agent", details, ip)
	logs.WriteLogFile(fmt.Sprintf("[IDENTITY] %s", details))
}

// auditNodeNameCollision trace une collision de nom : un node_name est réclamé
// par une identité vivante différente (autre agent_id, autre empreinte) dans la
// fenêtre collisionDetectionWindowMinutes.
func auditNodeNameCollision(database *sql.DB, driver, agentId, nodeName, existingAgentId, existingThumb, newThumb, ip string) {
	details := fmt.Sprintf(
		"Name collision on node_name=%s: live agent %s (thumbprint %s) vs incoming agent %s (thumbprint %s)",
		nodeName, existingAgentId, existingThumb, agentId, newThumb,
	)
	_ = db.InsertAudit(database, driver, nodeActor(agentId), "agent.collision", "agent", details, ip)
	logs.WriteLogFile(fmt.Sprintf("[IDENTITY] ALERT %s", details))
}

// detectAndAuditIdentityChange analyse l'enregistrement entrant et trace les
// écrasements d'identité / collisions de nom. Elle doit être appelée AVANT
// l'upsert : l'ancienne empreinte est lue dans l'état pré-modification.
//
//   - isNewNode : l'agent_id n'existait pas encore en base.
//   - tempAgentID : non vide si le nœud provient d'une pré-inscription TEMP.
//     Dans ce cas l'identité remplace un placeholder, pas une autre identité
//     vivante : on ne déclenche pas de collision de nom.
func detectAndAuditIdentityChange(database *sql.DB, driver, agentId, nodeName, newThumb, regType, ip string, isNewNode bool, tempAgentID string) {
	if database == nil || strings.TrimSpace(nodeName) == "" {
		return
	}

	// --- Cas A : écrasement de l'empreinte d'un agent déjà enregistré ---
	if !isNewNode {
		var oldThumb string
		_ = database.QueryRow(`SELECT certificate_thumbprint FROM agents WHERE agent_id = ?`, agentId).Scan(&oldThumb)
		auditNodeIdentityOverwrite(database, driver, agentId, nodeName, oldThumb, newThumb, regType, ip)
	}

	// --- Cas B : collision de nom avec une autre identité vivante ---
	// Une pré-inscription TEMP ne fait que matérialiser le placeholder du même
	// nœud : on exclut les lignes TEMP pour ne pas se déclencher soi-même.
	if tempAgentID != "" {
		return
	}
	rows, err := database.Query(`
		SELECT agent_id, certificate_thumbprint, COALESCE(last_communication, registered_at)
		FROM agents
		WHERE node_name = ? AND agent_id != ? AND agent_id NOT LIKE 'TEMP-%' AND deleted_at IS NULL`,
		nodeName, agentId)
	if err != nil {
		return
	}

	// On collecte d'abord les collisions puis on ferme le curseur : les écritures
	// d'audit (InsertAudit) utilisent la même connexion, or un pool ":memory:" à
	// une connexion unique se bloquerait si le curseur restait ouvert.
	type collisionHit struct {
		id, thumb string
	}
	var hits []collisionHit
	now := time.Now().UTC()
	for rows.Next() {
		var existingID, existingThumb string
		var lastSeen interface{}
		if err := rows.Scan(&existingID, &existingThumb, &lastSeen); err != nil {
			continue
		}
		// Même empreinte => même identité physique qui se ré-enrôle (par ex. le
		// nœud a perdu son agent_id mais garde son certificat) : pas une collision.
		if strings.EqualFold(existingThumb, newThumb) {
			continue
		}
		if identityIsLive(lastSeen, now) {
			hits = append(hits, collisionHit{id: existingID, thumb: existingThumb})
		}
	}
	rows.Close()

	for _, h := range hits {
		auditNodeNameCollision(database, driver, agentId, nodeName, h.id, h.thumb, newThumb, ip)
	}
}

// identityIsLive détermine si une identité est encore « vivante » (a communiqué
// dans la fenêtre collisionDetectionWindowMinutes). Quand la fraîcheur ne peut
// pas être établie (valeur absente ou non parsable), on considère l'identité
// comme vivante par prudence : l'objectif est de ne pas masquer une collision,
// au prix éventuel d'une entrée d'audit supplémentaire.
func identityIsLive(lastSeen interface{}, now time.Time) bool {
	if collisionDetectionWindowMinutes <= 0 {
		return false
	}
	window := time.Duration(collisionDetectionWindowMinutes) * time.Minute
	if lastSeen == nil {
		return true // prudence
	}
	switch v := lastSeen.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return true // prudence
		}
		if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
			return now.Sub(t) <= window
		}
		return true // non parsable => prudence
	case time.Time:
		return now.Sub(v) <= window
	default:
		return true // prudence
	}
}
