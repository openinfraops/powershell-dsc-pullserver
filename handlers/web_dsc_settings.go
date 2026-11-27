package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"

	"go-dsc-pull/utils"
)

// WebDscSettingsHandler serves the DSC settings admin page. Read-only page: the
// form is populated and saved client-side via the admin API /api/v1/dsc/settings.
func WebDscSettingsHandler(w http.ResponseWriter, r *http.Request) {
	exeDir, err := utils.ExePath()
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	baseDir := filepath.Dir(exeDir)
	tmpl, err := template.New("layout.tmpl").
		ParseFiles(
			filepath.Join(baseDir, "templates/layout.tmpl"),
			filepath.Join(baseDir, "templates/head.tmpl"),
			filepath.Join(baseDir, "templates/menu.tmpl"),
			filepath.Join(baseDir, "templates/footer.tmpl"),
			filepath.Join(baseDir, "templates/dsc_settings.tmpl"),
		)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := map[string]interface{}{"Title": "DSC Settings"}
	err = tmpl.ExecuteTemplate(w, "layout", data)
	if err != nil {
		http.Error(w, "Render error: "+err.Error(), http.StatusInternalServerError)
	}
}
