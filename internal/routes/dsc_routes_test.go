package routes

import (
	"database/sql"
	"net/http"
	"testing"
)

func TestRegisterDSCRoutesNoConflict(t *testing.T) {
	// Enregistrer les routes DSC sur un mux neuf ne doit pas déclencher de panic
	// de conflit du ServeMux (les littéraux CertificateRotation/ModuleContent
	// doivent rester plus spécifiques que le wildcard {reports}).
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterDSCRoutes panicked: %v", r)
		}
	}()
	mux := http.NewServeMux()
	RegisterDSCRoutes(mux, (*sql.DB)(nil))
}
