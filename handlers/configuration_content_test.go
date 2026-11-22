package handlers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
)

// genDSCClientCert génère un certificat auto-signé représentant le certificat
// client qu'un nœud DSC présente à la poignée de main TLS.
func genDSCClientCert(t *testing.T) *x509.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "https://dsc-console.boursorama.fr:8484/PSDSCPullServer.svc"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

// TestConfigurationContentHandler_ExtractsAgentIdFromDSCWrapper vérifie que le
// handler extrait correctement l'AgentId du format DSC réel
// "Nodes(AgentId='...')/Configurations(ConfigurationName='...')/ConfigurationContent".
//
// Avant le correctif, agentId était le wrapper entier "Nodes(AgentId='...')" :
// la liaison certificat (verifyNodeCertBinding) et la liaison configuration
// (agent_configurations) ne matchaient jamais la ligne du nœud → 403, même avec
// un certificat dont l'empreinte correspondait exactement à celle en base.
func TestConfigurationContentHandler_ExtractsAgentIdFromDSCWrapper(t *testing.T) {
	database := setupAgentsDB(t)
	if _, err := database.Exec(`CREATE TABLE agent_configurations (agent_id TEXT, configuration_name TEXT)`); err != nil {
		t.Fatalf("create agent_configurations: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE configuration_model (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, original_name TEXT, previous_id INTEGER, upload_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP, uploaded_by TEXT NOT NULL, mof_file BLOB NOT NULL, last_usage TIMESTAMP NULL)`); err != nil {
		t.Fatalf("create configuration_model: %v", err)
	}

	cert := genDSCClientCert(t)
	sum := sha1.Sum(cert.Raw)
	thumbprint := strings.ToLower(hex.EncodeToString(sum[:]))

	const (
		agentID    = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
		configName = "rwappsaigui001"
	)

	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		agentID, "RWAPPSAIGUI001", thumbprint, "pending_apply"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`, agentID, configName); err != nil {
		t.Fatalf("seed config link: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO configuration_model (name, uploaded_by, mof_file) VALUES (?, ?, ?)`, configName, "tester", []byte("MOF-CONTENT")); err != nil {
		t.Fatalf("seed config model: %v", err)
	}

	// Injecte la base partagée et la config applicative (validation cert client ON).
	oldShared := db.Shared()
	oldAppConfig := global.AppConfig
	defer func() {
		db.SetShared(oldShared)
		global.AppConfig = oldAppConfig
	}()
	db.SetShared(database)
	global.AppConfig = &schema.AppConfig{
		DSCPullServer: schema.HTTPConfig{EnableClientCertValidation: true, BypassCAValidation: true},
	}

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+agentID+"')/Configurations(ConfigurationName='"+configName+"')/ConfigurationContent",
		nil)
	// Reproduit le routage ServeMux (Go 1.22+) qui découpe {node} / {config}.
	req.SetPathValue("node", "Nodes(AgentId='"+agentID+"')")
	req.SetPathValue("config", "Configurations(ConfigurationName='"+configName+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	ConfigurationContentHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "MOF-CONTENT" {
		t.Fatalf("expected MOF body, got %q", got)
	}
}
