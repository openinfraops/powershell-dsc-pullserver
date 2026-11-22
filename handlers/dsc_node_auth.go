package handlers

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strings"

	"go-dsc-pull/internal/global"
)

// clientCertThumbprint renvoie l'empreinte SHA-1 (hex, minuscule) du premier
// certificat client présenté par le nœud, ou "" si aucun certificat n'a été
// présenté (par ex. en HTTP clair).
func clientCertThumbprint(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	sum := sha1.Sum(r.TLS.PeerCertificates[0].Raw)
	return strings.ToLower(hex.EncodeToString(sum[:]))
}

// verifyNodeCertBinding vérifie que le certificat client présenté correspond à
// l'agent identifié par agentId (liaison identité cert → nœud). C'est le
// complément du filtre global « empreinte connue » du middleware DSC : ici on
// garantit que l'appelant est bien LE nœud qu'il prétend être, et pas un autre
// agent enregistré qui présenterait n'importe quelle empreinte connue.
//
// Si la validation de certificat client est désactivée, le déploiement a
// explicitement renoncé à l'identité mTLS (alerte émise au boot) : on ne bloque
// pas. Si elle est activée mais qu'aucun certificat n'est présenté (ex. HTTP
// clair), on ferme (fail-closed) car l'identité ne peut pas être établie.
func verifyNodeCertBinding(r *http.Request, dbConn *sql.DB, agentId string) (bool, string) {
	if global.AppConfig == nil || !global.AppConfig.DSCPullServer.EnableClientCertValidation {
		return true, ""
	}
	presented := clientCertThumbprint(r)
	if presented == "" {
		return false, "no client certificate presented"
	}
	var registered string
	if err := dbConn.QueryRow(
		`SELECT certificate_thumbprint FROM agents WHERE agent_id = ?`, agentId,
	).Scan(&registered); err != nil {
		return false, "unknown agent or missing certificate"
	}
	if registered != presented {
		return false, "client certificate does not match node"
	}
	return true, ""
}
