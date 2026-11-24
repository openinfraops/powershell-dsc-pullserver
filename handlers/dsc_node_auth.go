package handlers

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
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

// publicKeyAlgorithm renvoie le nom de l'algorithme d'une clé publique
// (RSA / ECDSA / Ed25519), ou "UNKNOWN" pour un type non reconnu.
func publicKeyAlgorithm(pub crypto.PublicKey) string {
	switch pub.(type) {
	case *rsa.PublicKey:
		return "RSA"
	case *ecdsa.PublicKey:
		return "ECDSA"
	case ed25519.PublicKey:
		return "Ed25519"
	default:
		return "UNKNOWN"
	}
}

// marshalPublicKeyPEM encode une clé publique en PEM "PUBLIC KEY" (DER
// SubjectPublicKeyInfo). Renvoie "" si l'encodage échoue.
func marshalPublicKeyPEM(pub crypto.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// clientPublicKeyInfo renvoie la clé publique (PEM "PUBLIC KEY") et l'algorithme
// du premier certificat client présenté par le nœud au TLS. C'est la clé du
// certificat COURANT. Renvoie ("","") si aucun certificat n'a été présenté
// (ex. HTTP clair) ou si la clé n'est pas encodable.
func clientPublicKeyInfo(r *http.Request) (publicKey, algorithm string) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", ""
	}
	pub := r.TLS.PeerCertificates[0].PublicKey
	return marshalPublicKeyPEM(pub), publicKeyAlgorithm(pub)
}

// parsePublicKeyField interprète le champ "PublicKey" d'un CertificateInformation
// (spécifié en base64, §3.12) et renvoie la clé publique en PEM + l'algorithme.
// Tolère aussi un DER brut non encodé en base64. Renvoie ("","") si le champ est
// vide ou non exploitable (auquel cas l'appelant conserve la valeur existante).
func parsePublicKeyField(value string) (publicKey, algorithm string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	der, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		// Pas du base64 : on tente de l'interpréter comme du DER brut.
		der = []byte(value)
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return "", ""
	}
	return marshalPublicKeyPEM(pub), publicKeyAlgorithm(pub)
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
	// Compare case-insensitively: registered thumbprints were historically stored
	// in uppercase (the raw DSC registration Thumbprint), while clientCertThumbprint
	// always returns lowercase. Exact equality would reject pre-existing nodes.
	if !strings.EqualFold(registered, presented) {
		return false, "client certificate does not match node"
	}
	return true, ""
}
