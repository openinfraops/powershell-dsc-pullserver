package logs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"go-dsc-pull/internal/schema"
)

const (
	// syslogQueueSize est la taille maximale de la file d'attente non bloquante.
	syslogQueueSize = 10000
	// syslogDialTimeout borne la connexion à un serveur syslog.
	syslogDialTimeout = 3 * time.Second
	// syslogWriteTimeout borne chaque écriture réseau.
	syslogWriteTimeout = 3 * time.Second
	// syslogShutdownFlushTime est la fenêtre de vidage de la file au shutdown.
	syslogShutdownFlushTime = 1 * time.Second
	// syslogShutdownTimeout borne l'attente d'arrêt du goroutine syslog.
	syslogShutdownTimeout = 5 * time.Second
	// syslogInitialBackoff / syslogMaxBackoff : reconnection avec backoff.
	syslogInitialBackoff = 500 * time.Millisecond
	syslogMaxBackoff     = 30 * time.Second
	// syslogDropWarnInterval limite la fréquence des WARN de file pleine.
	syslogDropWarnInterval = 30 * time.Second
	// syslogMaxMsgLen borne la longueur d'un message RFC 5424 émis.
	syslogMaxMsgLen = 48 * 1024
	// syslogAuditSDID est l'ID de données structurées portant les événements
	// d'audit, afin qu'ils forment un flux distinct et filtrable.
	syslogAuditSDID = "audit@48577"
	// syslogAuditSeverity est la sévérité (notice) des événements d'audit.
	syslogAuditSeverity = 5
)

// syslogMessage est un enregistrement prêt à être formaté en RFC 5424.
type syslogMessage struct {
	severity int
	msgID    string
	sd       string
	msg      string
}

// syslogSink est la sortie syslog (singleton). L'émission est non bloquante :
// l'appelant dépose un message dans une file bornée ; un goroutine de fond
// s'en sert pour ouvrir la connexion, formater et envoyer les messages, avec
// reconnection automatique et backoff.
type syslogSink struct {
	cfg         schema.SyslogConfig
	network     string
	address     string
	facility    int
	appName     string
	minSeverity int
	tlsConfig   *tls.Config
	queue       chan syslogMessage
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	dropped     atomic.Int64
	lastWarn    atomic.Int64
}

var (
	syslogMu  sync.RWMutex
	syslogPtr *syslogSink
)

// getSink retourne la sortie syslog courante, ou nil si désactivée.
func getSink() *syslogSink {
	syslogMu.RLock()
	defer syslogMu.RUnlock()
	return syslogPtr
}

