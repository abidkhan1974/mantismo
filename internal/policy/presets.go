// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package policy

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed presets/*.rego
var embeddedPresets embed.FS

// ValidPresets lists the built-in preset names.
var ValidPresets = []string{"balanced", "paranoid", "permissive"}

// PresetSource returns the Rego source for the named preset.
// Returns an error if the name is not a valid preset.
func PresetSource(name string) (string, error) {
	data, err := embeddedPresets.ReadFile("presets/" + name + ".rego")
	if err != nil {
		return "", fmt.Errorf("unknown preset %q: valid presets are balanced, paranoid, permissive", name)
	}
	return string(data), nil
}

// NewEngineFromPreset creates a policy Engine using an embedded preset.
func NewEngineFromPreset(preset string) (*Engine, error) {
	src, err := PresetSource(preset)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "mantismo-policy-*")
	if err != nil {
		return nil, fmt.Errorf("preset temp dir: %w", err)
	}
	path := filepath.Join(dir, preset+".rego")
	if err := os.WriteFile(path, []byte(src), 0600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write preset: %w", err)
	}
	eng, err := NewEngine(dir)
	_ = os.RemoveAll(dir)
	if err != nil {
		return nil, err
	}
	return eng, nil
}

// WritePresetToDir writes the named preset .rego file into destDir.
func WritePresetToDir(preset, destDir string) (string, error) {
	src, err := PresetSource(preset)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}
	dst := filepath.Join(destDir, "policy.rego")
	if err := os.WriteFile(dst, []byte(src), 0600); err != nil {
		return "", fmt.Errorf("write policy: %w", err)
	}
	return dst, nil
}
