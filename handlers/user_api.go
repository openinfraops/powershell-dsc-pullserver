package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"golang.org/x/crypto/bcrypt"
	"log"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/global"
	"time"
	"strconv"
	"github.com/golang-jwt/jwt/v5"
	samlsp "github.com/crewjam/saml/samlsp"
	internaldb "go-dsc-pull/internal/db"
	"go-dsc-pull/internal/schema"
)

// callerUserID résout l'identifiant numérique de l'utilisateur courant depuis le
// contexte du middleware d'authentification. Le middleware y stocke soit l'email
// (JWT, chaîne) soit l'id utilisateur (API token, entier). Retourne 0 si non résolu.
func callerUserID(db *sql.DB, r *http.Request) int64 {
	v := r.Context().Value("userId")
	switch id := v.(type) {
	case string:
		var uid int64
		_ = db.QueryRow("SELECT id FROM users WHERE email = ?", id).Scan(&uid)
		return uid
	case int:
		return int64(id)
	case int64:
		return id
	case float64:
		return int64(id)
	}
	return 0
}

// isAdminOrSelf retourne vrai si l'utilisateur courant est admin, ou si son id
// correspond à targetID (id utilisateur cible, au format chaîne).
func isAdminOrSelf(db *sql.DB, r *http.Request, targetID string) bool {
	if auth.IsAdmin(r, db) {
		return true
	}
	uid := callerUserID(db, r)
	return uid > 0 && targetID != "" && strconv.FormatInt(uid, 10) == targetID
}

// isAdminOrSelfByToken retourne vrai si l'utilisateur courant est admin, ou si le
// token (par son id) appartient à l'utilisateur courant.
func isAdminOrSelfByToken(db *sql.DB, r *http.Request, tokenID string) bool {
	if auth.IsAdmin(r, db) {
		return true
	}
	var ownerID int64
	if err := db.QueryRow("SELECT user_id FROM user_api_tokens WHERE id = ?", tokenID).Scan(&ownerID); err != nil {
		return false
	}
	uid := callerUserID(db, r)
	return uid > 0 && ownerID == uid
}

// Retourne les infos de l'utilisateur courant (d'après le JWT)
func MyUserInfoHandler(db *sql.DB) http.HandlerFunc {
	       return func(w http.ResponseWriter, r *http.Request) {
		       // Récupère l'email depuis le contexte (middleware JWT)
		       userId := r.Context().Value("userId")
		       email, ok := userId.(string)
		       if !ok || email == "" {
			       http.Error(w, "Unauthorized", http.StatusUnauthorized)
			       return
		       }
		       row := db.QueryRow("SELECT id, first_name, last_name, email, role, is_active, created_at, last_logon_date FROM users WHERE email = ?", email)
		       var u schema.User
		       var lastLogon sql.NullString
		       var isActiveBool bool
		       if err := row.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Role, &isActiveBool, &u.CreatedAt, &lastLogon); err != nil {
			       http.Error(w, "Not found", http.StatusNotFound)
			       return
		       }
		       u.IsActive = isActiveBool
		       if lastLogon.Valid { u.LastLogonDate = &lastLogon.String } else { u.LastLogonDate = nil }
		       w.Header().Set("Content-Type", "application/json")
		       json.NewEncoder(w).Encode(u)
	       }
}

// Liste les tokens API d'un utilisateur
func ListUserAPITokensHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userId := r.PathValue("id")
		if !isAdminOrSelf(db, r, userId) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		rows, err := db.Query("SELECT id, user_id, label, is_active, created_at, revoked_at FROM user_api_tokens WHERE user_id = ?", userId)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var tokens []schema.APIToken
		for rows.Next() {
			var t schema.APIToken
			var revokedAt sql.NullString
			rows.Scan(&t.ID, &t.UserID, &t.Label, &t.IsActive, &t.CreatedAt, &revokedAt)
			if revokedAt.Valid { t.RevokedAt = &revokedAt.String } else { t.RevokedAt = nil }
			tokens = append(tokens, t)
		}
		w.Header().Set("Content-Type", "application/json")
        // Toujours retourner un tableau (éventuellement vide)
        if tokens == nil {
            w.Write([]byte("[]"))
        } else {
            json.NewEncoder(w).Encode(tokens)
        }
	}
}

