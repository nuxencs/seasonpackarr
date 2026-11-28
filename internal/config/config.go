// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/shoutrrrurl"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"

	"github.com/fsnotify/fsnotify"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// defaultConfigFile is the file that the first start writes. It must equal config.yaml at the repository root (TestWriteConfig_WritesRepositoryConfigFile).
const defaultConfigFile = `# yaml-language-server: $schema=https://raw.githubusercontent.com/nuxencs/seasonpackarr/develop/schemas/config-schema.json
# config.yaml

# Hostname / IP
# CLI commands reuse this address. Wildcard addresses become loopback addresses.
#
# Default: "0.0.0.0"
#
host: "0.0.0.0"

# Port
#
# Default: 42069
#
port: 42069

# Optional Prowlarr discovery: RSS monitoring and search.
search:
  # Empty: all enabled torrent indexers that support the discovery mode.
  indexerIDs: []
  prowlarrURL: ""
  apiKey: ""
  # 0s disables RSS monitoring. Set to 15m to enable it. Minimum: 10m.
  rssInterval: "0s"
  # Minimum spacing between Prowlarr requests: 10s.
  requestInterval: "10s"

clients:
  # Client name used in the autobrr filter, can be customized to whatever you like
  # Note that a client name has to be unique and can only be used once
  #
  # Default: default
  #
  default:
    # Client type
    # Supported values: "qbittorrent" (default), "transmission", "deluge-v1", "deluge-v2"
    # "deluge" is an alias for "deluge-v2".
    #
    # Default: "qbittorrent"
    #
    type: "qbittorrent"

    # Hostname / IP
    #
    # Default: "127.0.0.1"
    #
    host: "127.0.0.1"

    # Port
    # qBittorrent listens on 8080 by default, Transmission on 9091, Deluge daemon RPC on 58846
    #
    # Default: 8080
    #
    port: 8080

    # Username
    #
    # Default: "admin"
    #
    username: "admin"

    # Password
    #
    # Default: "adminadmin"
    #
    password: "adminadmin"

    # API Key (qBittorrent only)
    # Requires qBittorrent 5.2.0 or newer. If set, apiKey is used instead of username/password authentication.
    #
    # Optional
    #
    apiKey: ""

    # Import Policy
    # Controls how /api/import re-imports the matched season pack back into the client
    # after the local episodes have been hardlinked into place.
    #
    import:
      # Save Path
      # Final import destination. Used both as the hardlink root and as the client's save directory.
      # Optional for qBittorrent (follows its automatic-management and manual category-path preferences)
      # and for transmission (falls back to the session download dir). Required for Deluge.
      # When set it must be an absolute path that already exists.
      # A qBittorrent client must configure either savePath or category.
      #
      # Default: ""
      #
      savePath: ""

      # Tags
      # Tags (qBittorrent) / labels (Transmission) applied to the imported torrent.
      # Deluge accepts at most one entry through its optional Label plugin.
      #
      # Optional
      #
      tags: [ "seasonpackarr" ]

      # Category (qBittorrent only)
      # Category to add the torrent with. Also used to resolve the import destination when savePath is empty.
      #
      # Default: ""
      #
      category: ""

      # Download Path (qBittorrent only)
      # Temporary (incomplete) download path. Never used as the final import destination.
      #
      # Optional
      #
      # downloadPath: ""

      # Content Layout (qBittorrent only)
      # Options: "subfolder", "nosubfolder", "original"; empty defers to the qBittorrent default.
      #
      # Optional
      #
      # contentLayout: "subfolder"

    # Inventory Scope (qBittorrent only)
    # Limits the torrents that seasonpackarr reads from the client for the candidate check, the match, and discovery.
    # With includes, only torrents with an include category or an include tag are read. Without includes, every torrent is read.
    # Torrents with an exclude category or an exclude tag are then dropped. An exclude always wins.
    # Includes make the read from the client smaller. Excludes only drop torrents after the read.
    # Keep every category and tag where season packs end up, for example the import category.
    # Episodes outside the scope are not reused. See the configuration guide before you set a scope.
    #
    # Optional
    #
    inventory:
      includeCategories: []
      includeTags: []
      excludeCategories: []
      excludeTags: []

    # Source Folders
    # Absolute paths of folders whose episode files join the inventory of this client, for example a library of Usenet downloads.
    # seasonpackarr reads the release name of each file from its file name only, and reads the folders one time at startup.
    # A folder that does not exist gives a warning, not an error. Read the configuration guide before you add a folder.
    #
    # Optional
    #
    sourceFolders: []

  # Below you can find an example on how to define a second qBittorrent client
  # If you want to define even more clients just copy this segment and adjust the values accordingly
  #
  #multi_client_example:
  #  type: "qbittorrent"
  #
  #  host: "127.0.0.1"
  #
  #  port: 9090
  #
  #  username: "example"
  #
  #  password: "example"
  #
  #  apiKey: ""
  #
  #  import:
  #    category: "tv-hd"
  #    savePath: ""
  #    tags: [ "seasonpackarr" ]

  # Example Transmission client configuration
  # Transmission listens on port 9091 by default, so set the port accordingly.
  # Transmission has no categories or content layout, so configure import.savePath
  # (or leave it empty to use the session download dir).
  #
  #transmission_example:
  #  type: "transmission"
  #
  #  host: "127.0.0.1"
  #
  #  port: 9091
  #
  #  username: ""
  #
  #  password: ""
  #
  #  import:
  #    savePath: "/data/torrents/tv-hd"
  #    tags: [ "seasonpackarr" ]

  # Example Deluge 2 client configuration. Use "deluge-v1" for Deluge 1.3.
  # Connects to the native daemon RPC port, not the Deluge Web port.
  # import.savePath is required. Enable Deluge's Label plugin to apply import.tags.
  #
  #deluge_example:
  #  type: "deluge-v2"
  #
  #  host: "127.0.0.1"
  #
  #  port: 58846
  #
  #  username: "localclient"
  #
  #  password: "change-me"
  #
  #  import:
  #    savePath: "/data/torrents/tv-hd"
  #    tags: [ "seasonpackarr" ]

# seasonpackarr logs file
# If not defined, logs to stdout
# Make sure to use forward slashes and include the filename with extension. eg: "logs/seasonpackarr.log", "C:/seasonpackarr/logs/seasonpackarr.log"
#
# Optional
#
# logPath: ""

# Log level
#
# Default: "DEBUG"
#
# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"
#
logLevel: "DEBUG"

# Log Max Size
# Max log size in megabytes
#
# Default: 50
#
# logMaxSize: 50

# Log Max Backups
# Max amount of old log files
#
# Default: 3
#
# logMaxBackups: 3

# Smart Mode
# Toggles smart mode to accept only packs with enough reusable torrent episode files already in the client
#
# Default: false
#
# smartMode: false

# Smart Mode Threshold
# Sets the minimum reusable share of distinct valid episode files in the announced torrent
# In this example 75% of the torrent episode files must be reusable for the pack to be accepted
#
# Default: 0.75
#
# smartModeThreshold: 0.75

# Fuzzy Matching
# You can decide for which criteria the matching should be less strict, e.g. repack status and HDR format
#
fuzzyMatching:
  # Skip Repack Compare
  # Toggle comparing of the repack status of a release, e.g. repacked episodes will be treated the same as a non-repacked ones
  #
  # Default: false
  #
  skipRepackCompare: false

  # Simplify HDR Compare
  # Toggle simplification of HDR formats for comparing, e.g. HDR10+ will be treated the same as HDR
  #
  # Default: false
  #
  simplifyHdrCompare: false

  # Simplify WEB Compare
  # Toggle simplification of WEB-DL for comparing, e.g. WEB-DL will be treated the same as WEB
  #
  # Default: false
  #
  simplifyWebCompare: false

  # Skip Year Compare
  # Toggle comparing of the year of a release, e.g. a release without a year will be treated the same as a release with a year
  #
  # Default: false
  #
  skipYearCompare: false

# API Token
# CLI commands read this token automatically. --api or SEASONPACKARR__API_TOKEN overrides it.
# If not defined, removes api authentication
#
# Optional
#
# apiToken: ""

# Notifications
# You can decide which notifications you want to receive
#
notifications:
  # Notification Level
  # Decides what notifications you want to receive
  #
  # Default: [ "MATCH", "ERROR" ]
  #
  # Options: "MATCH", "INFO", "ERROR"
  #
  # Examples:
  # [ "MATCH", "INFO", "ERROR" ] would send everything
  # [ "MATCH", "INFO" ] would send all matches and rejection infos
  # [ "MATCH", "ERROR" ] would send all matches and errors
  # [ "ERROR" ] would only send all errors
  #
  notificationLevel: [ "MATCH", "ERROR" ]

  # Discord
  # Uses the given Discord webhook to send notifications for various events
  #
  # Optional
  #
  discord: ""

  # Shoutrrr
  # Sends each notification to every Shoutrrr URL in the list, for example Gotify, ntfy, Pushover, or Telegram
  # URL format: https://shoutrrr.nickfedor.com/latest/services/overview/
  # A discord:// URL sends plain text. Use the discord key above for embeds.
  #
  # Optional
  #
  # Example:
  # shoutrrr: [ "gotify://gotify.example.com/token", "ntfy://ntfy.sh/topic" ]
  #
  shoutrrr: []
`

