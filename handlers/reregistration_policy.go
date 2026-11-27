package handlers

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/logs"
)

// reregistrationModeDefault est le mode appliqué quand dsc_settings n'en porte
// pas. deny est le plus sûr : aucun ré-enregistrement d'un nœud persistant
// n'est accepté sans contrôle explicite (#87).
const reregistrationModeDefault = "deny"

// reregistrationModeAllow est le mode le plus permissif : tout ré-enregistrement
// d'un nœud persistant est accepté (avec audit renforcé).
const reregistrationModeAllow = "allow"

// reregAction est le verdict de l'application de la politique de ré-enregistrement
// d'un nœud persistant (#87).
type reregAction int

const (
	// reregAllow : l'enregistrement est accepté, le flux normal continue.
	reregAllow reregAction = iota
	// reregPending : l'enregistrement est accepté mais le nœud passe en
	// 'pending_reregistration' ; la configuration n'est plus servie tant qu'un
	// admin n'a pas approuvé.
	reregPending
	// reregReject : l'enregistrement est refusé (HTTP 403).
	reregReject
)

// reregDecision résume le verdict de la politique de ré-enregistrement.
type reregDecision struct {
	// action est le verdict (allow / pending / reject).
	action reregAction
	// mode est le mode de politique appliqué (deny/approval/proof/window/allow).
	mode string
	// outcome est l'issue consignée dans l'audit (allowed/denied/pending/...).
	outcome string
	// oldThumb est l'ancienne empreinte de certificat du nœud (état pré-modif).
	oldThumb string
}

// validReregistrationMode vérifie qu'un mode de politique de ré-enregistrement
// est reconnu (#87).
func validReregistrationMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "deny", "approval", "proof", "window", "allow":
		return true
	}
	return false
}

// applyReregistrationPolicy applique la politique de ré-enregistrement (#87) à un
// nœud persistant. Elle doit être appelée AVANT l'upsert, pour que l'ancienne
// empreinte soit lue dans son état pré-modification.
//
// Le verdict dépend du mode (dsc_settings.reregistration_mode) :
//   - deny    : rejette tout ré-enregistrement d'un nœud persistant (défaut).
//   - approval: accepte mais passe le nœud en 'pending_reregistration' ; la
//     configuration n'est plus servie tant qu'un admin n'a pas approuvé.
//   - proof   : accepte seulement si l'empreinte entrante == l'empreinte
//     précédemment enrôlée ; sinon rejette.
//   - window  : accepte seulement si la fenêtre temporelle ouverte par un admin
//     (agents.reregistration_window_until) est encore ouverte ; sinon rejette.
//   - allow   : accepte (audit renforcé + avertissement au démarrage).
//
// Un nœud non persistant, un nouvel enrôlement (agent_id inconnu) ou une
// migration TEMP ne sont pas soumis à la politique : on autorise le flux normal.
// Chaque tentative concernée est auditée (agent.reregistration) avec l'ancienne
// et la nouvelle empreinte, l'adresse IP, le mode et l'issue.
func applyReregistrationPolicy(database *sql.DB, driver, agentId, nodeName, newThumb, ip string, isNewNode bool, tempAgentID string) reregDecision {
	dec := reregDecision{action: reregAllow, mode: "", outcome: "not_applicable"}
	if database == nil || isNewNode || tempAgentID != "" || strings.TrimSpace(nodeName) == "" {
		return dec
	}

	// Lire l'état pré-modification du nœud : marqueur persistant, ancienne
	// empreinte, état courant, borne de fenêtre de ré-enregistrement.
	var persistent bool
	var oldThumb, state string
	var windowUntil sql.NullString
	err := database.QueryRow(`
		SELECT persistent, COALESCE(certificate_thumbprint, ''), COALESCE(state, ''), COALESCE(reregistration_window_until, '')
		FROM agents WHERE agent_id = ?`, agentId).Scan(&persistent, &oldThumb, &state, &windowUntil)
	if err != nil {
		// Nœud introuvable (course entre deux requêtes) : on laisse passer, le
		// flux d'upsert créera la ligne.
		return dec
	}
	dec.oldThumb = oldThumb

	// La politique ne s'applique qu'aux nœuds explicitement marqués persistants.
	if !persistent {
		return dec
	}

	mode := reregistrationModeDefault
	if settings, sErr := db.GetDscSettings(database); sErr == nil && validReregistrationMode(settings.ReregistrationMode) {
		mode = settings.ReregistrationMode
	}
	dec.mode = mode

	switch mode {
	case reregistrationModeAllow:
		dec.action = reregAllow
		dec.outcome = "allowed"
	case "approval":
		dec.action = reregPending
		dec.outcome = "pending"
	case "proof":
		if oldThumb != "" && strings.EqualFold(strings.TrimSpace(newThumb), strings.TrimSpace(oldThumb)) {
			dec.action = reregAllow
			dec.outcome = "proof_ok"
		} else {
			dec.action = reregReject
			dec.outcome = "proof_failed"
		}
	case "window":
		if reregWindowOpen(windowUntil, time.Now().UTC()) {
			dec.action = reregAllow
			dec.outcome = "window_open"
		} else {
			dec.action = reregReject
			dec.outcome = "window_closed"
		}
	default: // deny (et tout mode inconnu)
		dec.action = reregReject
		dec.outcome = "denied"
	}

	// Chaque tentative de ré-enregistrement d'un nœud persistant est auditée.
	auditReregistration(database, driver, agentId, nodeName, oldThumb, newThumb, ip, mode, dec.outcome)
	return dec
}

// reregWindowOpen détermine si la fenêtre de ré-enregistrement d'un nœud est
// encore ouverte à l'instant `now`. Une fenêtre absente ou non parsable est
// considérée fermée (aucun ré-enregistrement de secours).
func reregWindowOpen(until sql.NullString, now time.Time) bool {
	if !until.Valid || strings.TrimSpace(until.String) == "" {
		return false
	}
	raw := strings.TrimSpace(until.String)
	var t time.Time
	var err error
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05.999999999"} {
		if t, err = time.Parse(layout, raw); err == nil {
			break
		}
	}
	if err != nil {
		return false
	}
	return now.Before(t) || now.Equal(t)
}

// auditReregistration consigne une tentative de ré-enregistrement d'un nœud
// persistant : ancienne/nouvelle empreinte, IP, mode et issue. L'acteur est le
// nœud lui-même (node:<agent_id>).
func auditReregistration(database *sql.DB, driver, agentId, nodeName, oldThumb, newThumb, ip, mode, outcome string) {
	details := fmt.Sprintf(
		"Reregistration policy for node_name=%s agent_id=%s: mode=%s outcome=%s certificate_thumbprint %s -> %s",
		nodeName, agentId, mode, outcome, oldThumb, newThumb,
	)
	_ = db.InsertAudit(database, driver, nodeActor(agentId), "agent.reregistration", "agent", details, ip)
	logs.WriteLogFile(fmt.Sprintf("[REREGISTRATION] %s", details))
}
