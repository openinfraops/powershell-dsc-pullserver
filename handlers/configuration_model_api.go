package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	jwt "github.com/golang-jwt/jwt/v5"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/logs"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/utils"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// POST /api/v1/configuration_models
func CreateConfigurationModelHandler(w http.ResponseWriter, r *http.Request) {
	dbConn := db.Shared()
	if !auth.IsAdmin(r, dbConn) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	// Borne la taille du corps multipart d'upload MOF.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestBody)
	file, header, err := r.FormFile("mof_file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Missing MOF file"))
		return
	}
	defer file.Close()
	// Vérifie l'extension .mof
	filename := strings.ToLower(header.Filename)
	log.Printf("[REGISTER][CONFIG] Uploading configuration model: %s", filename)
	if len(filename) < 5 || filename[len(filename)-4:] != ".mof" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Le fichier doit être au format .mof"))
		return
	}
	// Calcule le nom du modèle (nom du fichier sans .mof)
	name := strings.ToLower(filename[:len(filename)-4])
	log.Printf("[REGISTER][CONFIG] Base name extracted: %s", name)
	mofBytes, err := ioutil.ReadAll(file)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// 1. Récupérer la configuration ayant le même nom (si il y en a une), changer son nom "champ name" avec le nom "versionné", et stocker l'ID de cette configuration en variable
	var previousID *int64 = nil
	var existingId int64
	var existingDate string
	var originalName sql.NullString
	var existingMof []byte
	err = dbConn.QueryRow(`SELECT id, upload_date, original_name, mof_file FROM configuration_model WHERE name = ?`, name).Scan(&existingId, &existingDate, &originalName, &existingMof)
	var newName string
	if err == nil && existingId > 0 {
		// Contrôle du contenu : si le fichier est identique à la version actuelle, on retourne 200 OK avec un message
		if string(existingMof) == string(mofBytes) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"info": "Configuration identical to current version, no update performed."})
			return
		}
		safeDate := existingDate
		safeDate = strings.ReplaceAll(safeDate, ":", "")
		safeDate = strings.ReplaceAll(safeDate, "-", "")
		safeDate = strings.ReplaceAll(safeDate, " ", "_")
		newName = name + "-" + safeDate
		_, errUpdate := dbConn.Exec("UPDATE configuration_model SET name = ? WHERE id = ?", newName, existingId)
		if errUpdate != nil {
			logs.WriteLogFile("[ERROR] Error updating configuration_model versioning: " + errUpdate.Error())
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Versioning error"))
			return
		}
		previousID = &existingId
	}

	// 2. Tenter la création de la configuration, et indiquer si besoin l'ID de la précédente configuration (celle modifiée précédemment) si besoin dans le champ prévu à cet effet
	uploadedBy := "?"
	if r.Context().Value("userId") != nil {
		if sub, ok := r.Context().Value("userId").(string); ok {
			uploadedBy = sub
		}
	} else if authHeader := r.Header.Get("Authorization"); len(authHeader) > 7 {
		tokenStr := authHeader[7:]
		secretValue := auth.ResolveJWTSecret(global.AppConfig)
		if secretValue == "" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Missing shared_secret"))
			return
		}
		secret := []byte(secretValue)
		token, _ := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) { return secret, nil })
		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			if sub, ok := claims["sub"].(string); ok {
				uploadedBy = sub
			}
		}
	}

	cm := &schema.ConfigurationModel{
		Name:         name,
		OriginalName: &name,
		UploadedBy:   uploadedBy,
		MofFile:      mofBytes,
		PreviousID:   previousID,
	}
	err = db.CreateConfigurationModel(dbConn, cm)

	// 3. En cas d'échec de l'étape de création, on restore le nom de la configuration précédente (de versionnée à non versionnée) et on retourne l'opération en échec (et on log le détail du pourquoi en ERROR)
	if err != nil {
		if previousID != nil {
			_, _ = dbConn.Exec("UPDATE configuration_model SET name = ? WHERE id = ?", name, *previousID)
		}
		logs.WriteLogFile("[ERROR] Error creating configuration_model: " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Creation error"))
		return
	}

	// Audit création
	driverName := global.AppConfig.Database.Driver
	_ = db.InsertAudit(dbConn, driverName, uploadedBy, "create", "configuration_model", "Created configuration: "+name, "")

	// Met à jour le statut des agents liés à cette configuration
	var res sql.Result
	res, err = dbConn.Exec("UPDATE agents SET state = 'pending_apply' WHERE agent_id IN (SELECT agent_id FROM agent_configurations WHERE configuration_name = ?)", name)
	if err != nil {
		logs.WriteLogFile("[ERROR] Error updating agents pending_apply: " + err.Error())
	} else {
		n, _ := res.RowsAffected()
		msg := fmt.Sprintf("[INFO] New configuration uploaded, impacted nodes: %d", n)
		logs.WriteLogFile(msg)
	}
	w.WriteHeader(http.StatusCreated)
}

