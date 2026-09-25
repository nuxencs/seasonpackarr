// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// ConnectionSettings contains the local settings needed to call the running service.
type ConnectionSettings struct {
	Host string
	// Port stays unparsed so callers can apply overrides before validating it.
	Port     string
	APIToken string
	Clients  []string
}

// ReadConnectionSettings reads existing YAML and environment overrides without
// creating files, starting watchers, or validating server-only settings.
// A missing implicit config is allowed; a missing explicit config is an error.
func ReadConnectionSettings(configDir string) (ConnectionSettings, error) {
	cfg := defaultConfig("", "", false)
	if os.Getenv("SEASONPACKARR__DISABLE_CONFIG_FILE") != "true" {
		configFile := filepath.Join(configDir, "config.yaml")
		var err error
		if configDir == "" {
			configFile, err = findConfigFile()
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return ConnectionSettings{}, fmt.Errorf("find config: %w", err)
		}
		if configFile != "" {
			k := koanf.New(".")
			if err := k.Load(file.Provider(configFile), yaml.Parser()); err != nil {
				return ConnectionSettings{}, fmt.Errorf("could not read config %q; check that the file exists and contains valid YAML", configFile)
			}
			// Decode only connection fields so remote use does not require valid import policies.
			settings := struct {
				Host     string
				Port     int
				APIToken string
				Clients  map[string]any
			}{Host: cfg.Host, Port: cfg.Port}
			if err := k.Unmarshal("", &settings); err != nil {
				return ConnectionSettings{}, fmt.Errorf("invalid connection settings in %q; check host, port, apiToken, and clients", configFile)
			}
			cfg.Host, cfg.Port, cfg.APIToken = settings.Host, settings.Port, settings.APIToken
			for name := range settings.Clients {
				cfg.Clients[name] = &domain.Client{}
			}
		}
	}
	applyEnvironment(&cfg)
	return ConnectionSettings{
		Host: cfg.Host, Port: cmp.Or(os.Getenv("SEASONPACKARR__PORT"), strconv.Itoa(cfg.Port)), APIToken: cfg.APIToken,
		Clients: slices.Sorted(maps.Keys(cfg.Clients)),
	}, nil
}
