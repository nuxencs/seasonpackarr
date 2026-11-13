// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSnapshot_MissingOptionalConfigKeysUseDefaults(t *testing.T) {
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
	require.Equal(t, "", snapshot.APIToken)
	require.Equal(t, []string{"MATCH", "ERROR"}, snapshot.Notifications.NotificationLevel)
	require.Equal(t, "", snapshot.Notifications.Discord)
}

func TestLoadSnapshot_RejectsUnknownKeys(t *testing.T) {
	configFile := writeTestConfig(t, `
host: "127.0.0.1"
logLevl: "INFO"
fuzzyMatching:
  skipRepackCompar: true
clients:
  default:
    type: qbittorrent
    hots: qbit
    import:
      category: tv-hd
      savepath: "/data"
      tag: [seasonpackarr]
`)
	cfg := &AppConfig{configFile: configFile, version: "test"}
	_, err := cfg.loadSnapshot()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown config keys: clients.default.hots, clients.default.import.tag, fuzzyMatching.skipRepackCompar, logLevl")
	assert.Contains(t, err.Error(), configFile)
}

// TestLoadSnapshot_IgnoresUnknownClientEnvironmentVariables keeps the promise
// of the startup warning: an unknown client variable must not add a client,
// because an empty client fails validation and stops startup.
func TestLoadSnapshot_IgnoresUnknownClientEnvironmentVariables(t *testing.T) {
	configFile := writeTestConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: tv-hd
`)
	t.Setenv("SEASONPACKARR__CLIENTS_GHOST_IMPORT_SAVEPATH", "/data")
	t.Setenv("SEASONPACKARR__CLIENTS__HOST", "qbit")
	require.Equal(t, []string{"SEASONPACKARR__CLIENTS_GHOST_IMPORT_SAVEPATH", "SEASONPACKARR__CLIENTS__HOST"},
		UnknownEnvironmentVariables(os.Environ()))

	cfg := &AppConfig{configFile: configFile, version: "test"}
	snapshot, err := cfg.loadSnapshot()
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, slices.Sorted(maps.Keys(snapshot.Clients)))
}

func TestLoadSnapshot_RejectsInternalFieldsAsKeys(t *testing.T) {
	configFile := writeTestConfig(t, "clients: {}\nversion: v9\nconfigPath: /tmp\ndisableConfigFile: true\n")
	cfg := &AppConfig{configFile: configFile, version: "test"}
	_, err := cfg.loadSnapshot()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown config keys: configPath, disableConfigFile, version")
}

func TestLoadSnapshot_KeepsCaseInsensitiveKeyMatching(t *testing.T) {
	configFile := writeTestConfig(t, `
LogLevel: "INFO"
clients:
  default:
    TYPE: qbittorrent
    Import:
      Category: tv-hd
`)
	cfg := &AppConfig{configFile: configFile, version: "test"}
	snapshot, err := cfg.loadSnapshot()
	require.NoError(t, err)
	assert.Equal(t, "INFO", snapshot.LogLevel)
	assert.Equal(t, "test", snapshot.Version)
	require.Contains(t, snapshot.Clients, "default")
	assert.Equal(t, "qbittorrent", snapshot.Clients["default"].Type)
	assert.Equal(t, "tv-hd", snapshot.Clients["default"].Import.Category)
}

// TestLoadSnapshot_ReadsKeyCaseVariants: a case variant once competed with the
// default of the same key, and map order chose the winner. A lost apiToken
// turns off the API authentication.
func TestLoadSnapshot_ReadsKeyCaseVariants(t *testing.T) {
	for _, key := range []string{"apiToken", "apitoken", "APITOKEN", "APIToken"} {
		t.Run(key, func(t *testing.T) {
			configFile := writeTestConfig(t, "clients: {}\nNOTIFICATIONS:\n  DISCORD: hook\n"+key+": secret\n")
			// One load proves nothing when map order decides the result.
			for range 20 {
				snapshot, err := (&AppConfig{configFile: configFile, version: "test"}).loadSnapshot()
				require.NoError(t, err)
				require.Equal(t, "secret", snapshot.APIToken)
				require.Equal(t, "hook", snapshot.Notifications.Discord)
			}
		})
	}
}

func TestLoadSnapshot_KeepsMigrationErrorsForRemovedKeys(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{name: "parseTorrentFile", content: "clients: {}\nparseTorrentFile: false\n", want: "parseTorrentFile was removed"},
		{name: "metadata", content: "clients: {}\nmetadata:\n  tvdbAPIKey: key\n", want: "metadata was removed"},
		{name: "preImportPath", content: "clients:\n  default:\n    preImportPath: /data\n", want: "clients.default.preImportPath was removed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &AppConfig{configFile: writeTestConfig(t, tt.content), version: "test"}
			_, err := cfg.loadSnapshot()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, err.Error(), "unknown config keys")
		})
	}
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

	t.Run("rejects empty parseTorrentFile env", func(t *testing.T) {
		lookup := func(key string) (string, bool) {
			return "", key == deprecatedParseTorrentFileEnv
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

	t.Run("rejects empty preImportPath env", func(t *testing.T) {
		t.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH", "")
		err := validateDeprecatedConfigInputs(nil, os.LookupEnv)
		require.Error(t, err)
		require.Contains(t, err.Error(), "SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH")
	})

	t.Run("rejects metadata config", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(loadTestKoanf(t, "metadata:\n  tvdbAPIKey: old-key\n"), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "metadata was removed")
		require.Contains(t, err.Error(), "/api/match")
	})

	t.Run("rejects metadata env", func(t *testing.T) {
		lookup := func(key string) (string, bool) {
			if key == deprecatedMetadataTVDBAPIKeyEnv {
				return "old-key", true
			}
			return "", false
		}
		err := validateDeprecatedConfigInputs(nil, lookup)
		require.Error(t, err)
		require.Contains(t, err.Error(), deprecatedMetadataTVDBAPIKeyEnv)
		require.Contains(t, err.Error(), "/api/match")
	})

	t.Run("rejects empty metadata env", func(t *testing.T) {
		lookup := func(key string) (string, bool) {
			return "", key == deprecatedMetadataTVDBPINEnv
		}
		err := validateDeprecatedConfigInputs(nil, lookup)
		require.Error(t, err)
		require.Contains(t, err.Error(), deprecatedMetadataTVDBPINEnv)
	})

	t.Run("allows current config", func(t *testing.T) {
		k := loadTestKoanf(t, "host: 0.0.0.0\nclients: {}\n")
		err := validateDeprecatedConfigInputs(k, func(string) (string, bool) { return "", false })
		require.NoError(t, err)
	})
}

// TestLoadFromEnv_ParsesMultiWordClientSettings guards the env-parser fix: keys
// like IMPORT_SAVE_PATH (multiple underscores) must survive, including for a
// client that only exists in the environment.
func TestLoadFromEnv_ParsesMultiWordClientSettings(t *testing.T) {
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
			// Exported shell values for the other keys would change the result.
			for _, other := range tests {
				t.Setenv(other.key, "false")
			}
			t.Setenv(tt.key, "true")

			cfg := domain.Config{Clients: map[string]*domain.Client{}}
			applyEnvironment(&cfg)

			require.Equal(t, tt.want, cfg.FuzzyMatching)
		})
	}
}

func TestReload_RejectsInvalidConfigAndKeepsLastSnapshot(t *testing.T) {
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

func TestReload_RejectsUnknownKeyAndKeepsLastSnapshot(t *testing.T) {
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
    type: qbittorrent
    import:
      category: tv-other
logLevel: TRACE
smartMod: true
`)
	_, err := cfg.reload()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown config keys: smartMod")
	assert.Equal(t, before, cfg.Snapshot())
}

