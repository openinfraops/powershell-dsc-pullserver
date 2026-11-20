package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"

	"go-dsc-pull/utils"
)

// WebNodesHandler affiche la page de vue des nœuds (liste des agents, corbeille,
// purge, pré-enregistrement).
func WebNodesHandler(w http.ResponseWriter, r *http.Request) {
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
			filepath.Join(baseDir, "templates/nodes.tmpl"),
		)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := map[string]interface{}{
		"Title": "Nodes",
	}
	err = tmpl.ExecuteTemplate(w, "layout", data)
	if err != nil {
		http.Error(w, "Render error: "+err.Error(), http.StatusInternalServerError)
	}
}
