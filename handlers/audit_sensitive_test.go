package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/schema"
)

// newAuditTestDB crée une base SQLite en mémoire avec les tables `users` et
// `audit` (schéma réel de `db.go`/`migration_v0.0.6_to_v1.0.0.sql`), afin de
// tester le chemin d'audit d'une action sensible.
func newAuditTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	stmts := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			password_hash TEXT NOT NULL
		)`,
		`CREATE TABLE audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email TEXT,
			action TEXT,
			target TEXT,
			details TEXT,
			ip_address TEXT,
			created_at TIMESTAMP
		)`,
	}
	for _, s := range stmts {
		if _, err := database.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	return database
}

// TestChangeUserPasswordAudit_NoSecretLeak couvre une action sensible — le
// changement de mot de passe — et vérifie deux choses :
//  1. un événement d'audit est bien écrit (action "update", target "user") ;
//  2. le secret (mot de passe en clair) n'apparaît JAMAIS dans l'entrée
//     d'audit ni dans le message détaillé.
func TestChangeUserPasswordAudit_NoSecretLeak(t *testing.T) {
	database := newAuditTestDB(t)
	if _, err := database.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`,
		"target@example.com", "$2a$10$oldhash"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Injecte la config applicative (driver sqlite) et restaure l'état.
	oldAppConfig := global.AppConfig
	global.AppConfig = &schema.AppConfig{Database: schema.DBConfig{Driver: "sqlite"}}
	defer func() { global.AppConfig = oldAppConfig }()

	const actorEmail = "admin@example.com"
	const newPassword = "SuperSecret123!"

	body, _ := json.Marshal(map[string]string{"new_password": newPassword})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/1/password", strings.NewReader(string(body)))
	req.SetPathValue("id", "1")
	// Le handler lit l'acteur depuis `userId` (le JWT `sub`, l'email).
	req = req.WithContext(context.WithValue(req.Context(), "userId", actorEmail))

	rec := httptest.NewRecorder()
	ChangeUserPasswordHandler(database)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// L'entrée d'audit doit exister et ne doit pas contenir le secret.
	var (
		userEmail, action, target, details string
	)
	err := database.QueryRow(
		`SELECT user_email, action, target, details FROM audit ORDER BY id DESC LIMIT 1`,
	).Scan(&userEmail, &action, &target, &details)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}

	if action != "update" {
		t.Fatalf("expected action 'update', got %q", action)
	}
	if target != "user" {
		t.Fatalf("expected target 'user', got %q", target)
	}
	if userEmail != actorEmail {
		t.Fatalf("expected actor %q, got %q", actorEmail, userEmail)
	}
	if !strings.Contains(details, "Changed password for user") {
		t.Fatalf("expected audit details to mention the password change, got %q", details)
	}
	if strings.Contains(details, newPassword) {
		t.Fatalf("audit details leaked the plaintext password: %q", details)
	}
}
