package db

import (
	"database/sql"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/utils"
)

// ConfigurationModel représente un modèle de configuration MOF
// property: nom de la propriété
// value: valeur de la propriété
// mofFile: contenu du fichier MOF (BLOB)
// uploadDate: date d'upload
func CreateConfigurationModel(db *sql.DB, cm *schema.ConfigurationModel) error {
	_, err := db.Exec(`INSERT INTO configuration_model (name, uploaded_by, mof_file, original_name) VALUES (?, ?, ?, ?)`, cm.Name, cm.UploadedBy, cm.MofFile, cm.OriginalName)
	return err
}

func GetConfigurationModel(db *sql.DB, id int64) (*schema.ConfigurationModel, error) {
	row := db.QueryRow(`SELECT id, name, original_name, previous_id, uploaded_by, mof_file, upload_date, last_usage FROM configuration_model WHERE id = ?`, id)
	var cm schema.ConfigurationModel
	var uploadDate string
	var lastUsage sql.NullString
	var originalName sql.NullString
	var previousID sql.NullInt64
	if err := row.Scan(&cm.ID, &cm.Name, &originalName, &previousID, &cm.UploadedBy, &cm.MofFile, &uploadDate, &lastUsage); err != nil {
		return nil, err
	}
	if originalName.Valid {
		cm.OriginalName = &originalName.String
	} else {
		cm.OriginalName = nil
	}
	if previousID.Valid {
		cm.PreviousID = &previousID.Int64
	} else {
		cm.PreviousID = nil
	}
	t := utils.ParseTimeFlexible(uploadDate)
	cm.UploadDate = t
	if lastUsage.Valid && lastUsage.String != "" {
		t2 := utils.ParseTimeFlexible(lastUsage.String)
		cm.LastUsage = &t2
	} else {
		cm.LastUsage = nil
	}
	return &cm, nil
}

func ListConfigurationModels(db *sql.DB) ([]schema.ConfigurationModel, error) {
	rows, err := db.Query(`SELECT id, name, original_name, previous_id, uploaded_by, mof_file, upload_date, last_usage FROM configuration_model ORDER BY upload_date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []schema.ConfigurationModel
	for rows.Next() {
		var cm schema.ConfigurationModel
		var uploadDate string
		var lastUsage sql.NullString
		var originalName sql.NullString
		var previousID sql.NullInt64
		if err := rows.Scan(&cm.ID, &cm.Name, &originalName, &previousID, &cm.UploadedBy, &cm.MofFile, &uploadDate, &lastUsage); err != nil {
			return nil, err
		}
		if originalName.Valid {
			cm.OriginalName = &originalName.String
		} else {
			cm.OriginalName = nil
		}
		if previousID.Valid {
			cm.PreviousID = &previousID.Int64
		} else {
			cm.PreviousID = nil
		}
		t := utils.ParseTimeFlexible(uploadDate)
		cm.UploadDate = t
		if lastUsage.Valid && lastUsage.String != "" {
			t2 := utils.ParseTimeFlexible(lastUsage.String)
			cm.LastUsage = &t2
		} else {
			cm.LastUsage = nil
		}
		list = append(list, cm)
	}
	return list, nil
}

func UpdateConfigurationModel(db *sql.DB, cm *schema.ConfigurationModel) error {
	_, err := db.Exec(`UPDATE configuration_model SET name = ?, uploaded_by = ?, mof_file = ? WHERE id = ?`, cm.Name, cm.UploadedBy, cm.MofFile, cm.ID)
	return err
}

func DeleteConfigurationModel(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM configuration_model WHERE id = ?`, id)
	return err
}
