package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"go-dsc-pull/internal/global"
	"golang.org/x/crypto/bcrypt"
	"time"
)

// maxActiveAPITokensPerUser borne le nombre de tokens API actifs qu'un même
// utilisateur peut créer. L'auto-service (web/profile.js) permet à un
// utilisateur de créer des tokens pour lui-même ; sans plafond, un utilisateur
// pourrait multiplier les tokens et ralentir la vérification (qui compare
// chaque token actif par bcrypt) jusqu'à provoquer un déni de service.
const maxActiveAPITokensPerUser = 10

// Génère un token API aléatoire (48 bytes, base64)
func GenerateAPIToken() (string, error) {
	tokenBytes := make([]byte, 48)
	_, err := rand.Read(tokenBytes)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(tokenBytes), nil
}

// Stocke le hash du token API pour un utilisateur
func StoreAPIToken(db *sql.DB, userId int64, plainToken, label string) error {
	// Plafond de tokens actifs par utilisateur (voir maxActiveAPITokensPerUser).
	var activeCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM user_api_tokens WHERE user_id = ? AND revoked_at IS NULL AND is_active = 1`, userId).Scan(&activeCount)
	if activeCount >= maxActiveAPITokensPerUser {
		return fmt.Errorf("too many active API tokens for this user (max %d)", maxActiveAPITokensPerUser)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plainToken), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// Detect driver
	driver := "sqlite"
	if global.AppConfig != nil && global.AppConfig.Database.Driver != "" {
		driver = global.AppConfig.Database.Driver
	}
	if driver == "mssql" || driver == "sqlserver" {
		// Use GETUTCDATE() for SQL Server (CURRENT_TIMESTAMP is server-local time).
		_, err = db.Exec(`INSERT INTO user_api_tokens (user_id, token_hash, label, is_active, created_at) VALUES (?, ?, ?, 1, `+NowExpr(driver)+`)`,
			userId, string(hash), label)
	} else {
		// Use formatted string for SQLite
		_, err = db.Exec(`INSERT INTO user_api_tokens (user_id, token_hash, label, is_active, created_at) VALUES (?, ?, ?, 1, ?)`,
			userId, string(hash), label, time.Now().Format("2006-01-02 15:04:05"))
	}
	return err
}

// Vérifie si un token API est valide et actif, retourne l'user_id si OK
func CheckAPIToken(db *sql.DB, token string) (int64, error) {
       rows, err := db.Query(`SELECT user_id, token_hash, is_active FROM user_api_tokens WHERE revoked_at IS NULL`)
       if err != nil {
	       return 0, err
       }
       defer rows.Close()
       for rows.Next() {
	       var userId int64
	       var hash string
	       var isActive bool
	       if err := rows.Scan(&userId, &hash, &isActive); err != nil {
		       continue
	       }
	       if !isActive {
		       continue
	       }
	       if bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil {
		       return userId, nil
	       }
       }
       return 0, errors.New("invalid token")
}
