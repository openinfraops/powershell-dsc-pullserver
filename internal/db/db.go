package db

import (
	"time"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"log"
	"path/filepath"
	"go-dsc-pull/utils"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal"
	 _ "modernc.org/sqlite"
	 _ "github.com/denisenkom/go-mssqldb"
)

// Insert an audit entry (generic)
func InsertAudit(db *sql.DB, driverName string, userEmail, action, target, details, ip string) error {
	if driverName == "mssql" || driverName == "sqlserver" {
		_, err := db.Exec("INSERT INTO audit (user_email, action, target, details, ip_address, created_at) VALUES (?, ?, ?, ?, ?, "+NowExpr(driverName)+")",
			userEmail, action, target, details, ip)
		return err
	}
	_, err := db.Exec("INSERT INTO audit (user_email, action, target, details, ip_address, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		userEmail, action, target, details, ip, time.Now().Format("2006-01-02 15:04:05"))
	return err
}

func UpdateLastLogon(db *sql.DB, userId interface{}) error {

	// Détermine le type de driver via LoadAppConfig
	appCfg, err := internal.LoadAppConfig("config.json")
	if err != nil {
		log.Printf("[ERROR] Could not load app config: %v", err)
		return err
	}
	driverName := appCfg.Database.Driver
	if driverName == "mssql" || driverName == "sqlserver" {
		_, err = db.Exec("UPDATE users SET last_logon_date="+NowExpr(driverName)+" WHERE id=?", userId)
	} else {
		_, err = db.Exec("UPDATE users SET last_logon_date=? WHERE id=?", time.Now().Format("2006-01-02 15:04:05"), userId)
	}
	if err != nil {
		return err
	}
	// Ajout audit: connexion utilisateur
	err = InsertAudit(db, driverName, getUserEmail(db, userId), "login", "users", "User login", "")
	if err != nil {
		log.Printf("[ERROR] Could not insert audit entry: %v", err)
	}
	return err
}

// TouchLastLogon updates last_logon_date without creating an audit entry.
// Useful for token-based auth where this path may be called frequently.
func TouchLastLogon(db *sql.DB, userId interface{}) error {
	appCfg, err := internal.LoadAppConfig("config.json")
	if err != nil {
		log.Printf("[ERROR] Could not load app config: %v", err)
		return err
	}
	driverName := appCfg.Database.Driver
	if driverName == "mssql" || driverName == "sqlserver" {
		_, err = db.Exec("UPDATE users SET last_logon_date="+NowExpr(driverName)+" WHERE id=?", userId)
	} else {
		_, err = db.Exec("UPDATE users SET last_logon_date=? WHERE id=?", time.Now().Format("2006-01-02 15:04:05"), userId)
	}
	return err
}

// Récupère l'email de l'utilisateur par son id
func getUserEmail(db *sql.DB, userId interface{}) string {
	var email string
	err := db.QueryRow("SELECT email FROM users WHERE id=?", userId).Scan(&email)
	if err != nil {
		return ""
	}
	return email
}

func UpdateRole(db *sql.DB, userId interface{}, role string) error {
	_, err := db.Exec("UPDATE users SET role=? WHERE id=?", role, userId)
	return err
}

func LoadDBConfig(path string) (*schema.DBConfig, error) {
	var absPath string
	if filepath.IsAbs(path) {
		absPath = path
	} else {
		exeDir, err := utils.ExePath()
		if err != nil { return nil, err }
		absPath = filepath.Join(filepath.Dir(exeDir), path)
	}
	f, err := os.Open(absPath)
	if err != nil { return nil, err }
	defer f.Close()
	var raw map[string]interface{}
	if err := json.NewDecoder(f).Decode(&raw); err != nil { return nil, err }
	dbRaw, ok := raw["database"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("database config not found or invalid format")
	}
	cfg := &schema.DBConfig{}
	if v, ok := dbRaw["driver"].(string); ok { cfg.Driver = v }
	if v, ok := dbRaw["server"].(string); ok { cfg.Server = v }
	if v, ok := dbRaw["port"].(float64); ok { cfg.Port = int(v) }
	if v, ok := dbRaw["user"].(string); ok { cfg.User = v }
	if v, ok := dbRaw["password"].(string); ok { cfg.Password = v }
	if v, ok := dbRaw["name"].(string); ok { cfg.Database = v }
	return cfg, nil
}

func OpenDB(cfg *schema.DBConfig) (*sql.DB, error) {
	dsn := cfg.Database
	if cfg.Driver == "sqlite" && !filepath.IsAbs(dsn) {
		exePath, err := utils.ExePath()
		if err == nil {
			baseDir := filepath.Dir(exePath)
			dsn = filepath.Join(baseDir, dsn)
		}
	}
	if cfg.Driver == "mysql" {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", cfg.User, cfg.Password, cfg.Server, cfg.Port, cfg.Database)
	} else if cfg.Driver == "postgres" {
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Server, cfg.Port, cfg.Database)
	} else if cfg.Driver == "mssql" || cfg.Driver == "sqlserver" {
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s", cfg.User, cfg.Password, cfg.Server, cfg.Port, cfg.Database)
	}
	return sql.Open(cfg.Driver, dsn)
}

// shared is the process-wide connection pool created once at startup.
//
// Handlers must reuse this pool via Shared() instead of opening a brand new pool
// per request through OpenDB (which would exhaust connections and file handles).
var shared *sql.DB

// SetShared registers the process-wide connection pool. It must be called once
// during startup, before any handler runs. bootstrap_admin.go opens and closes
// its own dedicated connection and must NOT call this.
func SetShared(dbConn *sql.DB) { shared = dbConn }

// Shared returns the process-wide connection pool registered by SetShared.
//
// The returned handle is owned by the process and must never be closed by a
// handler: closing it would invalidate the shared pool for every subsequent
// request.
func Shared() *sql.DB { return shared }