// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger/loggertest"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_MissingOptionalConfigKeysUseDefaults(t *testing.T) {
	configFile := writeTestConfig(t, `
host: "127.0.0.1"
port: 42069
clients: {}
logLevel: "INFO"
`)
	snapshot, err := loadFile(configFile)
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

func TestLoad_RejectsUnknownKeys(t *testing.T) {
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
	_, err := loadFile(configFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown config keys: clients.default.hots, clients.default.import.tag, fuzzyMatching.skipRepackCompar, logLevl")
	assert.Contains(t, err.Error(), configFile)
}

// TestLoad_IgnoresUnknownClientEnvironmentVariables keeps the promise
// of the startup warning: an unknown client variable must not add a client,
// because an empty client fails validation and stops startup.
func TestLoad_IgnoresUnknownClientEnvironmentVariables(t *testing.T) {
	configFile := writeTestConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: tv-hd
`)
	environ := []string{"SEASONPACKARR__CLIENTS_GHOST_IMPORT_SAVEPATH=/data", "SEASONPACKARR__CLIENTS__HOST=qbit"}
	require.Equal(t, []string{"SEASONPACKARR__CLIENTS_GHOST_IMPORT_SAVEPATH", "SEASONPACKARR__CLIENTS__HOST"},
		UnknownEnvironmentVariables(environ))

	snapshot, err := loadFile(configFile, environ...)
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, slices.Sorted(maps.Keys(snapshot.Clients)))
}

func TestLoad_RejectsInternalFieldsAsKeys(t *testing.T) {
	configFile := writeTestConfig(t, "clients: {}\nversion: v9\nconfigPath: /tmp\ndisableConfigFile: true\n")
	_, err := loadFile(configFile)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown config keys: configPath, disableConfigFile, version")
}

func TestLoad_KeepsCaseInsensitiveKeyMatching(t *testing.T) {
	configFile := writeTestConfig(t, `
LogLevel: "INFO"
clients:
  default:
    TYPE: qbittorrent
    Import:
      Category: tv-hd
`)
	snapshot, err := loadFile(configFile)
	require.NoError(t, err)
	assert.Equal(t, "INFO", snapshot.LogLevel)
	assert.Equal(t, "test", snapshot.Version)
	require.Contains(t, snapshot.Clients, "default")
	assert.Equal(t, "qbittorrent", snapshot.Clients["default"].Type)
	assert.Equal(t, "tv-hd", snapshot.Clients["default"].Import.Category)
}

// TestLoad_ReadsKeyCaseVariants: a case variant once competed with the
// default of the same key, and map order chose the winner. A lost apiToken
// turns off the API authentication.
func TestLoad_ReadsKeyCaseVariants(t *testing.T) {
	for _, key := range []string{"apiToken", "apitoken", "APITOKEN", "APIToken"} {
		t.Run(key, func(t *testing.T) {
			configFile := writeTestConfig(t, "clients: {}\nNOTIFICATIONS:\n  DISCORD: hook\n"+key+": secret\n")
			// One load proves nothing when map order decides the result.
			for range 20 {
				snapshot, err := loadFile(configFile)
				require.NoError(t, err)
				require.Equal(t, "secret", snapshot.APIToken)
				require.Equal(t, "hook", snapshot.Notifications.Discord)
			}
		})
	}
}

func TestLoad_KeepsMigrationErrorsForRemovedKeys(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{name: "parseTorrentFile", content: "clients: {}\nparseTorrentFile: false\n", want: "parseTorrentFile was removed"},
		{name: "metadata", content: "clients: {}\nmetadata:\n  tvdbAPIKey: key\n", want: "metadata was removed"},
		{name: "preImportPath", content: "clients:\n  default:\n    preImportPath: /data\n", want: "clients.default.preImportPath was removed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWithFile(t, tt.content)
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
		err := validateDeprecatedConfigInputs(loadTestKoanf(t, "parseTorrentFile: false\n"), environment{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "parseTorrentFile was removed")
	})

	t.Run("rejects parseTorrentFile env", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(nil, newEnvironment([]string{deprecatedParseTorrentFileEnv + "=false"}))
		require.Error(t, err)
		require.Contains(t, err.Error(), deprecatedParseTorrentFileEnv)
	})

	t.Run("rejects empty parseTorrentFile env", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(nil, newEnvironment([]string{deprecatedParseTorrentFileEnv + "="}))
		require.Error(t, err)
		require.Contains(t, err.Error(), deprecatedParseTorrentFileEnv)
	})

	t.Run("rejects preImportPath in yaml", func(t *testing.T) {
		k := loadTestKoanf(t, "clients:\n  default:\n    preImportPath: /data/tv-hd\n")
		err := validateDeprecatedConfigInputs(k, environment{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "clients.default.preImportPath")
		require.Contains(t, err.Error(), "import.savePath or import.category")
	})

	t.Run("rejects preImportPath env", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(nil, newEnvironment([]string{"SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH=/data/tv-hd"}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH")
	})

	t.Run("rejects empty preImportPath env", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(nil, newEnvironment([]string{"SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH="}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH")
	})

	t.Run("rejects metadata config", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(loadTestKoanf(t, "metadata:\n  tvdbAPIKey: old-key\n"), environment{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "metadata was removed")
		require.Contains(t, err.Error(), "/api/match")
	})

	t.Run("rejects metadata env", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(nil, newEnvironment([]string{deprecatedMetadataTVDBAPIKeyEnv + "=old-key"}))
		require.Error(t, err)
		require.Contains(t, err.Error(), deprecatedMetadataTVDBAPIKeyEnv)
		require.Contains(t, err.Error(), "/api/match")
	})

	t.Run("rejects empty metadata env", func(t *testing.T) {
		err := validateDeprecatedConfigInputs(nil, newEnvironment([]string{deprecatedMetadataTVDBPINEnv + "="}))
		require.Error(t, err)
		require.Contains(t, err.Error(), deprecatedMetadataTVDBPINEnv)
	})

	t.Run("allows current config", func(t *testing.T) {
		k := loadTestKoanf(t, "host: 0.0.0.0\nclients: {}\n")
		err := validateDeprecatedConfigInputs(k, newEnvironment([]string{"HOME=/home/user"}))
		require.NoError(t, err)
	})
}

// TestLoadFromEnv_ParsesMultiWordClientSettings guards the env-parser fix: keys
// like IMPORT_SAVE_PATH (multiple underscores) must survive, including for a
// client that only exists in the environment.
func TestLoadFromEnv_ParsesMultiWordClientSettings(t *testing.T) {
	env := newEnvironment([]string{
		"SEASONPACKARR__CLIENTS_ENVONLY_TYPE=qbittorrent",
		"SEASONPACKARR__CLIENTS_ENVONLY_IMPORT_SAVE_PATH=/data/tv-hd",
		"SEASONPACKARR__CLIENTS_ENVONLY_IMPORT_CATEGORY=tv-hd",
		"SEASONPACKARR__CLIENTS_ENVONLY_IMPORT_TAGS=a, b",
		"SEASONPACKARR__CLIENTS_ENVONLY_INVENTORY_INCLUDE_CATEGORIES=tv-hd, tv-hd.cross-seed,",
		"SEASONPACKARR__CLIENTS_ENVONLY_INVENTORY_INCLUDE_TAGS=keep",
		"SEASONPACKARR__CLIENTS_ENVONLY_INVENTORY_EXCLUDE_CATEGORIES=tv-hd-4k",
		"SEASONPACKARR__CLIENTS_ENVONLY_INVENTORY_EXCLUDE_TAGS=noreuse, trash",
	})

	cfg := domain.Config{Clients: map[string]*domain.Client{}}
	require.NoError(t, applyEnvironment(&cfg, env))

	client, ok := cfg.Clients["envonly"]
	require.True(t, ok, "env-only client should be lazily created")
	require.Equal(t, "qbittorrent", client.Type)
	require.Equal(t, "/data/tv-hd", client.Import.SavePath)
	require.Equal(t, "tv-hd", client.Import.Category)
	require.Equal(t, []string{"a", "b"}, client.Import.Tags)
	require.Equal(t, domain.InventoryScope{
		IncludeCategories: []string{"tv-hd", "tv-hd.cross-seed"},
		IncludeTags:       []string{"keep"},
		ExcludeCategories: []string{"tv-hd-4k"},
		ExcludeTags:       []string{"noreuse", "trash"},
	}, client.Inventory)
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

	rejection := reloadWithFile(t, cfg, configFile, `
clients:
  default:
    type: transmission
    import:
      category: tv-hd
logLevel: TRACE
`)
	require.Contains(t, rejection, "qBittorrent only")
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

	rejection := reloadWithFile(t, cfg, configFile, `
clients:
  default:
    type: qbittorrent
    import:
      category: tv-other
logLevel: TRACE
smartMod: true
`)
	assert.Contains(t, rejection, "unknown config keys: smartMod")
	assert.Equal(t, before, cfg.Snapshot())
}

func TestReload_ReappliesEnvironmentOverrides(t *testing.T) {
	cfg, configFile := newTestAppConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: from-file-before
`, "SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_CATEGORY=from-env")
	require.Equal(t, "from-env", cfg.Snapshot().Clients["default"].Import.Category)

	rejection := reloadWithFile(t, cfg, configFile, `
clients:
  default:
    type: qbittorrent
    import:
      category: from-file-after
`)
	require.Empty(t, rejection)
	require.Equal(t, "from-env", cfg.Snapshot().Clients["default"].Import.Category)
}

