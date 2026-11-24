package handlers

import (
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
)

func TestParseCertTime(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{in: "", want: time.Time{}},
		{in: "2024-01-02T03:04:05Z", want: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{in: "2024-01-02T03:04:05.0000000Z", want: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{in: "2024-01-02T03:04:05.1234567", want: time.Date(2024, 1, 2, 3, 4, 5, 123456700, time.UTC)},
		{in: "2024-01-02 03:04:05", want: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{in: "2024-01-02", want: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
		{in: "Mon, 02 Jan 2024 03:04:05 GMT", want: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{in: "not-a-date", wantErr: true},
	}
	for _, c := range cases {
		got, err := parseCertTime(c.in)
		if c.wantErr && err == nil {
			t.Errorf("parseCertTime(%q): expected error", c.in)
			continue
		}
		if !c.wantErr && err != nil {
			t.Errorf("parseCertTime(%q): unexpected error %v", c.in, err)
			continue
		}
		if !c.wantErr && !got.Equal(c.want) {
			t.Errorf("parseCertTime(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCertNeedsRotation(t *testing.T) {
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		notAfter     string
		renewalDays  int
		wantRotation bool
	}{
		{notAfter: "2024-01-22T00:00:00Z", renewalDays: 30, wantRotation: true},
		{notAfter: "2024-02-16T00:00:00Z", renewalDays: 30, wantRotation: false},
		{notAfter: "2023-12-01T00:00:00Z", renewalDays: 30, wantRotation: true},
		{notAfter: "garbage", renewalDays: 30, wantRotation: true},
		{notAfter: "2024-01-05T00:00:00Z", renewalDays: 0, wantRotation: false},
		{notAfter: "", renewalDays: 30, wantRotation: false},
	}
	for _, c := range cases {
		got, err := certNeedsRotation(c.notAfter, c.renewalDays, now)
		if err != nil {
			t.Errorf("certNeedsRotation(%q,%d): unexpected error %v", c.notAfter, c.renewalDays, err)
			continue
		}
		if got != c.wantRotation {
			t.Errorf("certNeedsRotation(%q,%d) = %v, want %v", c.notAfter, c.renewalDays, got, c.wantRotation)
		}
	}
}

// setupDscSettingsDB crée la table dsc_settings et insère la rangée id=1.
func setupDscSettingsDB(database *sql.DB, enabled bool) error {
	if _, err := database.Exec(`CREATE TABLE dsc_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		certificate_rotation_enabled INTEGER NOT NULL DEFAULT 0,
		certificate_renewal_days INTEGER NOT NULL DEFAULT 30,
		legacy_imported INTEGER NOT NULL DEFAULT 0,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	en := 0
	if enabled {
		en = 1
	}
	_, err := database.Exec(`INSERT INTO dsc_settings (id, certificate_rotation_enabled, certificate_renewal_days, legacy_imported) VALUES (1, ?, 30, 1)`, en)
	return err
}

func injectShared(t *testing.T, database *sql.DB, enableCertValidation bool) {
	t.Helper()
	oldShared := db.Shared()
	oldAppConfig := global.AppConfig
	t.Cleanup(func() {
		db.SetShared(oldShared)
		global.AppConfig = oldAppConfig
	})
	db.SetShared(database)
	global.AppConfig = &schema.AppConfig{
		DSCPullServer: schema.HTTPConfig{EnableClientCertValidation: enableCertValidation},
	}
}

func TestCertificateRotationHandler_RotatesCert(t *testing.T) {
	database := setupAgentsDB(t)
	if err := setupDscSettingsDB(database, true); err != nil {
		t.Fatal(err)
	}
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	sum := sha1.Sum(cert.Raw)
	oldThumb := strings.ToLower(hex.EncodeToString(sum[:]))
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		agentID, "NODE", oldThumb, "compliant"); err != nil {
		t.Fatal(err)
	}

	const newThumb = "aabbccddeeff00112233445566778899aabbccdd"
	body := fmt.Sprintf(`{"RotationInformation":{"CertificateInformation":{"Thumbprint":"%s","Subject":"NewSubj","Issuer":"NewIssuer","NotBefore":"2026-01-01T00:00:00Z","NotAfter":"2031-01-01T00:00:00Z"}}}`, newThumb)
	req := httptest.NewRequest(http.MethodPost, "/PSDSCPullServer.svc/Nodes(AgentId='"+agentID+"')/CertificateRotation", strings.NewReader(body))
	req.SetPathValue("node", "Nodes(AgentId='"+agentID+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	CertificateRotationHandler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var stored string
	if err := database.QueryRow(`SELECT certificate_thumbprint FROM agents WHERE agent_id = ?`, agentID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != newThumb {
		t.Fatalf("expected thumbprint %q, got %q", newThumb, stored)
	}
}

// TestCertificateRotationHandler_StoresPublicKey vérifie que le renouvellement
// stocke la clé publique du NOUVEAU certificat (champ "PublicKey" du
// CertificateInformation, base64 DER §3.12) dans certificate_public_key /
// certificate_key_algorithm.
func TestCertificateRotationHandler_StoresPublicKey(t *testing.T) {
	database := setupAgentsDB(t)
	if err := setupDscSettingsDB(database, true); err != nil {
		t.Fatal(err)
	}
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	sum := sha1.Sum(cert.Raw)
	oldThumb := strings.ToLower(hex.EncodeToString(sum[:]))
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		agentID, "NODE", oldThumb, "compliant"); err != nil {
		t.Fatal(err)
	}

	// La clé publique du nouveau certificat, encodée en base64 (DER SubjectPublicKeyInfo).
	der, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	pubKeyB64 := base64.StdEncoding.EncodeToString(der)

	const newThumb = "aabbccddeeff00112233445566778899aabbccdd"
	body := fmt.Sprintf(`{"RotationInformation":{"CertificateInformation":{"Thumbprint":"%s","Subject":"NewSubj","Issuer":"NewIssuer","NotBefore":"2026-01-01T00:00:00Z","NotAfter":"2031-01-01T00:00:00Z","PublicKey":"%s"}}}`, newThumb, pubKeyB64)
	req := httptest.NewRequest(http.MethodPost, "/PSDSCPullServer.svc/Nodes(AgentId='"+agentID+"')/CertificateRotation", strings.NewReader(body))
	req.SetPathValue("node", "Nodes(AgentId='"+agentID+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	CertificateRotationHandler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var storedPubKey, storedAlgo string
	if err := database.QueryRow(`SELECT certificate_public_key, certificate_key_algorithm FROM agents WHERE agent_id = ?`, agentID).Scan(&storedPubKey, &storedAlgo); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(storedPubKey, "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("expected PEM PUBLIC KEY in certificate_public_key, got %q", storedPubKey)
	}
	if storedAlgo != "ECDSA" {
		t.Fatalf("expected certificate_key_algorithm ECDSA, got %q", storedAlgo)
	}
}

func TestCertificateRotationHandler_DisabledRejects(t *testing.T) {
	database := setupAgentsDB(t)
	if err := setupDscSettingsDB(database, false); err != nil {
		t.Fatal(err)
	}
	injectShared(t, database, true)

	cert := genDSCClientCert(t)
	sum := sha1.Sum(cert.Raw)
	thumb := strings.ToLower(hex.EncodeToString(sum[:]))
	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	if _, err := database.Exec(`INSERT INTO agents (agent_id, node_name, certificate_thumbprint, state) VALUES (?, ?, ?, ?)`,
		agentID, "NODE", thumb, "compliant"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/PSDSCPullServer.svc/Nodes(AgentId='"+agentID+"')/CertificateRotation",
		strings.NewReader(`{"CertificateInformation":{"Thumbprint":"aabbccddeeff00112233445566778899aabbccdd"}}`))
	req.SetPathValue("node", "Nodes(AgentId='"+agentID+"')")
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}

	rec := httptest.NewRecorder()
	CertificateRotationHandler(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when rotation disabled, got %d", rec.Code)
	}
}

func TestMaybeSetCertificateRotationHeader_WithinWindow(t *testing.T) {
	database := setupAgentsDB(t)
	if err := setupDscSettingsDB(database, true); err != nil {
		t.Fatal(err)
	}
	injectShared(t, database, false)

	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	future := time.Now().UTC().Add(10 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	if _, err := database.Exec(`INSERT INTO agents (agent_id, certificate_notafter) VALUES (?, ?)`, agentID, future); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	maybeSetCertificateRotationHeader(rec, database, agentID)
	if got := rec.Header().Get("DSC-certificateRotation"); got != "True" {
		t.Fatalf("expected DSC-certificateRotation: True, got %q", got)
	}
}

func TestMaybeSetCertificateRotationHeader_OutsideWindow(t *testing.T) {
	database := setupAgentsDB(t)
	if err := setupDscSettingsDB(database, true); err != nil {
		t.Fatal(err)
	}
	injectShared(t, database, false)

	const agentID = "09B8DF8D-2A1E-11EE-84B4-005056B8D6BE"
	far := time.Now().UTC().Add(90 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	if _, err := database.Exec(`INSERT INTO agents (agent_id, certificate_notafter) VALUES (?, ?)`, agentID, far); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	maybeSetCertificateRotationHeader(rec, database, agentID)
	if got := rec.Header().Get("DSC-certificateRotation"); got != "" {
		t.Fatalf("expected no DSC-certificateRotation header, got %q", got)
	}
}
