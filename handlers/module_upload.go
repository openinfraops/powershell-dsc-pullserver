package handlers

import (
	"log"
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"path/filepath"
	"go-dsc-pull/internal/global"
	"go-dsc-pull/internal/auth"
	internaldb "go-dsc-pull/internal/db"
)

// Helper: extract module name/version from .psd1
func extractModuleInfo(psd1Content string) (name, version string) {
	name, version = "", ""
	lines := strings.Split(psd1Content, "\n")
	for _, line := range lines {
		l := strings.ToLower(line)
		if strings.Contains(l, "moduleversion") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				version = strings.TrimSpace(parts[1])
				version = strings.Trim(version, "'\"")
			}
		}
	}
	// Nom du module = nom du fichier .psd1 (à passer en paramètre si besoin)
	// Ici, fallback sur "Unknown" si non trouvé
	if name == "" {
		name = "Unknown"
	}
	return name, version
}

// Bornes de décompression des archives de modules (anti zip-bomb) et de nombre
// d'entrées. Une archive légitime (module DSC) fait quelques Mo ; ces valeurs
// bloquent une archive comprimée qui se décompresse de manière arbitraire.
const (
	maxModuleZipEntries    = 1000
	maxModuleZipEntryBytes = 20 << 20 // 20 MiB par entrée
	maxModuleZipTotalBytes = 64 << 20 // 64 MiB au total décompressé
)

// unsafeZipEntryName reporte si un nom d'entrée d'archive est dangereux
// (traversée de répertoire `..`, chemin absolu, backslash), afin d'éviter un
// zip-slip côté client lorsque l'archive est re-distribuée.
func unsafeZipEntryName(name string) bool {
	return strings.Contains(name, "..") || strings.Contains(name, "\\") ||
		strings.HasPrefix(name, "/") || strings.HasPrefix(name, "./") ||
		strings.HasPrefix(name, "../")
}

