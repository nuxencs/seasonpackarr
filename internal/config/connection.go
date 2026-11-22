// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strconv"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// ConnectionSettings contains the local settings needed to call the running service.
type ConnectionSettings struct {
	Host string
	// Port stays unparsed so callers can apply overrides before validating it.
	Port     string
	APIToken string
	Clients  []string
}

// connectionEnvSettings omits the server-only variables, so a bad one does not stop the CLI. PORT is read as text below, so flags can override it.
var connectionEnvSettings = map[string]func(*domain.Config, string) error{
	"HOST":      envSettings["HOST"],
	"API_TOKEN": envSettings["API_TOKEN"],
}

// connectionClientEnvSettings creates each named client but ignores the value, because the CLI reads only the client names.
var connectionClientEnvSettings = func() map[string]func(*domain.Client, string) error {
	settings := make(map[string]func(*domain.Client, string) error, len(clientEnvSettings))
	for setting := range clientEnvSettings {
		settings[setting] = func(*domain.Client, string) error { return nil }
	}
	return settings
}()

// ReadConnectionSettings reads existing YAML and environment overrides without
// creating files, starting watchers, or validating server-only settings.
// A missing implicit config is allowed; a missing explicit config is an error.
// It ignores in.Version and rejects in.CreateFile.
func ReadConnectionSettings(in Input) (ConnectionSettings, error) {
	if in.CreateFile {
		return ConnectionSettings{}, errCreateFileOnlyNew
	}
	cfg := defaultConfig("", "")
	src, err := newSource(in)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ConnectionSettings{}, fmt.Errorf("find config: %w", err)
	}
	if src.configFile != "" {
		k := koanf.New(".")
		if err := k.Load(file.Provider(src.configFile), yaml.Parser()); err != nil {
			return ConnectionSettings{}, fmt.Errorf("could not read config %q; check that the file exists and contains valid YAML", src.configFile)
		}
		// Decode only connection fields so remote use does not require valid import policies.
		settings := struct {
			Host     string
			Port     int
			APIToken string
			Clients  map[string]any
		}{Host: cfg.Host, Port: cfg.Port}
		if err := k.Unmarshal("", &settings); err != nil {
			return ConnectionSettings{}, fmt.Errorf("invalid connection settings in %q; check host, port, apiToken, and clients", src.configFile)
		}
		cfg.Host, cfg.Port, cfg.APIToken = settings.Host, settings.Port, settings.APIToken
		for name := range settings.Clients {
			cfg.Clients[name] = &domain.Client{}
		}
	}
	if err := applyEnvironmentSettings(&cfg, src.env, connectionEnvSettings, connectionClientEnvSettings); err != nil {
		return ConnectionSettings{}, err
	}
	return ConnectionSettings{
		Host: cfg.Host, Port: cmp.Or(src.env.get(envPrefix+"PORT"), strconv.Itoa(cfg.Port)), APIToken: cfg.APIToken,
		Clients: slices.Sorted(maps.Keys(cfg.Clients)),
	}, nil
}
