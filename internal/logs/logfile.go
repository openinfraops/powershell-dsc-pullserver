package logs

import (
	"bufio"
    "fmt"
    "os"
    "path/filepath"
    "sort"
	"strings"
    "sync"
	"time"
	"go-dsc-pull/internal/global"
    "go-dsc-pull/utils"
)

const (
	defaultMaxLogSizeMB  = 10
	defaultMaxLogBackups = 5
    defaultMaxLogAgeDays = 30
)

var logMu sync.Mutex

func currentLogPath() (string, error) {
    exePath, err := utils.ExePath()
    if err != nil {
        return "", fmt.Errorf("[LOG] Impossible de localiser l'exécutable: %v", err)
    }
    return filepath.Join(filepath.Dir(exePath), "dsc-pull-server.log"), nil
}

// WriteLogFile écrit un message dans le fichier de log local (à côté du binaire)
// puis le transmet à syslog (si activé). L'envoi syslog est non bloquant : un
// serveur syslog lent ou injoignable n'affecte jamais l'écriture locale ni les
// requêtes.
func WriteLogFile(message string) error {
	err := writeFileLine(message)
	enqueueSyslogFromMessage(message)
	return err
}

// writeFileLine écrit une ligne dans le fichier de log local sans renvoyer vers
// syslog. Utilisé par WriteLogFile et par les avertissements internes syslog
// (pour éviter toute boucle de ré-injection).
func writeFileLine(message string) error {
	logMu.Lock()
	defer logMu.Unlock()

    logPath, err := currentLogPath()
    if err != nil {
        return err
    }

	if err := rotateLogIfNeeded(logPath); err != nil {
		return err
	}

    f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return fmt.Errorf("[LOG] Impossible d'ouvrir le fichier log: %v", err)
    }
    defer f.Close()
    now := time.Now().Format("2006-01-02 15:04:05")
    logLine := fmt.Sprintf("[%s] %s\n", now, SanitizeLogMessage(message))
    if _, err := f.WriteString(logLine); err != nil {
        return fmt.Errorf("[LOG] Impossible d'écrire dans le fichier log: %v", err)
    }
    return nil
}

func rotateLogIfNeeded(logPath string) error {
    _, err := rotateAndCleanupIfNeeded(logPath)
    return err
}

func rotateAndCleanupIfNeeded(logPath string) (int, error) {
    enabled, maxLogSizeBytes, _, maxLogAgeDays := logRotationConfig()
    if !enabled {
        return 0, nil
    }

    info, err := os.Stat(logPath)
    if err != nil {
        if os.IsNotExist(err) {
            return 0, nil
        }
        return 0, fmt.Errorf("[LOG] Impossible de vérifier la taille du fichier log: %v", err)
    }

    shouldRotateBySize := info.Size() >= maxLogSizeBytes
    ageReference := info.ModTime()
    if firstEntryAt, tsErr := firstLogEntryTimestamp(logPath); tsErr == nil {
        ageReference = firstEntryAt
    }
    shouldRotateByAge := maxLogAgeDays > 0 && time.Since(ageReference) >= time.Duration(maxLogAgeDays)*24*time.Hour

    if !shouldRotateBySize && !shouldRotateByAge {
        // Enforce backup retention even when current log does not rotate.
        deleted, err := cleanupOldLogBackups(logPath)
        if err != nil {
            return deleted, err
        }
        return deleted, nil
    }

    rotatedPath := fmt.Sprintf("%s.%s", logPath, time.Now().Format("20060102-150405"))
    if err := os.Rename(logPath, rotatedPath); err != nil {
        return 0, fmt.Errorf("[LOG] Impossible de faire la rotation du fichier log: %v", err)
    }

    rotationReason := "size"
    if shouldRotateByAge && !shouldRotateBySize {
        rotationReason = "age"
    } else if shouldRotateByAge && shouldRotateBySize {
        rotationReason = "age,size"
    }
    now := time.Now().Format("2006-01-02 15:04:05")
    if f, openErr := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); openErr == nil {
        _, _ = f.WriteString(fmt.Sprintf("[%s] INFO [LOG ROTATION] Rotation initiated. previous_file=%s reason=%s\n", now, filepath.Base(rotatedPath), rotationReason))
        _ = f.Close()
    }

    deleted, err := cleanupOldLogBackups(logPath)
    if err != nil {
        return deleted, err
    }

    return deleted, nil
}