func TestReload_RestoresDefaultsForOmittedKeys(t *testing.T) {
	cfg, configFile := newTestAppConfig(t, `
clients: {}
logMaxSize: 99
`)
	require.Equal(t, 99, cfg.Snapshot().LogMaxSize)

	rejection := reloadWithFile(t, cfg, configFile, "clients: {}\n")
	require.Empty(t, rejection)
	require.Equal(t, 50, cfg.Snapshot().LogMaxSize)
}

func TestNew_ReadsDiscoveredFileAndCreatesNone(t *testing.T) {
	workingDir := t.TempDir()
	homeDir := t.TempDir()
	configFile := filepath.Join(homeDir, ".config", "seasonpackarr", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configFile), 0o755))
	writeConfigFile(t, configFile, "clients: {}\nport: 4242\n")
	t.Chdir(workingDir)

	cfg, err := New(Input{Environ: []string{"HOME=" + homeDir}, Version: "test"})

	require.NoError(t, err)
	assert.Equal(t, filepath.Dir(configFile), cfg.Snapshot().ConfigPath)
	assert.Equal(t, 4242, cfg.Snapshot().Port)
	entries, err := os.ReadDir(workingDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestSnapshot_CannotMutateStoredConfig(t *testing.T) {
	cfg, _ := newTestAppConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: tv-hd
      tags: [one, two]
    inventory:
      includeCategories: [tv-hd, tv-hd.cross-seed]
      includeTags: [keep]
      excludeCategories: [tv-hd-4k]
      excludeTags: [noreuse]
notifications:
  notificationLevel: [MATCH, ERROR]
  shoutrrr: ["ntfy://ntfy.sh/first"]
`)

	snapshot := cfg.Snapshot()
	snapshot.Clients["default"].Import.Category = "changed"
	snapshot.Clients["default"].Import.Tags[0] = "changed"
	snapshot.Clients["default"].Inventory.IncludeCategories[0] = "changed"
	snapshot.Clients["default"].Inventory.IncludeTags[0] = "changed"
	snapshot.Clients["default"].Inventory.ExcludeCategories[0] = "changed"
	snapshot.Clients["default"].Inventory.ExcludeTags[0] = "changed"
	snapshot.Notifications.NotificationLevel[0] = "changed"
	snapshot.Notifications.Shoutrrr[0] = "changed"
	delete(snapshot.Clients, "default")

	stored := cfg.Snapshot()
	require.Equal(t, "tv-hd", stored.Clients["default"].Import.Category)
	require.Equal(t, []string{"one", "two"}, stored.Clients["default"].Import.Tags)
	require.Equal(t, domain.InventoryScope{
		IncludeCategories: []string{"tv-hd", "tv-hd.cross-seed"},
		IncludeTags:       []string{"keep"},
		ExcludeCategories: []string{"tv-hd-4k"},
		ExcludeTags:       []string{"noreuse"},
	}, stored.Clients["default"].Inventory)
	require.Equal(t, []string{"MATCH", "ERROR"}, stored.Notifications.NotificationLevel)
	require.Equal(t, []string{"ntfy://ntfy.sh/first"}, stored.Notifications.Shoutrrr)
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

	t.Run("qbittorrent accepts an inventory scope", func(t *testing.T) {
		err := validateClientConfig("default", &domain.Client{
			Type:      "qbittorrent",
			Import:    domain.ImportPolicy{Category: "tv-hd"},
			Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd"}, IncludeTags: []string{"keep"}},
		})
		require.NoError(t, err)
	})

	for _, clientType := range []string{"transmission", "deluge", "deluge-v1", "deluge-v2"} {
		t.Run(clientType+" rejects an inventory scope", func(t *testing.T) {
			err := validateClientConfig("default", &domain.Client{
				Type:      clientType,
				Import:    domain.ImportPolicy{SavePath: t.TempDir()},
				Inventory: domain.InventoryScope{ExcludeTags: []string{"noreuse"}},
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must not set inventory")
		})
	}

	for name, scope := range map[string]domain.InventoryScope{
		"empty category entry is rejected":         {IncludeCategories: []string{"tv-hd", ""}},
		"blank tag entry is rejected":              {IncludeTags: []string{" "}},
		"empty exclude category entry is rejected": {ExcludeCategories: []string{""}},
		"empty exclude tag entry is rejected":      {ExcludeTags: []string{"noreuse", ""}},
		// qBittorrent compares the exact name, so " keep " would match no torrent.
		"include tag with spaces is rejected":       {IncludeTags: []string{" keep "}},
		"exclude category with a space is rejected": {ExcludeCategories: []string{"movies "}},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateClientConfig("default", &domain.Client{
				Type:      "qbittorrent",
				Import:    domain.ImportPolicy{Category: "tv-hd"},
				Inventory: scope,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "inventory list")
		})
	}

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

// TestFindConfigFile_ImageWorkingDirectoryIsConfigVolume keeps docker exec
// working without --config: the image starts in the config volume, and
// discovery checks ./config.yaml first.
func TestFindConfigFile_ImageWorkingDirectoryIsConfigVolume(t *testing.T) {
	t.Run("discovery checks the working directory first", func(t *testing.T) {
		home := t.TempDir()
		homeConfig := filepath.Join(home, ".config", "seasonpackarr", "config.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(homeConfig), 0o755))
		writeConfigFile(t, homeConfig, "clients: {}\nport: 1111\n")
		workingDir := t.TempDir()
		writeConfigFile(t, filepath.Join(workingDir, "config.yaml"), "clients: {}\nport: 2222\n")
		t.Chdir(workingDir)

		cfg, err := Load(Input{Environ: []string{"HOME=" + home}, Version: "test"})
		require.NoError(t, err)
		assert.Equal(t, 2222, cfg.Port)
		assert.Equal(t, ".", cfg.ConfigPath)
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

// newGeneratedConfigDir returns a directory with the config.yaml that start creates. The environment gives
// the default client of that file an import category, because the file alone does not pass the validation.
func newGeneratedConfigDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	_, err := New(Input{ConfigDir: dir, Environ: []string{"SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_CATEGORY=tv"}, Version: "test", CreateFile: true})
	require.NoError(t, err)
	return dir
}

func TestWriteConfig_WritesRepositoryConfigFile(t *testing.T) {
	dir := newGeneratedConfigDir(t)

	want, err := os.ReadFile("../../config.yaml")
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestUnknownConfigKeys_AcceptsConfigExamples(t *testing.T) {
	generatedDir := newGeneratedConfigDir(t)

	for name, configFile := range map[string]string{
		"generated config": filepath.Join(generatedDir, "config.yaml"),
		"config.yaml":      "../../config.yaml",
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

const configSchemaFile = "schemas/config-schema.json"

// joinKey adds key to the dotted path of its parent object.
func joinKey(path, key string) string {
	return strings.TrimPrefix(path+"."+key, ".")
}

// configSchema holds the keys of the config schema as dotted paths, with "*"
// for the name of a map entry such as a client.
type configSchema struct {
	// properties maps each key to its schema node.
	properties map[string]map[string]any
	// required maps the path of each object to the keys that it always requires.
	// The required lists inside if, then, allOf, and anyOf are cross-field rules, so they are not in it.
	required map[string][]string
}

func readConfigSchema(t *testing.T) configSchema {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("../..", configSchemaFile))
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(contents, &root))
	defs, _ := root["$defs"].(map[string]any)

	schema := configSchema{properties: map[string]map[string]any{}, required: map[string][]string{}}
	var walk func(node map[string]any, path string)
	walk = func(node map[string]any, path string) {
		if ref, ok := node["$ref"].(string); ok {
			def, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
			require.True(t, ok, "unresolved $ref %q at %q", ref, path)
			node = def
		}
		if required, ok := node["required"].([]any); ok {
			for _, key := range required {
				name, ok := key.(string)
				require.True(t, ok, "required entry %v at %q is not a string", key, path)
				schema.required[path] = append(schema.required[path], name)
			}
		}
		add := func(key string, child any) {
			key = joinKey(path, key)
			node, ok := child.(map[string]any)
			require.True(t, ok, "schema node %q is not an object", key)
			schema.properties[key] = node
			walk(node, key)
		}
		props, _ := node["properties"].(map[string]any)
		for name, child := range props {
			add(name, child)
		}
		patterns, _ := node["patternProperties"].(map[string]any)
		for _, child := range patterns {
			add("*", child)
		}
	}
	walk(root, "")
	return schema
}

// serviceConfigDefaults maps each config key, as a dotted path with "*" for the
// name of a map entry, to the value that the service uses when no source sets the key.
// The keys come from the yaml tags. A map entry, such as a client, starts as a zero value.
func serviceConfigDefaults() map[string]any {
	defaults := map[string]any{}
	var walk func(v reflect.Value, path string)
	walk = func(v reflect.Value, path string) {
		for field, value := range v.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if name == "" || name == "-" {
				continue
			}
			key := joinKey(path, name)
			defaults[key] = value.Interface()
			switch value.Kind() {
			case reflect.Struct:
				walk(value, key)
			case reflect.Map:
				entry := reflect.Zero(value.Type().Elem())
				if entry.Kind() == reflect.Pointer {
					entry = reflect.Zero(entry.Type().Elem())
				}
				defaults[key+".*"] = entry.Interface()
				if entry.Kind() == reflect.Struct {
					walk(entry, key+".*")
				}
			}
		}
	}
	walk(reflect.ValueOf(defaultConfig("test", "")), "")
	return defaults
}

func TestConfigSchema_KeysMatchConfigKeys(t *testing.T) {
	schema := readConfigSchema(t)
	defaults := serviceConfigDefaults()
	require.Contains(t, schema.properties, "clients.*.host")
	require.Contains(t, defaults, "clients.*.host")

	for _, key := range slices.Sorted(maps.Keys(defaults)) {
		assert.Contains(t, schema.properties, key, "config key %s is not in %s", key, configSchemaFile)
	}
	for _, key := range slices.Sorted(maps.Keys(schema.properties)) {
		assert.Contains(t, defaults, key, "%s has the key %s, which is not a config key", configSchemaFile, key)
	}
}

func TestConfigSchema_DefaultsMatchServiceDefaults(t *testing.T) {
	schema := readConfigSchema(t)
	defaults := serviceConfigDefaults()

	for _, key := range slices.Sorted(maps.Keys(schema.properties)) {
		schemaDefault, ok := schema.properties[key]["default"]
		serviceDefault, known := defaults[key]
		if !ok || !known {
			continue
		}
		// The JSON round trip gives the service default the types of the schema
		// default. JSON v2 writes a nil list as [].
		encoded, err := json.Marshal(serviceDefault)
		require.NoError(t, err, key)
		var want any
		require.NoError(t, json.Unmarshal(encoded, &want), key)
		assert.Equal(t, want, schemaDefault, "default of %s in %s is not the service default", key, configSchemaFile)
	}
}

func TestConfigSchema_RequiresNoOptionalKey(t *testing.T) {
	// The load requires no key on its own, as these files show. Only cross-field
	// rules require keys. The schema states them in if, then, allOf, and anyOf.
	for name, contents := range map[string]string{
		"empty file":          "",
		"client without host": "clients:\n  default:\n    import:\n      category: tv\n",
	} {
		_, err := loadWithFile(t, contents)
		assert.NoError(t, err, name)
	}

	schema := readConfigSchema(t)
	var required []string
	for path, keys := range schema.required {
		for _, key := range keys {
			required = append(required, joinKey(path, key))
		}
	}
	slices.Sort(required)
	assert.Empty(t, required, "%s requires these keys, but the config load treats them as optional", configSchemaFile)
}

// schemaAccepts checks value against the enum, pattern, minimum, maximum, and anyOf rules of node, the rules that
// TestConfigSchema_AcceptsWhatTheLoadAccepts needs. A JSON number is a float64.
func schemaAccepts(t *testing.T, node map[string]any, value any) bool {
	t.Helper()
	if enum, ok := node["enum"].([]any); ok && !slices.Contains(enum, value) {
		return false
	}
	if pattern, ok := node["pattern"].(string); ok {
		text, _ := value.(string)
		if !regexp.MustCompile(pattern).MatchString(text) {
			return false
		}
	}
	number, _ := value.(float64)
	if minimum, ok := node["minimum"].(float64); ok && number < minimum {
		return false
	}
	if maximum, ok := node["maximum"].(float64); ok && number > maximum {
		return false
	}
	if anyOf, ok := node["anyOf"].([]any); ok {
		return slices.ContainsFunc(anyOf, func(sub any) bool {
			subNode, ok := sub.(map[string]any)
			require.True(t, ok, "anyOf entry %v is not an object", sub)
			return schemaAccepts(t, subNode, value)
		})
	}
	return true
}

// TestConfigSchema_AcceptsWhatTheLoadAccepts keeps the value rules of the schema and of the load equal, so that an
// editor does not flag a config that the service loads, or accept one that stops the load.
func TestConfigSchema_AcceptsWhatTheLoadAccepts(t *testing.T) {
	schema := readConfigSchema(t)
	tests := []struct {
		key  string
		yaml string
		// value is the decoded JSON form of yaml.
		value any
	}{
		{"clients.*.import.contentLayout", `""`, ""},
		{"clients.*.import.contentLayout", "subfolder", "subfolder"},
		{"clients.*.import.contentLayout", "NoSubfolder", "NoSubfolder"},
		{"clients.*.import.contentLayout", "ORIGINAL", "ORIGINAL"},
		{"clients.*.import.contentLayout", "subfolders", "subfolders"},
		{"clients.*.port", "-1", float64(-1)},
		{"clients.*.port", "0", float64(0)},
		{"clients.*.port", "65535", float64(65535)},
		{"clients.*.port", "65536", float64(65536)},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s=%s", tt.key, tt.yaml), func(t *testing.T) {
			contents := "clients:\n  default:\n    import:\n      category: tv\n"
			if field, ok := strings.CutPrefix(tt.key, "clients.*.import."); ok {
				contents += fmt.Sprintf("      %s: %s\n", field, tt.yaml)
			} else {
				contents += fmt.Sprintf("    %s: %s\n", strings.TrimPrefix(tt.key, "clients.*."), tt.yaml)
			}
			node, ok := schema.properties[tt.key]
			require.True(t, ok, "%s has no key %s", configSchemaFile, tt.key)

			_, err := loadWithFile(t, contents)

			assert.Equal(t, err == nil, schemaAccepts(t, node, tt.value),
				"%s and the load disagree on %s: %s (load error: %v)", configSchemaFile, tt.key, tt.yaml, err)
		})
	}
}

func TestLoad_RejectsInvalidShoutrrrURL(t *testing.T) {
	tests := []struct {
		name, url, destination, secret string
	}{
		{"gotify token format", "gotify://gotify.example/sentinel-invalid-gotify-token", "(gotify)", "sentinel-invalid-gotify-token"},
		{"telegram token without bot ID", "telegram://sentinel-telegram-token@telegram?chats=1", "(telegram)", "sentinel-telegram-token"},
		{"unknown service", "unknownservice://sentinel-user:sentinel-password@host/path", "(unknownservice)", "sentinel-password"},
		{"port is not a number", "gotify://gotify.example:sentinel-port/Asentinelgotify", "(gotify)", "sentinel-port"},
		{"no scheme", "sentinel-no-scheme", "(invalid scheme)", "sentinel-no-scheme"},
		{"ntfy priority", "ntfy://ntfy.sh/sentinel-ntfy-topic?priority=sentinel-priority", "(ntfy)", "sentinel-ntfy"},
		{"discord without webhook ID", "discord://sentinel-discord-token@", "(discord)", "sentinel-discord-token"},
		{"discord color is quoted", "discord://sentinel-discord-token@123?color=sentinel-color", "(discord)", "sentinel-color"},
		{"generic token in the query", "generic://host/hook?token=sentinel-generic-token&disabletls=maybe", "(generic)", "sentinel-generic-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configFile := writeTestConfig(t, fmt.Sprintf(`
notifications:
  shoutrrr:
    - ntfy://ntfy.sh/valid-topic
    - %q
`, tt.url))

			_, err := loadFile(configFile)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "notifications.shoutrrr[1] "+tt.destination)
			assert.NotContains(t, err.Error(), tt.secret)
			assert.NotContains(t, err.Error(), "valid-topic")
		})
	}
}

func TestReload_RejectsInvalidShoutrrrURLAndKeepsLastSnapshot(t *testing.T) {
	cfg, configFile := newTestAppConfig(t, `
notifications:
  shoutrrr: ["ntfy://ntfy.sh/first"]
`)
	before := cfg.Snapshot()

	rejection := reloadWithFile(t, cfg, configFile, `
notifications:
  shoutrrr: ["ntfy://ntfy.sh/first", "gotify://gotify.example/invalid"]
`)

	require.Contains(t, rejection, "notifications.shoutrrr[1] (gotify)")
	require.Equal(t, before, cfg.Snapshot())
}

func TestLoad_SplitsShoutrrrEnvironmentOnWhitespace(t *testing.T) {
	configFile := writeTestConfig(t, `
notifications:
  shoutrrr: ["ntfy://ntfy.sh/from-file"]
`)

	snapshot, err := loadFile(configFile, "SEASONPACKARR__NOTIFICATIONS_SHOUTRRR= ntfy://ntfy.sh/first\ttelegram://123:abc@telegram?chats=1,2  ")

	require.NoError(t, err)
	assert.Equal(t, []string{"ntfy://ntfy.sh/first", "telegram://123:abc@telegram?chats=1,2"}, snapshot.Notifications.Shoutrrr)
}

func TestLogInventoryScopeWarnings(t *testing.T) {
	tests := map[string]struct {
		client domain.Client
		warn   bool
	}{
		"no scope": {
			client: domain.Client{Import: domain.ImportPolicy{Category: "tv-hd"}},
		},
		"scope includes the import category": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd"},
				Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd-completed", "tv-hd"}},
			},
		},
		"scope includes an import tag": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr", "tv"}},
				Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd-completed"}, IncludeTags: []string{"tv"}},
			},
		},
		"scope misses the import category and tags": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr"}},
				Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd-completed"}},
			},
			warn: true,
		},
		"exclude-only scope keeps the import category": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr"}},
				Inventory: domain.InventoryScope{ExcludeCategories: []string{"movies"}},
			},
		},
		// qBittorrent reads tv/hd for tv only when subcategories are on, which the config cannot see.
		"include parent of the import subcategory": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv/hd"},
				Inventory: domain.InventoryScope{IncludeCategories: []string{"tv"}},
			},
			warn: true,
		},
		"exclude of the import category wins over an include tag": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr"}},
				Inventory: domain.InventoryScope{IncludeTags: []string{"seasonpackarr"}, ExcludeCategories: []string{"tv-hd"}},
			},
			warn: true,
		},
		"exclude of an import tag": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: "tv-hd", Tags: []string{"seasonpackarr"}},
				Inventory: domain.InventoryScope{ExcludeTags: []string{"seasonpackarr"}},
			},
			warn: true,
		},
		// The add trims the import category and tags, so the warning compares the trimmed values.
		"scope includes the trimmed import tag": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: " tv-hd ", Tags: []string{" keep ", " "}},
				Inventory: domain.InventoryScope{IncludeTags: []string{"keep"}},
			},
		},
		"scope includes the trimmed import category": {
			client: domain.Client{
				Import:    domain.ImportPolicy{Category: " tv-hd "},
				Inventory: domain.InventoryScope{IncludeCategories: []string{"tv-hd"}},
			},
		},
		"scope with an import savePath only": {
			client: domain.Client{
				Import:    domain.ImportPolicy{SavePath: "/data/tv-hd"},
				Inventory: domain.InventoryScope{IncludeTags: []string{"keep"}},
			},
			warn: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			log := loggertest.New()

			LogInventoryScopeWarnings(log, domain.Config{Clients: map[string]*domain.Client{"default": &tt.client}})

			if !tt.warn {
				assert.Empty(t, log.String())
				return
			}
			assert.Contains(t, log.String(), `"level":"warn"`)
			assert.Contains(t, log.String(), `"clientname":"default"`)
			assert.Contains(t, log.String(), "inventory scope does not select new packs")
		})
	}
}

// would replace the scope of config.yaml with an empty list, which reads every torrent.
func TestLoad_RejectsInventoryEnvironmentWithoutEntries(t *testing.T) {
	for _, value := range []string{" ", ",", " , "} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			configFile := writeTestConfig(t, `
clients:
  default:
    type: qbittorrent
    import:
      category: tv-hd
    inventory:
      excludeCategories: [movies]
`)

			_, err := loadFile(configFile, "SEASONPACKARR__CLIENTS_DEFAULT_INVENTORY_EXCLUDE_CATEGORIES="+value)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "SEASONPACKARR__CLIENTS_DEFAULT_INVENTORY_EXCLUDE_CATEGORIES")
		})
	}
}
