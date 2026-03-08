// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abidkhan1974/mantismo/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("/nonexistent/path/config.toml")
	require.NoError(t, err)

	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, 7777, cfg.API.Port)
	assert.Equal(t, "127.0.0.1", cfg.API.BindAddr)
	assert.Equal(t, "balanced", cfg.Policy.Preset)
	assert.False(t, cfg.Vault.Enabled)
}

func TestLoadConfigFromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.toml")

	content := `
log_level = "debug"

[api]
port = 8888
bind_addr = "127.0.0.1"

[policy]
preset = "paranoid"

[vault]
enabled = true
`
	require.NoError(t, os.WriteFile(cfgFile, []byte(content), 0600))

	cfg, err := config.LoadConfig(cfgFile)
	require.NoError(t, err)

	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, 8888, cfg.API.Port)
	assert.Equal(t, "paranoid", cfg.Policy.Preset)
	assert.True(t, cfg.Vault.Enabled)
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("MANTISMO_LOG_LEVEL", "warn")
	t.Setenv("MANTISMO_POLICY_PRESET", "permissive")

	cfg, err := config.LoadConfig("")
	require.NoError(t, err)

	assert.Equal(t, "warn", cfg.LogLevel)
	assert.Equal(t, "permissive", cfg.Policy.Preset)
}

func TestDataDirDefault(t *testing.T) {
	t.Parallel()
	cfg, err := config.LoadConfig("")
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, ".mantismo"), cfg.DataDir)
}

func TestDataDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANTISMO_DATA_DIR", dir)

	cfg, err := config.LoadConfig("")
	require.NoError(t, err)

	assert.Equal(t, dir, cfg.DataDir)
}
