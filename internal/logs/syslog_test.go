package logs

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-dsc-pull/internal/schema"
)

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestParseLevel(t *testing.T) {
	cases := map[string]string{
		"INFO [BUILD] 1.8.0":        "info",
		"ERROR [DSC] boom":          "error",
		"WARN [SECURITY] no secret": "warning",
		"WARNING [X] y":             "warning",
		"ALERT [ERROR DETECTION] x": "critical",
		"[ERROR] failed something":  "error",
		"[DSC][TLS] client cert":    "info",
		"AgentTagsListHandler: err": "info",
		"DEBUG [D] trace":           "debug",
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Fatalf("parseLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSeverityFromLevel(t *testing.T) {
	cases := map[string]int{
		"debug":    7,
		"info":     6,
		"":         6,
		"warning":  4,
		"warn":     4,
		"error":    3,
		"critical": 2,
		"alert":    2,
	}
	for in, want := range cases {
		if got := severityFromLevel(in); got != want {
			t.Fatalf("severityFromLevel(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFacilityNumber(t *testing.T) {
	cases := map[string]int{
		"local0": 16,
		"local7": 23,
		"auth":   4,
		"daemon": 3,
		"":       16,
	}
	for in, want := range cases {
		if got, err := facilityNumber(in); err != nil || got != want {
			t.Fatalf("facilityNumber(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := facilityNumber("bogus"); err == nil {
		t.Fatal("facilityNumber(bogus) should error")
	}
}

func TestFormatRFC5424(t *testing.T) {
	frame := formatRFC5424(16, 6, "dsc-pull-server", "AUDIT", `[audit@48577 user="a"]`, "hello")
	if !strings.HasPrefix(frame, "<134>1 ") {
		t.Fatalf("PRI/version prefix wrong: %q", frame)
	}
	if !strings.Contains(frame, " dsc-pull-server ") {
		t.Fatalf("missing app-name: %q", frame)
	}
	if !strings.Contains(frame, ` [audit@48577 user="a"] hello`) {
		t.Fatalf("missing structured data / msg: %q", frame)
	}
	if !strings.Contains(frame, " AUDIT ") {
		t.Fatalf("missing msgid: %q", frame)
	}
}

func TestSanitizeSDParam(t *testing.T) {
	got := sanitizeSDParam(`a"b\c]d`)
	if got != "abcd" {
		t.Fatalf("sanitizeSDParam = %q, want abcd", got)
	}
}

func TestTruncateString(t *testing.T) {
	s := "héllo wörld"
	if got := truncateString(s, 8); len(got) > 8 || !strings.Contains(got, "h") {
		t.Fatalf("truncateString over %q: %q", s, got)
	}
	// ASCII case
	if got := truncateString("abcdef", 3); got != "abc" {
		t.Fatalf("truncateString abcdef = %q", got)
	}
}

// TestSyslogQueueDrop vérifie la politique drop-oldest: quand la file est
// pleine, le message le plus ancien est retiré et un compteur de pertes est
// incrémenté.
func TestSyslogQueueDrop(t *testing.T) {
	s := &syslogSink{
		queue: make(chan syslogMessage, 3),
	}
	for i := 0; i < 3; i++ {
		s.enqueue(syslogMessage{msg: fmt.Sprintf("msg-%d", i), severity: 6})
	}
	for i := 0; i < 20; i++ {
		s.enqueue(syslogMessage{msg: fmt.Sprintf("extra-%d", i), severity: 6})
	}
	if got := s.dropped.Load(); got == 0 {
		t.Fatal("expected dropped counter > 0, got 0")
	}
	if got := len(s.queue); got != 3 {
		t.Fatalf("expected queue len 3, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// startUDPReceiver démarre un serveur syslog UDP éphémère et renvoie son
// adresse et un canal de messages reçus.
func startUDPReceiver(t *testing.T) (addr string, got chan string) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("udp listen: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	got = make(chan string, 256)
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			got <- string(buf[:n])
		}
	}()
	return conn.LocalAddr().String(), got
}

// startStreamReceiver démarre un serveur syslog TCP (ou TLS) avec framing par
// saut de ligne et renvoie son adresse et un canal de messages reçus.
func startStreamReceiver(t *testing.T, isTLS bool, srvCert *tls.Certificate) (addr string, got chan string) {
	t.Helper()
	var ln net.Listener
	var err error
	if isTLS {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{*srvCert}})
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 256)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveStreamConn(c, got)
		}
	}()
	return ln.Addr().String(), got
}

func serveStreamConn(c net.Conn, got chan string) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		got <- strings.TrimRight(line, "\r\n")
	}
}

// generateTestCert génère une CA auto-signée et un certificat de serveur signé
// par cette CA (SAN 127.0.0.1 / localhost). Renvoie le PEM de la CA.
func generateTestCert(t *testing.T) (caPEM string, serverCert tls.Certificate) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen ca key: %v", err)
	}
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca cert: %v", err)
	}
	caPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))

	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen server key: %v", err)
	}
	srvTpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTpl, caTpl, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	srvPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvDER})
	keyDER, err := x509.MarshalECPrivateKey(srvKey)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	serverCert, err = tls.X509KeyPair(srvPEM, keyPEM)
	if err != nil {
		t.Fatalf("key pair: %v", err)
	}
	return caPEM, serverCert
}

