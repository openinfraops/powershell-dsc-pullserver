package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/utils"
)

// parseCertTime interprète une date de certificat au format envoyé par le LCM.
// Windows DSC sérialise ces dates en chaînes libres (RFC3339 avec ou sans
// fraction, format .NET, date seule, etc.). On tente une liste de layouts
// connus ; une chaîne vide renvoie le time.Time zéro (pas d'erreur) afin que
// l'appelant traite « aucune date » comme « pas de rotation requise ».
func parseCertTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"01/02/2006",
		time.RFC1123Z,
		time.RFC1123,
		"20060102150405Z",
		"20060102150405.0000000Z",
		"20060102150405",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized certificate time format %q", value)
}

// certNeedsRotation indique si le certificat (dont NotAfter est donné en
// chaîne) est à renouveler : il est déjà expiré, ou expire dans les
// renewalDays prochains jours. Une date d'expiration inconnue ou illisible
// déclenche une rotation (fail-safe : on préfère proposer le renouvellement).
func certNeedsRotation(notAfter string, renewalDays int, now time.Time) (bool, error) {
	if renewalDays <= 0 {
		return false, nil
	}
	expiresAt, err := parseCertTime(notAfter)
	if err != nil {
		// Date illisible : on ne peut pas prouver que le certificat est valide,
		// on propose donc le renouvellement.
		return true, nil
	}
	if expiresAt.IsZero() {
		return false, nil
	}
	threshold := now.Add(time.Duration(renewalDays) * 24 * time.Hour)
	return !expiresAt.After(threshold), nil
}

// maybeSetCertificateRotationHeader émet l'en-tête de réponse
// « DSC-certificateRotation: True » (§2.2.2.8, MAY) si le renouvellement est
// activé en base (dsc_settings.certificate_rotation_enabled) et que le
// certificat du nœud est dans la fenêtre certificate_renewal_days (ou déjà
// expiré). L'en-tête invite le client (LCM) à tenter le message CertificateRotation.
//
// Appelé par les handlers DSC liés à un nœud (GetDscAction, ConfigurationContent,
// SendReport). Doit être invoqué avant d'écrire la réponse (avant WriteHeader).
func maybeSetCertificateRotationHeader(w http.ResponseWriter, dbConn *sql.DB, agentId string) {
	if dbConn == nil {
		return
	}
	var force int
	var notAfter string
	// CAST(... AS INT) : force_cert_rotation est BIT (MSSQL) ou INTEGER (SQLite).
	// Le CAST rend le scan en int portable entre les deux drivers.
	if err := dbConn.QueryRow(`SELECT CAST(force_cert_rotation AS INT), certificate_notafter FROM agents WHERE agent_id = ?`, agentId).Scan(&force, &notAfter); err != nil {
		_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Failed to read agent %q for rotation decision: %v", agentId, err))
		return
	}
	// Demande forcée par un admin (bouton « renouveler maintenant ») : on émet
	// l'en-tête sans tenir compte de la fenêtre ni du flag global d'activation.
	if force == 1 {
		w.Header().Set("DSC-certificateRotation", "True")
		_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Soliciting certificate rotation for agentId=%q: forced by admin (force_cert_rotation=1)", agentId))
		return
	}
	settings, err := db.GetDscSettings(dbConn)
	if err != nil {
		_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Failed to load dsc_settings: %v", err))
		return
	}
	if !settings.CertificateRotationEnabled {
		return
	}
	needs, err := certNeedsRotation(notAfter, settings.CertificateRenewalDays, time.Now().UTC())
	if err != nil {
		_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Failed to evaluate certificate renewal for agentId=%q: %v", agentId, err))
		return
	}
	if needs {
		w.Header().Set("DSC-certificateRotation", "True")
		_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Soliciting certificate rotation for agentId=%q: certificate within renewal window (notAfter=%q, days=%d)", agentId, notAfter, settings.CertificateRenewalDays))
	}
}

