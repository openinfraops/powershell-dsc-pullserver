package handlers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClientPublicKeyInfo_FromTLSCert vérifie qu'on extrait la clé publique
// (PEM "PUBLIC KEY") et l'algorithme du certificat client présenté au TLS.
func TestClientPublicKeyInfo_FromTLSCert(t *testing.T) {
	cert := genDSCClientCert(t)

	req := httptest.NewRequest(http.MethodGet, "/PSDSCPullServer.svc/Modules(ModuleName='x')/ModuleContent", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	pemStr, algo := clientPublicKeyInfo(req)
	if !strings.HasPrefix(pemStr, "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("expected PEM PUBLIC KEY, got %q", pemStr)
	}
	if algo != "ECDSA" {
		t.Fatalf("expected algorithm ECDSA, got %q", algo)
	}
}

func TestClientPublicKeyInfo_NoTLS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	pemStr, algo := clientPublicKeyInfo(req)
	if pemStr != "" || algo != "" {
		t.Fatalf("expected empty for no TLS, got %q %q", pemStr, algo)
	}
}

// TestParsePublicKeyField_Base64 vérifie qu'un champ PublicKey base64 (DER
// SubjectPublicKeyInfo) est re-parsé en PEM + algorithme.
func TestParsePublicKeyField_Base64(t *testing.T) {
	cert := genDSCClientCert(t)
	der, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(der)

	pemStr, algo := parsePublicKeyField(b64)
	if !strings.HasPrefix(pemStr, "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("expected PEM PUBLIC KEY, got %q", pemStr)
	}
	if algo != "ECDSA" {
		t.Fatalf("expected algorithm ECDSA, got %q", algo)
	}
}

func TestParsePublicKeyField_EmptyAndGarbage(t *testing.T) {
	if pemStr, algo := parsePublicKeyField(""); pemStr != "" || algo != "" {
		t.Fatalf("expected empty for empty input, got %q %q", pemStr, algo)
	}
	if pemStr, algo := parsePublicKeyField("not-base64-not-der!!!"); pemStr != "" || algo != "" {
		t.Fatalf("expected empty for garbage input, got %q %q", pemStr, algo)
	}
}

func TestPublicKeyAlgorithm_RSA(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	if algo := publicKeyAlgorithm(&priv.PublicKey); algo != "RSA" {
		t.Fatalf("expected RSA, got %q", algo)
	}
}