// writeConfig creates configFile with the content of config.yaml. It keeps an existing file.
func writeConfig(configFile string) (err error) {
	if err := os.MkdirAll(filepath.Dir(configFile), os.ModePerm); err != nil {
		return err
	}

	// The load reports a file that it cannot read.
	if _, err := os.Stat(configFile); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	f, err := os.Create(configFile) // perm 0666
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()
	if _, err := f.WriteString(defaultConfigFile); err != nil {
		return err
	}
	return f.Sync()
}

type Provider interface {
	Snapshot() domain.Config
}

// AppConfig holds the current snapshot. Each reload reads the same source, with the environment that New received.
type AppConfig struct {
	current atomic.Pointer[domain.Config]
	source  source
	version string
}

// New loads the config as Load does and returns a holder that can reload it.
// With in.CreateFile, New first creates a missing config.yaml in in.ConfigDir.
func New(in Input) (*AppConfig, error) {
	if in.CreateFile && in.ConfigDir == "" {
		return nil, errors.New("config: CreateFile needs a ConfigDir")
	}
	src, snapshot, err := fullLoad(in)
	if err != nil {
		return nil, err
	}
	c := &AppConfig{source: src, version: in.Version}
	c.current.Store(snapshot)
	return c, nil
}

// Load reads and validates the config as the service does at startup, from the
// config file and the environment of in. It creates no file and starts no watcher.
func Load(in Input) (domain.Config, error) {
	if in.CreateFile {
		return domain.Config{}, errCreateFileOnlyNew
	}
	_, snapshot, err := fullLoad(in)
	if err != nil {
		return domain.Config{}, err
	}
	return *snapshot, nil
}

