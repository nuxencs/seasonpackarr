// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			_, err := loadWithFile(t, qbitClientConfig, tt.variable+"="+tt.value)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.variable)
			assert.NotContains(t, err.Error(), "sentinel")
		})
	}
}

func TestLoad_ParsesEnvironmentValueOfEachKind(t *testing.T) {
	cfg, err := loadWithFile(t, qbitClientConfig,
		"SEASONPACKARR__API_TOKEN=token",
		"SEASONPACKARR__SMART_MODE=true",
		"SEASONPACKARR__LOG_MAX_SIZE=7",
		"SEASONPACKARR__SMART_MODE_THRESHOLD=0.5",
		"SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_TAGS= a , ,b ",
		"SEASONPACKARR__SEARCH_INDEXER_IDS=3, 4",
		"SEASONPACKARR__CLIENTS_DEFAULT_PORT=8080",
	)

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
				_, err := loadWithFile(t, "notifications:\n  notificationLevel: [MATCH]\n  shoutrrr: [\"ntfy://ntfy.sh/topic\"]\n", variable+"="+value)

				require.Error(t, err)
				assert.Contains(t, err.Error(), variable)
			})
		}
	}
}

func TestLoad_IgnoresEmptyEnvironmentValue(t *testing.T) {
	cfg, err := loadWithFile(t, "port: 4242\nnotifications:\n  notificationLevel: [MATCH]\n  shoutrrr: [\"ntfy://ntfy.sh/topic\"]\n",
		"SEASONPACKARR__PORT=", "SEASONPACKARR__SMART_MODE=", "SEASONPACKARR__NOTIFICATIONS_NOTIFICATION_LEVEL=", "SEASONPACKARR__NOTIFICATIONS_SHOUTRRR=")

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
				contents := "clients: {}\n"
				var environ []string
				if source == "file" {
					contents += fmt.Sprintf("%s: %s\n", tt.key, tt.value)
				} else {
					environ = append(environ, tt.variable+"="+tt.value)
				}

				_, err := loadWithFile(t, contents, environ...)

				if tt.wantErr == "" {
					assert.NoError(t, err)
				} else {
					assert.ErrorContains(t, err, tt.wantErr)
				}
			})
		}
	}
}

// TestLoad_ClientPortRange shows that a client port with no valid meaning stops the load. 0 stays allowed, because an
// unset port means a type-specific fallback.
func TestLoad_ClientPortRange(t *testing.T) {
	const wantErr = `port for client "default" (SEASONPACKARR__CLIENTS_DEFAULT_PORT) must be from 0 to 65535`
	for _, tt := range []struct{ value, wantErr string }{
		{"-1", wantErr},
		{"65536", wantErr},
		{"0", ""},
		{"65535", ""},
	} {
		for _, source := range []string{"file", "environment"} {
			t.Run(fmt.Sprintf("port=%s from the %s", tt.value, source), func(t *testing.T) {
				contents := qbitClientConfig + "    port: 8080\n"
				var environ []string
				if source == "file" {
					contents = qbitClientConfig + "    port: " + tt.value + "\n"
				} else {
					environ = append(environ, "SEASONPACKARR__CLIENTS_DEFAULT_PORT="+tt.value)
				}

				_, err := loadWithFile(t, contents, environ...)

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
			contents := qbitClientConfig
			var environ []string
			if source == "file" {
				contents += "      contentLayout: NoSubfolder\n"
			} else {
				environ = append(environ, "SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_CONTENT_LAYOUT=NoSubfolder")
			}

			cfg, err := loadWithFile(t, contents, environ...)

			require.NoError(t, err)
			assert.Equal(t, "nosubfolder", cfg.Clients["default"].Import.ContentLayout)
		})
	}
}

func TestReload_RejectsBadValueAndKeepsLastSnapshot(t *testing.T) {
	for name, tt := range map[string]struct {
		environ []string
		bad     string
	}{
		"file value out of range": {bad: "clients: {}\nport: 0\n"},
		// The holder applies its environment again, so a file change can make a valid environment value invalid.
		"environment value invalid with the new file": {
			environ: []string{"SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_CONTENT_LAYOUT=subfolder"},
			bad:     "clients:\n  default:\n    type: transmission\nport: 4243\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, configFile := newTestAppConfig(t, qbitClientConfig+"port: 4242\n", tt.environ...)
			before := cfg.Snapshot()

			rejection := reloadWithFile(t, cfg, configFile, tt.bad)

			require.NotEmpty(t, rejection)
			assert.Equal(t, before, cfg.Snapshot())
		})
	}
}
