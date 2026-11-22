package handlers

import (
	"errors"
	"io"
	"net/http"
)

// bornes de taille des corps HTTP, en octets.
const (
	// maxDSCRequestBody borne les corps des endpoints DSC (enregistrement,
	// rapports SendReport, GetDscAction). La spec DSC renvoie des métadonnées et
	// des rapports de quelques Ko à quelques Mo ; 20 MiB est très large tout en
	// prévenant l'épuisement mémoire par un corps énorme.
	maxDSCRequestBody = 20 << 20 // 20 MiB

	// maxUploadRequestBody borne les corps d'upload multipart (nupkg/zip, MOF,
	// certificat SAML). 64 MiB couvre les gros modules tout en bornant la RAM.
	maxUploadRequestBody = 64 << 20 // 64 MiB
)

// readBodyLimited lit le corps de la requête en le bornant à limit octets.
// Si limit <= 0, une valeur par défaut est utilisée. En cas de dépassement, la
// réponse HTTP appropriée est écrite (413) et une erreur est retournée ; en cas
// d'autre erreur de lecture, un 400 est écrit.
func readBodyLimited(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = maxDSCRequestBody
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Unable to read request body", http.StatusBadRequest)
		}
		return nil, err
	}
	return body, nil
}