// fullLoad is the one full load of Load and New. ReadConnectionSettings shares only newSource with it.
func fullLoad(in Input) (source, *domain.Config, error) {
	if err := checkLegacyConfigFile(in.ConfigDir); err != nil {
		return source{}, nil, err
	}
	src, err := newSource(in)
	if err != nil {
		return source{}, nil, err
	}
	if in.CreateFile && !src.disabled {
		if err := writeConfig(src.configFile); err != nil {
			return source{}, nil, fmt.Errorf("writing config file: %w", err)
		}
	}
	snapshot, err := src.load(in.Version)
	if err != nil {
		return source{}, nil, err
	}
	return src, snapshot, nil
}

// checkLegacyConfigFile rejects a config.toml in configDir, which v0.x read.
func checkLegacyConfigFile(configDir string) error {
	if _, err := os.Stat(filepath.Join(configDir, "config.toml")); err != nil {
		return nil
	}
	return errors.New("A legacy 'config.toml' file has been detected. " +
		"To continue, please migrate your configuration to the new 'config.yaml' format. " +
		"You can easily do this by copying the settings from 'config.toml' to 'config.yaml' and then renaming 'config.toml' to 'config.toml.old'. " +
		"The only difference between the old and the new config is, that the qbit client info is now stored in an array to allow for multiple clients to be configured.")
}