func TestReload_ReappliesEnvironmentOverrides(t *testing.T) {
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

func TestReload_RestoresDefaultsForOmittedKeys(t *testing.T) {
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

func TestResolveConfigFile_ReturnsDiscoveredFile(t *testing.T) {
	workingDir := t.TempDir()
	homeDir := t.TempDir()
	configFile := filepath.Join(homeDir, ".config", "seasonpackarr", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configFile), 0o755))
	writeConfigFile(t, configFile, "clients: {}\n")
	t.Chdir(workingDir)
	t.Setenv("HOME", homeDir)

	cfg := &AppConfig{}
	got, err := cfg.resolveConfigFile("")
	require.NoError(t, err)
	require.Equal(t, configFile, got)
}

func TestSnapshot_CannotMutateStoredConfig(t *testing.T) {
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

func TestDynamicReload_PublishesSnapshotForConcurrentReaders(t *testing.T) {
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
		require.NoError(t, cfg.watcher.Close())
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
		require.FailNow(t, "timed out waiting for config reload")
	}
	select {
	case err := <-readerResult:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "concurrent snapshot reader did not observe reload")
	}
	require.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
}

func TestDynamicReload_KeepsSnapshotWhileConfigWriteIsIncomplete(t *testing.T) {
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
		require.NoError(t, cfg.watcher.Close())
	})

	configWriter, err := os.OpenFile(configFile, os.O_WRONLY|os.O_TRUNC, 0)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = configWriter.Close()
	})

	select {
	case <-reloads:
		require.FailNow(t, "published a reload while the config file was empty")
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
		require.FailNow(t, "timed out waiting for completed config reload")
	}
	require.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
}

