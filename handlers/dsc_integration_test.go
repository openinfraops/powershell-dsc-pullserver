package handlers

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/service"
)

const (
	// Clé d'enregistrement utilisée par les tests d'enregistrement (signature DSC).
	dscRegKey = "dsc-registration-test-key-0123456789abcdef"
	// Date x-ms-date signée pour la requête d'enregistrement.
	dscRegXmsDate = "2026-11-26T00:00:00Z"
	// Identité du nœud utilisé par le test d'enregistrement.
	dscRegAgentID    = "11111111-2222-3333-4444-555555555555"
	dscRegNodeName   = "REGISTER-NODE"
	dscRegConfigName = "webconfig"

	// Identité du nœud utilisé par les tests GetDscAction / ConfigurationContent.
	// Distincte de l'identité d'enregistrement pour ne jamais partager la clé de
	// compilation en cours (service.activeCompiles) entre deux tests.
	dscAgentID    = "66666666-7777-8888-9999-000000000000"
	dscNodeName   = "PULL-NODE"
	dscConfigName = "webconfig"

	// Module utilisé par le test de téléchargement de module.
	dscModuleName    = "xWebSite"
	dscModuleVersion = "2.6.0.0"
)

// setupDSCIntegrationDB crée une base SQLite en mémoire avec toutes les tables
// nécessaires aux handler DSC : agents (via setupAgentsDB), agent_configurations,
// configuration_model et modules.
func setupDSCIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	database := setupAgentsDB(t)

	mustExec(t, database, `CREATE TABLE agent_configurations (
		agent_id TEXT,
		configuration_name TEXT,
		PRIMARY KEY (agent_id, configuration_name)
	)`)

	mustExec(t, database, `CREATE TABLE configuration_model (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		original_name TEXT,
		previous_id INTEGER,
		upload_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		uploaded_by TEXT NOT NULL,
		mof_file BLOB NOT NULL,
		last_usage TIMESTAMP
	)`)

	mustExec(t, database, `CREATE TABLE modules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		version TEXT NOT NULL,
		checksum TEXT NOT NULL,
		zip_blob BLOB NOT NULL,
		uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)

	return database
}

// injectDSCShared complète injectShared avec les champs requis par le handler
// d'enregistrement : le driver SQL (pour choisir la branche INSERT SQLite) et la
// clé d'enregistrement. Le nettoyage (restauration de db.Shared et
// global.AppConfig) est géré par injectShared dans t.Cleanup.
func injectDSCShared(t *testing.T, database *sql.DB, driver, registrationKey string, enableCertValidation bool) {
	t.Helper()
	injectShared(t, database, enableCertValidation)
	global.AppConfig.Database.Driver = driver
	global.AppConfig.DSCPullServer.RegistrationKey = registrationKey
}

// thumbprintOf calcule l'empreinte SHA-1 (hex, minuscule) d'un certificat client,
// exactement comme clientCertThumbprint la calcule côté handler.
func thumbprintOf(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	raw := sha1.Sum(cert.Raw)
	return strings.ToLower(hex.EncodeToString(raw[:]))
}

// dscRegAuth construit l'en-tête Authorization de la signature DSC
// (« Shared <sig> ») pour un corps et une date donnés, conformément à
// ValidateDSCRegistrationKey : base64(sha256(body)) + "\n" + xmsDate, puis
// HMAC-SHA256 avec la clé d'enregistrement.
func dscRegAuth(body []byte, xmsDate, registrationKey string) string {
	key := []byte(registrationKey)
	bodyHash := sha256.Sum256(body)
	bodyHashB64 := base64.StdEncoding.EncodeToString(bodyHash[:])
	stringToSign := bodyHashB64 + "\n" + xmsDate
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(stringToSign))
	return "Shared " + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// dscRegisterRequest construit la requête d'enregistrement DSC typique (PUT) en
// signant correctement le corps fourni.
func dscRegisterRequest(t *testing.T, body []byte, reqAgentID string, cert *x509.Certificate) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+reqAgentID+"')",
		strings.NewReader(string(body)))
	req.SetPathValue("node", "Nodes(AgentId='"+reqAgentID+"')")
	req.Header.Set("x-ms-date", dscRegXmsDate)
	req.Header.Set("Authorization", dscRegAuth(body, dscRegXmsDate, dscRegKey))
	if cert != nil {
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	}
	return req
}

// TestRegisterHandler_Success vérifie le parcours nominal d'enregistrement :
// signature valide → 200, agent inséré avec state='pending_apply',
// configuration liée au nœud.
func TestRegisterHandler_Success(t *testing.T) {
	database := setupDSCIntegrationDB(t)
	injectDSCShared(t, database, "sqlite", dscRegKey, true)

	cert := genDSCClientCert(t)
	thumb := thumbprintOf(t, cert)
	body := []byte(fmt.Sprintf(`{"AgentInformation":{"NodeName":%q,"LCMVersion":"1.0"},"RegistrationInformation":{"RegistrationMessageType":"Initial","CertificateInformation":{"Thumbprint":%q}},"ConfigurationNames":[%q]}`,
		dscRegNodeName, thumb, dscRegConfigName))

	rec := httptest.NewRecorder()
	RegisterHandler(rec, dscRegisterRequest(t, body, dscRegAgentID, cert))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp schema.RegisterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if resp.AgentId != dscRegAgentID {
		t.Fatalf("expected AgentId %q, got %q", dscRegAgentID, resp.AgentId)
	}

	var nodeName, state string
	if err := database.QueryRow(`SELECT node_name, state FROM agents WHERE agent_id = ?`, dscRegAgentID).Scan(&nodeName, &state); err != nil {
		t.Fatalf("query registered agent: %v", err)
	}
	if nodeName != dscRegNodeName {
		t.Fatalf("expected node_name %q, got %q", dscRegNodeName, nodeName)
	}
	if state != "pending_apply" {
		t.Fatalf("expected state 'pending_apply', got %q", state)
	}

	var cfgCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agent_configurations WHERE agent_id = ? AND configuration_name = ?`, dscRegAgentID, dscRegConfigName).Scan(&cfgCount); err != nil {
		t.Fatalf("query linked configuration: %v", err)
	}
	if cfgCount != 1 {
		t.Fatalf("expected 1 linked configuration, got %d", cfgCount)
	}

	// L'enregistrement déclenche une compilation asynchrone (no-op sans dépôt de
	// configuration). On attend sa fin pour éviter qu'elle lise db.Shared() après
	// le nettoyage (restauration du DB partagé) du test.
	service.WaitForNodeCompile(dscRegNodeName, 2*time.Second)
}

