//go:build !windows

package service

import (
	"context"
)

// StartWindowsService démarre l'application en tant que service Windows.
//
// Sur les plateformes non-Windows, il n'existe pas de service Windows (SCM).
// Cette implémentation existe uniquement pour que le code compile sur toutes
// les plateformes : main.go ne l'appelle jamais ailleurs qu'en mode Windows
// (il passe par la gestion de signaux SIGINT/SIGTERM). On exécute donc
// runFunc directement avec un contexte annulable, comme en mode hors service.
//
// runFunc reçoit un contexte annulable : il est annulé à la fin du programme.
// runFunc doit respecter ctx.Done() pour arrêter proprement ses goroutines de
// fond (workers planifiés, serveurs HTTP) avant de retourner.
func StartWindowsService(runFunc func(ctx context.Context)) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFunc(ctx)
	return nil
}
