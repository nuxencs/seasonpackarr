// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchConfig_IntervalLimits(t *testing.T) {
	const connection = "clients: {}\nsearch:\n  prowlarrURL: http://prowlarr:9696\n  apiKey: key\n"
	for _, tt := range []struct {
		name     string
		interval string
		spacing  string
		wantErr  string
	}{
		{name: "disabled", interval: "0s", spacing: "10s"},
		{name: "minimums", interval: "1h", spacing: "10s"},
		{name: "equivalent units", interval: "60m", spacing: "10000ms"},
		{name: "longer", interval: "24h", spacing: "30s"},
		{name: "old schedule minimum", interval: "1m", spacing: "10s", wantErr: "search.interval must be 0s (disabled) or at least 1h"},
		{name: "below schedule minimum", interval: "59m59.999999999s", spacing: "10s", wantErr: "search.interval must be 0s (disabled) or at least 1h"},
		{name: "zero spacing", interval: "0s", spacing: "0s", wantErr: "search.requestInterval must be at least 10s"},
		{name: "old spacing default", interval: "0s", spacing: "2s", wantErr: "search.requestInterval must be at least 10s"},
		{name: "below spacing minimum", interval: "1h", spacing: "9.999999999s", wantErr: "search.requestInterval must be at least 10s"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, source := range []string{"file", "environment"} {
				t.Run(source, func(t *testing.T) {
					cfg, file := newTestAppConfig(t, connection)
					before := cfg.Snapshot()
					if source == "environment" {
						t.Setenv("SEASONPACKARR__SEARCH_INTERVAL", tt.interval)
						t.Setenv("SEASONPACKARR__SEARCH_REQUEST_INTERVAL", tt.spacing)
					} else {
						writeConfigFile(t, file, connection+fmt.Sprintf("  interval: %s\n  requestInterval: %s\n", tt.interval, tt.spacing))
					}
					_, loadErr := cfg.loadSnapshot()
					next, reloadErr := cfg.reload()
					if tt.wantErr != "" {
						require.ErrorContains(t, loadErr, tt.wantErr)
						require.ErrorContains(t, reloadErr, tt.wantErr)
						require.Equal(t, before, cfg.Snapshot(), "invalid reload must preserve active config")
						return
					}
					require.NoError(t, loadErr)
					require.NoError(t, reloadErr)
					require.Equal(t, tt.interval, next.Search.Interval)
					require.Equal(t, tt.spacing, next.Search.RequestInterval)
				})
			}
		})
	}
}

func TestSearchConfig_DefaultsEnvironmentAndReload(t *testing.T) {
	cfg, file := newTestAppConfig(t, "clients: {}\n")
	require.Equal(t, "0s", cfg.Snapshot().Search.Interval)
	require.Equal(t, "10s", cfg.Snapshot().Search.RequestInterval)
	t.Setenv("SEASONPACKARR__SEARCH_PROWLARR_URL", "http://prowlarr:9696")
	t.Setenv("SEASONPACKARR__SEARCH_API_KEY", "key")
	t.Setenv("SEASONPACKARR__SEARCH_INTERVAL", "12h")
	t.Setenv("SEASONPACKARR__SEARCH_INDEXER_IDS", "2, 5")
	t.Setenv("SEASONPACKARR__SEARCH_REQUEST_INTERVAL", "30s")
	next, err := cfg.reload()
	require.NoError(t, err)
	require.Equal(t, "http://prowlarr:9696", next.Search.ProwlarrURL)
	require.Equal(t, "12h", next.Search.Interval)
	require.Equal(t, "30s", next.Search.RequestInterval)
	require.Equal(t, []int{2, 5}, next.Search.IndexerIDs)
	isolated := cfg.Snapshot()
	isolated.Search.IndexerIDs[0] = 99
	require.Equal(t, []int{2, 5}, cfg.Snapshot().Search.IndexerIDs)
	t.Setenv("SEASONPACKARR__SEARCH_INTERVAL", "bad")
	writeConfigFile(t, file, "clients: {}\nsearch:\n  interval: 1h\n")
	_, err = cfg.reload()
	require.Error(t, err)
	require.Equal(t, "12h", cfg.Snapshot().Search.Interval)
}

func TestSearchConfig_RejectsInvalidSettings(t *testing.T) {
	for _, search := range []string{
		"indexerIDs: [0]", "indexerIDs: [-1]", "indexerIDs: [2, 2]", "indexerIDs: [bad]",
		"interval: -1h", "interval: 1s", "interval: invalid", "requestInterval: -1s",
		"prowlarrURL: http://user:secret@prowlarr", "prowlarrURL: file:///tmp/prowlarr", "interval: 1h",
	} {
		t.Run(search, func(t *testing.T) {
			cfg := &AppConfig{configFile: writeTestConfig(t, "clients: {}\nsearch:\n  "+search+"\n")}
			_, err := cfg.loadSnapshot()
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestSearchConfig_InvalidIndexerEnvironmentDoesNotBroadenSelection(t *testing.T) {
	for _, value := range []string{"bad", "1,", "1,1", "-1", "999999999999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SEASONPACKARR__SEARCH_INDEXER_IDS", value)
			cfg := &AppConfig{configFile: writeTestConfig(t, "clients: {}\n")}
			_, err := cfg.loadSnapshot()
			require.ErrorContains(t, err, "search.indexerIDs")
		})
	}
}
