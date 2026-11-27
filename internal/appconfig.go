package internal

import (
	"encoding/json"
	"os"
	"path/filepath"

	"go-dsc-pull/internal/schema"
	"go-dsc-pull/utils"
)

// configFileOverride, when set, overrides the location of the configuration
// file. It is set by the --config flag (see main.go) and may also come from
// the DSC_CONFIG environment variable. When empty, the default file
// ("config.json") is resolved next to the executable.
var configFileOverride string

// SetConfigFile records a configuration file override from the --config flag.
func SetConfigFile(path string) {
	if path != "" {
		configFileOverride = path
	}
}

// ConfigFilePath returns the effective configuration file path.
//
// Precedence, highest first:
//  1. the --config override registered via SetConfigFile,
//  2. the DSC_CONFIG environment variable,
//  3. the supplied defaultName (normally "config.json").
//
// Absolute paths are used as-is. Relative paths are resolved next to the
// executable, so a Windows service finds its configuration regardless of the
// working directory (the SCM starts the process in system32).
func ConfigFilePath(defaultName string) (string, error) {
	path := defaultName
	if configFileOverride != "" {
		path = configFileOverride
	} else if env := os.Getenv("DSC_CONFIG"); env != "" {
		path = env
	}
	if path == "" {
		path = "config.json"
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	exePath, err := utils.ExePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exePath), path), nil
}

// GetSAMLUserMapping lit le mapping user_mapping depuis la config
func GetSAMLUserMapping() (map[string]string, error) {
	var mapping map[string]string
	// On lit le champ brut du JSON
	configPath, err := ConfigFilePath("config.json")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var raw map[string]interface{}
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	samlRaw, ok := raw["saml"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	userMappingRaw, ok := samlRaw["user_mapping"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	mapping = make(map[string]string)
	for k, v := range userMappingRaw {
		if s, ok := v.(string); ok {
			mapping[k] = s
		}
	}
	return mapping, nil
}

func LoadAppConfig(path string) (*schema.AppConfig, error) {
	absPath, err := ConfigFilePath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cfg schema.AppConfig
	dec := json.NewDecoder(f)
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
