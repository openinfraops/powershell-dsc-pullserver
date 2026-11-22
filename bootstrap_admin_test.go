package main

import (
	"strings"
	"testing"
)

func TestRandomPasswordLength(t *testing.T) {
	pw, err := randomPassword(bootstrapAdminPasswordLen)
	if err != nil {
		t.Fatalf("randomPassword returned error: %v", err)
	}
	if len(pw) != bootstrapAdminPasswordLen {
		t.Fatalf("expected password length %d, got %d", bootstrapAdminPasswordLen, len(pw))
	}
}

func TestRandomPasswordCharset(t *testing.T) {
	pw, err := randomPassword(bootstrapAdminPasswordLen)
	if err != nil {
		t.Fatalf("randomPassword returned error: %v", err)
	}
	for _, c := range pw {
		if !strings.ContainsRune(bootstrapAdminAlphabet, c) {
			t.Fatalf("password contains character %q outside the allowed alphabet", c)
		}
	}
}

func TestRandomPasswordUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		pw, err := randomPassword(bootstrapAdminPasswordLen)
		if err != nil {
			t.Fatalf("randomPassword returned error: %v", err)
		}
		if seen[pw] {
			t.Fatalf("duplicate password generated: %q", pw)
		}
		seen[pw] = true
	}
}

func TestRandomPasswordRejectsNonPositiveLength(t *testing.T) {
	if _, err := randomPassword(0); err == nil {
		t.Fatal("expected error for length 0")
	}
	if _, err := randomPassword(-1); err == nil {
		t.Fatal("expected error for negative length")
	}
}
