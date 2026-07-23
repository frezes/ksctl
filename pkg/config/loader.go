package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kubesphere/ksctl/internal/securefile"
	"sigs.k8s.io/yaml"
)

func DefaultPath() string {
	if path := os.Getenv("KSCTL_CONFIG"); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".ksctl/config.yaml"
	}
	return filepath.Join(home, ".ksctl", "config.yaml")
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}

	cfg := New()
	if len(data) == 0 {
		return cfg, nil
	}
	if err := yaml.UnmarshalStrict(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := defaultAndValidate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func defaultAndValidate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = ConfigAPIVersion
	}
	if cfg.APIVersion != ConfigAPIVersion {
		return fmt.Errorf("unsupported config apiVersion %q, want %q", cfg.APIVersion, ConfigAPIVersion)
	}
	if cfg.Kind == "" {
		cfg.Kind = ConfigKind
	}
	if cfg.Kind != ConfigKind {
		return fmt.Errorf("unsupported config kind %q, want %q", cfg.Kind, ConfigKind)
	}
	if cfg.Fleets == nil {
		cfg.Fleets = map[string]Fleet{}
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]Context{}
	}
	return nil
}

func Save(path string, cfg *Config) error {
	if err := defaultAndValidate(cfg); err != nil {
		return err
	}
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	return securefile.Write(path, data)
}
