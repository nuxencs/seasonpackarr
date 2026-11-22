// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Input holds each input of a config load except the config file contents. Load, New, and ReadConnectionSettings take it.
type Input struct {
	// ConfigDir is the directory of config.yaml. When it is empty, the load searches the default locations.
	ConfigDir string
	// Environ is the environment in os.Environ form ("KEY=value"). The config package reads no other environment.
	Environ []string
	// Version goes into the snapshot.
	Version string
	// CreateFile makes New create a missing config.yaml in ConfigDir. Only start sets it, and only with a ConfigDir.
	// Load and ReadConnectionSettings reject it, so that no other call creates a file.
	CreateFile bool
}

var errCreateFileOnlyNew = errors.New("config: only New creates a config file")

// environment is the environment of an Input.
type environment []string

// lookup returns the value of the first entry for key, as os.LookupEnv does.
func (e environment) lookup(key string) (string, bool) {
	for _, entry := range e {
		if name, value, ok := strings.Cut(entry, "="); ok && name == key {
			return value, true
		}
	}
	return "", false
}

func (e environment) get(key string) string {
	value, _ := e.lookup(key)
	return value
}

// source is where a load reads its settings: the config file and the environment.
// The full load and the read of the connection settings share it, so that both resolve the same file.
type source struct {
	// configFile is empty when the config file is disabled, or when newSource returns fs.ErrNotExist.
	configFile string
	disabled   bool
	env        environment
}

// newSource resolves the config file of in. It returns an error that wraps fs.ErrNotExist when
// no default location has a config file. The returned source is then valid without a config file.
func newSource(in Input) (source, error) {
	env := environment(in.Environ)
	src := source{
		// Only this check decides whether a load reads the config file. Nothing reads domain.Config.DisableConfigFile.
		disabled: env.get(envPrefix+"DISABLE_CONFIG_FILE") == "true",
		env:      env,
	}
	switch {
	case src.disabled:
	case in.ConfigDir != "":
		src.configFile = filepath.Join(filepath.Clean(in.ConfigDir), "config.yaml")
	default:
		configFile, err := findConfigFile(env)
		if err != nil {
			return src, err
		}
		src.configFile = configFile
	}
	return src, nil
}

func findConfigFile(env environment) (string, error) {
	locations := []string{
		"./config.yaml",
		"$HOME/.config/seasonpackarr/config.yaml",
		"$HOME/.seasonpackarr/config.yaml",
	}
	for _, location := range locations {
		// os.Expand with the passed environment does what os.ExpandEnv does with the process environment.
		configFile := os.Expand(location, env.get)
		info, err := os.Stat(configFile)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if err == nil && !info.IsDir() {
			return configFile, nil
		}
	}

	return "", fmt.Errorf("could not find config file: %w", fs.ErrNotExist)
}
