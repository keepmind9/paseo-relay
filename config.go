package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TLSConfig holds TLS certificate settings.
type TLSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
}

// Config holds all relay server configuration.
type Config struct {
	Listen   string    `yaml:"listen"`
	LogLevel string    `yaml:"log_level"`
	TLS      TLSConfig `yaml:"tls"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Listen:   "0.0.0.0:8080",
		LogLevel: "info",
		TLS: TLSConfig{
			Enabled: false,
		},
	}
}

// LoadConfigFromFile reads a YAML config file.
func LoadConfigFromFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}
	return cfg, nil
}

// ApplyEnvOverrides sets config fields from environment variables.
func ApplyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("TLS_CERT"); v != "" {
		cfg.TLS.Cert = v
	}
	if v := os.Getenv("TLS_KEY"); v != "" {
		cfg.TLS.Key = v
	}
	if cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		cfg.TLS.Enabled = true
	}
}

// ResolveConfig merges config from file, env, and flags.
// Priority: flags > env > file > defaults.
func ResolveConfig(listen, tlsCert, tlsKey, logLevel, configPath string) Config {
	cfg := DefaultConfig()

	if configPath != "" {
		if fileCfg, err := LoadConfigFromFile(configPath); err == nil {
			cfg = fileCfg
		}
	}

	ApplyEnvOverrides(&cfg)

	if listen != "" {
		cfg.Listen = listen
	}
	if tlsCert != "" {
		cfg.TLS.Cert = tlsCert
	}
	if tlsKey != "" {
		cfg.TLS.Key = tlsKey
	}
	if cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		cfg.TLS.Enabled = true
	}
	if logLevel != "" {
		cfg.LogLevel = logLevel
	}

	return cfg
}
