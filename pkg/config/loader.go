package config

import (
	"errors"
	"os"
	"path/filepath"

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
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = ConfigAPIVersion
	}
	if cfg.Kind == "" {
		cfg.Kind = ConfigKind
	}
	if cfg.Fleets == nil {
		cfg.Fleets = map[string]Fleet{}
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]Context{}
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	if cfg.APIVersion == "" {
		cfg.APIVersion = ConfigAPIVersion
	}
	if cfg.Kind == "" {
		cfg.Kind = ConfigKind
	}
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
