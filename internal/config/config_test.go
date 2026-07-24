// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMissingOptionalConfigKeysUseDefaults(t *testing.T) {
	cfg := &AppConfig{
		Config: &domain.Config{},
		k:      koanf.New("."),
	}
	cfg.defaults()

	configFile := writeTestConfig(t, `
host: "127.0.0.1"
port: 42069
clients: {}
logLevel: "INFO"
`)

	require.NoError(t, cfg.k.Load(file.Provider(configFile), yaml.Parser()))
	require.NoError(t, cfg.k.Unmarshal("", cfg.Config))

	require.Equal(t, "INFO", cfg.Config.LogLevel)
	require.Equal(t, "", cfg.Config.LogPath)
	require.Equal(t, 50, cfg.Config.LogMaxSize)
	require.Equal(t, 3, cfg.Config.LogMaxBackups)
	require.False(t, cfg.Config.SmartMode)
	require.Equal(t, float32(0.75), cfg.Config.SmartModeThreshold)
	require.False(t, cfg.Config.ParseTorrentFile)
	require.False(t, cfg.Config.FuzzyMatching.SkipRepackCompare)
	require.False(t, cfg.Config.FuzzyMatching.SimplifyHdrCompare)
	require.False(t, cfg.Config.FuzzyMatching.SimplifyWebCompare)
	require.False(t, cfg.Config.FuzzyMatching.SkipYearCompare)
	require.Equal(t, "", cfg.Config.Metadata.TVDBAPIKey)
	require.Equal(t, "", cfg.Config.Metadata.TVDBPIN)
	require.Equal(t, "", cfg.Config.APIToken)
	require.Equal(t, []string{"MATCH", "ERROR"}, cfg.Config.Notifications.NotificationLevel)
	require.Equal(t, "", cfg.Config.Notifications.Discord)
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0o644))

	return configFile
}