func cleanupOldLogBackups(logPath string) (int, error) {
    _, _, maxLogBackups, maxLogAgeDays := logRotationConfig()
    deletedCount := 0

    files, err := filepath.Glob(logPath + ".*")
    if err != nil {
        return 0, fmt.Errorf("[LOG] Impossible de lister les backups de logs: %v", err)
    }

    // First, delete files older than max age.
    cutoff := time.Now().Add(-time.Duration(maxLogAgeDays) * 24 * time.Hour)
    keptFiles := make([]string, 0, len(files))
    for _, file := range files {
        info, statErr := os.Stat(file)
        if statErr != nil {
            continue
        }
        if info.ModTime().Before(cutoff) {
            if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
                return deletedCount, fmt.Errorf("[LOG] Impossible de supprimer le backup de log expiré %s: %v", file, err)
            }
            deletedCount++
            continue
        }
        keptFiles = append(keptFiles, file)
    }

    if len(keptFiles) <= maxLogBackups {
        return deletedCount, nil
    }

    sort.Slice(keptFiles, func(i, j int) bool {
        fi, errI := os.Stat(keptFiles[i])
        fj, errJ := os.Stat(keptFiles[j])
        if errI != nil || errJ != nil {
            return keptFiles[i] > keptFiles[j]
        }
        return fi.ModTime().After(fj.ModTime())
    })

    for _, oldFile := range keptFiles[maxLogBackups:] {
        if err := os.Remove(oldFile); err != nil && !os.IsNotExist(err) {
            return deletedCount, fmt.Errorf("[LOG] Impossible de supprimer l'ancien backup de log %s: %v", oldFile, err)
        }
        deletedCount++
    }

    return deletedCount, nil
}

// RunLogBackupCleanupNow removes expired and excess rotated log files immediately.
func RunLogBackupCleanupNow() (int, error) {
	logMu.Lock()
	defer logMu.Unlock()

	logPath, err := currentLogPath()
	if err != nil {
		return 0, err
	}
    return rotateAndCleanupIfNeeded(logPath)
}

func logRotationConfig() (enabled bool, maxSizeBytes int64, maxBackups int, maxAgeDays int) {
    enabled = true
    maxSizeMB := defaultMaxLogSizeMB
    maxBackups = defaultMaxLogBackups
    maxAgeDays = defaultMaxLogAgeDays

    if cfg := global.AppConfig; cfg != nil {
        legacyDefaults := !cfg.DSCPullServer.EnableLogRotation && cfg.DSCPullServer.LogRotateMaxSizeMB == 0 && cfg.DSCPullServer.LogRotateMaxBackups == 0 && cfg.DSCPullServer.LogRotateMaxAgeDays == 0
        if !legacyDefaults {
            enabled = cfg.DSCPullServer.EnableLogRotation
        }
        if cfg.DSCPullServer.LogRotateMaxSizeMB > 0 {
            maxSizeMB = cfg.DSCPullServer.LogRotateMaxSizeMB
        }
        if cfg.DSCPullServer.LogRotateMaxBackups > 0 {
            maxBackups = cfg.DSCPullServer.LogRotateMaxBackups
        }
        if cfg.DSCPullServer.LogRotateMaxAgeDays > 0 {
            maxAgeDays = cfg.DSCPullServer.LogRotateMaxAgeDays
        }
    }

    maxSizeBytes = int64(maxSizeMB) * 1024 * 1024
    return enabled, maxSizeBytes, maxBackups, maxAgeDays
}

// SanitizeLogMessage neutralise les caractères de contrôle (retours à la ligne,
// tabulations, etc.) afin d'empêcher l'injection de lignes de log via des
// champs contrôlés par l'utilisateur (URL, RemoteAddr, auteur, noms de modules…,
// et notamment le Subject d'un certificat client DSC).
func SanitizeLogMessage(s string) string {
    var b strings.Builder
    for _, r := range s {
        switch {
        case r == '\n' || r == '\r' || r == '\t':
            b.WriteByte(' ')
        case r < 0x20 || r == 0x7f:
            b.WriteByte('?')
        default:
            b.WriteRune(r)
        }
    }
    return b.String()
}

func firstLogEntryTimestamp(logPath string) (time.Time, error) {
    f, err := os.Open(logPath)
    if err != nil {
        return time.Time{}, err
    }
    defer f.Close()

    s := bufio.NewScanner(f)
    for s.Scan() {
        line := strings.TrimSpace(s.Text())
        if line == "" {
            continue
        }
        if !strings.HasPrefix(line, "[") {
            continue
        }
        end := strings.Index(line, "]")
        if end <= 1 {
            continue
        }
        raw := line[1:end]
        ts, parseErr := time.ParseInLocation("2006-01-02 15:04:05", raw, time.Local)
        if parseErr != nil {
            continue
        }
        return ts, nil
    }
    if err := s.Err(); err != nil {
        return time.Time{}, err
    }
    return time.Time{}, fmt.Errorf("no parsable log timestamp")
}