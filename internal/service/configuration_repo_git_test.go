package service

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestSyncGitRepositoryClonesAndFastForwards(t *testing.T) {
	root := t.TempDir()
	remotePath := filepath.Join(root, "remote")
	remote, err := git.PlainInit(remotePath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remotePath, "config.psd1"), []byte("version one"), 0600); err != nil {
		t.Fatal(err)
	}
	remoteWorktree, err := remote.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remoteWorktree.Add("config.psd1"); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteWorktree.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.invalid"}}); err != nil {
		t.Fatal(err)
	}

	checkoutPath := filepath.Join(root, "checkout")
	if err := SyncGitRepository(checkoutPath, remotePath, "master"); err != nil {
		t.Fatalf("initial clone: %v", err)
	}
	checkout, err := git.PlainOpen(checkoutPath)
	if err != nil {
		t.Fatal(err)
	}
	before, err := checkout.Head()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(remotePath, "config.psd1"), []byte("version two"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteWorktree.Add("config.psd1"); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteWorktree.Commit("update", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.invalid"}}); err != nil {
		t.Fatal(err)
	}

	if err := SyncGitRepository(checkoutPath, remotePath, "master"); err != nil {
		t.Fatalf("fast-forward: %v", err)
	}
	after, err := checkout.Head()
	if err != nil {
		t.Fatal(err)
	}
	if after.Hash() == before.Hash() {
		t.Fatal("checkout did not advance to the updated remote commit")
	}
	content, err := os.ReadFile(filepath.Join(checkoutPath, "config.psd1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "version two" {
		t.Fatalf("checkout content = %q, want %q", content, "version two")
	}
}

func TestSyncGitRepositoryRejectsNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	checkoutPath := filepath.Join(root, "not-a-repository")
	if err := os.MkdirAll(checkoutPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkoutPath, "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SyncGitRepository(checkoutPath, filepath.Join(root, "missing.git"), "main"); err == nil {
		t.Fatal("expected non-empty directory to be rejected")
	}
	if _, err := os.Stat(filepath.Join(checkoutPath, "keep.txt")); err != nil {
		t.Fatalf("existing directory content was changed: %v", err)
	}
}
