package db

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestListNodeCompilationContextsUsesPropertyValues(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)

	for _, statement := range []string{
		`CREATE TABLE agents (agent_id TEXT PRIMARY KEY, node_name TEXT NOT NULL)`,
		`CREATE TABLE agent_tags (agent_id TEXT NOT NULL, tag_key TEXT NOT NULL, tag_value TEXT NOT NULL)`,
		`CREATE TABLE properties (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE node_properties (node_id TEXT NOT NULL, property_id INTEGER NOT NULL, value TEXT)`,
		`INSERT INTO agents (agent_id, node_name) VALUES ('agent-1', 'node-1')`,
		`INSERT INTO agent_tags (agent_id, tag_key, tag_value) VALUES ('agent-1', 'os', 'windows')`,
		`INSERT INTO properties (id, name) VALUES (7, 'OS')`,
		`INSERT INTO node_properties (node_id, property_id, value) VALUES ('agent-1', 7, '7')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	contexts, agentIDs, err := ListNodeCompilationContextsWithAgentIDs(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 1 {
		t.Fatalf("got %d compilation contexts, want 1", len(contexts))
	}
	if got := contexts[0].Tags["OS"]; !reflect.DeepEqual(got, []string{"windows"}) {
		t.Fatalf("OS tag = %#v, want [windows] to take precedence over property ID", got)
	}
	if got := agentIDs["node-1"]; got != "agent-1" {
		t.Fatalf("agent ID = %q, want agent-1", got)
	}
}
