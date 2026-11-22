package handlers

import (
	"database/sql"
	"fmt"
	"strings"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/service"
)

// compileNodeConfig compile la configuration d'un nœud depuis le dépôt de
// configurations et assigne les MOF générés à l'agent. Retourne nil si la
// compilation n'est pas activée ou si le dépôt est incomplet (rien à faire).
func compileNodeConfig(database *sql.DB, agentId, nodeName string) error {
	repoSettings, err := db.GetConfigurationRepoSettings(database)
	if err != nil || !repoSettings.CompileOnNodeRegistration {
		return nil
	}
	if strings.TrimSpace(repoSettings.URL) == "" || strings.TrimSpace(repoSettings.Directory) == "" || strings.TrimSpace(repoSettings.CompilerScript) == "" {
		return nil
	}
	nodeTags, _ := db.GetLegacyCompilationTags(database, agentId)
	if nodeTags == nil {
		nodeTags = map[string][]string{}
	}
	configNames := []string{}
	rows, queryErr := database.Query(`SELECT configuration_name FROM agent_configurations WHERE agent_id = ?`, agentId)
	if queryErr == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil && name != "" {
				configNames = append(configNames, name)
			}
		}
		rows.Close()
	}
	if len(configNames) == 0 {
		configNames = append(configNames, nodeName)
	}
	repoConfig := service.ConfigurationRepoConfig{
		URL:                  repoSettings.URL,
		Branch:               repoSettings.Branch,
		Directory:            repoSettings.Directory,
		CompilerScript:       repoSettings.CompilerScript,
		PowerShellExecutable: repoSettings.PowerShellExecutable,
		RepositoryUsername:   repoSettings.RepositoryUsername,
		RepositoryPassword:   repoSettings.RepositoryPassword,
	}
	_, compileErr := service.RunConfigurationRepoSyncForNodeContext(repoConfig, configNames, nodeName, nodeTags, func(compiled []service.CompiledConfiguration) (int, error) {
		assignments := make(map[string]string, len(compiled)+1)
		assignments[strings.ToLower(strings.TrimSpace(nodeName))] = agentId
		for _, configuration := range compiled {
			assignments[strings.ToLower(strings.TrimSpace(configuration.Name))] = agentId
		}
		return db.ImportCompiledConfigurationsAndAssign(database, compiled, assignments)
	})
	return compileErr
}

// triggerNodeRecompileAsync lance la compilation d'un nœud en arrière-plan et
// réinitialise le flag needs_recompile à 0 en cas de succès. Ouvre sa propre
// connexion DB (le nœud peut être compilé après que le handler a répondu).
func triggerNodeRecompileAsync(agentId, nodeName string) {
	finish := service.MarkNodeCompileStart(nodeName)
	go func() {
		defer finish()
		database := db.Shared()
		if err := compileNodeConfig(database, agentId, nodeName); err != nil {
			_ = logs.WriteLogFile(fmt.Sprintf("[RECOMPILE] Compilation échouée pour %s: %v", nodeName, err))
			return
		}
		_, _ = database.Exec(`UPDATE agents SET needs_recompile = 0 WHERE agent_id = ?`, agentId)
		_ = logs.WriteLogFile(fmt.Sprintf("[RECOMPILE] Compilation terminée pour %s", nodeName))
	}()
}
