package db

import "strings"

// NowExpr renvoie l'expression SQL du timestamp courant en UTC, cohérente entre
// les pilotes.
//
//   - SQLite : CURRENT_TIMESTAMP renvoie déjà l'heure UTC.
//   - MSSQL  : CURRENT_TIMESTAMP (≡ GETDATE()) renvoie l'heure LOCALE du serveur,
//     ce qui décale tous les horodatages de l'offset UTC du serveur (ex. +1 h en
//     France l'hiver) par rapport à l'affichage front qui suppose UTC. On utilise
//     donc GETUTCDATE().
//
// L'affichage (web/js parseUtc) et le scheduler (time.Now().UTC()) supposent déjà
// un stockage UTC ; cette fonction aligne la couche de persistance MSSQL dessus.
func NowExpr(driver string) string {
	if d := strings.ToLower(driver); d == "mssql" || d == "sqlserver" {
		return "GETUTCDATE()"
	}
	return "CURRENT_TIMESTAMP"
}