const (
	deprecatedParseTorrentFileKey   = "parseTorrentFile"
	deprecatedParseTorrentFileEnv   = "SEASONPACKARR__PARSE_TORRENT_FILE"
	deprecatedPreImportPathKey      = "preImportPath"
	deprecatedPreImportPathEnv      = "PREIMPORTPATH"
	deprecatedMetadataKey           = "metadata"
	deprecatedMetadataTVDBAPIKeyEnv = "SEASONPACKARR__METADATA_TVDB_API_KEY"
	deprecatedMetadataTVDBPINEnv    = "SEASONPACKARR__METADATA_TVDB_PIN"
)

// validateDeprecatedConfigInputs hard-fails when removed config keys or env vars
// are still present, so operators are told to migrate instead of silently
// getting the old behavior. parseTorrentFile and preImportPath were removed:
// torrent parsing is always on and the import destination now comes from the
// per-client import policy.
func validateDeprecatedConfigInputs(k *koanf.Koanf, env environment) error {
	if k != nil && k.Exists(deprecatedParseTorrentFileKey) {
		return fmt.Errorf("deprecated config detected: %s was removed; torrent parsing is always enabled now - remove the key, add the new per-client import section (see the example config / README) and update your autobrr filter to use a single Webhook action",
			deprecatedParseTorrentFileKey)
	}

	if _, ok := env.lookup(deprecatedParseTorrentFileEnv); ok {
		return fmt.Errorf("deprecated environment variable detected: %s was removed; torrent parsing is always enabled now - remove this env var, add the new per-client import section (see the example config / README) and update your autobrr filter to use a single Webhook action",
			deprecatedParseTorrentFileEnv)
	}

	if k != nil && k.Exists(deprecatedMetadataKey) {
		return fmt.Errorf("deprecated config detected: %s was removed; smart mode now uses the actual torrent sent to /api/match - remove the metadata block and update the autobrr external filters as described in the README",
			deprecatedMetadataKey)
	}

	for _, envKey := range []string{deprecatedMetadataTVDBAPIKeyEnv, deprecatedMetadataTVDBPINEnv} {
		if _, ok := env.lookup(envKey); ok {
			return fmt.Errorf("deprecated environment variable detected: %s was removed; smart mode now uses the actual torrent sent to /api/match - remove this variable and update the autobrr external filters as described in the README",
				envKey)
		}
	}

	if k != nil {
		for _, clientName := range k.MapKeys("clients") {
			clientPreImportPathKey := fmt.Sprintf("clients.%s.%s", clientName, deprecatedPreImportPathKey)
			if k.Exists(clientPreImportPathKey) {
				return fmt.Errorf("deprecated config detected: %s was removed; use import.savePath or import.category as the final destination instead",
					clientPreImportPathKey)
			}
		}
	}

	for variable := range env.variables() {
		if strings.HasPrefix(variable.folded, envPrefix+"CLIENTS_") && strings.HasSuffix(variable.folded, "_"+deprecatedPreImportPathEnv) {
			return fmt.Errorf("deprecated environment variable detected: %s was removed; use the import save path/category environment variables instead",
				variable.name)
		}
	}

	return nil
}

// validateClientConfig checks the rules that need no client type, asks the
// torrent client package for the others, and verifies that configured local
// paths exist.
func validateClientConfig(clientName string, client *domain.Client) error {
	// 0 is the unset port, whose meaning depends on the client type, so only the values outside every port range stop the load.
	if client.Port < 0 || client.Port > 65535 {
		return fmt.Errorf("port for client %q (%sCLIENTS_%s_PORT) must be from 0 to 65535", clientName, envPrefix, strings.ToUpper(clientName))
	}
	if err := torrentclient.Validate(*client); err != nil {
		return fmt.Errorf("client %q: %w", clientName, err)
	}

	if err := validateSourceFolders(clientName, client.SourceFolders); err != nil {
		return err
	}

	imp := client.Import
	if imp.SavePath != "" {
		if _, err := os.Stat(imp.SavePath); errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("import.savePath for client %q doesn't exist, please make sure you entered the correct path", clientName)
		}
	}

	if imp.DownloadPath != "" {
		if _, err := os.Stat(imp.DownloadPath); errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("import.downloadPath for client %q doesn't exist, please make sure you entered the correct path", clientName)
		}
	}

	return nil
}