// GET /api/v1/configuration_models/{id}
func GetConfigurationModelHandler(w http.ResponseWriter, r *http.Request) {
	dbConn := db.Shared()
	// Extract id from URL path: /api/v1/configuration_models/{id}
	parts := strings.Split(r.URL.Path, "/")
	idStr := parts[len(parts)-1]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	cm, err := db.GetConfigurationModel(dbConn, id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cm)
}

// GET /api/v1/configuration_models
func ListConfigurationModelsHandler(w http.ResponseWriter, r *http.Request) {
	// Mode serverSide de DataTable : le navigateur envoie le paramètre draw.
	// On délègue alors au handler paginé/trié/recherche.
	if r.URL.Query().Get("draw") != "" {
		ListConfigurationModelsDataTableHandler(w, r)
		return
	}

	dbConn := db.Shared()

	// Support de l'option ?count=1
	if r.URL.Query().Get("count") == "1" {
		row := dbConn.QueryRow("SELECT COUNT(*) FROM configuration_model")
		var count int
		if err := row.Scan(&count); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"count": count})
		return
	}

	// Support de l'option ?current=1 pour ne retourner que la dernière version de chaque configuration (original_name = name)
	if r.URL.Query().Get("current") == "1" {
		query := `SELECT id, name, original_name, previous_id, upload_date, uploaded_by, mof_file, last_usage FROM configuration_model WHERE original_name = name`
		rows, err := dbConn.Query(query)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var list []schema.ConfigurationModel
		for rows.Next() {
			var cm schema.ConfigurationModel
			var origName sql.NullString
			var prevID sql.NullInt64
			var lastUsage sql.NullTime
			err := rows.Scan(&cm.ID, &cm.Name, &origName, &prevID, &cm.UploadDate, &cm.UploadedBy, &cm.MofFile, &lastUsage)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if origName.Valid {
				cm.OriginalName = &origName.String
			}
			if prevID.Valid {
				v := prevID.Int64
				cm.PreviousID = &v
			}
			if lastUsage.Valid {
				t := lastUsage.Time
				cm.LastUsage = &t
			} else {
				cm.LastUsage = nil
			}
			list = append(list, cm)
		}
		w.Header().Set("Content-Type", "application/json")
		if list == nil {
			list = make([]schema.ConfigurationModel, 0)
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	// Sinon, comportement normal (toutes les versions)
	list, err := db.ListConfigurationModels(dbConn)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if list == nil {
		list = make([]schema.ConfigurationModel, 0)
	}
	json.NewEncoder(w).Encode(list)
}

// PUT /api/v1/configuration_models/{id}
func UpdateConfigurationModelHandler(w http.ResponseWriter, r *http.Request) {
	dbConn := db.Shared()
	// Extract id from URL path: /api/v1/configuration_models/{id}
	parts := strings.Split(r.URL.Path, "/")
	idStr := parts[len(parts)-1]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !auth.IsAdmin(r, dbConn) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	// Borne la taille du corps multipart d'upload MOF.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestBody)
	name := r.FormValue("name")
	uploadedBy := r.FormValue("uploaded_by")
	file, _, err := r.FormFile("mof_file")
	var mofBytes []byte
	if err == nil {
		defer file.Close()
		mofBytes, _ = ioutil.ReadAll(file)
	}
	cm := &schema.ConfigurationModel{
		ID:         id,
		Name:       name,
		UploadedBy: uploadedBy,
		MofFile:    mofBytes,
	}
	err = db.UpdateConfigurationModel(dbConn, cm)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// DELETE /api/v1/configuration_models/{id}
func DeleteConfigurationModelHandler(w http.ResponseWriter, r *http.Request) {
	dbConn := db.Shared()
	if !auth.IsAdmin(r, dbConn) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	// Récupère l'id soit dans le chemin, soit en paramètre
	var idStr string
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) > 0 {
		idStr = parts[len(parts)-1]
	}
	if idStr == "" || idStr == "configuration_models" {
		idStr = r.URL.Query().Get("id")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid configuration id"))
		return
	}
	// Récupère le nom de la configuration
	var configName string
	err = dbConn.QueryRow("SELECT name FROM configuration_model WHERE id = ?", id).Scan(&configName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Erreur lors de la récupération du nom de la configuration."))
		return
	}
	// Vérifie si un noeud utilise cette configuration via le nom
	var count int
	err = dbConn.QueryRow("SELECT COUNT(*) FROM agent_configurations WHERE configuration_name = ?", configName).Scan(&count)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Erreur lors de la vérification des associations de noeuds."))
		return
	}
	if count > 0 {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("This confguration is in use and cannot be deleted."))
		return
	}
	err = db.DeleteConfigurationModel(dbConn, id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// Audit suppression
	driverName := global.AppConfig.Database.Driver
	// Récupère l'utilisateur
	user := "?"
	if r.Context().Value("userId") != nil {
		if sub, ok := r.Context().Value("userId").(string); ok {
			user = sub
		}
	}
	_ = db.InsertAudit(dbConn, driverName, user, "delete", "configuration_model", "Deleted configuration: "+configName, "")
	w.WriteHeader(http.StatusOK)
}

