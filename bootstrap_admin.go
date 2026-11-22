package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"path/filepath"

	"go-dsc-pull/internal"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/schema"
	"golang.org/x/crypto/bcrypt"
	"go-dsc-pull/utils"
)

const (
	bootstrapAdminEmail       = "admin@localhost"
	bootstrapAdminPasswordLen = 30
)

// bootstrapAdminAlphabet est l'alphabet utilisé pour générer le mot de passe
// aléatoire du compte admin initial. On exclut les caractères ambigus (I, O, l,
// 0, 1) et les caractères qui posent problème en copier/coller (guillemets,
// backslash, point-virgule, chevrons).
const bootstrapAdminAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*-_=+"

// randomPassword génère un mot de passe aléatoire cryptographiquement sûr de la
// longueur demandée, en s'appuyant sur crypto/rand (et non math/rand).
func randomPassword(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be > 0")
	}
	max := big.NewInt(int64(len(bootstrapAdminAlphabet)))
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = bootstrapAdminAlphabet[n.Int64()]
	}
	return string(out), nil
}

// runInitBootstrapAdmin crée le compte administrateur initial avec un mot de
// passe aléatoire, puis l'affiche une seule fois sur stdout. À exécuter en
// local, une seule fois, avant le premier démarrage du serveur.
//
// Ce mode remplace l'ancien compte par défaut "admin@localhost / password"
// (vulnérabilité C1) : plus aucune identité admin prévisible n'est créée
// automatiquement au démarrage du serveur.
func runInitBootstrapAdmin() int {
	appCfg, err := internal.LoadAppConfig("config.json")
	if err != nil {
		fmt.Printf("Error: failed to load config.json: %v\n", err)
		return 1
	}

	dbCfg := &schema.DBConfig{
		Driver:   appCfg.Database.Driver,
		Server:   appCfg.Database.Server,
		Port:     appCfg.Database.Port,
		User:     appCfg.Database.User,
		Password: appCfg.Database.Password,
		Database: appCfg.Database.Database,
	}
	if dbCfg.Driver == "sqlite" && !filepath.IsAbs(dbCfg.Database) {
		exePath, err := utils.ExePath()
		if err == nil {
			dbCfg.Database = filepath.Join(filepath.Dir(exePath), dbCfg.Database)
		}
	}

	db.InitDB(dbCfg)
	database, err := db.OpenDB(dbCfg)
	if err != nil {
		fmt.Printf("Error: failed to open the database: %v\n", err)
		return 1
	}
	defer database.Close()

	// Refuse de créer un admin s'il existe déjà un utilisateur : on ne doit pas
	// pouvoir réinitialiser / écraser un compte existant en relançant la commande.
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		fmt.Printf("Error: failed to read users: %v\n", err)
		return 1
	}
	if count > 0 {
		fmt.Println("A user already exists: bootstrap admin creation refused.")
		fmt.Println("Log in with an existing account, or empty the users table before rerunning init-bootstrap-admin.")
		return 1
	}

	password, err := randomPassword(bootstrapAdminPasswordLen)
	if err != nil {
		fmt.Printf("Error: failed to generate password: %v\n", err)
		return 1
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("Error: failed to hash the password: %v\n", err)
		return 1
	}

	if _, err := database.Exec(
		"INSERT INTO users (first_name, last_name, email, password_hash, is_active, role, source) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"Admin", "User", bootstrapAdminEmail, string(hash), true, "admin", "local",
	); err != nil {
		fmt.Printf("Error: failed to create the administrator: %v\n", err)
		return 1
	}

	fmt.Println("=============================================================")
	fmt.Println("Initial admin account created successfully.")
	fmt.Printf("  Email    : %s\n", bootstrapAdminEmail)
	fmt.Printf("  Password : %s\n", password)
	fmt.Println("  (Password generated randomly, displayed only once.)")
	fmt.Println("Keep it in a safe place and change it after the first login.")
	fmt.Println("=============================================================")
	return 0
}
