package service

import (
	"go-dsc-pull/internal/logs"
)

// WriteServiceLog écrit un message dans un fichier de log à côté du binaire
func WriteServiceLog(message string) error {
	return logs.WriteLogFile(message)
}

// ServiceName is the Windows SCM service name registered by the executable.
// It MUST stay in sync with the service name used by the install/update/uninstall
// scripts (scripts/*-windows.ps1) and the installation guide (doc/installation.md),
// which create/stop/delete the service under this name.
const ServiceName = "DSC-Console"