// validateSourceFolders rejects a relative path, because the scan result would depend on the working directory.
// A folder that does not exist is no error: a mount can come up after the start, and the scan warns about it.
func validateSourceFolders(clientName string, folders []string) error {
	setting := fmt.Sprintf("sourceFolders for client %q (%sCLIENTS_%s_SOURCE_FOLDERS)", clientName, envPrefix, strings.ToUpper(clientName))
	for _, folder := range folders {
		if strings.TrimSpace(folder) == "" {
			return fmt.Errorf("%s has an empty entry; remove it", setting)
		}
		if !filepath.IsAbs(folder) {
			return fmt.Errorf("%s: %q is not an absolute path", setting, folder)
		}
	}
	return nil
}

// LogInventoryScopeWarnings warns about each client whose inventory scope does
// not select a torrent with the import category and import tags. A new pack
// stays in the import category until the arr moves it, so a second announce of
// the same pack does not find it and seasonpackarr imports a duplicate. It is
// only a warning, because a scope of the arr's completed categories can be correct.
func LogInventoryScopeWarnings(log logger.Logger, cfg domain.Config) {
	for _, name := range slices.Sorted(maps.Keys(cfg.Clients)) {
		client := cfg.Clients[name]
		if client == nil || torrentclient.InventorySelectsImports(*client) {
			continue
		}
		log.Warn().Str("clientname", name).Str("import_category", client.Import.Category).Strs("import_tags", client.Import.Tags).
			Msg("inventory scope does not select new packs, which have the import category and import tags; a second announce of a pack that is still in the import category is not found (see the configuration guide)")
	}
}

// defaultConfig copies the declared defaults and adds the runtime fields and an empty client map.
func defaultConfig(version, configFile string) domain.Config {
	cfg := configDefaults.Clone()
	cfg.Version = version
	if configFile != "" {
		cfg.ConfigPath = filepath.Dir(configFile)
	}
	return cfg
}

// load reads the config file and the environment of s into a validated snapshot. A reload calls it again with the same source.
func (s source) load(version string) (*domain.Config, error) {
	cfg := defaultConfig(version, s.configFile)

	// The defaults live only in cfg. As keys next to the file keys, a default and a
	// case variant from the file would both match one field, and map order would pick.
	fileK := koanf.New(".")
	if !s.disabled {
		if err := fileK.Load(file.Provider(s.configFile), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("reading config file %s: %w", s.configFile, err)
		}
	}
	if err := validateDeprecatedConfigInputs(fileK, s.env); err != nil {
		return nil, fmt.Errorf("validating config file %s: %w", s.configFile, err)
	}
	if err := fileK.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("decoding config file %s: %w", s.configFile, err)
	}
	unknown, err := unknownConfigKeys(fileK)
	if err != nil {
		return nil, fmt.Errorf("checking config keys in %s: %w", s.configFile, err)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("validating config file %s: unknown config keys: %s; remove or rename them (see the configuration guide)",
			s.configFile, strings.Join(unknown, ", "))
	}

	if err := applyEnvironment(&cfg, s.env); err != nil {
		return nil, err
	}
	normalizeConfig(&cfg)
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("validating config file %s: %w", s.configFile, err)
	}

	snapshot := cfg.Clone()
	return &snapshot, nil
}

var unusedKeyPath = strings.NewReplacer("[", ".", "]", "")

