package handlers

import (
	"net/http"
	"log"
	"time"
	"database/sql"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	samlsp "github.com/crewjam/saml/samlsp"
	jwt "github.com/golang-jwt/jwt/v5"
)

// SAMLLoginHandler handles SAML login, user mapping, and JWT issuance
func SAMLLoginHandler(dbConn *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		   attributesIface := samlsp.SessionFromContext(r.Context())
		   appCfg := global.AppConfig
		   if appCfg == nil {
			   log.Printf("[REGISTER][CONFIG] Error loading config: global.AppConfig is nil")
			   http.Error(w, "Server configuration error: unable to load config", http.StatusInternalServerError)
			   return
		   }
		//log.Printf("[SAML] SAML session attributes: %+v", attributesIface[])
		var email, firstName, lastName, role string
		if attributesIface == nil {
			log.Printf("[SAML] No SAML session attributes found in context")
			http.Error(w, "SAML session missing", http.StatusForbidden)
			return
		}
		attributes, ok := attributesIface.(samlsp.SessionWithAttributes)
		if !ok {
			log.Printf("[SAML] SessionFromContext is not SessionWithAttributes")
			http.Error(w, "SAML session invalid", http.StatusForbidden)
			return
		}
		attrMap := attributes.GetAttributes()
		if vals, ok := attrMap[appCfg.SAML.UserMapping.Email]; ok && len(vals) > 0 {
			email = vals[0]
		}
		if vals, ok := attrMap[appCfg.SAML.UserMapping.GivenName]; ok && len(vals) > 0 {
			firstName = vals[0]
		}
		if vals, ok := attrMap[appCfg.SAML.UserMapping.Sn]; ok && len(vals) > 0 {
			lastName = vals[0]
		}
		// SAML group extraction and role assignment
		groupAttr := appCfg.SAML.GroupMapping.Attribute
		adminValue := appCfg.SAML.GroupMapping.AdminValue
		userValue := appCfg.SAML.GroupMapping.UserValue
		isAdmin := false
		isUser := false
		if groupAttr != "" {
			if groupVals, ok := attrMap[groupAttr]; ok && len(groupVals) > 0 {
				for _, g := range groupVals {
					if g == adminValue {
						isAdmin = true
						log.Printf("[SAML] User is in admin group: %s", g)
					}
					if g == userValue {
						isUser = true
					}
				}
			}
		}
		if isAdmin {
			role = "admin"
		} else if isUser {
			role = "user"
		} else {
			http.Redirect(w, r, "/web/login?error=deny", http.StatusFound)
			return
		}

		if email == "" {
			log.Printf("[SAML] No email found in SAML assertions, access denied")
			http.Error(w, "Email SAML manquant", http.StatusForbidden)
			return
		}
		var userId int
		var isActiveBool bool
		err := dbConn.QueryRow("SELECT id, is_active FROM users WHERE email = ?", email).Scan(&userId, &isActiveBool)
		if err == sql.ErrNoRows {
			log.Printf("[SAML] Creating new user: %s %s <%s> (role: %s)", firstName, lastName, email, role)
			_, err := dbConn.Exec("INSERT INTO users (first_name, last_name, email, password_hash, is_active, last_logon_date, source, role) VALUES (?, ?, ?, '', 1, ?, 'saml', ?)", firstName, lastName, email, time.Now().Format("2006-01-02 15:04:05"), role)
			if err != nil {
				log.Printf("[SAML] Error creating user: %v", err)
				http.Error(w, "Erreur création utilisateur", http.StatusInternalServerError)
				return
			}
		} else if err != nil && err != sql.ErrNoRows {
			log.Printf("[SAML] DB error: %v", err)
			http.Error(w, "Erreur DB", http.StatusInternalServerError)
			return
		} else if !isActiveBool {
			// Compte inactif : refuse l'accès et redirige avec message
			log.Printf("[SAML] Account inactive for %s", email)
			http.Redirect(w, r, "/web/login?error=blocked", http.StatusFound)
			return
		} else {
			// Met à jour la date de dernière connexion
			if err := db.UpdateLastLogon(dbConn, userId); err != nil {
				log.Printf("[SAML] Error updating last_logon_date: %v", err)
			}
			if err := db.UpdateRole(dbConn, userId, role); err != nil {
				log.Printf("[SAML] Error updating role: %v", err)
			}
		}
		

	secretValue := auth.ResolveJWTSecret(appCfg)
	if secretValue == "" {
		log.Printf("[SAML] Missing shared_secret in config")
		http.Error(w, "Server configuration error: missing shared_secret", http.StatusInternalServerError)
		return
	}
	secret := []byte(secretValue)
	expiresAt := time.Now().Add(60 * time.Minute).Unix()
		claims := jwt.MapClaims{
			"sub": email,
			"exp": expiresAt,
			"role": role,
		}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		log.Printf("[SAML] Error generating JWT: %v", err)
		http.Error(w, "Erreur JWT", http.StatusInternalServerError)
		return
	}
	setJWTCookie(w, signed, time.Unix(expiresAt, 0))
	http.Redirect(w, r, "/web", http.StatusFound)
	}
}