// InitSyslog initialise (ou ré-initialise) la sortie syslog à partir de la
// configuration. Retourne une erreur pour une configuration invalide (le
// démarrage doit alors échouer). Si enabled est false, la sortie syslog est
// désactivée et la fonction est un no-op.
func InitSyslog(cfg schema.SyslogConfig) error {
	syslogMu.Lock()
	defer syslogMu.Unlock()

	// Arrête toute sortie précédente (utile en cas de ré-initialisation).
	if syslogPtr != nil {
		syslogPtr.stop()
		syslogPtr = nil
	}
	if !cfg.Enabled {
		return nil
	}

	network := strings.ToLower(strings.TrimSpace(cfg.Network))
	if network == "" {
		network = "tcp+tls"
	}
	if network != "udp" && network != "tcp" && network != "tcp+tls" {
		return fmt.Errorf("syslog.network %q invalide: doit être udp, tcp ou tcp+tls", cfg.Network)
	}
	if strings.TrimSpace(cfg.Address) == "" {
		return fmt.Errorf("syslog.address est obligatoire lorsque syslog est activé")
	}
	facility, err := facilityNumber(cfg.Facility)
	if err != nil {
		return err
	}
	appName := sanitizeAppName(cfg.AppName)
	minSev, err := severityFromLevelString(cfg.MinLevel)
	if err != nil {
		return err
	}

	var tlsCfg *tls.Config
	if network == "tcp+tls" {
		tlsCfg, err = buildSyslogTLSConfig(cfg)
		if err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &syslogSink{
		cfg:         cfg,
		network:     network,
		address:     cfg.Address,
		facility:    facility,
		appName:     appName,
		minSeverity: minSev,
		tlsConfig:   tlsCfg,
		queue:       make(chan syslogMessage, syslogQueueSize),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Sécurité : avertissements de démarrage pour les transports peu sûrs.
	if network == "udp" {
		writeFileLine("WARN [SYSLOG] Transport UDP (non chiffré, non fiable). Préférez tcp+tls (RFC 5425) en production.")
	}
	if network == "tcp+tls" && cfg.InsecureSkipVerify {
		writeFileLine("WARN [SYSLOG] La vérification du certificat du serveur syslog est désactivée (insecure_skip_verify: true).")
	}

	syslogPtr = s
	s.wg.Add(1)
	go s.run()
	return nil
}

// CloseSyslog arrête la sortie syslog et tente de vider la file restante.
// Idempotent ; à appeler au shutdown.
func CloseSyslog() {
	syslogMu.Lock()
	defer syslogMu.Unlock()
	if syslogPtr == nil {
		return
	}
	syslogPtr.stop()
	syslogPtr = nil
}

// buildSyslogTLSConfig construit la configuration TLS pour le transport
// tcp+tls (RFC 5425), en validant les fichiers d'autorités et la paire de
// certificat client éventuelle.
func buildSyslogTLSConfig(cfg schema.SyslogConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.InsecureSkipVerify {
		// #nosec G402 -- InsecureSkipVerify is an explicit, operator-controlled
		// setting (insecure_skip_verify) that is only honoured when the operator
		// opts in; it triggers a startup WARN and is refused by default. It is
		// never enabled implicitly.
		tlsCfg.InsecureSkipVerify = true
		return tlsCfg, nil
	}
	if cfg.CAFile != "" {
		pemData, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("syslog.ca_file: %v", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("syslog.ca_file: aucun certificat valide dans %s", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	if (cfg.ClientCertFile == "") != (cfg.ClientKeyFile == "") {
		return nil, fmt.Errorf("syslog.client_cert_file et syslog.client_key_file doivent être tous deux renseignés")
	}
	if cfg.ClientCertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("syslog certificat client: %v", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return tlsCfg, nil
}

// enqueueSyslogFromMessage analyse le niveau d'un message applicatif et le
// transmet à syslog (si activé), en respectant min_level. Non bloquant.
func enqueueSyslogFromMessage(message string) {
	s := getSink()
	if s == nil {
		return
	}
	sev := severityFromLevel(parseLevel(message))
	if sev > s.minSeverity {
		return
	}
	s.enqueue(syslogMessage{severity: sev, msgID: "-", sd: "-", msg: SanitizeLogMessage(message)})
}

// WriteAudit transmet un événement d'audit à syslog (si activé) comme flux
// distinct via des données structurées RFC 5424 (SD-ID audit@48577). L'audit
// ignore le filtre min_level : il est toujours émis. Non bloquant.
func WriteAudit(userEmail, action, target, details, ip string) {
	s := getSink()
	if s == nil {
		return
	}
	sd := fmt.Sprintf("[%s user=%q action=%q target=%q ip=%q]",
		syslogAuditSDID,
		sanitizeSDParam(userEmail),
		sanitizeSDParam(action),
		sanitizeSDParam(target),
		sanitizeSDParam(ip))
	s.enqueue(syslogMessage{severity: syslogAuditSeverity, msgID: "AUDIT", sd: sd, msg: SanitizeLogMessage(details)})
}

// enqueue dépose un message dans la file bornée. Si la file est pleine, elle
// applique une politique drop-oldest (retire le message le plus ancien) et
// incrémente un compteur de pertes, avec un WARN local limité en fréquence.
func (s *syslogSink) enqueue(m syslogMessage) {
	if m.msg == "" {
		m.msg = "-"
	}
	select {
	case s.queue <- m:
		return
	default:
	}
	// File pleine : retire le plus ancien message puis réessaie (drop-oldest).
	select {
	case <-s.queue:
		s.dropped.Add(1)
		s.warnDropped()
	default:
	}
	select {
	case s.queue <- m:
	default:
		s.dropped.Add(1)
		s.warnDropped()
	}
}

func (s *syslogSink) warnDropped() {
	now := time.Now().UnixNano()
	last := s.lastWarn.Load()
	if now-last < int64(syslogDropWarnInterval) {
		return
	}
	if s.lastWarn.CompareAndSwap(last, now) {
		writeFileLine(fmt.Sprintf("WARN [SYSLOG] File d'attente syslog pleine, %d message(s) supprimé(s) (drop-oldest).", s.dropped.Load()))
	}
}

// run est le goroutine de fond : ouvre la connexion, envoie les messages et se
// reconnecte avec backoff en cas d'échec. À la fin du contexte (shutdown), il
// tente de vider la file restante puis s'arrête.
func (s *syslogSink) run() {
	defer s.wg.Done()
	backoff := syslogInitialBackoff
	for {
		conn, err := s.connect()
		if err != nil {
			writeFileLine(fmt.Sprintf("WARN [SYSLOG] Connexion à %s/%s impossible: %v", s.address, s.network, err))
			if !s.sleepCtx(backoff) {
				s.dropRemaining()
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = syslogInitialBackoff

		reconnect := false
		for !reconnect {
			select {
			case <-s.ctx.Done():
				_ = conn.Close()
				s.flushRemaining(conn)
				return
			case m := <-s.queue:
				if err := s.write(conn, m); err != nil {
					writeFileLine(fmt.Sprintf("WARN [SYSLOG] Envoi à %s échoué: %v", s.address, err))
					_ = conn.Close()
					reconnect = true
				}
			}
		}
	}
}

// connect ouvre une connexion vers le serveur syslog selon le transport.
func (s *syslogSink) connect() (net.Conn, error) {
	dialer := &net.Dialer{Timeout: syslogDialTimeout}
	switch s.network {
	case "udp":
		return dialer.Dial("udp", s.address)
	case "tcp":
		return dialer.Dial("tcp", s.address)
	case "tcp+tls":
		return tls.DialWithDialer(dialer, "tcp", s.address, s.tlsConfig)
	}
	return nil, fmt.Errorf("network inconnue %q", s.network)
}

// write formate et envoie un message RFC 5424 sur la connexion. Pour les
// transports stream (tcp, tcp+tls), un saut de ligne est ajouté comme
// délimiteur (framing non-transparent, accepté par rsyslog/syslog-ng).
func (s *syslogSink) write(conn net.Conn, m syslogMessage) error {
	frame := formatRFC5424(s.facility, m.severity, s.appName, m.msgID, m.sd, m.msg)
	frame = truncateString(frame, syslogMaxMsgLen)
	if s.network != "udp" {
		frame += "\n"
	}
	_ = conn.SetWriteDeadline(time.Now().Add(syslogWriteTimeout))
	_, err := conn.Write([]byte(frame))
	return err
}

// flushRemaining tente d'envoyer les messages restants de la file sur une
// connexion ouverte, dans la limite de syslogShutdownFlushTime.
func (s *syslogSink) flushRemaining(conn net.Conn) {
	deadline := time.Now().Add(syslogShutdownFlushTime)
	for {
		select {
		case m := <-s.queue:
			if err := s.write(conn, m); err != nil {
				s.dropRemaining()
				return
			}
		default:
			return
		}
		if time.Now().After(deadline) {
			s.dropRemaining()
			return
		}
	}
}

// dropRemaining vide la file sans envoyer (utilisé au shutdown ou en cas
// d'échec de flush), en comptant les messages perdus.
func (s *syslogSink) dropRemaining() {
	for {
		select {
		case <-s.queue:
			s.dropped.Add(1)
		default:
			return
		}
	}
}

func (s *syslogSink) sleepCtx(d time.Duration) bool {
	select {
	case <-s.ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// stop annule le contexte et attend la fin du goroutine, dans une fenêtre
// bornée, sans jamais bloquer indéfiniment.
func (s *syslogSink) stop() {
	s.cancel()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(syslogShutdownTimeout):
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	next := cur * 2
	if next > syslogMaxBackoff {
		return syslogMaxBackoff
	}
	return next
}

// parseLevel extrait le niveau d'un message de log à partir de son préfixe
// (ex: "INFO [...]", "ERROR [...]", "WARN [...]", "ALERT [...]",
// "[ERROR] ..."). Retourne "info" par défaut si le préfixe n'est pas reconnu.
func parseLevel(msg string) string {
	fields := strings.Fields(msg)
	if len(fields) == 0 {
		return "info"
	}
	tok := strings.Trim(fields[0], "[]")
	switch strings.ToUpper(tok) {
	case "DEBUG":
		return "debug"
	case "INFO", "NOTICE":
		return "info"
	case "WARN", "WARNING":
		return "warning"
	case "ERROR", "ERR":
		return "error"
	case "ALERT", "CRITICAL", "CRIT", "FATAL", "EMERG":
		return "critical"
	}
	return "info"
}

// severityFromLevel mappe le niveau d'un message applicatif vers la sévérité
// RFC 5424 (plus la valeur est basse, plus c'est prioritaire).
func severityFromLevel(level string) int {
	switch level {
	case "debug":
		return 7
	case "warning", "warn", "notice":
		return 4
	case "error", "err":
		return 3
	case "critical", "crit", "fatal", "alert", "emerg":
		return 2
	default:
		return 6
	}
}

// severityFromLevelString mappe un min_level configuré vers la sévérité.
func severityFromLevelString(level string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return 7, nil
	case "info", "notice", "":
		return 6, nil
	case "warning", "warn":
		return 4, nil
	case "error", "err":
		return 3, nil
	case "critical", "crit", "alert":
		return 2, nil
	}
	return 0, fmt.Errorf("syslog.min_level %q invalide (debug|info|warning|error|critical)", level)
}

// facilityNumber mappe une facility nommée vers son code RFC 5424.
func facilityNumber(name string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "kern":
		return 0, nil
	case "user":
		return 1, nil
	case "mail":
		return 2, nil
	case "daemon":
		return 3, nil
	case "auth", "security":
		return 4, nil
	case "syslog":
		return 5, nil
	case "lpr":
		return 6, nil
	case "news":
		return 7, nil
	case "uucp":
		return 8, nil
	case "cron":
		return 9, nil
	case "authpriv":
		return 10, nil
	case "ftp":
		return 11, nil
	case "local0":
		return 16, nil
	case "local1":
		return 17, nil
	case "local2":
		return 18, nil
	case "local3":
		return 19, nil
	case "local4":
		return 20, nil
	case "local5":
		return 21, nil
	case "local6":
		return 22, nil
	case "local7":
		return 23, nil
	case "":
		return 16, nil
	}
	return 0, fmt.Errorf("syslog.facility %q inconnue", name)
}

// formatRFC5424 construit un message RFC 5424.
//
//	<PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MSG
func formatRFC5424(facility, severity int, appName, msgID, sd, msg string) string {
	pri := facility*8 + severity
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	host := "-"
	if h, err := os.Hostname(); err == nil && h != "" {
		host = sanitizeHostname(h)
	}
	pid := strconv.Itoa(os.Getpid())
	if msgID == "" {
		msgID = "-"
	}
	if sd == "" {
		sd = "-"
	}
	if msg == "" {
		msg = "-"
	}
	return fmt.Sprintf("<%d>1 %s %s %s %s %s %s %s", pri, ts, host, appName, pid, msgID, sd, msg)
}

// sanitizeAppName nettoie l'APP-NAME RFC 5424 (max 48 caractères, sans espaces
// ni caractères de contrôle). Vide / illisible -> "dsc-pull-server".
func sanitizeAppName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "dsc-pull-server"
	}
	var b strings.Builder
	for _, r := range name {
		if r <= 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 48 {
			break
		}
	}
	if b.Len() == 0 {
		return "dsc-pull-server"
	}
	return b.String()
}

// sanitizeHostname nettoie le HOSTNAME RFC 5424 (max 255 caractères, sans
// espaces ni caractères de contrôle).
func sanitizeHostname(h string) string {
	h = strings.TrimSpace(h)
	var b strings.Builder
	for _, r := range h {
		if r <= 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 255 {
			break
		}
	}
	if b.Len() == 0 {
		return "-"
	}
	return b.String()
}

// sanitizeSDParam nettoie la valeur d'un paramètre de données structurées
// RFC 5424 (interdits: `"`, `\`, `]` et les caractères de contrôle).
func sanitizeSDParam(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '"' || r == '\\' || r == ']' || r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// truncateString tronque une chaîne à max octets sans casser un rune UTF-8.
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
