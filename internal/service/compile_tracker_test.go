package service

import (
	"sync"
	"testing"
	"time"
)

func TestWaitForNodeCompileNoCompileReturnsImmediately(t *testing.T) {
	start := time.Now()
	WaitForNodeCompile("nonexistent-node", 10*time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("expected immediate return, took %s", elapsed)
	}
}

func TestWaitForNodeCompileBlocksUntilFinish(t *testing.T) {
	nodeName := "BlockingNode"
	finish := MarkNodeCompileStart(nodeName)

	released := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		WaitForNodeCompile(nodeName, 5*time.Second)
		close(released)
	}()

	select {
	case <-released:
		t.Fatal("WaitForNodeCompile returned before the compile finished")
	case <-time.After(200 * time.Millisecond):
		// still waiting, good
	}

	finish()
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForNodeCompile did not return after the compile finished")
	}
	wg.Wait()
}

func TestWaitForNodeCompileTimesOut(t *testing.T) {
	nodeName := "NeverFinishesNode"
	_ = MarkNodeCompileStart(nodeName)

	start := time.Now()
	WaitForNodeCompile(nodeName, 200*time.Millisecond)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("expected WaitForNodeCompile to return after the timeout, took %s", elapsed)
	}
}

func TestMarkNodeCompileStartIsCaseInsensitive(t *testing.T) {
	finish := MarkNodeCompileStart("MiXeDNoDe")
	done := make(chan struct{})
	go func() {
		WaitForNodeCompile("mixednode", 2*time.Second)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("case-insensitive wait returned before finish")
	default:
	}
	finish()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("case-insensitive wait did not return")
	}
}
