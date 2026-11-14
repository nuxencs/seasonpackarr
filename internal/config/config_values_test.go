// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadWithFile runs the public load against a config.yaml with contents.
func loadWithFile(t *testing.T, contents string) (domain.Config, error) {
	t.Helper()
	return Load(filepath.Dir(writeTestConfig(t, contents)), "test")
}

const qbitClientConfig = `clients:
  default:
    type: qbittorrent
    import:
      category: tv-hd
`

func TestLoad_RejectsEnvironmentValueThatDoesNotParse(t *testing.T) {
	tests := map[string]struct{ variable, value string }{
		"string":     {"SEASONPACKARR__API_TOKEN", "sentinel-string-value\n"},
		"boolean":    {"SEASONPACKARR__SMART_MODE", "sentinel-bool-value"},
		"integer":    {"SEASONPACKARR__LOG_MAX_SIZE", "sentinel-int-value"},
		"float":      {"SEASONPACKARR__SMART_MODE_THRESHOLD", "sentinel-float-value"},
		"list":       {"SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_TAGS", "tv,sentinel-list\x01value"},
		"int list":   {"SEASONPACKARR__SEARCH_INDEXER_IDS", "1, sentinel-int-list-value"},
		"client int": {"SEASONPACKARR__CLIENTS_DEFAULT_PORT", "sentinel-client-port-value"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			clearConnectionEnvironment(t)
			t.Setenv(tt.variable, tt.value)

			_, err := loadWithFile(t, qbitClientConfig)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.variable)
			assert.NotContains(t, err.Error(), "sentinel")
		})
	}
}

func TestLoad_ParsesEnvironmentValueOfEachKind(t *testing.T) {
	clearConnectionEnvironment(t)
	t.Setenv("SEASONPACKARR__API_TOKEN", "token")
	t.Setenv("SEASONPACKARR__SMART_MODE", "true")
	t.Setenv("SEASONPACKARR__LOG_MAX_SIZE", "7")
	t.Setenv("SEASONPACKARR__SMART_MODE_THRESHOLD", "0.5")
	t.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_TAGS", " a , ,b ")
	t.Setenv("SEASONPACKARR__SEARCH_INDEXER_IDS", "3, 4")
	t.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_PORT", "8080")

	cfg, err := loadWithFile(t, qbitClientConfig)

	require.NoError(t, err)
	assert.Equal(t, "token", cfg.APIToken)
	assert.True(t, cfg.SmartMode)
	assert.Equal(t, 7, cfg.LogMaxSize)
	assert.Equal(t, float32(0.5), cfg.SmartModeThreshold)
	assert.Equal(t, []string{"a", "b"}, cfg.Clients["default"].Import.Tags)
	assert.Equal(t, []int{3, 4}, cfg.Search.IndexerIDs)
	assert.Equal(t, 8080, cfg.Clients["default"].Port)
}

func TestLoad_RejectsListEnvironmentWithoutEntries(t *testing.T) {
	tests := map[string][]string{
		"SEASONPACKARR__NOTIFICATIONS_NOTIFICATION_LEVEL": {" ", ",", " , "},
		// Whitespace separates the Shoutrrr URLs, so a comma is an entry there.
		"SEASONPACKARR__NOTIFICATIONS_SHOUTRRR": {" ", "\t \n"},
	}
	for variable, values := range tests {
		for _, value := range values {
			t.Run(fmt.Sprintf("%s=%q", variable, value), func(t *testing.T) {
				clearConnectionEnvironment(t)
				t.Setenv(variable, value)

				_, err := loadWithFile(t, "notifications:\n  notificationLevel: [MATCH]\n  shoutrrr: [\"ntfy://ntfy.sh/topic\"]\n")

				require.Error(t, err)
				assert.Contains(t, err.Error(), variable)
			})
		}
	}
}

func TestLoad_IgnoresEmptyEnvironmentValue(t *testing.T) {
	clearConnectionEnvironment(t)
	for _, variable := range []string{"SEASONPACKARR__PORT", "SEASONPACKARR__SMART_MODE", "SEASONPACKARR__NOTIFICATIONS_NOTIFICATION_LEVEL", "SEASONPACKARR__NOTIFICATIONS_SHOUTRRR"} {
		t.Setenv(variable, "")
	}

	cfg, err := loadWithFile(t, "port: 4242\nnotifications:\n  notificationLevel: [MATCH]\n  shoutrrr: [\"ntfy://ntfy.sh/topic\"]\n")

	require.NoError(t, err)
	assert.Equal(t, 4242, cfg.Port)
	assert.Equal(t, []string{"MATCH"}, cfg.Notifications.NotificationLevel)
	assert.Equal(t, []string{"ntfy://ntfy.sh/topic"}, cfg.Notifications.Shoutrrr)
}

