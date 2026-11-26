//go:build windows

package service

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
)

// StartWindowsService démarre l'application en tant que service Windows.
//
// runFunc reçoit un contexte annulable : il est annulé lors d'un arrêt propre
// du service (Stop / Shutdown) ou, en mode hors service, à la fin du programme.
// runFunc doit respecter ctx.Done() pour arrêter proprement ses goroutines de
// fond (workers planifiés, serveurs HTTP) avant de retourner.
func StartWindowsService(runFunc func(ctx context.Context)) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return fmt.Errorf("[SERVICE] Erreur détection mode service: %v", err)
	}
	if isService {
		return svc.Run(ServiceName, &appService{runFunc: runFunc})
	}
	// Si pas en mode service, exécute normalement. On crée un contexte annulable
	// pour permettre un arrêt propre du processus.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFunc(ctx)
	return nil
}

// appService implémente svc.Handler
type appService struct {
	runFunc func(ctx context.Context)
}

func (s *appService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runFunc(ctx)
	}()

	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	for {
		c := <-r
		switch c.Cmd {
		case svc.Interrogate:
			status <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			// Annule le contexte : les goroutines de fond (workers planifiés,
			// serveurs HTTP) s'arrêtent proprement, puis on attend leur fin.
			cancel()
			<-done
			return false, 0
		}
	}
}
