package db

import (
	"database/sql"
	"testing"

	"go-dsc-pull/internal/service"
)

func TestImportCompiledConfigurationsVersionsChanges(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	for _, statement := range []string{
		`CREATE TABLE configuration_model (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, original_name TEXT, previous_id INTEGER, upload_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP, uploaded_by TEXT NOT NULL, mof_file BLOB NOT NULL)`,
		`CREATE TABLE agents (agent_id TEXT PRIMARY KEY, state TEXT NOT NULL)`,
		`CREATE TABLE agent_configurations (agent_id TEXT NOT NULL, configuration_name TEXT NOT NULL)`,
		`INSERT INTO agents (agent_id, state) VALUES ('node-1', 'compliant')`,
		`INSERT INTO agent_configurations (agent_id, configuration_name) VALUES ('node-1', 'example')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	first := []service.CompiledConfiguration{{Name: "Example", Data: []byte("mof-v1")}}
	changed, err := ImportCompiledConfigurations(database, first)
	if err != nil || changed != 1 {
		t.Fatalf("first import = (%d, %v), want (1, nil)", changed, err)
	}
	changed, err = ImportCompiledConfigurations(database, first)
	if err != nil || changed != 0 {
		t.Fatalf("identical import = (%d, %v), want (0, nil)", changed, err)
	}

	second := []service.CompiledConfiguration{{Name: "example", Data: []byte("mof-v2")}}
	changed, err = ImportCompiledConfigurations(database, second)
	if err != nil || changed != 1 {
		t.Fatalf("updated import = (%d, %v), want (1, nil)", changed, err)
	}

	var currentName, currentOriginal, previousName, agentState string
	if err := database.QueryRow(`SELECT name, original_name FROM configuration_model WHERE mof_file = ?`, []byte("mof-v2")).Scan(&currentName, &currentOriginal); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT name FROM configuration_model WHERE mof_file = ?`, []byte("mof-v1")).Scan(&previousName); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT state FROM agents WHERE agent_id = 'node-1'`).Scan(&agentState); err != nil {
		t.Fatal(err)
	}
	if currentName != "example" || currentOriginal != "example" || previousName == "example" || agentState != "pending_apply" {
		t.Fatalf("unexpected result: current=(%q, %q), previous=%q, agent state=%q", currentName, currentOriginal, previousName, agentState)
	}

	if _, err := database.Exec(`INSERT INTO agents (agent_id, state) VALUES ('node-2', 'compliant')`); err != nil {
		t.Fatal(err)
	}
	changed, err = ImportCompiledConfigurationsAndAssign(database, second, map[string]string{"example": "node-2"})
	if err != nil || changed != 0 {
		t.Fatalf("identical assigned import = (%d, %v), want (0, nil)", changed, err)
	}
	var assignedName, assignedState string
	if err := database.QueryRow(`SELECT configuration_name FROM agent_configurations WHERE agent_id = 'node-2'`).Scan(&assignedName); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT state FROM agents WHERE agent_id = 'node-2'`).Scan(&assignedState); err != nil {
		t.Fatal(err)
	}
	if assignedName != "example" || assignedState != "pending_apply" {
		t.Fatalf("unchanged MOF was not assigned: configuration=%q, state=%q", assignedName, assignedState)
	}
}
