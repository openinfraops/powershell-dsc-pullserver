package handlers


import (
	"encoding/json"
	"net/http"
	"strconv"
	"go-dsc-pull/internal/db"
	"go-dsc-pull/internal/schema"
	"go-dsc-pull/internal/auth"
	"go-dsc-pull/internal/global"
)

// --- Properties CRUD ---
func PropertiesListHandler(w http.ResponseWriter, r *http.Request) {
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	rows, _ := database.Query("SELECT id, name, description, priority FROM properties ORDER BY priority, name")
	var props []schema.Property
	for rows.Next() {
		var p schema.Property
		_ = rows.Scan(&p.ID, &p.Name, &p.Description, &p.Priority)
		props = append(props, p)
	}
	   w.Header().Set("Content-Type", "application/json")
	   if props == nil {
		   props = make([]schema.Property, 0)
	   }
	   _ = json.NewEncoder(w).Encode(props)
}

func PropertiesCreateHandler(w http.ResponseWriter, r *http.Request) {
	var p schema.Property
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	res, err := database.Exec("INSERT INTO properties (name, description, priority) VALUES (?, ?, ?)", p.Name, p.Description, p.Priority)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	id, _ := res.LastInsertId()
	p.ID = int(id)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

func PropertiesGetHandler(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	row := database.QueryRow("SELECT id, name, description, priority FROM properties WHERE id = ?", id)
	var p schema.Property
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Priority); err != nil {
		http.Error(w, "Not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

func PropertiesUpdateHandler(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	var p schema.Property
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	_, err := database.Exec("UPDATE properties SET name=?, description=?, priority=? WHERE id=?", p.Name, p.Description, p.Priority, id)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.WriteHeader(204)
}

func PropertiesDeleteHandler(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	if !auth.IsAdmin(r, database) {
		http.Error(w, "Forbidden: admin only", http.StatusForbidden)
		return
	}
	_, err := database.Exec("DELETE FROM properties WHERE id=?", id)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.WriteHeader(204)
}

// --- Node Properties CRUD ---
func NodePropertiesListHandler(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("nodename")
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	rows, _ := database.Query("SELECT node_id, property_id, value FROM node_properties WHERE node_id = ?", node)
	var props []schema.NodeProperty
	for rows.Next() {
		var p schema.NodeProperty
		_ = rows.Scan(&p.NodeName, &p.PropertyID, &p.Value)
		props = append(props, p)
	}
	   w.Header().Set("Content-Type", "application/json")
	   if props == nil {
		   props = make([]schema.NodeProperty, 0)
	   }
	   _ = json.NewEncoder(w).Encode(props)
}

func NodePropertiesCreateHandler(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("nodename")
	var p schema.NodeProperty
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	p.NodeName = node
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	_, err := database.Exec("INSERT INTO node_properties (node_id, property_id, value) VALUES (?, ?, ?)", p.NodeName, p.PropertyID, p.Value)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.WriteHeader(201)
}

func NodePropertyGetHandler(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("nodename")
	pid, _ := strconv.Atoi(r.PathValue("property_id"))
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	row := database.QueryRow("SELECT node_id, property_id, value FROM node_properties WHERE node_id = ? AND property_id = ?", node, pid)
	var p schema.NodeProperty
	if err := row.Scan(&p.NodeName, &p.PropertyID, &p.Value); err != nil {
		http.Error(w, "Not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

func NodePropertyUpdateHandler(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("nodename")
	pid, _ := strconv.Atoi(r.PathValue("property_id"))
	var p schema.NodeProperty
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	_, err := database.Exec("UPDATE node_properties SET value=? WHERE node_id=? AND property_id=?", p.Value, node, pid)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.WriteHeader(204)
}

func NodePropertyDeleteHandler(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("nodename")
	pid, _ := strconv.Atoi(r.PathValue("property_id"))
	database, _ := db.OpenDB(&global.AppConfig.Database)
	defer database.Close()
	_, err := database.Exec("DELETE FROM node_properties WHERE node_id=? AND property_id=?", node, pid)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.WriteHeader(204)
}
