// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package config provides TOML-based configuration loading with environment variable overrides.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the top-level Mantismo configuration.
type Config struct {
	DataDir   string          `toml:"data_dir"`
	LogLevel  string          `toml:"log_level"` // debug, info, warn, error
	API       APIConfig       `toml:"api"`
	Proxy     ProxyConfig     `toml:"proxy"`
	Policy    PolicyConfig    `toml:"policy"`
	Vault     VaultConfig     `toml:"vault"`
	Dashboard DashboardConfig `toml:"dashboard"`
}

// APIConfig configures the internal API server.
type APIConfig struct {
	Port     int    `toml:"port"`      // default 7777
	BindAddr string `toml:"bind_addr"` // default 127.0.0.1
}

// ProxyConfig holds proxy transport settings (reserved for future transport options).
type ProxyConfig struct{}

// PolicyConfig controls the policy engine.
type PolicyConfig struct {
	Preset    string `toml:"preset"`     // paranoid, balanced, permissive
	PolicyDir string `toml:"policy_dir"` // path to custom .rego files
}

// VaultConfig controls the encrypted vault.
type VaultConfig struct {
	Enabled bool   `toml:"enabled"`
	DBPath  string `toml:"db_path"`
}

// DashboardConfig controls the web dashboard.
type DashboardConfig struct {
	Enabled  bool   `toml:"enabled"`
	AutoOpen bool   `toml:"auto_open"`
	Port     int    `toml:"port"`      // default 7777
	BindAddr string `toml:"bind_addr"` // default 127.0.0.1
}

// defaults returns a Config populated with sensible defaults.
func defaults() *Config {
	dataDir := defaultDataDir()
	return &Config{
		DataDir:  dataDir,
		LogLevel: "info",
		API: APIConfig{
			Port:     7777,
			BindAddr: "127.0.0.1",
		},
		Proxy: ProxyConfig{},
		Policy: PolicyConfig{
			Preset:    "balanced",
			PolicyDir: filepath.Join(dataDir, "policies"),
		},
		Vault: VaultConfig{
			Enabled: false,
			DBPath:  filepath.Join(dataDir, "vault.db"),
		},
		Dashboard: DashboardConfig{
			Enabled:  true,
			AutoOpen: false,
			Port:     7777,
			BindAddr: "127.0.0.1",
		},
	}
}

// defaultDataDir returns the default data directory respecting XDG on Linux.
func defaultDataDir() string {
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "mantismo")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mantismo"
	}
	return filepath.Join(home, ".mantismo")
}

// LoadConfig loads configuration from the given path (or default path if empty),
// merges environment variable overrides, and returns a fully populated Config.
func LoadConfig(path string) (*Config, error) {
	cfg := defaults()

	// Determine config file path
	configPath := path
	if configPath == "" {
		configPath = filepath.Join(cfg.DataDir, "config.toml")
	}

	// Attempt to read and parse the config file (not a fatal error if missing)
	if data, err := os.ReadFile(configPath); err == nil {
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	return cfg, nil
}

// applyEnvOverrides merges MANTISMO_* environment variables into cfg.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("MANTISMO_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("MANTISMO_LOG_LEVEL"); v != "" {
		cfg.LogLevel = strings.ToLower(v)
	}
	if v := os.Getenv("MANTISMO_POLICY_PRESET"); v != "" {
		cfg.Policy.Preset = v
	}
	if v := os.Getenv("MANTISMO_API_PORT"); v != "" {
		if port := parsePort(v); port > 0 {
			cfg.API.Port = port
		}
	}
	if v := os.Getenv("MANTISMO_VAULT_ENABLED"); v != "" {
		cfg.Vault.Enabled = strings.ToLower(v) == "true" || v == "1"
	}
}

// parsePort converts a string to an int port, returning 0 on failure.
func parsePort(s string) int {
	var port int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		port = port*10 + int(c-'0')
	}
	if port < 1 || port > 65535 {
		return 0
	}
	return port
}