// ---------------------------------------------------------------------------
// Integration tests (real network delivery over UDP / TCP / TCP+TLS)
// ---------------------------------------------------------------------------

func TestSyslogUDPSend(t *testing.T) {
	addr, got := startUDPReceiver(t)
	t.Cleanup(CloseSyslog)
	if err := InitSyslog(schema.SyslogConfig{Enabled: true, Network: "udp", Address: addr, Facility: "local0", AppName: "test", MinLevel: "info"}); err != nil {
		t.Fatalf("InitSyslog: %v", err)
	}
	WriteLogFile("INFO [TEST] hello over udp")
	WriteAudit("user@x", "login", "users", "User login", "10.0.0.1")
	select {
	case m := <-got:
		if !strings.Contains(m, "hello over udp") {
			t.Fatalf("missing msg: %q", m)
		}
		if !strings.Contains(m, " test ") {
			t.Fatalf("missing app-name: %q", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for UDP syslog message")
	}
}

func TestSyslogTCPSend(t *testing.T) {
	addr, got := startStreamReceiver(t, false, nil)
	t.Cleanup(CloseSyslog)
	if err := InitSyslog(schema.SyslogConfig{Enabled: true, Network: "tcp", Address: addr, Facility: "local0", AppName: "test", MinLevel: "info"}); err != nil {
		t.Fatalf("InitSyslog: %v", err)
	}
	WriteLogFile("ERROR [TEST] hello over tcp")
	select {
	case m := <-got:
		if !strings.Contains(m, "hello over tcp") {
			t.Fatalf("missing msg: %q", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for TCP syslog message")
	}
}

func TestSyslogTLSSend(t *testing.T) {
	caPEM, srvCert := generateTestCert(t)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, []byte(caPEM), 0600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	addr, got := startStreamReceiver(t, true, &srvCert)
	t.Cleanup(CloseSyslog)
	if err := InitSyslog(schema.SyslogConfig{Enabled: true, Network: "tcp+tls", Address: addr, CAFile: caPath, Facility: "local0", AppName: "test", MinLevel: "info"}); err != nil {
		t.Fatalf("InitSyslog: %v", err)
	}
	WriteLogFile("INFO [TEST] hello over tls")
	select {
	case m := <-got:
		if !strings.Contains(m, "hello over tls") {
			t.Fatalf("missing msg: %q", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for TLS syslog message")
	}
}

func TestSyslogTLSSendRejectsUntrusted(t *testing.T) {
	// CA du serveur non fournie -> la validation du certificat doit échouer
	// (aucun message ne doit partir sur une connexion non vérifiée).
	caPEM, srvCert := generateTestCert(t)
	_ = caPEM
	addr, got := startStreamReceiver(t, true, &srvCert)
	t.Cleanup(CloseSyslog)
	if err := InitSyslog(schema.SyslogConfig{Enabled: true, Network: "tcp+tls", Address: addr, Facility: "local0", AppName: "test", MinLevel: "info"}); err != nil {
		t.Fatalf("InitSyslog: %v", err)
	}
	WriteLogFile("ERROR [TEST] must not be delivered over untrusted tls")
	select {
	case m := <-got:
		t.Fatalf("message should not have been delivered: %q", m)
	case <-time.After(1200 * time.Millisecond):
	}
}

func TestSyslogMinLevelFilter(t *testing.T) {
	addr, got := startUDPReceiver(t)
	t.Cleanup(CloseSyslog)
	if err := InitSyslog(schema.SyslogConfig{Enabled: true, Network: "udp", Address: addr, Facility: "local0", AppName: "test", MinLevel: "error"}); err != nil {
		t.Fatalf("InitSyslog: %v", err)
	}
	WriteLogFile("INFO [TEST] should be filtered")
	WriteLogFile("ERROR [TEST] should be sent")
	select {
	case m := <-got:
		if !strings.Contains(m, "should be sent") {
			t.Fatalf("expected the error message, got %q", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for syslog message")
	}
	select {
	case m := <-got:
		t.Fatalf("info message should have been filtered, got %q", m)
	case <-time.After(500 * time.Millisecond):
	}
}

// TestSyslogNonBlocking vérifie que l'émission syslog ne bloque jamais, même
// quand le serveur syslog est injoignable (le goroutine de fond gère la
// reconnection avec backoff). On isole le chemin syslog en appelant directement
// l'enqueue, indépendamment de l'écriture du fichier local (qui, elle, reste
// volontairement synchrone et n'est pas le sujet de cette fonctionnalité).
func TestSyslogNonBlocking(t *testing.T) {
	// Port 1 sur loopback: refus de connexion quasi immédiat.
	t.Cleanup(CloseSyslog)
	if err := InitSyslog(schema.SyslogConfig{Enabled: true, Network: "tcp", Address: "127.0.0.1:1", Facility: "local0", AppName: "test", MinLevel: "info"}); err != nil {
		t.Fatalf("InitSyslog: %v", err)
	}
	start := time.Now()
	for i := 0; i < 10000; i++ {
		enqueueSyslogFromMessage("INFO [TEST] non-blocking")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("syslog enqueue blocked/felt slow: %v", elapsed)
	}
}

func TestInitSyslogValidation(t *testing.T) {
	t.Cleanup(CloseSyslog)
	cases := []schema.SyslogConfig{
		{Enabled: true, Network: "bogus", Address: "x:1"},
		{Enabled: true, Network: "tcp", Address: ""},
		{Enabled: true, Network: "tcp", Address: "x:1", MinLevel: "bogus"},
		{Enabled: true, Network: "tcp", Address: "x:1", Facility: "bogus"},
		{Enabled: true, Network: "tcp+tls", Address: "x:1", ClientCertFile: "only-cert.pem"},
	}
	for i, c := range cases {
		if err := InitSyslog(c); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestSyslogDisabledIsNoop(t *testing.T) {
	t.Cleanup(CloseSyslog)
	if err := InitSyslog(schema.SyslogConfig{Enabled: false}); err != nil {
		t.Fatalf("InitSyslog(disabled): %v", err)
	}
	if getSink() != nil {
		t.Fatal("expected no sink when disabled")
	}
	// WriteLogFile/WriteAudit must not panic when no sink.
	WriteLogFile("INFO [TEST] no sink")
	WriteAudit("u", "a", "t", "d", "1.2.3.4")
}
