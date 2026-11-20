package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/utils"
)

// configModelDTRow est la ligne renvoyée à DataTable (sans le BLOB mof_file,
// inutile pour une liste et volumineux à transférer).
type configModelDTRow struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	OriginalName *string    `json:"original_name,omitempty"`
	PreviousID   *int64     `json:"previous_id,omitempty"`
	UploadDate   time.Time  `json:"upload_date"`
	UploadedBy   string     `json:"uploaded_by"`
	LastUsage    *time.Time `json:"last_usage,omitempty"`
}

// configModelDTResponse est la réponse JSON attendue par DataTable en mode
// serverSide (protocole DataTables v1.x).
type configModelDTResponse struct {
	Draw            int                `json:"draw"`
	RecordsTotal    int                `json:"recordsTotal"`
	RecordsFiltered int                `json:"recordsFiltered"`
	Data            []configModelDTRow `json:"data"`
}

// isMSSQLDriver détermine si le driver DB courant est MSSQL (vs SQLite).
func isMSSQLDriver(driver string) bool {
	d := strings.ToLower(driver)
	return strings.Contains(d, "mssql") || strings.Contains(d, "sqlserver")
}

// ListConfigurationModelsDataTableHandler implémente le protocole
// serverSide de DataTables pour la page Configurations. Il est branché
// derrière l'endpoint /api/v1/configuration_models (au lieu du handler
// "current" qui renvoie la liste complète) quand le paramètre draw est
// présent, ce qui permet la pagination, le tri de colonnes et la recherche.
func ListConfigurationModelsDataTableHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	dbConn, err := db.OpenDB(&global.AppConfig.Database)
	if err != nil {
		http.Error(w, "DB open error", http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	draw, _ := strconv.Atoi(r.URL.Query().Get("draw"))
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	length, _ := strconv.Atoi(r.URL.Query().Get("length"))
	if length <= 0 || length > 5000 {
		length = 25
	}
	if start < 0 {
		start = 0
	}

	search := strings.TrimSpace(r.URL.Query().Get("search[value]"))

	// Tri : mappe l'index de colonne DataTable vers une colonne SQL.
	sortableCols := map[int]string{
		0: "id",
		1: "name",
		2: "uploaded_by",
		3: "upload_date",
		4: "last_usage",
	}
	orderColIdx, _ := strconv.Atoi(r.URL.Query().Get("order[0][column]"))
	orderDir := r.URL.Query().Get("order[0][dir]")
	if orderDir != "asc" {
		orderDir = "desc"
	}
	orderCol, ok := sortableCols[orderColIdx]
	if !ok {
		orderCol = "id"
		orderDir = "desc"
	}

	isMSSQL := isMSSQLDriver(global.AppConfig.Database.Driver)

	// Clauses WHERE communes : uniquement la dernière version de chaque config.
	where := " WHERE original_name = name"
	args := []interface{}{}

	if search != "" {
		pattern := "%" + search + "%"
		if isMSSQL {
			where += " AND (name LIKE ? OR uploaded_by LIKE ? OR CONVERT(NVARCHAR(50), id) LIKE ?)"
		} else {
			where += " AND (name LIKE ? OR uploaded_by LIKE ? OR CAST(id AS TEXT) LIKE ?)"
		}
		args = append(args, pattern, pattern, pattern)
	}

	// Nombre total (sans filtre) et nombre filtré.
	var recordsTotal, recordsFiltered int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM configuration_model WHERE original_name = name").Scan(&recordsTotal); err != nil {
		http.Error(w, "DB count error", http.StatusInternalServerError)
		return
	}
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM configuration_model"+where, args...).Scan(&recordsFiltered); err != nil {
		http.Error(w, "DB count error", http.StatusInternalServerError)
		return
	}

	// Construction de la requête paginée + triée.
	query := "SELECT id, name, original_name, previous_id, upload_date, uploaded_by, last_usage FROM configuration_model" + where
	query += " ORDER BY " + orderCol + " " + orderDir

	if isMSSQL {
		query += " OFFSET ? ROWS FETCH NEXT ? ROWS ONLY"
		args = append(args, start, length)
	} else {
		query += " LIMIT ? OFFSET ?"
		args = append(args, length, start)
	}

	rows, err := dbConn.Query(query, args...)
	if err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	data := make([]configModelDTRow, 0)
	for rows.Next() {
		var row configModelDTRow
		var origName sql.NullString
		var prevID sql.NullInt64
		var uploadDate string
		var lastUsage sql.NullString
		if err := rows.Scan(&row.ID, &row.Name, &origName, &prevID, &uploadDate, &row.UploadedBy, &lastUsage); err != nil {
			http.Error(w, "DB scan error", http.StatusInternalServerError)
			return
		}
		if origName.Valid {
			row.OriginalName = &origName.String
		}
		if prevID.Valid {
			v := prevID.Int64
			row.PreviousID = &v
		}
		row.UploadDate = utils.ParseTimeFlexible(uploadDate)
		if lastUsage.Valid && lastUsage.String != "" {
			t := utils.ParseTimeFlexible(lastUsage.String)
			row.LastUsage = &t
		} else {
			row.LastUsage = nil
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "DB query error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configModelDTResponse{
		Draw:            draw,
		RecordsTotal:    recordsTotal,
		RecordsFiltered: recordsFiltered,
		Data:            data,
	})
}