func TestLoad_RangeRules(t *testing.T) {
	tests := []struct {
		key, variable, value, wantErr string
	}{
		{"port", "SEASONPACKARR__PORT", "0", "port (SEASONPACKARR__PORT) must be from 1 to 65535"},
		{"port", "SEASONPACKARR__PORT", "65536", "port (SEASONPACKARR__PORT) must be from 1 to 65535"},
		{"port", "SEASONPACKARR__PORT", "1", ""},
		{"port", "SEASONPACKARR__PORT", "65535", ""},
		{"logMaxSize", "SEASONPACKARR__LOG_MAX_SIZE", "0", "logMaxSize (SEASONPACKARR__LOG_MAX_SIZE) must be at least 1"},
		{"logMaxSize", "SEASONPACKARR__LOG_MAX_SIZE", "1", ""},
		{"logMaxBackups", "SEASONPACKARR__LOG_MAX_BACKUPS", "-1", "logMaxBackups (SEASONPACKARR__LOG_MAX_BACKUPS) must be at least 0"},
		{"logMaxBackups", "SEASONPACKARR__LOG_MAX_BACKUPS", "0", ""},
		{"smartModeThreshold", "SEASONPACKARR__SMART_MODE_THRESHOLD", "0", "smartModeThreshold (SEASONPACKARR__SMART_MODE_THRESHOLD) must be greater than 0 and at most 1"},
		{"smartModeThreshold", "SEASONPACKARR__SMART_MODE_THRESHOLD", "7", "smartModeThreshold (SEASONPACKARR__SMART_MODE_THRESHOLD) must be greater than 0 and at most 1"},
		{"smartModeThreshold", "SEASONPACKARR__SMART_MODE_THRESHOLD", "NaN", "smartModeThreshold (SEASONPACKARR__SMART_MODE_THRESHOLD) must be greater than 0 and at most 1"},
		{"smartModeThreshold", "SEASONPACKARR__SMART_MODE_THRESHOLD", "1", ""},
	}
	for _, tt := range tests {
		for _, source := range []string{"file", "environment"} {
			t.Run(fmt.Sprintf("%s=%s from the %s", tt.key, tt.value, source), func(t *testing.T) {
				clearConnectionEnvironment(t)
				contents := "clients: {}\n"
				if source == "file" {
					contents += fmt.Sprintf("%s: %s\n", tt.key, tt.value)
				} else {
					t.Setenv(tt.variable, tt.value)
				}

				_, err := loadWithFile(t, contents)

				if tt.wantErr == "" {
					assert.NoError(t, err)
				} else {
					assert.ErrorContains(t, err, tt.wantErr)
				}
			})
		}
	}
}

func TestLoad_LowercasesContentLayoutFromEachSource(t *testing.T) {
	for _, source := range []string{"file", "environment"} {
		t.Run(source, func(t *testing.T) {
			clearConnectionEnvironment(t)
			contents := qbitClientConfig
			if source == "file" {
				contents += "      contentLayout: NoSubfolder\n"
			} else {
				t.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_CONTENT_LAYOUT", "NoSubfolder")
			}

			cfg, err := loadWithFile(t, contents)

			require.NoError(t, err)
			assert.Equal(t, "nosubfolder", cfg.Clients["default"].Import.ContentLayout)
		})
	}
}

func TestReload_RejectsBadValueAndKeepsLastSnapshot(t *testing.T) {
	for name, bad := range map[string]func(t *testing.T, configFile string){
		"file value out of range": func(t *testing.T, configFile string) {
			writeConfigFile(t, configFile, "clients: {}\nport: 0\n")
		},
		"environment value does not parse": func(t *testing.T, configFile string) {
			t.Setenv("SEASONPACKARR__SMART_MODE", "yes")
			writeConfigFile(t, configFile, "clients: {}\nport: 4243\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			clearConnectionEnvironment(t)
			cfg, configFile := newTestAppConfig(t, "clients: {}\nport: 4242\n")
			before := cfg.Snapshot()

			bad(t, configFile)
			_, err := cfg.reload()

			require.Error(t, err)
			assert.Equal(t, before, cfg.Snapshot())
		})
	}
}