func TestDynamicReload_DebouncesTruncateFirstWrites(t *testing.T) {
	previousLogLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLogLevel)
	})

	cfg, configFile := newTestAppConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: before
logLevel: DEBUG
`)
	log := loggertest.New()
	reloads, err := cfg.DynamicReload(log)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NotNil(t, cfg.watcher)
		require.NoError(t, cfg.watcher.Close())
	})

	writer, err := os.OpenFile(configFile, os.O_WRONLY|os.O_TRUNC, 0)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	time.Sleep(configReloadDebounceDelay / 4)

	writer, err = os.OpenFile(configFile, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = writer.WriteString(`
clients:
  default:
    type: qbittorrent
    import:
      category: after
logLevel: DEBUG
`)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	time.Sleep(configReloadDebounceDelay / 4)

	writer, err = os.OpenFile(configFile, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = writer.WriteString("\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	select {
	case <-reloads:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "timed out waiting for config reload")
	}
	time.Sleep(2 * configReloadDebounceDelay)

	logs := log.String()
	require.NotContains(t, logs, "config reload rejected", logs)
	require.Equal(t, 1, bytes.Count([]byte(logs), []byte("config file reloaded")), logs)
	require.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
}

func TestDynamicReload_SurvivesRenameAndRecreateSave(t *testing.T) {
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
	log := loggertest.New()
	reloads, err := cfg.DynamicReload(log)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NotNil(t, cfg.watcher)
		require.NoError(t, cfg.watcher.Close())
	})

	backupFile := configFile + ".backup"
	require.NoError(t, os.Rename(configFile, backupFile))
	time.Sleep(2 * configReloadDebounceDelay)
	require.Contains(t, log.String(), "config reload rejected; keeping previous config")

	writeConfigFile(t, configFile, `
clients:
  default:
    type: qbittorrent
    import:
      category: after-first-save
`)

	select {
	case <-reloads:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "timed out waiting for reload after rename-and-recreate save")
	}
	require.Equal(t, "after-first-save", cfg.Snapshot().Clients["default"].Import.Category)

	writeConfigFile(t, configFile, `
clients:
  default:
    type: qbittorrent
    import:
      category: after-second-save
`)

	select {
	case <-reloads:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "timed out waiting for reload after the next save")
	}
	require.Equal(t, "after-second-save", cfg.Snapshot().Clients["default"].Import.Category)
	require.NotContains(t, log.String(), "error watching config file")
}

func TestDynamicReload_FollowsReplacedConfigSymlink(t *testing.T) {
	previousLogLevel := zerolog.GlobalLevel()
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(previousLogLevel)
	})

	configDir := t.TempDir()
	firstTarget := filepath.Join(configDir, "config-first.yaml")
	secondTarget := filepath.Join(configDir, "config-second.yaml")
	configFile := filepath.Join(configDir, "config.yaml")
	writeConfigFile(t, firstTarget, `
clients:
  default:
    type: qbittorrent
    import:
      category: before
`)
	writeConfigFile(t, secondTarget, `
clients:
  default:
    type: qbittorrent
    import:
      category: after
`)
	if err := os.Symlink(firstTarget, configFile); err != nil {
		t.Skipf("config symlinks are unavailable: %v", err)
	}
	nextSymlink := configFile + ".next"
	require.NoError(t, os.Symlink(secondTarget, nextSymlink))

	cfg := &AppConfig{configFile: configFile, version: "test"}
	snapshot, err := cfg.loadSnapshot()
	require.NoError(t, err)
	cfg.current.Store(snapshot)
	reloads, err := cfg.DynamicReload(logger.New(snapshot))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NotNil(t, cfg.watcher)
		require.NoError(t, cfg.watcher.Close())
	})

	time.Sleep(configReloadDebounceDelay)
	require.NoError(t, os.Rename(nextSymlink, configFile))
	writeConfigFile(t, secondTarget, `
clients:
  default:
    type: qbittorrent
    import:
      category: after