// TestRegisterHandler_BadSignature vérifie qu'une signature DSC invalide est
// rejetée en 401.
func TestRegisterHandler_BadSignature(t *testing.T) {
	database := setupDSCIntegrationDB(t)
	injectDSCShared(t, database, "sqlite", dscRegKey, true)

	body := []byte(`{"AgentInformation":{"NodeName":"NODE-BAD"}}`)
	req := dscRegisterRequest(t, body, dscRegAgentID, nil)
	// Corrompt la signature : on remplace l'en-tête par une signature erronée.
	req.Header.Set("Authorization", "Shared c29tZS1iYWQtc2lnbmF0dXJl")

	rec := httptest.NewRecorder()
	RegisterHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestGetDscAction_Success_OK vérifie que GetDscAction renvoie NodeStatus=OK
// lorsque l'empreinte envoyée par le nœud correspond au MOF stocké.
func TestGetDscAction_Success_OK(t *testing.T) {
	database := setupDSCIntegrationDB(t)
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	thumb := thumbprintOf(t, cert)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		dscAgentID, dscNodeName, thumb, "compliant")
	mustExec(t, database, `INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`,
		dscAgentID, dscConfigName)

	mof := []byte("instance of MSFT_RoleResource as $ref { ResourceID = \"[xWebSite]demo\"; };")
	mustExec(t, database, `INSERT INTO configuration_model (name, mof_file, uploaded_by) VALUES (?, ?, ?)`,
		dscConfigName, mof, "test")

	checksum := sha256SumHex(mof)
	body := fmt.Sprintf(`{"ClientStatus":[{"ConfigurationName":%q,"Checksum":%q,"ChecksumAlgorithm":"SHA-256"}]}`,
		dscConfigName, checksum)

	req := httptest.NewRequest(http.MethodPost,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+dscAgentID+"')/GetDscAction",
		strings.NewReader(body))
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	GetDscActionNodeHandlerWithId(rec, req, dscAgentID)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp schema.DscActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode GetDscAction response: %v", err)
	}
	if resp.NodeStatus != "OK" {
		t.Fatalf("expected NodeStatus OK, got %q", resp.NodeStatus)
	}
	if len(resp.Details) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(resp.Details))
	}
	if resp.Details[0].ConfigurationName != dscConfigName || resp.Details[0].Status != "OK" {
		t.Fatalf("unexpected detail: %+v", resp.Details[0])
	}
}