// Crée un nouveau token API pour un utilisateur (retourne le token plain)
func CreateUserAPITokenHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userId := r.PathValue("id")
		if !isAdminOrSelf(db, r, userId) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		var req struct{ Label string `json:"label"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		token, err := internaldb.GenerateAPIToken()
		if err != nil {
			http.Error(w, "Token generation error", http.StatusInternalServerError)
			return
		}
		// Stocke le hash
		id64, _ := strconv.ParseInt(userId, 10, 64)
		if err := internaldb.StoreAPIToken(db, id64, token, req.Label); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	}
}

// Révoque un token API (soft delete)
func RevokeUserAPITokenHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenId := r.PathValue("tokenid")
		if !isAdminOrSelfByToken(db, r, tokenId) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		driver := "sqlite"
		if global.AppConfig != nil && global.AppConfig.Database.Driver != "" {
			driver = global.AppConfig.Database.Driver
		}

		var err error
		if driver == "mssql" || driver == "sqlserver" {
			_, err = db.Exec("UPDATE user_api_tokens SET is_active=0, revoked_at=CURRENT_TIMESTAMP WHERE id=?", tokenId)
		} else {
			_, err = db.Exec("UPDATE user_api_tokens SET is_active=0, revoked_at=? WHERE id=?", time.Now(), tokenId)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// Supprime un token API (hard delete)
func DeleteUserAPITokenHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenId := r.PathValue("tokenid")
		if !isAdminOrSelfByToken(db, r, tokenId) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		_, err := db.Exec("DELETE FROM user_api_tokens WHERE id=?", tokenId)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// Handler de login JWT
func LoginHandler(db *sql.DB) http.HandlerFunc {
	// Limiteur en mémoire par IP pour la protection brute-force du login.
	limiter := auth.NewLoginRateLimiter(auth.MaxLoginAttempts, auth.LoginRateLimitWindow)
	return func(w http.ResponseWriter, r *http.Request) {
		type LoginRequest struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		// Protection brute-force : bloque l'IP si trop d'échecs dans la fenêtre.
		ip := auth.ClientIP(r)
		if !limiter.LoginAllowed(ip) {
			w.Header().Set("Retry-After", "900")
			w.Header().Set("X-RateLimit-Limit", "5")
			http.Error(w, "Trop de tentatives. Réessayez plus tard.", http.StatusTooManyRequests)
			return
		}
		   // Plus besoin de LoginResponse, le token sera dans le cookie
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		       // On récupère aussi la source
			       row := db.QueryRow("SELECT id, password_hash, source, role FROM users WHERE email = ? AND is_active = 1", req.Username)
			       var id int64
			       var hash string
			       var source string
			       var role string
				       err := row.Scan(&id, &hash, &source, &role)
			       if err == sql.ErrNoRows {
				       limiter.LoginFailed(ip)
				       http.Error(w, "Utilisateur ou mot de passe incorrect", http.StatusUnauthorized)
				       return
			       } else if err != nil {
				       log.Printf("[LOGIN] Erreur DB: %v", err)
				       http.Error(w, "Erreur interne", http.StatusInternalServerError)
				       return
			       }
		       // Si source = saml, on refuse l'authentification par mot de passe
		       if strings.ToLower(source) == "saml" {
			       limiter.LoginFailed(ip)
			       http.Error(w, "Authentification par mot de passe interdite pour les utilisateurs SAML", http.StatusUnauthorized)
			       return
		       }
		       // Vérification du mot de passe
		       if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
			       limiter.LoginFailed(ip)
			       http.Error(w, "Unauthorized", http.StatusUnauthorized)
			       return
		       }
		       limiter.LoginSucceeded(ip)
		// Met à jour la date de dernière connexion
		if err := internaldb.UpdateLastLogon(db, id); err != nil {
			log.Printf("[LOGIN] Erreur update last_logon_date: %v", err)
		}
		appCfg := global.AppConfig
		if appCfg == nil {
			log.Printf("[REGISTER][CONFIG] Error loading config: %v", err)
			http.Error(w, "Server configuration error: unable to load config", http.StatusInternalServerError)
			return
		}
		secretValue := auth.ResolveJWTSecret(appCfg)
		if secretValue == "" {
			http.Error(w, "Server configuration error: missing shared_secret", http.StatusInternalServerError)
			return
		}
		secret := []byte(secretValue)
		expiresAt := time.Now().Add(60 * time.Minute).Unix()
		   claims := jwt.MapClaims{
			   "sub": req.Username,
			   "exp": expiresAt,
			   "role": role,
		   }
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString(secret)
		if err != nil {
			http.Error(w, "Token error", http.StatusInternalServerError)
			return
		}
		setJWTCookie(w, signed, time.Unix(expiresAt, 0))
		   w.Header().Set("Content-Type", "application/json")
		   json.NewEncoder(w).Encode(map[string]interface{}{"expires_at": expiresAt})
	}
}

// List users
func ListUsersHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT id, first_name, last_name, email, role, is_active, created_at, last_logon_date, source FROM users")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var users []schema.User
		for rows.Next() {
			var u schema.User
			var lastLogon sql.NullString
			var isActiveBool bool
			if err := rows.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Role, &isActiveBool, &u.CreatedAt, &lastLogon, &u.Source); err != nil {
				continue
			}
			u.IsActive = isActiveBool
			if lastLogon.Valid {
				u.LastLogonDate = &lastLogon.String
			} else {
				u.LastLogonDate = nil
			}
			users = append(users, u)
		}
		json.NewEncoder(w).Encode(users)
	}
}

// Get user by id
func GetUserHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		row := db.QueryRow("SELECT id, first_name, last_name, email, role, is_active, created_at, last_logon_date, source FROM users WHERE id = ?", id)
		var u schema.User
		var lastLogon sql.NullString
		var isActiveBool bool
		if err := row.Scan(&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.Role, &isActiveBool, &u.CreatedAt, &lastLogon, &u.Source); err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		u.IsActive = isActiveBool
		if lastLogon.Valid {
			u.LastLogonDate = &lastLogon.String
		} else {
			u.LastLogonDate = nil
		}
		json.NewEncoder(w).Encode(u)
	}
}

// Create user
func CreateUserHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Email     string `json:"email"`
			Password  string `json:"password"`
			Role      string `json:"role"`
			IsActive  string `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if req.Password == "" {
			http.Error(w, "Le mot de passe est obligatoire", http.StatusBadRequest)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "Erreur hash mot de passe", http.StatusInternalServerError)
			return
		}
		isActive := req.IsActive == "1" || req.IsActive == "true"
		res, err := db.Exec("INSERT INTO users (first_name, last_name, email, password_hash, role, is_active) VALUES (?, ?, ?, ?, ?, ?)", req.FirstName, req.LastName, req.Email, string(hash), req.Role, isActive)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id, _ := res.LastInsertId()
		u := schema.User{
			ID:        id,
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Email:     req.Email,
			Role:      req.Role,
			IsActive:  isActive,
			Source:    "local",
		}
		json.NewEncoder(w).Encode(u)
	}
}

