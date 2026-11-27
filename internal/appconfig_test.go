package internal

import (
	"os"
	"path/filepath"
	"testing"

	"go-dsc-pull/utils"
)

// resetConfigOverride remet l'override de config et la variable d'environnement
// DSC_CONFIG à un état vide, pour rendre les tests déterministes.
func resetConfigOverride(t *testing.T) {
	t.Helper()
	configFileOverride = ""
	os.Unsetenv("DSC_CONFIG")
}

// TestConfigFilePath_defaultResolvesToExeDir vérifie que, sans override, le
// chemin relatif est résolu à côté de l'exécutable (indépendant du CWD).
func TestConfigFilePath_defaultResolvesToExeDir(t *testing.T) {
	resetConfigOverride(t)
	got, err := ConfigFilePath("config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	exe, err := utils.ExePath()
	if err != nil {
		t.Fatalf("exe path: %v", err)
	}
	want := filepath.Join(filepath.Dir(exe), "config.json")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestConfigFilePath_absoluteOverrideWins vérifie qu'un override absolu
// (--config) est utilisé tel quel.
func TestConfigFilePath_absoluteOverrideWins(t *testing.T) {
	resetConfigOverride(t)
	override := filepath.Join(t.TempDir(), "custom.json")
	configFileOverride = override
	got, err := ConfigFilePath("config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != override {
		t.Fatalf("got %q, want %q", got, override)
	}
}

// TestConfigFilePath_relativeOverrideResolvesToExeDir vérifie qu'un override
// relatif (--config) est résolu à côté de l'exécutable.
func TestConfigFilePath_relativeOverrideResolvesToExeDir(t *testing.T) {
	resetConfigOverride(t)
	configFileOverride = "custom.json"
	got, err := ConfigFilePath("config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	exe, err := utils.ExePath()
	if err != nil {
		t.Fatalf("exe path: %v", err)
	}
	want := filepath.Join(filepath.Dir(exe), "custom.json")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestConfigFilePath_envOverride vérifie que la variable d'environnement
// DSC_CONFIG est honorée comme override.
func TestConfigFilePath_envOverride(t *testing.T) {
	resetConfigOverride(t)
	envPath := filepath.Join(t.TempDir(), "env.json")
	os.Setenv("DSC_CONFIG", envPath)
	defer os.Unsetenv("DSC_CONFIG")
	got, err := ConfigFilePath("config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != envPath {
		t.Fatalf("got %q, want %q", got, envPath)
	}
}