// unknownConfigKeys returns the sorted dotted paths of the file keys that no
// config field uses.
func unknownConfigKeys(fileK *koanf.Koanf) ([]string, error) {
	var (
		md      mapstructure.Metadata
		discard domain.Config
	)
	// The yaml tag gives the paths their YAML names. The real decode matches field
	// names instead, which is the same without case (TestUnknownConfigKeys_YAMLTagsMatchFieldNames).
	// WeaklyTypedInput is the koanf default, so values that the real decode accepts decode here too.
	dc := &mapstructure.DecoderConfig{Metadata: &md, WeaklyTypedInput: true}
	if err := fileK.UnmarshalWithConf("", &discard, koanf.UnmarshalConf{Tag: "yaml", DecoderConfig: dc}); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(md.Unused))
	for _, key := range md.Unused {
		keys = append(keys, unusedKeyPath.Replace(key))
	}
	slices.Sort(keys)
	return keys, nil
}

// normalizeConfig runs after the file and the environment are merged, so both sources get the same result.
func normalizeConfig(cfg *domain.Config) {
	for _, client := range cfg.Clients {
		if client != nil {
			client.Import.ContentLayout = strings.ToLower(client.Import.ContentLayout)
			client.SourceFolders = cleanSourceFolders(client.SourceFolders)
		}
	}
}

// cleanSourceFolders cleans each absolute path and removes a path that is in the list twice. It keeps the other
// entries as written, so the validation error names the text of the config.
func cleanSourceFolders(folders []string) []string {
	var cleaned []string
	for _, folder := range folders {
		if filepath.IsAbs(folder) {
			folder = filepath.Clean(folder)
		}
		if !slices.Contains(cleaned, folder) {
			cleaned = append(cleaned, folder)
		}
	}
	return cleaned
}

// validateConfig runs after the merge, so its range rules cover the file and the environment.
func validateConfig(cfg domain.Config) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("port (SEASONPACKARR__PORT) must be from 1 to 65535")
	}
	if cfg.LogMaxSize < 1 {
		return errors.New("logMaxSize (SEASONPACKARR__LOG_MAX_SIZE) must be at least 1")
	}
	if cfg.LogMaxBackups < 0 {
		return errors.New("logMaxBackups (SEASONPACKARR__LOG_MAX_BACKUPS) must be at least 0; 0 keeps all old log files")
	}
	// The negated form also rejects NaN, which every comparison fails.
	if !(cfg.SmartModeThreshold > 0 && cfg.SmartModeThreshold <= 1) {
		return errors.New("smartModeThreshold (SEASONPACKARR__SMART_MODE_THRESHOLD) must be greater than 0 and at most 1")
	}
	if err := validateSearch(cfg.Search); err != nil {
		return err
	}
	if err := validateNotifications(cfg.Notifications); err != nil {
		return err
	}
	for clientName, client := range cfg.Clients {
		if client == nil {
			return fmt.Errorf("client %q cannot be null", clientName)
		}
		if err := validateClientConfig(clientName, client); err != nil {
			return err
		}
	}
	return nil
}

// validateNotifications names an invalid Shoutrrr URL by its list position and
// scheme only, because the URL can hold a token.
func validateNotifications(notifications domain.Notifications) error {
	for i, rawURL := range notifications.Shoutrrr {
		if err := shoutrrrurl.Validate(rawURL); err != nil {
			return fmt.Errorf("notifications.%s: invalid Shoutrrr URL: %w", shoutrrrurl.Name(i, rawURL), err)
		}
	}
	return nil
}

func (c *AppConfig) Snapshot() domain.Config {
	snapshot := c.current.Load()
	if snapshot == nil {
		return domain.Config{}
	}
	return snapshot.Clone()
}

func (c *AppConfig) reload() (*domain.Config, error) {
	configFile := c.source.configFile
	if !c.source.disabled {
		contents, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("reading config file %s: %w", configFile, err)
		}
		// A start accepts an empty file as all defaults, because no previous snapshot exists. On a reload, an
		// empty file is almost always a write in progress, and the previous snapshot is the config to keep.
		if len(bytes.TrimSpace(contents)) == 0 {
			return nil, fmt.Errorf("config file %s is empty", configFile)
		}
	}

	snapshot, err := c.source.load(c.version)
	if err != nil {
		return nil, err
	}
	c.current.Store(snapshot)
	return snapshot, nil
}

