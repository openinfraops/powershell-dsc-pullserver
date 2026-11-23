package db

import "database/sql"

// DscSettings regroupe les réglages du serveur DSC stockés en base (table
// dsc_settings, mono-ligne id=1). Contrairement à la configuration du fichier
// config.json, ces valeurs sont ajustables à chaud via l'API admin sans
// redémarrage du service.
//
//   - CertificateRotationEnabled: active l'émission de l'en-tête
//     DSC-certificateRotation (§2.2.2.8 du MS-DSCPM) vers les nœuds dont le
//     certificat est dans la fenêtre de renouvellement. Opt-in (défaut 0).
//   - CertificateRenewalDays: fenêtre de renouvellement, en jours avant
//     l'expiration du certificat. Par défaut 30 jours.
type DscSettings struct {
	CertificateRotationEnabled bool   `json:"certificate_rotation_enabled"`
	CertificateRenewalDays     int    `json:"certificate_renewal_days"`
	LegacyImported             bool   `json:"-"`
	UpdatedAt                  string `json:"-"`
}

// GetDscSettings lit la rangée unique (id=1) de la table dsc_settings.
func GetDscSettings(database *sql.DB) (DscSettings, error) {
	var settings DscSettings
	err := database.QueryRow(`SELECT certificate_rotation_enabled, certificate_renewal_days, legacy_imported FROM dsc_settings WHERE id = 1`).Scan(
		&settings.CertificateRotationEnabled,
		&settings.CertificateRenewalDays,
		&settings.LegacyImported,
	)
	return settings, err
}

// SaveDscSettings écrit la rangée unique (id=1) de la table dsc_settings, en
// UPDATE si elle existe, sinon en INSERT. Le comportement diffère selon le
// pilote (SQLite : ON CONFLICT ; MSSQL : IF EXISTS/ELSE).
func SaveDscSettings(database *sql.DB, driver string, settings DscSettings) error {
	nowExpr := NowExpr(driver)
	if driver == "mssql" || driver == "sqlserver" {
		_, err := database.Exec(`IF EXISTS (SELECT 1 FROM dsc_settings WHERE id = 1)
UPDATE dsc_settings SET certificate_rotation_enabled = ?, certificate_renewal_days = ?, legacy_imported = ?, updated_at = `+nowExpr+` WHERE id = 1
ELSE
INSERT INTO dsc_settings (id, certificate_rotation_enabled, certificate_renewal_days, legacy_imported) VALUES (1, ?, ?, ?)`,
			settings.CertificateRotationEnabled, settings.CertificateRenewalDays, settings.LegacyImported,
			settings.CertificateRotationEnabled, settings.CertificateRenewalDays, settings.LegacyImported,
		)
		return err
	}
	_, err := database.Exec(`INSERT INTO dsc_settings (id, certificate_rotation_enabled, certificate_renewal_days, legacy_imported, updated_at)
VALUES (1, ?, ?, ?, `+nowExpr+`)
ON CONFLICT(id) DO UPDATE SET certificate_rotation_enabled = excluded.certificate_rotation_enabled, certificate_renewal_days = excluded.certificate_renewal_days, legacy_imported = excluded.legacy_imported, updated_at = `+nowExpr+``,
		settings.CertificateRotationEnabled, settings.CertificateRenewalDays, settings.LegacyImported,
	)
	return err
}

// InitializeDscSettings garantit que la rangée (id=1) existe et que les valeurs
// retenues sont saines (défauts applicables). Retourne les réglages courants.
func InitializeDscSettings(database *sql.DB, driver string) (DscSettings, error) {
	settings, err := GetDscSettings(database)
	if err != nil {
		return DscSettings{}, err
	}
	if settings.CertificateRenewalDays <= 0 {
		settings.CertificateRenewalDays = 30
	}
	if !settings.LegacyImported {
		settings.LegacyImported = true
		if err := SaveDscSettings(database, driver, settings); err != nil {
			return DscSettings{}, err
		}
	}
	if settings.CertificateRenewalDays <= 0 {
		settings.CertificateRenewalDays = 30
	}
	return settings, nil
}
