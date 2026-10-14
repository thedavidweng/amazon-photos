package config

import (
	"os"
	"path/filepath"
	"strings"
)

func DefaultConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "aphotos", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".config", "aphotos", "config.yaml")
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".config", "aphotos", "config.yaml")
	}
	return filepath.Join(cfgDir, "aphotos", "config.yaml")
}

func ResolveConfigPath(explicit string) string {
	if s := strings.TrimSpace(explicit); s != "" {
		return s
	}
	if envPath := strings.TrimSpace(os.Getenv("APHOTOS_CONFIG")); envPath != "" {
		return envPath
	}
	return DefaultConfigPath()
}

func ParseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