// GET /api/v1/configuration_models/{id}/detail
func GetConfigurationModelDetailHandler(w http.ResponseWriter, r *http.Request) {
	dbConn := db.Shared()
	// Extract name from URL path: /api/v1/configuration_models/{name}/detail
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	name := parts[len(parts)-2]

	// Récupère la config courante par nom
	// MSSQL: utilise TOP 1, SQLite: utilise LIMIT 1
	query := ""
	if strings.Contains(strings.ToLower(global.AppConfig.Database.Driver), "sqlserver") || strings.Contains(strings.ToLower(global.AppConfig.Database.Driver), "mssql") {
		query = `SELECT TOP 1 id, name, original_name, previous_id, upload_date, uploaded_by, mof_file, last_usage FROM configuration_model WHERE name = ? ORDER BY upload_date DESC`
	} else {
		query = `SELECT id, name, original_name, previous_id, upload_date, uploaded_by, mof_file, last_usage FROM configuration_model WHERE name = ? ORDER BY upload_date DESC LIMIT 1`
	}
	row := dbConn.QueryRow(query, name)
	var cm schema.ConfigurationModel
	var origName sql.NullString
	var prevID sql.NullInt64
	var uploadDate string
	var lastUsage sql.NullTime
	err := row.Scan(&cm.ID, &cm.Name, &origName, &prevID, &uploadDate, &cm.UploadedBy, &cm.MofFile, &lastUsage)
	if err != nil {
		log.Printf("[CONFIG DETAIL] Configuration '%s' not found: %v", name, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if origName.Valid {
		cm.OriginalName = &origName.String
	}
	if prevID.Valid {
		v := prevID.Int64
		cm.PreviousID = &v
	}

	cm.UploadDate = utils.ParseTimeFlexible(uploadDate)
	if lastUsage.Valid {
		t := lastUsage.Time
		cm.LastUsage = &t
	} else {
		cm.LastUsage = nil
	}
	// Historique des versions (même original_name)
	var versions []schema.ConfigurationModel
	if cm.OriginalName != nil {
		rows, err := dbConn.Query(`SELECT id, name, original_name, previous_id, upload_date, uploaded_by, mof_file, last_usage FROM configuration_model WHERE original_name = ? ORDER BY upload_date DESC`, *cm.OriginalName)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var v schema.ConfigurationModel
				var origName sql.NullString
				var prevID sql.NullInt64
				var uploadDate string
				var lastUsage sql.NullTime
				err := rows.Scan(&v.ID, &v.Name, &origName, &prevID, &uploadDate, &v.UploadedBy, &v.MofFile, &lastUsage)
				if err == nil {
					if origName.Valid {
						v.OriginalName = &origName.String
					}
					if prevID.Valid {
						vv := prevID.Int64
						v.PreviousID = &vv
					}
					v.UploadDate = utils.ParseTimeFlexible(uploadDate)
					if lastUsage.Valid {
						t := lastUsage.Time
						v.LastUsage = &t
					}
					versions = append(versions, v)
				}
			}
		}
	}
	// Extraction des modules/dépendances depuis le contenu MOF (UTF-8/UTF-16)
	modules := parseModulesFromMOFBytes(cm.MofFile)

	// Ajout des noeuds liés à la configuration (agents)
	linkedNodes := []map[string]interface{}{}
	rows, err := dbConn.Query(`SELECT a.agent_id, a.node_name, a.state, a.last_communication FROM agents a INNER JOIN agent_configurations ac ON a.agent_id = ac.agent_id WHERE ac.configuration_name = ?`, cm.Name)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var agentID, nodeName, state, lastComm string
			if err := rows.Scan(&agentID, &nodeName, &state, &lastComm); err == nil {
				linkedNodes = append(linkedNodes, map[string]interface{}{
					"agent_id":           agentID,
					"node_name":          nodeName,
					"state":              state,
					"last_communication": lastComm,
				})
			}
		}
	}

	resp := map[string]interface{}{
		"id":           cm.ID,
		"name":         cm.Name,
		"uploaded_by":  cm.UploadedBy,
		"upload_date":  cm.UploadDate,
		"last_usage":   cm.LastUsage,
		"versions":     versions,
		"modules":      modules,
		"linked_nodes": linkedNodes,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GET /api/v1/configuration_models/{id}/download
func DownloadConfigurationModelHandler(w http.ResponseWriter, r *http.Request) {
	dbConn := db.Shared()
	// Extract id from URL path: /api/v1/configuration_models/{id}/download
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	idStr := parts[len(parts)-2]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	cm, err := db.GetConfigurationModel(dbConn, id)
	if err != nil || cm.MofFile == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.mof\"", cm.Name))
	w.Header().Set("Content-Type", "text/plain")
	w.Write(cm.MofFile)
}
