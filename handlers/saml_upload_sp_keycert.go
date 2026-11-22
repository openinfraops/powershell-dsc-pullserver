package handlers

import (
	"archive/zip"
	"crypto/x509"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/ed25519"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"go-dsc-pull/utils"
)

var allowedSPFiles = map[string]bool{
	"sp.key":     true,
	"sp.crt.pem": true,
}

// SAMLUploadSPKeyCertHandler handles upload, validation, and archival of SP key/cert
func SAMLUploadSPKeyCertHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Parse multipart form. On borne la taille totale du corps (le seuil passé à
	// ParseMultipartForm ne fixe que la limite mémoire, pas la taille maximale).
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestBody)
	err := r.ParseMultipartForm(10 << 20) // 10MB max
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Invalid form data"}`)
		return
	}
	keyFile, _, err := r.FormFile("sp_key")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Missing private key file"}`)
		return
	}
	defer keyFile.Close()
	certFile, _, err := r.FormFile("sp_cert")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Missing certificate file"}`)
		return
	}
	defer certFile.Close()

	// Read files
	keyBytes, err := ioutil.ReadAll(keyFile)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to read key file"}`)
		return
	}
	certBytes, err := ioutil.ReadAll(certFile)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to read cert file"}`)
		return
	}

	// Validate key/cert match
	block, _ := pem.Decode(keyBytes)
	if block == nil || (block.Type != "PRIVATE KEY" && block.Type != "RSA PRIVATE KEY") {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Invalid private key format"}`)
		return
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		privKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":"Invalid private key"}`)
			return
		}
	}
	certBlock, _ := pem.Decode(certBytes)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Invalid certificate format"}`)
		return
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Invalid certificate"}`)
		return
	}
	// Check key matches cert
	pubKey := cert.PublicKey
	if !keysMatch(privKey, pubKey) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Key does not match certificate"}`)
		return
	}

	// Archive old key/cert if present
	exeDir, err := utils.ExePath()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to resolve executable path"}`)
		return
	}
	baseDir := filepath.Dir(exeDir)

	archiveDir := filepath.Join(baseDir, "saml_keycert_archive")
	os.MkdirAll(archiveDir, 0700)
	keyPath := "sp.key"
	certPath := "sp.crt.pem"
	if fileExistsSP(keyPath) && fileExistsSP(certPath) {
		zipName := filepath.Join(archiveDir, time.Now().Format("20060102_150405")+"_sp_keycert.zip")
		zipFile, err := os.Create(zipName)
		if err == nil {
			zipWriter := zip.NewWriter(zipFile)
			addFileToZip(zipWriter, keyPath)
			addFileToZip(zipWriter, certPath)
			zipWriter.Close()
			zipFile.Close()
		}
	}
	// Write new key/cert
	absKeyPath, err := resolveSPFilePath(keyPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Invalid key path"}`)
		return
	}
	absCertPath, err := resolveSPFilePath(certPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Invalid cert path"}`)
		return
	}
	if err := ioutil.WriteFile(absKeyPath, keyBytes, 0600); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to write key file"}`)
		return
	}
	if err := ioutil.WriteFile(absCertPath, certBytes, 0644); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to write cert file"}`)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func resolveSPFilePath(filename string) (string, error) {
	if !allowedSPFiles[filename] {
		return "", fmt.Errorf("unsupported SP file")
	}
	exePath, err := utils.ExePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exePath), filename), nil
}

func fileExistsSP(filename string) bool {
	absPath, err := resolveSPFilePath(filename)
	if err != nil {
		return false
	}
	_, err = os.Stat(absPath)
	return err == nil
}

func addFileToZip(zipWriter *zip.Writer, filename string) error {
	absPath, err := resolveSPFilePath(filename)
	if err != nil {
		return err
	}
	   file, err := os.Open(absPath)
	   if err != nil {
		   return err
	   }
	   defer file.Close()
	w, err := zipWriter.Create(filepath.Base(filename))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, file)
	return err
}

// keysMatch checks if private key matches public key
func keysMatch(priv interface{}, pub interface{}) bool {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return k.PublicKey.Equal(pub)
	case *ecdsa.PrivateKey:
		return k.PublicKey.Equal(pub)
	case ed25519.PrivateKey:
		return k.Public().(ed25519.PublicKey).Equal(pub)
	default:
		return false
	}
}