// TestGetDscAction_403_CertMismatch vérifie qu'un certificat client qui ne
// correspond pas à l'empreinte enregistrée du nœud est refusé en 403.
func TestGetDscAction_403_CertMismatch(t *testing.T) {
	database := setupDSCIntegrationDB(t)
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	// On enregistre une empreinte DIFFÉRENTE de celle du certificat présenté.
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		dscAgentID, dscNodeName, "aabbccddeeff00112233445566778899aabbccdd", "compliant")
	mustExec(t, database, `INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`,
		dscAgentID, dscConfigName)

	req := httptest.NewRequest(http.MethodPost,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+dscAgentID+"')/GetDscAction",
		strings.NewReader(`{"ClientStatus":[]}`))
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	GetDscActionNodeHandlerWithId(rec, req, dscAgentID)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestConfigurationContent_Success vérifie que ConfigurationContent sert le MOF
// et renvoie l'en-tête Checksum SHA-256 attendu.
func TestConfigurationContent_Success(t *testing.T) {
	database := setupDSCIntegrationDB(t)
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	thumb := thumbprintOf(t, cert)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		dscAgentID, dscNodeName, thumb, "compliant")
	mustExec(t, database, `INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`,
		dscAgentID, dscConfigName)

	mof := []byte("instance of MSFT_RoleResource as $ref { ResourceID = \"[xWebSite]demo\"; };")
	mustExec(t, database, `INSERT INTO configuration_model (name, mof_file, uploaded_by) VALUES (?, ?, ?)`,
		dscConfigName, mof, "test")

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+dscAgentID+"')/Configurations(ConfigurationName='"+dscConfigName+"')/ConfigurationContent",
		nil)
	req.SetPathValue("node", "Nodes(AgentId='"+dscAgentID+"')")
	req.SetPathValue("config", "Configurations(ConfigurationName='"+dscConfigName+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	ConfigurationContentHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != string(mof) {
		t.Fatalf("expected MOF %q, got %q", string(mof), got)
	}
	expectedChecksum := strings.ToUpper(hex.EncodeToString(func() []byte { h := sha256.Sum256(mof); return h[:] }()))
	if got := rec.Header().Get("Checksum"); got != expectedChecksum {
		t.Fatalf("expected Checksum %q, got %q", expectedChecksum, got)
	}
	if got := rec.Header().Get("ChecksumAlgorithm"); got != "SHA-256" {
		t.Fatalf("expected ChecksumAlgorithm SHA-256, got %q", got)
	}
}

