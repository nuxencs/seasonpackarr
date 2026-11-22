// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/envname"

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
			require.NoError(t, applyEnvironment(&cfg, newEnvironment([]string{tt.key + "=true"})))

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

// TestEnvironment_MatchesNamesAsWindowsDoes runs the Windows rule on each system: a variable that the shell keeps with
// another case is the same variable, as for os.LookupEnv. The messages of a set or unknown variable keep its case.
func TestEnvironment_MatchesNamesAsWindowsDoes(t *testing.T) {
	env := windowsEnvironment(
		"Home=/home/user",
		"Seasonpackarr__Host=127.0.0.1",
		"seasonpackarr__port=8080",
		"Seasonpackarr__Clients_Mixed_Host=qbit",
		"Seasonpackarr__Clients_Mixed_Import_Save_Path=/data",
		"Seasonpackarr__Log_Levl=INFO",
		"Seasonpackarr__Url=http://seasonpackarr:42069",
		"Seasonpackarr__Disable_Config_File=true",
	)

	assert.Equal(t, "/home/user", env.get("HOME"))
	disabled, err := configFileDisabled(env)
	require.NoError(t, err)
	assert.True(t, disabled)

	cfg := domain.Config{Clients: map[string]*domain.Client{}}
	require.NoError(t, applyEnvironment(&cfg, env))
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
	require.Contains(t, cfg.Clients, "mixed")
	assert.Equal(t, "qbit", cfg.Clients["mixed"].Host)
	assert.Equal(t, "/data", cfg.Clients["mixed"].Import.SavePath)

	assert.Equal(t, []string{"Seasonpackarr__Log_Levl"}, env.unknownVariables())

	assert.ErrorContains(t, applyEnvironment(&domain.Config{}, windowsEnvironment("Seasonpackarr__Port=fast")), "Seasonpackarr__Port")

	for _, removed := range []string{
		"Seasonpackarr__Parse_Torrent_File=false",
		"Seasonpackarr__Metadata_Tvdb_Api_Key=key",
		"Seasonpackarr__Clients_Mixed_PreImportPath=/data",
	} {
		err := validateDeprecatedConfigInputs(nil, windowsEnvironment(removed))
		assert.ErrorContains(t, err, "deprecated environment variable detected", removed)
	}
}

// windowsEnvironment returns entries as an environment with the Windows name rule.
func windowsEnvironment(entries ...string) environment {
	return environment{entries: entries, rule: envname.IgnoreCase}
}

// TestLoad_MatchesEnvironmentNamesAsTheSystemDoes: the load uses the rule of os.LookupEnv on the current system.
func TestLoad_MatchesEnvironmentNamesAsTheSystemDoes(t *testing.T) {
	cfg, err := Load(Input{Environ: []string{"SEASONPACKARR__DISABLE_CONFIG_FILE=true", "Seasonpackarr__Host=127.0.0.1"}})
	require.NoError(t, err)

	want := configDefaults.Host
	if runtime.GOOS == "windows" {
		want = "127.0.0.1"
	}
	assert.Equal(t, want, cfg.Host)
}
