// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"reflect"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnvKeysOf_PanicsOnBadDeclaration shows that a declaration mistake on a config field stops every test binary of the package at its start.
func TestEnvKeysOf_PanicsOnBadDeclaration(t *testing.T) {
	tests := map[string]reflect.Type{
		"default that does not parse": reflect.TypeFor[struct {
			Port int `yaml:"port" env:"PORT" default:"fast"`
		}](),
		"list default without entries": reflect.TypeFor[struct {
			Tags []string `yaml:"tags" env:"TAGS" default:","`
		}](),
		"kind without parser": reflect.TypeFor[struct {
			Size uint `yaml:"size" env:"SIZE"`
		}](),
		"missing env tag": reflect.TypeFor[struct {
			Host string `yaml:"host"`
		}](),
		"duplicate env name": reflect.TypeFor[struct {
			Host  string `yaml:"host" env:"HOST"`
			Other string `yaml:"other" env:"HOST"`
		}](),
		"unknown separator": reflect.TypeFor[struct {
			Tags []string `yaml:"tags" env:"TAGS" sep:";"`
		}](),
		"separator on a single value": reflect.TypeFor[struct {
			Host string `yaml:"host" env:"HOST" sep:"whitespace"`
		}](),
		"default on a field that no variable sets": reflect.TypeFor[struct {
			Version string `yaml:"-" env:"-" default:"1"`
		}](),
		"env tag on a group": reflect.TypeFor[struct {
			Group struct {
				Host string `yaml:"host" env:"GROUP_HOST"`
			} `yaml:"group" env:"GROUP"`
		}](),
	}
	for name, declaration := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Panics(t, func() { envKeysOf(declaration) })
		})
	}
}

func TestApplyEnvironment_SetsFuzzyMatching(t *testing.T) {
	tests := []struct {
		key  string
		want domain.FuzzyMatching
	}{
		{key: "SEASONPACKARR__FUZZY_MATCHING_SKIP_REPACK_COMPARE", want: domain.FuzzyMatching{SkipRepackCompare: true}},
		{key: "SEASONPACKARR__FUZZY_MATCHING_SIMPLIFY_HDR_COMPARE", want: domain.FuzzyMatching{SimplifyHdrCompare: true}},
		{key: "SEASONPACKARR__FUZZY_MATCHING_SIMPLIFY_WEB_COMPARE", want: domain.FuzzyMatching{SimplifyWebCompare: true}},
		{key: "SEASONPACKARR__FUZZY_MATCHING_SKIP_YEAR_COMPARE", want: domain.FuzzyMatching{SkipYearCompare: true}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			cfg := domain.Config{Clients: map[string]*domain.Client{}}
			require.NoError(t, applyEnvironment(&cfg, environment{tt.key + "=true"}))

			require.Equal(t, tt.want, cfg.FuzzyMatching)
		})
	}
}

func TestUnknownEnvironmentVariables(t *testing.T) {
	environ := []string{
		"HOME=/home/user",
		"SEASONPACKARR_TEST_E2E_API_TOKEN=token",
		"SEASONPACKARR__LOG_LEVEL=INFO",
		"SEASONPACKARR__LOG_LEVL=INFO",
		"SEASONPACKARR__URL=http://seasonpackarr:42069",
		"SEASONPACKARR__CLIENT=default",
		"SEASONPACKARR__AUTOBRR_URL=http://autobrr:7474",
		"SEASONPACKARR__AUTOBRR_API_KEY=key",
		"SEASONPACKARR__DISABLE_CONFIG_FILE=true",
		"SEASONPACKARR__CLIENTS_DEFAULT_HOST=qbit",
		"SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_SAVE_PATH=/data",
		"SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_SAVEPATH=/data",
		"SEASONPACKARR__CLIENTS_DEFAULT=",
		"SEASONPACKARR__SEARCH_API_KEY=",
		"SEASONPACKARR__SMART_MODE_TRESHOLD=0.5",
		// The removed variables stop startup with a migration error, so they are not ignored.
		"SEASONPACKARR__PARSE_TORRENT_FILE=false",
		"SEASONPACKARR__METADATA_TVDB_API_KEY=key",
		"SEASONPACKARR__METADATA_TVDB_PIN=pin",
		"SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH=",
	}
	require.Equal(t, []string{
		"SEASONPACKARR__CLIENTS_DEFAULT",
		"SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_SAVEPATH",
		"SEASONPACKARR__LOG_LEVL",
		"SEASONPACKARR__SMART_MODE_TRESHOLD",
	}, UnknownEnvironmentVariables(environ))
}
