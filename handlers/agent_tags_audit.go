package handlers

import (
	"database/sql"
	"strings"

	"go-dsc-pull/internal/db"
)

// tagValuesForAudit retourne les valeurs actuelles d'une clé de tag pour un
// agent, sous forme de chaîne lisible pour un message d'audit ("none" si la clé
// n'existe pas encore). Utile pour tracer la valeur avant/après d'un tag,
// notamment la clé "environment".
func tagValuesForAudit(database *sql.DB, agentId, key string) string {
	tags, err := db.GetAgentTags(database, agentId)
	if err != nil {
		return "unknown"
	}
	vals := tags[key]
	if len(vals) == 0 {
		return "none"
	}
	return strings.Join(vals, ", ")
}

// allTagsForAudit retourne l'ensemble des tags d'un agent sous forme de chaîne
// lisible ("key=value, key2=value2", "none" si aucun), pour l'audit d'une
// suppression globale de tags.
func allTagsForAudit(database *sql.DB, agentId string) string {
	tags, err := db.GetAgentTags(database, agentId)
	if err != nil {
		return "unknown"
	}
	if len(tags) == 0 {
		return "none"
	}
	var parts []string
	for k, vals := range tags {
		for _, v := range vals {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}
