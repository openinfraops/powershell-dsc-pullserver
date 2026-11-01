package db

import (
	"database/sql"
	"strings"

	"go-dsc-pull/internal/service"
)

func ListNodeCompilationContextsWithAgentIDs(database *sql.DB) ([]service.NodeCompilationContext, map[string]string, error) {
	rows, err := database.Query(`SELECT agent_id, node_name FROM agents WHERE node_name IS NOT NULL AND node_name <> '' ORDER BY node_name`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	contexts := make([]service.NodeCompilationContext, 0)
	agentIDs := make(map[string]string)
	for rows.Next() {
		var agentID, nodeName string
		if err := rows.Scan(&agentID, &nodeName); err != nil {
			return nil, nil, err
		}
		tags, err := GetAgentTags(database, agentID)
		if err != nil {
			return nil, nil, err
		}
		contexts = append(contexts, service.NodeCompilationContext{NodeName: nodeName, Tags: legacyNodeTags(tags)})
		agentIDs[strings.ToLower(strings.TrimSpace(nodeName))] = agentID
	}
	return contexts, agentIDs, rows.Err()
}

func legacyNodeTags(tags map[string][]string) map[string][]string {
	aliases := map[string]string{
		"os": "OS", "operatingsystem": "OS",
		"environment": "Environnement", "environnement": "Environnement",
		"application": "Application",
		"component":   "Composant", "composant": "Composant",
		"hostgroup": "Hostgroup", "hostgroups": "Hostgroup",
	}
	normalized := make(map[string][]string, len(tags))
	for key, values := range tags {
		canonical := key
		if alias, ok := aliases[strings.ToLower(strings.TrimSpace(key))]; ok {
			canonical = alias
		}
		normalized[canonical] = append(normalized[canonical], values...)
	}
	for key, values := range normalized {
		seen := make(map[string]bool, len(values))
		unique := values[:0]
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" && !seen[value] {
				seen[value] = true
				unique = append(unique, value)
			}
		}
		normalized[key] = unique
	}
	return normalized
}
