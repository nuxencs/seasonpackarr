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
		{name: "minimums", interval: "10m", spacing: "10s"},
		{name: "equivalent units", interval: "600s", spacing: "10000ms"},
		{name: "longer", interval: "24h", spacing: "30s"},
		{name: "one minute RSS interval", interval: "1m", spacing: "10s", wantErr: "search.rssInterval must be 0s (disabled) or at least 10m"},
		{name: "below schedule minimum", interval: "9m59.999999999s", spacing: "10s", wantErr: "search.rssInterval must be 0s (disabled) or at least 10m"},
		{name: "zero spacing", interval: "0s", spacing: "0s", wantErr: "search.requestInterval must be at least 10s"},
		{name: "two second spacing", interval: "0s", spacing: "2s", wantErr: "search.requestInterval must be at least 10s"},
		{name: "below spacing minimum", interval: "10m", spacing: "9.999999999s", wantErr: "search.requestInterval must be at least 10s"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("environment", func(t *testing.T) {
				cfg, err := loadWithFile(t, connection,
					"SEASONPACKARR__SEARCH_RSS_INTERVAL="+tt.interval, "SEASONPACKARR__SEARCH_REQUEST_INTERVAL="+tt.spacing)
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tt.interval, cfg.Search.RSSInterval)
				require.Equal(t, tt.spacing, cfg.Search.RequestInterval)
			})
			t.Run("file", func(t *testing.T) {
				cfg, file := newTestAppConfig(t, connection)
				before := cfg.Snapshot()
				rejection := reloadWithFile(t, cfg, file, connection+fmt.Sprintf("  rssInterval: %s\n  requestInterval: %s\n", tt.interval, tt.spacing))
				_, loadErr := loadFile(file)
				if tt.wantErr != "" {
					require.ErrorContains(t, loadErr, tt.wantErr)
					require.Contains(t, rejection, tt.wantErr)
					require.Equal(t, before, cfg.Snapshot(), "invalid reload must preserve active config")
					return
				}
				require.NoError(t, loadErr)
				require.Empty(t, rejection)
				next := cfg.Snapshot()
				require.Equal(t, tt.interval, next.Search.RSSInterval)
				require.Equal(t, tt.spacing, next.Search.RequestInterval)
			})
		})
	}
}

func TestSearchConfig_DefaultsEnvironmentAndReload(t *testing.T) {
	defaults, err := loadWithFile(t, "clients: {}\n")
	require.NoError(t, err)
	require.Equal(t, "0s", defaults.Search.RSSInterval)
	require.Equal(t, "10s", defaults.Search.RequestInterval)

	cfg, file := newTestAppConfig(t, "clients: {}\n",
		"SEASONPACKARR__SEARCH_PROWLARR_URL=http://prowlarr:9696",
		"SEASONPACKARR__SEARCH_API_KEY=key",
		"SEASONPACKARR__SEARCH_RSS_INTERVAL=12h",
		"SEASONPACKARR__SEARCH_INDEXER_IDS=2, 5",
		"SEASONPACKARR__SEARCH_REQUEST_INTERVAL=30s",
	)
	next := cfg.Snapshot()
	require.Equal(t, "http://prowlarr:9696", next.Search.ProwlarrURL)
	require.Equal(t, "12h", next.Search.RSSInterval)
	require.Equal(t, "30s", next.Search.RequestInterval)
	require.Equal(t, []int{2, 5}, next.Search.IndexerIDs)
	isolated := cfg.Snapshot()
	isolated.Search.IndexerIDs[0] = 99
	require.Equal(t, []int{2, 5}, cfg.Snapshot().Search.IndexerIDs)

	// The reload applies the environment of New again, so it overrides the file.
	require.Empty(t, reloadWithFile(t, cfg, file, "clients: {}\nsearch:\n  rssInterval: 1h\n"))
	require.Equal(t, "12h", cfg.Snapshot().Search.RSSInterval)

	require.NotEmpty(t, reloadWithFile(t, cfg, file, "clients: {}\nport: 0\n"))
	require.Equal(t, "12h", cfg.Snapshot().Search.RSSInterval)
}

func TestSearchConfig_RejectsInvalidSettings(t *testing.T) {
	for _, search := range []string{
		"indexerIDs: [0]", "indexerIDs: [-1]", "indexerIDs: [2, 2]", "indexerIDs: [bad]",
		"rssInterval: -1h", "rssInterval: 1s", "rssInterval: invalid", "requestInterval: -1s",
		"prowlarrURL: http://user:secret@prowlarr", "prowlarrURL: file:///tmp/prowlarr", "rssInterval: 1h",
	} {
		t.Run(search, func(t *testing.T) {
			_, err := loadWithFile(t, "clients: {}\nsearch:\n  "+search+"\n")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestSearchConfig_InvalidIndexerEnvironmentDoesNotBroadenSelection(t *testing.T) {
	// The parse names the variable, and the validation names the key.
	for value, want := range map[string]string{
		"bad":                         "SEASONPACKARR__SEARCH_INDEXER_IDS",
		" , ":                         "SEASONPACKARR__SEARCH_INDEXER_IDS",
		"999999999999999999999999999": "SEASONPACKARR__SEARCH_INDEXER_IDS",
		"1,1":                         "search.indexerIDs",
		"-1":                          "search.indexerIDs",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := loadWithFile(t, "clients: {}\n", "SEASONPACKARR__SEARCH_INDEXER_IDS="+value)
			require.ErrorContains(t, err, want)
		})
	}
}