// Update user
func UpdateUserHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var u schema.User
		if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		_, err := db.Exec("UPDATE users SET first_name=?, last_name=?, email=?, role=?, is_active=?, last_logon_date=? WHERE id=?", u.FirstName, u.LastName, u.Email, u.Role, u.IsActive, u.LastLogonDate, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(u)
	}
}

// Delete user
func DeleteUserHandler(db *sql.DB) http.HandlerFunc {
	       return func(w http.ResponseWriter, r *http.Request) {
		       id := r.PathValue("id")
		       // actorEmail = userId/sub from context
		       actorEmail := "?"
		       if r.Context().Value("userId") != nil {
			       if sub, ok := r.Context().Value("userId").(string); ok {
				       actorEmail = sub
			       }
		       }
		       // Get email of the user being deleted
		       deletedEmail := "?"
		       row := db.QueryRow("SELECT email FROM users WHERE id = ?", id)
		       var email string
		       if err := row.Scan(&email); err == nil {
			       deletedEmail = email
		       }
		       _, err := db.Exec("DELETE FROM users WHERE id=?", id)
		       if err != nil {
			       http.Error(w, err.Error(), http.StatusInternalServerError)
			       return
		       }
		       // Audit suppression
		       driverName := global.AppConfig.Database.Driver
		       _ = internaldb.InsertAudit(db, driverName, actorEmail, "delete", "user", "Deleted user: "+id+" ("+deletedEmail+")", "")
		       w.WriteHeader(http.StatusNoContent)
	       }
}

