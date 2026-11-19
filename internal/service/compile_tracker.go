package service

import (
	"strings"
	"sync"
	"time"
)

var (
	compileMu       sync.Mutex
	activeCompiles  = make(map[string]chan struct{})
)

// MarkNodeCompileStart signale qu'une compilation démarre pour nodeName et
// retourne une fonction à appeler à la fin pour libérer les éventuelles attentes
// de WaitForNodeCompile. Clé insensible à la casse et sans espaces.
func MarkNodeCompileStart(nodeName string) func() {
	key := strings.ToLower(strings.TrimSpace(nodeName))
	ch := make(chan struct{})
	compileMu.Lock()
	activeCompiles[key] = ch
	compileMu.Unlock()
	return func() {
		compileMu.Lock()
		if c, ok := activeCompiles[key]; ok && c == ch {
			close(ch)
			delete(activeCompiles, key)
		}
		compileMu.Unlock()
	}
}

// WaitForNodeCompile bloque jusqu'à la fin de la compilation en cours pour
// nodeName (ou jusqu'à timeout). Retourne immédiatement s'il n'y a pas de
// compilation en cours pour ce nœud, ou si nodeName est vide.
func WaitForNodeCompile(nodeName string, timeout time.Duration) {
	key := strings.ToLower(strings.TrimSpace(nodeName))
	if key == "" {
		return
	}
	compileMu.Lock()
	ch, ok := activeCompiles[key]
	compileMu.Unlock()
	if !ok {
		return
	}
	select {
	case <-ch:
	case <-time.After(timeout):
	}
}
