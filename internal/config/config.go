package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type Config struct {
	CurrentProfile string              `yaml:"current_profile" json:"current_profile"`
	Profiles       map[string]*Profile `yaml:"profiles" json:"profiles"`
}

type Profile struct {
	Region       string            `yaml:"region,omitempty" json:"region,omitempty"`
	Cookies      map[string]string `yaml:"cookies" json:"cookies"`
	DriveURL     string            `yaml:"drive_url,omitempty" json:"drive_url,omitempty"`
	UploadURL    string            `yaml:"upload_url,omitempty" json:"upload_url,omitempty"`
	ThumbnailURL string            `yaml:"thumbnail_url,omitempty" json:"thumbnail_url,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &aphotos.ConfigError{
			Field:   "config_file",
			Message: fmt.Sprintf("read config %s: %v", path, err),
			Cause:   err,
		}
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, &aphotos.ConfigError{
			Field:   "config_file",
			Message: fmt.Sprintf("parse config %s: %v", path, err),
			Cause:   err,
		}
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}
	return &cfg, nil
}

func LoadOrCreate(path string) (*Config, error) {
	cfg, err := Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{
				CurrentProfile: "default",
				Profiles:       map[string]*Profile{},
			}, nil
		}
		return nil, err
	}
	if cfg.CurrentProfile == "" {
		cfg.CurrentProfile = "default"
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &aphotos.ConfigError{
			Field:   "config_dir",
			Message: fmt.Sprintf("create config dir %s: %v", dir, err),
			Cause:   err,
		}
	}
	_ = enforceConfigDirPermissions(dir)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return &aphotos.ConfigError{
			Field:   "config_file",
			Message: fmt.Sprintf("marshal config: %v", err),
			Cause:   err,
		}
	}

	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return &aphotos.ConfigError{
			Field:   "config_file",
			Message: fmt.Sprintf("create temp config file: %v", err),
			Cause:   err,
		}
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return &aphotos.ConfigError{
			Field:   "config_file",
			Message: fmt.Sprintf("chmod temp config file: %v", err),
			Cause:   err,
		}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return &aphotos.ConfigError{
			Field:   "config_file",
			Message: fmt.Sprintf("write temp config file: %v", err),
			Cause:   err,
		}
	}
	if err := tmp.Close(); err != nil {
		return &aphotos.ConfigError{
			Field:   "config_file",
			Message: fmt.Sprintf("close temp config file: %v", err),
			Cause:   err,
		}
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return &aphotos.ConfigError{
			Field:   "config_file",
			Message: fmt.Sprintf("rename temp config file: %v", err),
			Cause:   err,
		}
	}
	_ = enforceConfigFilePermissions(path)
	return nil
}

func (c *Config) GetProfile(name string) (*Profile, error) {
	if c == nil || len(c.Profiles) == 0 {
		return nil, &aphotos.ConfigError{
			Field:   "profile",
			Message: fmt.Sprintf("profile %q not found", name),
		}
	}
	target := strings.TrimSpace(name)
	if target == "" {
		target = c.CurrentProfile
	}
	if target == "" {
		target = "default"
	}
	p, ok := c.Profiles[target]
	if !ok || p == nil {
		return nil, &aphotos.ConfigError{
			Field:   "profile",
			Message: fmt.Sprintf("profile %q not found", target),
		}
	}
	return p, nil
}

func (c *Config) SetProfile(name string, p *Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	target := strings.TrimSpace(name)
	if target == "" {
		target = "default"
	}
	c.Profiles[target] = p
	if c.CurrentProfile == "" {
		c.CurrentProfile = target
	}
}

func (c *Config) DeleteProfile(name string) bool {
	if c == nil || len(c.Profiles) == 0 {
		return false
	}
	target := strings.TrimSpace(name)
	if target == "" {
		target = c.CurrentProfile
	}
	if target == "" {
		target = "default"
	}
	if _, ok := c.Profiles[target]; !ok {
		return false
	}
	delete(c.Profiles, target)
	return true
}