// TestConfigurationContent_403_CertMismatch vérifie qu'un certificat client qui
// ne correspond pas au nœud est refusé en 403.
func TestConfigurationContent_403_CertMismatch(t *testing.T) {
	database := setupDSCIntegrationDB(t)
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		dscAgentID, dscNodeName, "aabbccddeeff00112233445566778899aabbccdd", "compliant")
	mustExec(t, database, `INSERT INTO agent_configurations (agent_id, configuration_name) VALUES (?, ?)`,
		dscAgentID, dscConfigName)

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+dscAgentID+"')/Configurations(ConfigurationName='"+dscConfigName+"')/ConfigurationContent",
		nil)
	req.SetPathValue("node", "Nodes(AgentId='"+dscAgentID+"')")
	req.SetPathValue("config", "Configurations(ConfigurationName='"+dscConfigName+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	ConfigurationContentHandler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestConfigurationContent_403_UnknownAgent vérifie qu'un agentId inconnu en base
// (ou « en déshérence ») est refusé en 403 : la liaison identité échoue car le
// nœud n'existe pas.
func TestConfigurationContent_403_UnknownAgent(t *testing.T) {
	database := setupDSCIntegrationDB(t)
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	// Aucune ligne agents pour dscAgentID : verifyNodeCertBinding ne trouve pas
	// d'empreinte enregistrée → refus.
	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+dscAgentID+"')/Configurations(ConfigurationName='"+dscConfigName+"')/ConfigurationContent",
		nil)
	req.SetPathValue("node", "Nodes(AgentId='"+dscAgentID+"')")
	req.SetPathValue("config", "Configurations(ConfigurationName='"+dscConfigName+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	ConfigurationContentHandler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestConfigurationContent_404_ConfigNotBound vérifie qu'une configuration qui
// n'est pas rattachée au nœud n'est pas servie.
//
// NB : l'issue #82 décrit ce cas comme « 403 », mais le handler
// ConfigurationContent renvoie ici **404** (« Configuration not found ») — la
// liaison configuration se fait APRÈS la liaison identité (certificat) et
// renvoie 404, pas 403. Ce test documente le comportement réel.
func TestConfigurationContent_404_ConfigNotBound(t *testing.T) {
	database := setupDSCIntegrationDB(t)
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	thumb := thumbprintOf(t, cert)
	mustExec(t, database, `INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		dscAgentID, dscNodeName, thumb, "compliant")
	// Aucune ligne agent_configurations : la configuration n'est pas liée au nœud.

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Nodes(AgentId='"+dscAgentID+"')/Configurations(ConfigurationName='"+dscConfigName+"')/ConfigurationContent",
		nil)
	req.SetPathValue("node", "Nodes(AgentId='"+dscAgentID+"')")
	req.SetPathValue("config", "Configurations(ConfigurationName='"+dscConfigName+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	ConfigurationContentHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestModuleDownload_Success vérifie que ModuleDownload sert le zip du module
// quand nom, version et checksum correspondent.
func TestModuleDownload_Success(t *testing.T) {
	database := setupDSCIntegrationDB(t)

	zip := []byte("PK\x03\x04fake-dsc-module-zip-content")
	checksum := base64.StdEncoding.EncodeToString(zip)
	mustExec(t, database, `INSERT INTO modules (name, version, checksum, zip_blob) VALUES (?, ?, ?, ?)`,
		dscModuleName, dscModuleVersion, checksum, zip)

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Modules(ModuleName='"+dscModuleName+"')/ModuleContent?ModuleName="+dscModuleName+"&ModuleVersion="+dscModuleVersion+"&Checksum="+checksum,
		nil)
	rec := httptest.NewRecorder()
	ModuleDownloadHandler(database)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != string(zip) {
		t.Fatalf("expected module zip %q, got %q", string(zip), got)
	}
	if got := rec.Header().Get("Checksum"); got != checksum {
		t.Fatalf("expected Checksum %q, got %q", checksum, got)
	}
}

// TestModuleDownload_404_NotFound vérifie que ModuleDownload renvoie 404 pour un
// module inconnu.
func TestModuleDownload_404_NotFound(t *testing.T) {
	database := setupDSCIntegrationDB(t)

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Modules(ModuleName='nope')/ModuleContent?ModuleName=nope&ModuleVersion=9.9.9",
		nil)
	rec := httptest.NewRecorder()
	ModuleDownloadHandler(database)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestModuleDownload_400_ChecksumMismatch vérifie que ModuleDownload renvoie 400
// quand le checksum demandé ne correspond pas à celui du module.
func TestModuleDownload_400_ChecksumMismatch(t *testing.T) {
	database := setupDSCIntegrationDB(t)

	zip := []byte("PK\x03\x04fake-dsc-module-zip-content")
	checksum := base64.StdEncoding.EncodeToString(zip)
	mustExec(t, database, `INSERT INTO modules (name, version, checksum, zip_blob) VALUES (?, ?, ?, ?)`,
		dscModuleName, dscModuleVersion, checksum, zip)

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Modules(ModuleName='"+dscModuleName+"')/ModuleContent?ModuleName="+dscModuleName+"&ModuleVersion="+dscModuleVersion+"&Checksum=bG90cy1vZi13cm9uZy1jaGVja3N1bQ==",
		nil)
	rec := httptest.NewRecorder()
	ModuleDownloadHandler(database)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestModuleDownload_400_MissingFields vérifie que ModuleDownload renvoie 400
// quand ModuleName ou ModuleVersion est absent.
func TestModuleDownload_400_MissingFields(t *testing.T) {
	database := setupDSCIntegrationDB(t)

	req := httptest.NewRequest(http.MethodGet,
		"/PSDSCPullServer.svc/Modules(ModuleName='x')/ModuleContent?ModuleName="+dscModuleName,
		nil)
	rec := httptest.NewRecorder()
	ModuleDownloadHandler(database)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestRegisterHandler_NoConfigLink_WhenAgentNotCreated vérifie qu'en cas d'échec
// de création de l'agent (upsert en erreur), le handler NE crée PAS de liaison de
// configuration (agent_configurations). Sans ce garde, l'INSERT référençait un
// agent_id inexistant et violait la FOREIGN KEY en production MSSQL
// ("Failed to assign node configuration", FK__agent_con__agent__0B91BA14).
func TestRegisterHandler_NoConfigLink_WhenAgentNotCreated(t *testing.T) {
	database := setupAgentsDB(t)
	// Re-crée la table agents avec une colonne NOT NULL sans défaut que
	// upsertAgent ne fournit pas : l'INSERT de l'agent échoue, simulant un échec
	// de création de l'agent (comme constaté en production). On ré-attache aussi
	// agent_configurations (sans FK, pour observer le comportement du garde
	// indépendamment de la contrainte).
	mustExec(t, database, `DROP TABLE agents`)
	mustExec(t, database, `CREATE TABLE agents (
		agent_id TEXT PRIMARY KEY,
		node_name TEXT,
		lcm_version TEXT,
		registration_type TEXT,
		certificate_thumbprint TEXT,
		certificate_subject TEXT,
		certificate_issuer TEXT,
		certificate_notbefore TEXT,
		certificate_notafter TEXT,
		certificate_public_key TEXT,
		certificate_key_algorithm TEXT,
		registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_communication TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		has_error_last_report BOOLEAN DEFAULT 0,
		needs_recompile INTEGER DEFAULT 0,
		force_cert_rotation INTEGER DEFAULT 0,
		deleted_at TIMESTAMP NULL,
		deleted_by TEXT NULL,
		state TEXT,
		reboot_pending INTEGER DEFAULT 0,
		reboot_pending_since TIMESTAMP NULL,
		reboot_requested_by TEXT NULL,
		reboot_loop INTEGER DEFAULT 0,
		reboot_request_count INTEGER DEFAULT 0,
		persistent INTEGER DEFAULT 0,
		reregistration_window_until TIMESTAMP NULL,
		must_fail TEXT NOT NULL
	)`)
	mustExec(t, database, `CREATE TABLE agent_configurations (
		agent_id TEXT,
		configuration_name TEXT,
		PRIMARY KEY (agent_id, configuration_name)
	)`)
	injectDSCShared(t, database, "sqlite", dscRegKey, true)

	cert := genDSCClientCert(t)
	thumb := thumbprintOf(t, cert)
	// Aucun ConfigurationNames : le chemin « nouveau nœud sans config » qui
	// tentait d'assigner la config par défaut (node_name).
	body := []byte(fmt.Sprintf(`{"AgentInformation":{"NodeName":%q,"LCMVersion":"1.0"},"RegistrationInformation":{"RegistrationMessageType":"Initial","CertificateInformation":{"Thumbprint":%q}}}`,
		dscRegNodeName, thumb))

	rec := httptest.NewRecorder()
	RegisterHandler(rec, dscRegisterRequest(t, body, dscRegAgentID, cert))

	// Le handler répond 200 même si l'upsert échoue (l'erreur est seulement tracée).
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var cfgCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM agent_configurations WHERE agent_id = ?`, dscRegAgentID).Scan(&cfgCount); err != nil {
		t.Fatalf("query agent_configurations: %v", err)
	}
	if cfgCount != 0 {
		t.Fatalf("expected 0 linked configurations for an uncreated agent, got %d", cfgCount)
	}
}
