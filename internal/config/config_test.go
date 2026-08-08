// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestMissingOptionalConfigKeysUseDefaults(t *testing.T) {
	configFile := writeTestConfig(t, `
host: "127.0.0.1"
port: 42069
clients: {}
logLevel: "INFO"
`)
	cfg := &AppConfig{configFile: configFile, version: "test"}
	snapshot, err := cfg.loadSnapshot()
	require.NoError(t, err)

	require.Equal(t, "INFO", snapshot.LogLevel)
	require.Equal(t, "", snapshot.LogPath)
	require.Equal(t, 50, snapshot.LogMaxSize)
	require.Equal(t, 3, snapshot.LogMaxBackups)
	require.False(t, snapshot.SmartMode)
	require.Equal(t, float32(0.75), snapshot.SmartModeThreshold)
	require.False(t, snapshot.FuzzyMatching.SkipRepackCompare)
	require.False(t, snapshot.FuzzyMatching.SimplifyHdrCompare)
	require.False(t, snapshot.FuzzyMatching.SimplifyWebCompare)
	require.False(t, snapshot.FuzzyMatching.SkipYearCompare)
	require.Equal(t, "", snapshot.Metadata.TVDBAPIKey)
	require.Equal(t, "", snapshot.Metadata.TVDBPIN)
	require.Equal(t, "", snapshot.APIToken)
	require.Equal(t, []string{"MATCH", "ERROR"}, snapshot.Notifications.NotificationLevel)
	require.Equal(t, "", snapshot.Notifications.Discord)
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

	cfg := domain.Config{Clients: map[string]*domain.Client{}}
	applyEnvironment(&cfg)

	client, ok := cfg.Clients["envonly"]
	require.True(t, ok, "env-only client should be lazily created")
	require.Equal(t, "qbittorrent", client.Type)
	require.Equal(t, "/data/tv-hd", client.Import.SavePath)
	require.Equal(t, "tv-hd", client.Import.Category)
	require.Equal(t, []string{"a", "b"}, client.Import.Tags)
}

func TestReloadRejectsInvalidConfigAndKeepsLastSnapshot(t *testing.T) {
	previousLogLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLogLevel)
	})

	cfg, configFile := newTestAppConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: tv-hd
logLevel: INFO
`)
	before := cfg.Snapshot()

	writeConfigFile(t, configFile, `
clients:
  default:
    type: transmission
    import:
      category: tv-hd
logLevel: TRACE
`)
	_, err := cfg.reload()
	require.Error(t, err)
	require.Contains(t, err.Error(), "qBittorrent only")
	require.Equal(t, before, cfg.Snapshot())
	require.Equal(t, zerolog.WarnLevel, zerolog.GlobalLevel())
}

func TestReloadReappliesEnvironmentOverrides(t *testing.T) {
	t.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_CATEGORY", "from-env")
	cfg, configFile := newTestAppConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: from-file-before
`)
	require.Equal(t, "from-env", cfg.Snapshot().Clients["default"].Import.Category)

	writeConfigFile(t, configFile, `
clients:
  default:
    type: qbittorrent
    import:
      category: from-file-after
`)
	_, err := cfg.reload()
	require.NoError(t, err)
	require.Equal(t, "from-env", cfg.Snapshot().Clients["default"].Import.Category)
}

func TestReloadRestoresDefaultsForOmittedKeys(t *testing.T) {
	cfg, configFile := newTestAppConfig(t, `
clients: {}
logMaxSize: 99
`)
	require.Equal(t, 99, cfg.Snapshot().LogMaxSize)

	writeConfigFile(t, configFile, "clients: {}\n")
	_, err := cfg.reload()
	require.NoError(t, err)
	require.Equal(t, 50, cfg.Snapshot().LogMaxSize)
}

func TestResolveConfigFileReturnsDiscoveredFile(t *testing.T) {
	originalWorkingDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(originalWorkingDir))
	})

	workingDir := t.TempDir()
	homeDir := t.TempDir()
	configFile := filepath.Join(homeDir, ".config", "seasonpackarr", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configFile), 0o755))
	writeConfigFile(t, configFile, "clients: {}\n")
	require.NoError(t, os.Chdir(workingDir))
	t.Setenv("HOME", homeDir)

	cfg := &AppConfig{}
	got, err := cfg.resolveConfigFile("")
	require.NoError(t, err)
	require.Equal(t, configFile, got)
}