// Handler: upload nupkg, process, store as DSC module
func ModuleUploadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Contrôle d'accès : la publication de module est réservée aux admins,
		// quel que soit le format (zip natif ou nupkg). Le contrôle doit être fait
		// AVANT toute branche (auparavant le chemin .zip le contournait).
		if !auth.IsAdmin(r, db) {
			http.Error(w, "Forbidden: admin only", http.StatusForbidden)
			return
		}
		// Borne la taille totale du corps multipart (nupkg/zip) pour prévenir
		// l'épuisement mémoire. ParseMultipartForm(32<<20) ne fixe que le seuil
		// en mémoire, pas la taille maximale du corps.
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestBody)
		log.Printf("[DEBUG] ParseMultipartForm...")
		err := r.ParseMultipartForm(32 << 20)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Invalid form: %v\n", err)
			return
		}
		log.Printf("[DEBUG] FormFile...")
		file, _, err := r.FormFile("nupkg")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "File error: %v\n", err)
			return
		}
		defer file.Close()
		log.Printf("[DEBUG] ReadAll file...")
		nupkgBytes, err := ioutil.ReadAll(file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Read error: %v\n", err)
			return
		}
		// Si le fichier uploadé est un .zip DSC natif, on le stocke tel quel
			   if strings.HasSuffix(strings.ToLower(r.MultipartForm.File["nupkg"][0].Filename), ".zip") {
				   zipName := r.MultipartForm.File["nupkg"][0].Filename
				   base := strings.TrimSuffix(zipName, ".zip")
				   name := base
				   version := ""
				   if strings.Contains(base, "_") {
					   parts := strings.SplitN(base, "_", 2)
					   name = parts[0]
					   version = parts[1]
				   }
				   // (Suppression de la sauvegarde temporaire du zip)
				   log.Printf("[DEBUG] Zip size: %d", len(nupkgBytes))
				   log.Printf("[DEBUG] name: %s, version: %s", name, version)
				   log.Printf("[DEBUG] Checksum...")
				   sha := sha256.Sum256(nupkgBytes)
				   checksum := strings.ToUpper(fmt.Sprintf("%x", sha[:]))
				   log.Printf("[DEBUG] DB insert...")
			   		_, err := db.Exec(`INSERT INTO modules (name, version, checksum, zip_blob) VALUES (?, ?, ?, ?)`, name, version, checksum, nupkgBytes)
				   // Audit log for zip upload
				  if( err == nil) {
					user := "?"
					if r.Context().Value("userId") != nil {
						if sub, ok := r.Context().Value("userId").(string); ok {
							user = sub
						}
					}
					// Try to load driver name for audit
					driverName := global.AppConfig.Database.Driver
					if driverName != "" {
						// Insert audit log
						_, _ = db.Exec(`INSERT INTO audit (user, action, object, details, extra, timestamp) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`, user, "create", "module", fmt.Sprintf("Ajout module: %s v%s (zip)", name, version), "")
					}
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					fmt.Fprintf(w, "Module %s v%s uploaded and processed as zip.\n", name, version)
					return
				}
			}
		// Sinon, traite le nupkg comme avant
		log.Printf("[DEBUG] zip.NewReader...")
		zipReader, err := zip.NewReader(bytes.NewReader(nupkgBytes), int64(len(nupkgBytes)))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Not a valid zip: %v\n", err)
			return
		}
		var psd1Content string
		var psd1Name string
		var moduleFiles = make(map[string][]byte)
		log.Printf("[DEBUG] Loop zip files...")
		if len(zipReader.File) > maxModuleZipEntries {
			http.Error(w, "Archive contains too many entries", http.StatusBadRequest)
			return
		}
		var totalDecompressed int64
		for _, f := range zipReader.File {
			// Exclure les fichiers NuGet inutiles
			lower := strings.ToLower(f.Name)
			if strings.HasSuffix(lower, ".nuspec") ||
				lower == "[content_types].xml" ||
				strings.HasPrefix(lower, "_rels/") ||
				strings.HasPrefix(lower, "package/") {
				continue
			}
			// Rejet des noms d'entrée dangereux (traversée de répertoire).
			if unsafeZipEntryName(f.Name) {
				http.Error(w, "Archive contains an unsafe entry name", http.StatusBadRequest)
				return
			}
			fileReader, err := f.Open()
			if err != nil {
				http.Error(w, "Error reading archive entry", http.StatusBadRequest)
				return
			}
			// Borne la décompression de chaque entrée (anti zip-bomb).
			fileBytes, err := ioutil.ReadAll(io.LimitReader(fileReader, maxModuleZipEntryBytes+1))
			fileReader.Close()
			if err != nil {
				http.Error(w, "Error reading archive entry", http.StatusBadRequest)
				return
			}
			if len(fileBytes) > maxModuleZipEntryBytes {
				http.Error(w, "Archive entry too large", http.StatusBadRequest)
				return
			}
			totalDecompressed += int64(len(fileBytes))
			if totalDecompressed > maxModuleZipTotalBytes {
				http.Error(w, "Archive too large after decompression", http.StatusBadRequest)
				return
			}
			moduleFiles[f.Name] = fileBytes
			// Cherche le .psd1 à la racine
			if strings.HasSuffix(f.Name, ".psd1") && !strings.Contains(f.Name, "/") {
				psd1Name = f.Name
				psd1Content = string(fileBytes)
				log.Printf("[DEBUG] Found root psd1: %s", psd1Name)
			}
		}
		   log.Printf("[DEBUG] ExtractModuleInfo...")
		   name := ""
		   version := ""
		   if psd1Name != "" {
			   name = strings.TrimSuffix(filepath.Base(psd1Name), ".psd1")
			   version = ""
			   lines := strings.Split(psd1Content, "\n")
			   for _, line := range lines {
				   l := strings.ToLower(line)
				   if strings.Contains(l, "moduleversion") {
					   parts := strings.SplitN(line, "=", 2)
					   if len(parts) == 2 {
						   version = strings.TrimSpace(parts[1])
						   version = strings.Trim(version, "'\"")
					   }
				   }
			   }
		   }
		   if name == "" || version == "" {
			http.Error(w, "Module info not found in root psd1", http.StatusBadRequest)
			return
		}
		// Vérifie si le module existe déjà
		var count int
		err = db.QueryRow(`SELECT COUNT(*) FROM modules WHERE name = ? AND version = ?`, name, version).Scan(&count)
		if err == nil && count > 0 {
			http.Error(w, "Ce module et cette version existent déjà.", http.StatusConflict)
			return
		}
		// Vérifie si le module existe déjà
		err = db.QueryRow(`SELECT COUNT(*) FROM modules WHERE name = ? AND version = ?`, name, version).Scan(&count)
		if err == nil && count > 0 {
			http.Error(w, "Ce module et cette version existent déjà.", http.StatusConflict)
			return
		}
		log.Printf("[DEBUG] Rezip DSC format...")
		var buf bytes.Buffer
		zipWriter := zip.NewWriter(&buf)
		//dscRoot := version
		for path, data := range moduleFiles {
			// Place tous les fichiers dans le dossier racine NomModule_Version/Version/
			//newPath :=  dscRoot + "/" + path
			f, _ := zipWriter.Create(path)
			f.Write(data)
		}
		zipWriter.Close()
		zipBytes := buf.Bytes()
		// (Suppression de la sauvegarde temporaire du zip)
		log.Printf("[DEBUG] Nb fichiers extraits: %d, zipBytes size: %d", len(moduleFiles), len(zipBytes))
		log.Printf("[DEBUG] Checksum...")
		sha := sha256.Sum256(zipBytes)
		checksum := strings.ToUpper(fmt.Sprintf("%x", sha[:]))
		log.Printf("[DEBUG] DB insert...")
		_, err = db.Exec(`INSERT INTO modules (name, version, checksum, zip_blob) VALUES (?, ?, ?, ?)`, name, version, checksum, zipBytes)
		   w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		   fmt.Fprintf(w, "Module %s v%s uploaded and processed.\n", name, version)
		   // Audit log for nupkg upload
		   user := "?"
		   if r.Context().Value("userId") != nil {
			   if sub, ok := r.Context().Value("userId").(string); ok {
				   user = sub
			   }
		   }
		   // Try to load driver name for audit
		driverName := global.AppConfig.Database.Driver
		if driverName != "" {
			_ = internaldb.InsertAudit(db, driverName, user, "create", "module", fmt.Sprintf("Ajout module: %s v%s (nupkg)", name, version), "")
		}
	}
}
