
package db

import (
    "os"
    "go-dsc-pull/internal/logs"
    "go-dsc-pull/internal/schema"
    "io/ioutil"
    "path/filepath"
    "go-dsc-pull/utils"
)


// InitDB initialise la base de données à partir du schéma SQL (CREATE IF NOT EXISTS)
func InitDB(cfg *schema.DBConfig) {
    database, err := OpenDB(cfg)
    if err != nil {
        logs.WriteLogFile("ERROR [INITDB] Failed to open DB: " + err.Error())
        os.Exit(1)
    }
    defer database.Close()

    exeDir, err := utils.ExePath()
    if err != nil {
        logs.WriteLogFile("ERROR [INITDB] Failed to get executable path: " + err.Error())
        os.Exit(1)
    }
    var schemaFile string
    switch cfg.Driver {
    case "sqlite":
        schemaFile = "schema_sqlite.sql"
    case "mssql", "sqlserver":
        schemaFile = "schema_mssql.sql"
    default:
        schemaFile = "schema_sqlite.sql" // fallback
    }
    schemaPath := filepath.Join(filepath.Dir(exeDir), "db", schemaFile)
    schema, err := ioutil.ReadFile(schemaPath)
    if err != nil {
        logs.WriteLogFile("ERROR [INITDB] Failed to read " + schemaFile + ": " + err.Error())
        os.Exit(1)
    }

    _, err = database.Exec(string(schema))
    if err != nil {
        logs.WriteLogFile("ERROR [INITDB] Failed to execute SQL schema: " + err.Error())
        os.Exit(1)
    }

	if err := EnsureInfraSchema(database, cfg.Driver); err != nil {
		logs.WriteLogFile("ERROR [INITDB] Failed to ensure infra schema columns: " + err.Error())
		os.Exit(1)
	}

	if err := EnsureAgentSchema(database, cfg.Driver); err != nil {
		logs.WriteLogFile("ERROR [INITDB] Failed to ensure agent schema columns: " + err.Error())
		os.Exit(1)
	}

	if err := EnsureConfigurationRepoSettingsSchema(database, cfg.Driver); err != nil {
		logs.WriteLogFile("ERROR [INITDB] Failed to ensure configuration repo settings schema columns: " + err.Error())
		os.Exit(1)
	}

    logs.WriteLogFile("INFO [INITDB] DB schema checked/created.")
}