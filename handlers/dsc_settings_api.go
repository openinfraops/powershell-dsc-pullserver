package handlers

import (
	"encoding/json"
	"net/http"

	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/global"
)

// DscSettingsDTO est le corps JSON échangé avec l'API admin /api/v1/dsc/settings.
type DscSettingsDTO struct {
	CertificateRotationEnabled bool `json:"certificate_rotation_enabled"`
	CertificateRenewalDays     int  `json:"certificate_renewal_days"`
}

// DscSettingsAPIHandler expose en lecture/écriture les réglages DSC stockés en
// base (table dsc_settings) : activation du renouvellement de certificat et
// fenêtre (en jours) avant expiration. GET renvoie les réglages courants ; PUT
// les met à jour. Admin uniquement (filtré au niveau des routes).
func DscSettingsAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	database := db.Shared()
	if database == nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := db.GetDscSettings(database)
		if err != nil {
			http.Error(w, "Failed to load DSC settings", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(DscSettingsDTO{
			CertificateRotationEnabled: settings.CertificateRotationEnabled,
			CertificateRenewalDays:     settings.CertificateRenewalDays,
		})

	case http.MethodPut:
		var req DscSettingsDTO
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.CertificateRenewalDays <= 0 {
			http.Error(w, "certificate_renewal_days must be > 0", http.StatusBadRequest)
			return
		}
		driver := global.AppConfig.Database.Driver
		if err := db.SaveDscSettings(database, driver, db.DscSettings{
			CertificateRotationEnabled: req.CertificateRotationEnabled,
			CertificateRenewalDays:     req.CertificateRenewalDays,
			LegacyImported:             true,
		}); err != nil {
			http.Error(w, "Failed to save DSC settings", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(req)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
