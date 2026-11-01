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

	type nodeAgent struct {
		agentID  string
		nodeName string
	}
	var agents []nodeAgent
	for rows.Next() {
		var agent nodeAgent
		if err := rows.Scan(&agent.agentID, &agent.nodeName); err != nil {
			return nil, nil, err
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}

	contexts := make([]service.NodeCompilationContext, 0)
	agentIDs := make(map[string]string)
	for _, agent := range agents {
		tags, err := GetAgentTags(database, agent.agentID)
		if err != nil {
			return nil, nil, err
		}
		compilationTags := legacyNodeTags(tags)
		propertyRows, err := database.Query(`
SELECT p.name, np.value
FROM node_properties np
JOIN properties p ON p.id = np.property_id
WHERE np.node_id = ?`, agent.nodeName)
		if err != nil {
			return nil, nil, err
		}
		for propertyRows.Next() {
			var propertyName, value string
			if err := propertyRows.Scan(&propertyName, &value); err != nil {
				propertyRows.Close()
				return nil, nil, err
			}
			key := legacyNodeTagKey(propertyName)
			compilationTags[key] = []string{value}
		}
		if err := propertyRows.Err(); err != nil {
			propertyRows.Close()
			return nil, nil, err
		}
		if err := propertyRows.Close(); err != nil {
			return nil, nil, err
		}
		contexts = append(contexts, service.NodeCompilationContext{NodeName: agent.nodeName, Tags: compilationTags})
		agentIDs[strings.ToLower(strings.TrimSpace(agent.nodeName))] = agent.agentID
	}
	return contexts, agentIDs, nil
}

func legacyNodeTags(tags map[string][]string) map[string][]string {
	normalized := make(map[string][]string, len(tags))
	for key, values := range tags {
		canonical := legacyNodeTagKey(key)
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

func legacyNodeTagKey(key string) string {
	aliases := map[string]string{
		"os": "OS", "operatingsystem": "OS",
		"environment": "Environnement", "environnement": "Environnement",
		"application": "Application",
		"component":   "Composant", "composant": "Composant",
		"hostgroup": "Hostgroup", "hostgroups": "Hostgroup",
	}
	if canonical, ok := aliases[strings.ToLower(strings.TrimSpace(key))]; ok {
		return canonical
	}
	return key
}