const configReloadDebounceDelay = 100 * time.Millisecond

// Watch reloads the config after each change of the config file until ctx ends. It returns after the file watcher is set up.
// After each accepted reload, it calls each party in argument order with its own copy of the new snapshot. The parties
// run on the reload goroutine, one reload at a time. A rejected reload calls no party, and no party runs at the start.
// After ctx ends, no new reload starts. A reload in progress can finish, and Watch does not wait for it.
// With the config file disabled, Watch starts nothing.
func (c *AppConfig) Watch(ctx context.Context, log logger.Logger, parties ...func(domain.Config)) error {
	if c.source.disabled {
		return nil
	}

	changes, err := watchConfigFile(ctx, log, c.source.configFile)
	if err != nil {
		return fmt.Errorf("watching config file %s: %w", c.source.configFile, err)
	}
	go c.reloadOnChange(ctx, log, changes, parties)
	return nil
}

// watchConfigFile reports each change of configFile on the returned channel until ctx ends, then closes the watcher.
// One pending signal stands for any number of changes, so the watcher never blocks on a busy reload.
func watchConfigFile(ctx context.Context, log logger.Logger, configFile string) (<-chan struct{}, error) {
	watchedPath := filepath.Clean(configFile)
	realPath, err := filepath.EvalSymlinks(watchedPath)
	if err != nil {
		return nil, err
	}
	realPath = filepath.Clean(realPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(filepath.Dir(watchedPath)); err != nil {
		_ = watcher.Close()
		return nil, err
	}

	changes := make(chan struct{}, 1)
	changed := func() {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
	go func() {
		defer func() {
			if err := watcher.Close(); err != nil {
				log.Error().Err(err).Msg("error closing config file watcher")
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				eventPath := filepath.Clean(event.Name)
				currentRealPath, err := filepath.EvalSymlinks(watchedPath)
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						onWatchedFile := eventPath == watchedPath || eventPath == realPath
						if onWatchedFile && event.Has(fsnotify.Remove|fsnotify.Rename) {
							changed()
						}
					} else {
						log.Error().Err(err).Msg("error resolving watched config file")
					}
					continue
				}
				currentRealPath = filepath.Clean(currentRealPath)

				onWatchedFile := eventPath == watchedPath || eventPath == realPath
				targetChanged := currentRealPath != realPath
				if targetChanged || (onWatchedFile && event.Has(fsnotify.Create|fsnotify.Write)) {
					realPath = currentRealPath
					changed()
				}
			case watchErr, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error().Err(watchErr).Msg("error watching config file")
			}
		}
	}()

	return changes, nil
}

// reloadOnChange reloads the config when no change signal arrived for configReloadDebounceDelay, until ctx ends.
// Editors and bind mounts can emit several write events for one save. The quiet period prevents a transient rejection
// of a truncate-first write. It also prevents duplicate reloads from an event burst. Reloads run one at a time here.
func (c *AppConfig) reloadOnChange(ctx context.Context, log logger.Logger, changes <-chan struct{}, parties []func(domain.Config)) {
	timer := time.NewTimer(configReloadDebounceDelay)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
			timer.Reset(configReloadDebounceDelay)
		case <-timer.C:
			// select picks at random when ctx also ended, and no reload may start after the stop.
			if ctx.Err() != nil {
				return
			}
			// select also picks at random when a change is ready too, and a late wake-up can let one queue. Either way the
			// file is not quiet yet, so the change restarts the quiet period.
			select {
			case <-changes:
				timer.Reset(configReloadDebounceDelay)
				continue
			default:
			}
			snapshot, err := c.reload()
			if err != nil {
				log.Error().Err(err).Msg("config reload rejected; keeping previous config")
				continue
			}
			// A copy per party keeps a change by one party out of the next party and the stored snapshot.
			for _, party := range parties {
				party(snapshot.Clone())
			}
			log.Debug().Msg("config file reloaded")
		}
	}
}