// Activate/Deactivate user
func SetUserActiveHandler(db *sql.DB) http.HandlerFunc {
	       return func(w http.ResponseWriter, r *http.Request) {
		       id := r.PathValue("id")
		       active := r.URL.Query().Get("active")
		       isActive := 0
		       if strings.ToLower(active) == "true" || active == "1" {
			       isActive = 1
		       }
		       // actorEmail = userId/sub from context
		       actorEmail := "?"
		       if r.Context().Value("userId") != nil {
			       if sub, ok := r.Context().Value("userId").(string); ok {
				       actorEmail = sub
			       }
		       }
		       // Get email of the user being activated/deactivated
		       targetEmail := "?"
		       row := db.QueryRow("SELECT email FROM users WHERE id = ?", id)
		       var email string
		       if err := row.Scan(&email); err == nil {
			       targetEmail = email
		       }
		       _, err := db.Exec("UPDATE users SET is_active=? WHERE id=?", isActive, id)
		       if err != nil {
			       http.Error(w, err.Error(), http.StatusInternalServerError)
			       return
		       }
		       // Audit activation/désactivation
		       driverName := global.AppConfig.Database.Driver
		       action := "deactivate"
		       if isActive == 1 {
			       action = "activate"
		       }
		       _ = internaldb.InsertAudit(db, driverName, actorEmail, action, "user", action+" user: "+id+" ("+targetEmail+")", "")
		       w.WriteHeader(http.StatusNoContent)
	       }
}

// Change user password
func ChangeUserPasswordHandler(db *sql.DB) http.HandlerFunc {
       return func(w http.ResponseWriter, r *http.Request) {
	       id := r.PathValue("id")
	       var req struct { NewPassword string `json:"new_password"` }
	       if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewPassword == "" {
		       http.Error(w, "Bad request", http.StatusBadRequest)
		       return
	       }
	       // Resolve the target user email for a readable audit message.
	       targetEmail := "?"
	       {
		       row := db.QueryRow("SELECT email FROM users WHERE id = ?", id)
		       var email string
		       if err := row.Scan(&email); err == nil {
			       targetEmail = email
		       }
	       }
	       hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	       if err != nil {
		       http.Error(w, "Hash error", http.StatusInternalServerError)
		       return
	       }
	       // userId context contains JWT sub (email in this app).
	       actorEmail := "?"
	       if r.Context().Value("userId") != nil {
		       if sub, ok := r.Context().Value("userId").(string); ok {
			       actorEmail = sub
		       }
	       }
	       _, err = db.Exec("UPDATE users SET password_hash=? WHERE id=?", string(hash), id)
	       if err != nil {
		       http.Error(w, err.Error(), http.StatusInternalServerError)
		       return
	       }
	       // Audit changement de mot de passe
	       driverName := global.AppConfig.Database.Driver
	       _ = internaldb.InsertAudit(db, driverName, actorEmail, "update", "user", "Changed password for user: "+targetEmail+" (id="+id+")", "")
	       w.WriteHeader(http.StatusNoContent)
       }
}

// Handler de logout : supprime le cookie JWT côté serveur
func LogoutHandler(samlMiddleware *samlsp.Middleware) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearJWTCookie(w)
		if samlMiddleware != nil && samlMiddleware.Session != nil {
			if err := samlMiddleware.Session.DeleteSession(w, r); err != nil {
				log.Printf("[LOGOUT] Failed to delete SAML session: %v", err)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}