// asString renvoie la valeur string d'une clé d'un objet JSON, ou "".
func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// CertificateRotationHandler gère POST /PSDSCPullServer.svc/Nodes(AgentId='...')/CertificateRotation
// (§3.12 du MS-DSCPM). Le client (LCM) y publie son nouveau certificat ; le
// serveur met à jour les champs certificat du nœud en place, sans toucher à
// registered_at / state / last_communication.
//
// Sécurité : la liaison identité (verifyNodeCertBinding) exige que le certificat
// présenté au TLS soit celui actuellement enregistré pour ce nœud (l'ANCIEN
// certificat). Le corps de la requête porte le NOUVEAU CertificateInformation.
// Ainsi, un attaquant qui intercepterait la requête ne peut pas renouveler le
// certificat d'un nœud dont il ne possède pas l'ancienne clé privée (pas de
// vecteur de détournement : il faudrait déjà être en possession du certificat
// courant pour être autorisé à le remplacer).
func CertificateRotationHandler(w http.ResponseWriter, r *http.Request) {
	rawId := r.PathValue("node")
	agentId := utils.ExtractAgentId(rawId)
	if agentId == "" {
		http.Error(w, "Bad request: missing AgentId", http.StatusBadRequest)
		return
	}
	database := db.Shared()
	if database == nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Start for agentId=%q (raw=%s)", agentId, rawId))

	// Liaison identité : le certificat client présenté (l'ACTUEL) doit
	// correspondre au nœud qui demande la rotation.
	if ok, cause := verifyNodeCertBinding(r, database, agentId); !ok {
		_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Certificate-to-node binding refused for agentId=%q: %s", agentId, cause))
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// La rotation n'est acceptée que si le serveur a signalé le besoin (en-tête
	// DSC-certificateRotation émis précédemment) : activation globale en base OU
	// demande forcée d'un admin (bouton « renouveler maintenant »). Conforme §3.12
	// (MAY : le serveur peut rejeter une rotation non sollicitée).
	var force int
	_ = database.QueryRow(`SELECT CAST(force_cert_rotation AS INT) FROM agents WHERE agent_id = ?`, agentId).Scan(&force)
	settings, err := db.GetDscSettings(database)
	rotationAllowed := (err == nil && settings.CertificateRotationEnabled) || force == 1
	if !rotationAllowed {
		_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Certificate rotation not enabled for agentId=%q: rejecting", agentId))
		http.Error(w, "Conflict: certificate rotation is not enabled", http.StatusConflict)
		return
	}

	body, err := readBodyLimited(w, r, maxDSCRequestBody)
	if err != nil {
		return
	}
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Bad request: invalid JSON", http.StatusBadRequest)
		return
	}
	certInfo, _ := req["CertificateInformation"].(map[string]interface{})
	if certInfo == nil {
		http.Error(w, "Bad request: CertificateInformation missing", http.StatusBadRequest)
		return
	}
	thumbprint := strings.ToLower(strings.TrimSpace(asString(certInfo["Thumbprint"])))
	if thumbprint == "" {
		http.Error(w, "Bad request: CertificateInformation.Thumbprint missing", http.StatusBadRequest)
		return
	}
	subject := asString(certInfo["Subject"])
	issuer := asString(certInfo["Issuer"])
	notbefore := asString(certInfo["NotBefore"])
	notafter := asString(certInfo["NotAfter"])

	// Vérifie que le nœud existe (404 sinon).
	var exists int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = ?`, agentId).Scan(&exists); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if exists == 0 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// Met à jour le certificat et lève le drapeau de renouvellement forcé : la
	// rotation a abouti, le nœud ne doit plus être sollicité « ASAP ».
	if _, err := database.Exec(`UPDATE agents SET certificate_thumbprint=?, certificate_subject=?, certificate_issuer=?, certificate_notbefore=?, certificate_notafter=?, force_cert_rotation=0 WHERE agent_id=?`,
		thumbprint, subject, issuer, notbefore, notafter, agentId); err != nil {
		_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Failed to update certificate for agentId=%q: %v", agentId, err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	_ = logs.WriteLogFile(fmt.Sprintf("[CERTROTATION] Rotated certificate for agentId=%q: new thumbprint=%s", agentId, thumbprint))

	// Réponse : aucune donnée (§3.12.5.1.1.2), statut 201 Created.
	w.Header().Set("ProtocolVersion", "2.0")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("{}"))
}
