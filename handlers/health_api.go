package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"go-dsc-pull/internal/buildinfo"
	internaldb "go-dsc-pull/internal/db"
	"go-dsc-pull/internal/monitor"
)

// healthResponse is the JSON payload of GET /health. It is intentionally
// unauthenticated so load balancers, health checks and Zabbix can probe it, and
// it exposes no node names or secrets.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	DB      struct {
		Reachable bool   `json:"reachable"`
		Error     string `json:"error,omitempty"`
	} `json:"db"`
	Workers struct {
		// Alive reflects whether the process is able to do work: it is true
		// while the server is not shutting down and the database is reachable.
		// The server is single-process, so there is no separate worker pool to
		// probe; "alive" is a proxy for process + database health.
		Alive        bool `json:"alive"`
		RunningTasks int  `json:"running_tasks"`
	} `json:"workers"`
}

// HealthHandler returns the unauthenticated GET /health endpoint.
func HealthHandler(dbConn *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := healthResponse{Status: "ok", Version: buildinfo.Version}
		resp.DB.Reachable = true

		running, err := internaldb.CountRunningSchedulerTasks(dbConn)
		if err != nil {
			resp.DB.Reachable = false
			resp.DB.Error = err.Error()
			resp.Status = "degraded"
		} else {
			resp.Workers.RunningTasks = running
		}

		// "Alive" = process not shutting down AND the database is reachable.
		resp.Workers.Alive = !monitor.ShuttingDown() && resp.DB.Reachable
		switch {
		case monitor.ShuttingDown():
			resp.Status = "unavailable"
		case !resp.DB.Reachable:
			resp.Status = "degraded"
		default:
			resp.Status = "ok"
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}
	}
}