func TestSnapshotCannotMutateStoredConfig(t *testing.T) {
	cfg, _ := newTestAppConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: tv-hd
      tags: [one, two]
notifications:
  notificationLevel: [MATCH, ERROR]
`)

	snapshot := cfg.Snapshot()
	snapshot.Clients["default"].Import.Category = "changed"
	snapshot.Clients["default"].Import.Tags[0] = "changed"
	snapshot.Notifications.NotificationLevel[0] = "changed"
	delete(snapshot.Clients, "default")

	stored := cfg.Snapshot()
	require.Equal(t, "tv-hd", stored.Clients["default"].Import.Category)
	require.Equal(t, []string{"one", "two"}, stored.Clients["default"].Import.Tags)
	require.Equal(t, []string{"MATCH", "ERROR"}, stored.Notifications.NotificationLevel)
}

func TestDynamicReloadPublishesSnapshotForConcurrentReaders(t *testing.T) {
	previousLogLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLogLevel)
	})

	cfg, configFile := newTestAppConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: before
`)
	snapshot := cfg.Snapshot()
	reloads, err := cfg.DynamicReload(logger.New(&snapshot))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NotNil(t, cfg.watcher)
		require.NoError(t, cfg.watcher.Unwatch())
	})

	readerResult := make(chan error, 1)
	go func() {
		for {
			snapshot := cfg.Snapshot()
			client, ok := snapshot.Clients["default"]
			if !ok || client == nil {
				readerResult <- fmt.Errorf("published snapshot has no default client")
				return
			}
			if client.Import.Category == "after" {
				readerResult <- nil
				return
			}
		}
	}()

	writeConfigFile(t, configFile, `
clients:
  default:
    type: qbittorrent
    import:
      category: after
logLevel: INFO
`)
	select {
	case <-reloads:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for config reload")
	}
	select {
	case err := <-readerResult:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent snapshot reader did not observe reload")
	}
	require.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
}

func TestDynamicReloadKeepsSnapshotWhileConfigWriteIsIncomplete(t *testing.T) {
	previousLogLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLogLevel)
	})

	cfg, configFile := newTestAppConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: before
`)
	snapshot := cfg.Snapshot()
	reloads, err := cfg.DynamicReload(logger.New(&snapshot))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NotNil(t, cfg.watcher)
		require.NoError(t, cfg.watcher.Unwatch())
	})

	configWriter, err := os.OpenFile(configFile, os.O_WRONLY|os.O_TRUNC, 0)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = configWriter.Close()
	})

	select {
	case <-reloads:
		t.Fatal("published a reload while the config file was empty")
	case <-time.After(100 * time.Millisecond):
	}

	before := cfg.Snapshot()
	require.Contains(t, before.Clients, "default")
	require.NotNil(t, before.Clients["default"])
	require.Equal(t, "before", before.Clients["default"].Import.Category)

	_, err = configWriter.WriteString(`
clients:
  default:
    type: qbittorrent
    import:
      category: after
`)
	require.NoError(t, err)
	require.NoError(t, configWriter.Close())

	select {
	case <-reloads:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for completed config reload")
	}
	require.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
}

func newTestAppConfig(t *testing.T, contents string) (*AppConfig, string) {
	t.Helper()
	configFile := writeTestConfig(t, contents)
	cfg := &AppConfig{configFile: configFile, version: "test"}
	snapshot, err := cfg.loadSnapshot()
	require.NoError(t, err)
	cfg.current.Store(snapshot)
	return cfg, configFile
}

func writeConfigFile(t *testing.T, configFile, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(configFile, []byte(contents), 0o644))
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

	t.Run("deluge requires savePath", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "deluge"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "import.savePath")
	})

	t.Run("deluge with savePath is valid", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "deluge", Import: domain.ImportPolicy{SavePath: t.TempDir()}})
		require.NoError(t, err)
	})

	t.Run("deluge accepts one label", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{
			Type: "deluge-v1",
			Import: domain.ImportPolicy{
				SavePath: t.TempDir(),
				Tags:     []string{"seasonpackarr"},
			},
		})
		require.NoError(t, err)
	})

	t.Run("deluge rejects multiple labels", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{
			Type: "deluge-v2",
			Import: domain.ImportPolicy{
				SavePath: t.TempDir(),
				Tags:     []string{"seasonpackarr", "tv"},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "at most one")
	})

	t.Run("deluge rejects invalid label characters", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{
			Type: "deluge",
			Import: domain.ImportPolicy{
				SavePath: t.TempDir(),
				Tags:     []string{"tv shows"},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "is invalid")
	})

	t.Run("deluge rejects qbittorrent api key", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{
			Type:   "deluge",
			APIKey: "secret",
			Import: domain.ImportPolicy{SavePath: t.TempDir()},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "apiKey")
	})

	t.Run("invalid type", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{Type: "notarealclient", Import: domain.ImportPolicy{SavePath: t.TempDir()}})
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
