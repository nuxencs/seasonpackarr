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

func loadTestKoanf(t *testing.T, content string) *koanf.Koanf {
	t.Helper()

	k := koanf.New(".")
	require.NoError(t, k.Load(file.Provider(writeTestConfig(t, content)), yaml.Parser()))

	return k
}

func TestValidateDeprecatedConfigInputs(t *testing.T) {
	t.Run("rejects parseTorrentFile in yaml", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(loadTestKoanf(t, "parseTorrentFile: false\n"), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "parseTorrentFile was removed")
	})

	t.Run("rejects parseTorrentFile env", func(t *testing.T) {
		lookup := func(key string) (string, bool) {
			if key == deprecatedParseTorrentFileEnv {
				return "false", true
			}
			return "", false
		}
		err := validateDeprecatedConfigInputs(nil, lookup)
		require.Error(t, err)
		require.Contains(t, err.Error(), deprecatedParseTorrentFileEnv)
	})

	t.Run("rejects preImportPath in yaml", func(t *testing.T) {
		k := loadTestKoanf(t, "clients:\n  default:\n    preImportPath: /data/tv-hd\n")
		err := validateDeprecatedConfigInputs(k, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "clients.default.preImportPath")
		require.Contains(t, err.Error(), "import.savePath or import.category")
	})

	t.Run("rejects preImportPath env", func(t *testing.T) {
		t.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH", "/data/tv-hd")
		err := validateDeprecatedConfigInputs(nil, os.LookupEnv)
		require.Error(t, err)
		require.Contains(t, err.Error(), "SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH")
	})

	t.Run("allows current config", func(t *testing.T) {
		k := loadTestKoanf(t, "host: 0.0.0.0\nclients: {}\n")
		err := validateDeprecatedConfigInputs(k, func(string) (string, bool) { return "", false })
		require.NoError(t, err)
	})
}

// TestLoadFromEnvParsesMultiWordClientSettings guards the env-parser fix: keys
// like IMPORT_SAVE_PATH (multiple underscores) must survive, including for a
// client that only exists in the environment.
func TestLoadFromEnvParsesMultiWordClientSettings(t *testing.T) {
	t.Setenv("SEASONPACKARR__CLIENTS_ENVONLY_TYPE", "qbittorrent")
	t.Setenv("SEASONPACKARR__CLIENTS_ENVONLY_IMPORT_SAVE_PATH", "/data/tv-hd")
	t.Setenv("SEASONPACKARR__CLIENTS_ENVONLY_IMPORT_CATEGORY", "tv-hd")
	t.Setenv("SEASONPACKARR__CLIENTS_ENVONLY_IMPORT_TAGS", "a, b")

	cfg := &AppConfig{
		Config: &domain.Config{Clients: map[string]*domain.Client{}},
		k:      koanf.New("."),
	}
	cfg.loadFromEnv()

	client, ok := cfg.Config.Clients["envonly"]
	require.True(t, ok, "env-only client should be lazily created")
	require.Equal(t, "qbittorrent", client.Type)
	require.Equal(t, "/data/tv-hd", client.Import.SavePath)
	require.Equal(t, "tv-hd", client.Import.Category)
	require.Equal(t, []string{"a", "b"}, client.Import.Tags)
}

func TestValidateClientConfig(t *testing.T) {
	t.Run("qbittorrent requires category or savePath", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "qbittorrent"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "import.category or import.savePath")
	})

	t.Run("qbittorrent with category is valid", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{Category: "tv-hd"}})
		require.NoError(t, err)
	})

	t.Run("empty type defaults to qbittorrent rules", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Import: domain.ImportPolicy{Category: "tv-hd"}})
		require.NoError(t, err)
	})

	t.Run("transmission rejects qbittorrent-only fields", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "transmission", Import: domain.ImportPolicy{Category: "tv-hd"}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "qBittorrent only")
	})

	t.Run("transmission with savePath is valid", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "transmission", Import: domain.ImportPolicy{SavePath: t.TempDir()}})
		require.NoError(t, err)
	})

	t.Run("transmission without any policy is valid (uses session download dir)", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "transmission"})
		require.NoError(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "deluge", Import: domain.ImportPolicy{SavePath: t.TempDir()}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "is invalid")
	})

	t.Run("qbittorrent rejects invalid content layout", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{
			Type: "qbittorrent",
			Import: domain.ImportPolicy{
				SavePath:      t.TempDir(),
				ContentLayout: "flat",
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "contentLayout")
	})

	t.Run("nonexistent savePath", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "qbittorrent", Import: domain.ImportPolicy{SavePath: "/nope/does/not/exist"}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "doesn't exist")
	})
}