`)

	select {
	case <-reloads:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "timed out waiting for reload after config symlink replacement")
	}
	require.Equal(t, "after", cfg.Snapshot().Clients["default"].Import.Category)
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

// TestUnknownConfigKeys_YAMLTagsMatchFieldNames: the check matches keys against
// the yaml tags and the real decode against the field names, so both must be
// equal without case.
func TestUnknownConfigKeys_YAMLTagsMatchFieldNames(t *testing.T) {
	var check func(typ reflect.Type)
	check = func(typ reflect.Type) {
		for field := range typ.Fields() {
			tag := field.Tag.Get("yaml")
			if tag == "" || tag == "-" {
				continue
			}
			assert.True(t, strings.EqualFold(tag, field.Name), "%s.%s has yaml tag %q", typ.Name(), field.Name, tag)
			fieldType := field.Type
			for fieldType.Kind() == reflect.Pointer || fieldType.Kind() == reflect.Map || fieldType.Kind() == reflect.Slice {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct {
				check(fieldType)
			}
		}
	}
	check(reflect.TypeFor[domain.Config]())
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

// TestUnknownEnvironmentVariables_AllowsComposeExample keeps the example Compose
// file and the variables that applyEnvironment reads in sync.
func TestUnknownEnvironmentVariables_AllowsComposeExample(t *testing.T) {
	compose, err := os.ReadFile("../../docker-compose.yml")
	require.NoError(t, err)
	names := regexp.MustCompile(`SEASONPACKARR__[A-Z0-9_]+`).FindAllString(string(compose), -1)
	require.NotEmpty(t, names)
	environ := make([]string, 0, len(names))
	for _, name := range names {
		environ = append(environ, name+"=value")
	}
	require.Empty(t, UnknownEnvironmentVariables(environ))
}

// TestFindConfigFile_ImageWorkingDirectoryIsConfigVolume keeps docker exec
// working without --config: the image starts in the config volume, and
// discovery checks ./config.yaml first.
func TestFindConfigFile_ImageWorkingDirectoryIsConfigVolume(t *testing.T) {
	t.Run("discovery checks the working directory first", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		homeConfig := filepath.Join(home, ".config", "seasonpackarr", "config.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(homeConfig), 0o755))
		writeConfigFile(t, homeConfig, "clients: {}\n")
		workingDir := t.TempDir()
		writeConfigFile(t, filepath.Join(workingDir, "config.yaml"), "clients: {}\n")
		t.Chdir(workingDir)

		configFile, err := findConfigFile()
		require.NoError(t, err)
		assert.Equal(t, "./config.yaml", configFile)
	})
	for _, dockerfile := range []string{"Dockerfile", "ci.Dockerfile"} {
		t.Run(dockerfile, func(t *testing.T) {
			contents, err := os.ReadFile(filepath.Join("../..", dockerfile))
			require.NoError(t, err)
			workdirs := regexp.MustCompile(`(?m)^WORKDIR\s+(\S+)`).FindAllStringSubmatch(string(contents), -1)
			require.NotEmpty(t, workdirs)
			assert.Equal(t, "/config", workdirs[len(workdirs)-1][1], "WORKDIR of the runner stage")
			assert.Regexp(t, `(?m)^VOLUME /config$`, string(contents))
		})
	}
}

func TestUnknownConfigKeys_AcceptsConfigExamples(t *testing.T) {
	templateDir := t.TempDir()
	require.NoError(t, (&AppConfig{}).writeConfig(templateDir, "config.yaml"))

	for name, configFile := range map[string]string{
		"config template": filepath.Join(templateDir, "config.yaml"),
		"config.yaml":     "../../config.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			k := koanf.New(".")
			require.NoError(t, k.Load(file.Provider(configFile), yaml.Parser()))
			require.NotEmpty(t, k.Keys())
			unknown, err := unknownConfigKeys(k)
			require.NoError(t, err)
			assert.Empty(t, unknown)
		})
	}
}

// fakeProvider loads a nested map into koanf.
type fakeProvider map[string]any

func (f fakeProvider) ReadBytes() ([]byte, error)    { return nil, errors.New("not supported") }
func (f fakeProvider) Read() (map[string]any, error) { return f, nil }

func TestUnknownConfigKeys_AcceptsSchemaKeys(t *testing.T) {
	contents, err := os.ReadFile("../../schemas/config-schema.json")
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(contents, &schema))
	defs, _ := schema["$defs"].(map[string]any)

	// keys turns a schema object into a map with each property it allows, and
	// one example entry for each pattern property.
	var keys func(node map[string]any) map[string]any
	keys = func(node map[string]any) map[string]any {
		if ref, ok := node["$ref"].(string); ok {
			def, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
			require.True(t, ok, "unresolved $ref %q", ref)
			return keys(def)
		}
		props, _ := node["properties"].(map[string]any)
		patterns, _ := node["patternProperties"].(map[string]any)
		if len(props) == 0 && len(patterns) == 0 {
			return nil
		}
		out := map[string]any{}
		for name, child := range props {
			if sub := keys(child.(map[string]any)); sub != nil {
				out[name] = sub
			} else {
				out[name] = nil
			}
		}
		for _, child := range patterns {
			out["example"] = keys(child.(map[string]any))
		}
		return out
	}

	all := keys(schema)
	require.Contains(t, all, "clients")
	k := koanf.New(".")
	require.NoError(t, k.Load(fakeProvider(all), nil))
	unknown, err := unknownConfigKeys(k)
	require.NoError(t, err)
	require.Empty(t, unknown)
}
