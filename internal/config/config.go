// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/shoutrrrurl"

	"github.com/fsnotify/fsnotify"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

var configTemplate = `# yaml-language-server: $schema=https://raw.githubusercontent.com/nuxencs/seasonpackarr/develop/schemas/config-schema.json
# config.yaml

# Hostname / IP
# CLI commands reuse this address. Wildcard addresses become loopback addresses.
#
# Default: "0.0.0.0"
#
host: "{{ .host }}"

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
      # When set it must already exist. A qBittorrent client must configure either savePath or category.
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

func (c *AppConfig) writeConfig(configPath string, configFile string) error {
	cfgPath := filepath.Join(configPath, configFile)

	// check if configPath exists, if not create it
	if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) {
		err := os.MkdirAll(configPath, os.ModePerm)
		if err != nil {
			log.Println(err)
			return err
		}
	}

	// check if config exists, if not create it
	if _, err := os.Stat(cfgPath); errors.Is(err, fs.ErrNotExist) {
		// set default host
		host := "0.0.0.0"

		if _, err := os.Stat("/.dockerenv"); err == nil {
			// docker creates a .dockerenv file at the root
			// of the directory tree inside the container.
			// if this file exists then the viewer is running
			// from inside a docker container so return true
			host = "0.0.0.0"
		} else if _, err := os.Stat("/dev/.lxc-boot-id"); err == nil {
			// lxc creates this file containing the uuid
			// of the container in every boot.
			// if this file exists then the viewer is running
			// from inside a lxc container so return true
			host = "0.0.0.0"
		} else if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
			if strings.Contains(string(b), "/docker") || strings.Contains(string(b), "/lxc") {
				host = "0.0.0.0"
			}
		}

		f, err := os.Create(cfgPath)
		if err != nil { // perm 0666
			// handle failed create
			log.Printf("error creating file: %q", err)
			return err
		}
		defer func() {
			if err := f.Close(); err != nil {
				log.Printf("error closing config file: %q", err)
			}
		}()

		// setup text template to inject variables into
		tmpl, err := template.New("config").Parse(configTemplate)
		if err != nil {
			return fmt.Errorf("could not create config template: %w", err)
		}

		tmplVars := map[string]string{
			"host": host,
		}

		var buffer bytes.Buffer
		if err = tmpl.Execute(&buffer, &tmplVars); err != nil {
			return fmt.Errorf("could not write template output: %w", err)
		}

		if _, err = f.WriteString(buffer.String()); err != nil {
			log.Printf("error writing contents to file: %v %q", configPath, err)
			return err
		}

		return f.Sync()
	}

	return nil
}

type Provider interface {
	Snapshot() domain.Config
}

type AppConfig struct {
	current           atomic.Pointer[domain.Config]
	configFile        string
	version           string
	disableConfigFile bool
	watcher           *fsnotify.Watcher
}

func New(configPath string, version string) *AppConfig {
	if _, err := os.Stat(filepath.Join(configPath, "config.toml")); err == nil {
		log.Fatalf("A legacy 'config.toml' file has been detected. " +
			"To continue, please migrate your configuration to the new 'config.yaml' format. " +
			"You can easily do this by copying the settings from 'config.toml' to 'config.yaml' and then renaming 'config.toml' to 'config.toml.old'. " +
			"The only difference between the old and the new config is, that the qbit client info is now stored in an array to allow for multiple clients to be configured.")
	}

	c := &AppConfig{
		version:           version,
		disableConfigFile: os.Getenv("SEASONPACKARR__DISABLE_CONFIG_FILE") == "true",
	}

	if !c.disableConfigFile {
		configFile, err := c.resolveConfigFile(configPath)
		if err != nil {
			log.Fatal(err)
		}
		c.configFile = configFile
	}

	snapshot, err := c.loadSnapshot()
	if err != nil {
		// cmd/start.go logs these warnings after a successful load. Print them
		// here too, because an unknown variable can explain the load error.
		for _, name := range UnknownEnvironmentVariables(os.Environ()) {
			log.Printf("warning: unknown environment variable %s; seasonpackarr ignores it", name)
		}
		log.Fatal(err)
	}
	c.current.Store(snapshot)

	return c
}

// Load reads and validates the config as the service does at startup, from the
// config file and the environment variables. It creates no file and starts no
// watcher. configDir selects the directory of config.yaml. When it is empty,
// Load searches the default locations.
func Load(configDir, version string) (domain.Config, error) {
	c := &AppConfig{
		version:           version,
		disableConfigFile: os.Getenv("SEASONPACKARR__DISABLE_CONFIG_FILE") == "true",
	}
	switch {
	case c.disableConfigFile:
	case configDir != "":
		c.configFile = filepath.Join(filepath.Clean(configDir), "config.yaml")
	default:
		configFile, err := findConfigFile()
		if err != nil {
			return domain.Config{}, err
		}
		c.configFile = configFile
	}
	snapshot, err := c.loadSnapshot()
	if err != nil {
		return domain.Config{}, err
	}
	return *snapshot, nil
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
func validateDeprecatedConfigInputs(k *koanf.Koanf, lookupEnv func(string) (string, bool)) error {
	if k != nil && k.Exists(deprecatedParseTorrentFileKey) {
		return fmt.Errorf("deprecated config detected: %s was removed; torrent parsing is always enabled now - remove the key, add the new per-client import section (see the example config / README) and update your autobrr filter to use a single Webhook action",
			deprecatedParseTorrentFileKey)
	}

	if lookupEnv != nil {
		if _, ok := lookupEnv(deprecatedParseTorrentFileEnv); ok {
			return fmt.Errorf("deprecated environment variable detected: %s was removed; torrent parsing is always enabled now - remove this env var, add the new per-client import section (see the example config / README) and update your autobrr filter to use a single Webhook action",
				deprecatedParseTorrentFileEnv)
		}
	}

	if k != nil && k.Exists(deprecatedMetadataKey) {
		return fmt.Errorf("deprecated config detected: %s was removed; smart mode now uses the actual torrent sent to /api/match - remove the metadata block and update the autobrr external filters as described in the README",
			deprecatedMetadataKey)
	}

	if lookupEnv != nil {
		for _, envKey := range []string{deprecatedMetadataTVDBAPIKeyEnv, deprecatedMetadataTVDBPINEnv} {
			if _, ok := lookupEnv(envKey); ok {
				return fmt.Errorf("deprecated environment variable detected: %s was removed; smart mode now uses the actual torrent sent to /api/match - remove this variable and update the autobrr external filters as described in the README",
					envKey)
			}
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

	if lookupEnv != nil {
		for _, env := range os.Environ() {
			envKey, _, ok := strings.Cut(env, "=")
			if !ok {
				continue
			}
			if strings.HasPrefix(envKey, "SEASONPACKARR__CLIENTS_") && strings.HasSuffix(envKey, "_"+deprecatedPreImportPathEnv) {
				return fmt.Errorf("deprecated environment variable detected: %s was removed; use the import save path/category environment variables instead",
					envKey)
			}
		}
	}

	return nil
}

// validateClientConfig enforces each client's import policy and verifies that
// configured local paths exist.
func validateClientConfig(clientName string, client *domain.Client) error {
	if client.Type != "" && client.Type != "qbittorrent" && client.Type != "transmission" && client.Type != "deluge" && client.Type != "deluge-v1" && client.Type != "deluge-v2" {
		return fmt.Errorf("type for client %q is invalid: %q - must be \"qbittorrent\", \"transmission\", \"deluge\", \"deluge-v1\" or \"deluge-v2\"", clientName, client.Type)
	}

	// go-qbittorrent drops an empty filter value, which would read every torrent,
	// and qBittorrent compares exact names, so " tv" would match no torrent.
	scope := client.Inventory
	for _, value := range slices.Concat(scope.IncludeCategories, scope.IncludeTags, scope.ExcludeCategories, scope.ExcludeTags) {
		if value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("client %q has the inventory list entry %q, which is empty or starts or ends with a space", clientName, value)
		}
	}
	if client.Type != "" && client.Type != "qbittorrent" && client.Inventory.IsSet() {
		return fmt.Errorf("client %q (%s) must not set inventory - the inventory scope is qBittorrent only", clientName, client.Type)
	}

	imp := client.Import
	switch client.Type {
	case "", "qbittorrent":
		if imp.Category == "" && imp.SavePath == "" {
			return fmt.Errorf("client %q must configure import.category or import.savePath", clientName)
		}
		switch imp.ContentLayout {
		case "", "subfolder", "nosubfolder", "original":
		default:
			return fmt.Errorf("import.contentLayout for client %q is invalid: %q; use subfolder, nosubfolder or original", clientName, imp.ContentLayout)
		}
	case "transmission":
		if imp.Category != "" || imp.DownloadPath != "" || imp.ContentLayout != "" {
			return fmt.Errorf("client %q (transmission) must not set import.category, import.downloadPath or import.contentLayout - those are qBittorrent only", clientName)
		}
	case "deluge", "deluge-v1", "deluge-v2":
		if imp.SavePath == "" {
			return fmt.Errorf("client %q (deluge) must configure import.savePath", clientName)
		}
		if len(imp.Tags) > 1 {
			return fmt.Errorf("client %q (deluge) supports at most one import.tags entry", clientName)
		}
		if len(imp.Tags) == 1 && !validDelugeLabel(imp.Tags[0]) {
			return fmt.Errorf("client %q (deluge) import label %q is invalid - use letters, digits, underscores or hyphens", clientName, imp.Tags[0])
		}
		if imp.Category != "" || imp.DownloadPath != "" || imp.ContentLayout != "" {
			return fmt.Errorf("client %q (deluge) must not set import.category, import.downloadPath or import.contentLayout", clientName)
		}
		if client.APIKey != "" {
			return fmt.Errorf("client %q (deluge) must not set apiKey", clientName)
		}
	}

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

// LogInventoryScopeWarnings warns about each client whose inventory scope does
// not select a torrent with the import category and import tags. A new pack
// stays in the import category until the arr moves it, so a second announce of
// the same pack does not find it and seasonpackarr imports a duplicate. It is
// only a warning, because a scope of the arr's completed categories can be correct.
func LogInventoryScopeWarnings(log logger.Logger, cfg domain.Config) {
	for _, name := range slices.Sorted(maps.Keys(cfg.Clients)) {
		client := cfg.Clients[name]
		if client == nil || !client.Inventory.IsSet() {
			continue
		}
		// The add trims the category and the tags and drops blank tags, so compare what it sends.
		imp := client.Import
		category := strings.TrimSpace(imp.Category)
		tags := make([]string, 0, len(imp.Tags))
		for _, tag := range imp.Tags {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
		if client.Inventory.Selects(category, tags) {
			continue
		}
		log.Warn().Str("clientname", name).Str("import_category", imp.Category).Strs("import_tags", imp.Tags).
			Msg("inventory scope does not select new packs, which have the import category and import tags; a second announce of a pack that is still in the import category is not found (see the configuration guide)")
	}
}

func validDelugeLabel(label string) bool {
	label = strings.TrimSpace(label)
	if label == "" {
		return false
	}
	for _, r := range label {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func defaultConfig(version, configFile string, disableConfigFile bool) domain.Config {
	configPath := ""
	if configFile != "" {
		configPath = filepath.Dir(configFile)
	}

	return domain.Config{
		Version:            version,
		ConfigPath:         configPath,
		DisableConfigFile:  disableConfigFile,
		Host:               "0.0.0.0",
		Port:               42069,
		Clients:            make(map[string]*domain.Client),
		LogLevel:           "DEBUG",
		LogMaxSize:         50,
		LogMaxBackups:      3,
		SmartModeThreshold: 0.75,
		Search:             domain.Search{RSSInterval: "0s", RequestInterval: "10s"},
		Notifications: domain.Notifications{
			NotificationLevel: []string{"MATCH", "ERROR"},
		},
	}
}

const envPrefix = "SEASONPACKARR__"

func setBool(dst *bool, value string) {
	if b, err := strconv.ParseBool(value); err == nil {
		*dst = b
	}
}

func setPositiveInt(dst *int, value string) {
	if i, _ := strconv.ParseInt(value, 10, 32); i > 0 {
		*dst = int(i)
	}
}

// envSettings maps each service variable, without envPrefix, to the field that
// it sets. UnknownEnvironmentVariables reads the same table.
var envSettings = map[string]func(cfg *domain.Config, value string){
	"DISABLE_CONFIG_FILE": func(cfg *domain.Config, v string) { setBool(&cfg.DisableConfigFile, v) },

	"HOST": func(cfg *domain.Config, v string) { cfg.Host = v },
	"PORT": func(cfg *domain.Config, v string) { setPositiveInt(&cfg.Port, v) },

	"LOG_LEVEL":       func(cfg *domain.Config, v string) { cfg.LogLevel = v },
	"LOG_PATH":        func(cfg *domain.Config, v string) { cfg.LogPath = v },
	"LOG_MAX_SIZE":    func(cfg *domain.Config, v string) { setPositiveInt(&cfg.LogMaxSize, v) },
	"LOG_MAX_BACKUPS": func(cfg *domain.Config, v string) { setPositiveInt(&cfg.LogMaxBackups, v) },

	"SMART_MODE": func(cfg *domain.Config, v string) { setBool(&cfg.SmartMode, v) },
	"SMART_MODE_THRESHOLD": func(cfg *domain.Config, v string) {
		if f, _ := strconv.ParseFloat(v, 32); f > 0 {
			cfg.SmartModeThreshold = float32(f)
		}
	},

	"SEARCH_PROWLARR_URL":     func(cfg *domain.Config, v string) { cfg.Search.ProwlarrURL = v },
	"SEARCH_API_KEY":          func(cfg *domain.Config, v string) { cfg.Search.APIKey = v },
	"SEARCH_INDEXER_IDS":      func(cfg *domain.Config, v string) { cfg.Search.IndexerIDs = parseSearchIndexerIDs(v) },
	"SEARCH_RSS_INTERVAL":     func(cfg *domain.Config, v string) { cfg.Search.RSSInterval = v },
	"SEARCH_REQUEST_INTERVAL": func(cfg *domain.Config, v string) { cfg.Search.RequestInterval = v },

	"API_TOKEN": func(cfg *domain.Config, v string) { cfg.APIToken = v },

	"FUZZY_MATCHING_SKIP_REPACK_COMPARE":  func(cfg *domain.Config, v string) { setBool(&cfg.FuzzyMatching.SkipRepackCompare, v) },
	"FUZZY_MATCHING_SIMPLIFY_HDR_COMPARE": func(cfg *domain.Config, v string) { setBool(&cfg.FuzzyMatching.SimplifyHdrCompare, v) },
	"FUZZY_MATCHING_SIMPLIFY_WEB_COMPARE": func(cfg *domain.Config, v string) { setBool(&cfg.FuzzyMatching.SimplifyWebCompare, v) },
	"FUZZY_MATCHING_SKIP_YEAR_COMPARE":    func(cfg *domain.Config, v string) { setBool(&cfg.FuzzyMatching.SkipYearCompare, v) },

	"NOTIFICATIONS_DISCORD": func(cfg *domain.Config, v string) { cfg.Notifications.Discord = v },
	// Whitespace separates the URLs, because a Shoutrrr URL can hold commas, such as Telegram chats=a,b.
	"NOTIFICATIONS_SHOUTRRR": func(cfg *domain.Config, v string) { cfg.Notifications.Shoutrrr = strings.Fields(v) },
	"NOTIFICATIONS_NOTIFICATION_LEVEL": func(cfg *domain.Config, v string) {
		levels := strings.Split(v, ",")
		for i, level := range levels {
			levels[i] = strings.TrimSpace(level)
		}
		cfg.Notifications.NotificationLevel = levels
	},
}

// clientEnvSettings maps each setting of a SEASONPACKARR__CLIENTS_<NAME>_<SETTING>
// variable to the client field that it sets.
var clientEnvSettings = map[string]func(client *domain.Client, value string){
	"TYPE":                  func(c *domain.Client, v string) { c.Type = v },
	"HOST":                  func(c *domain.Client, v string) { c.Host = v },
	"PORT":                  func(c *domain.Client, v string) { setPositiveInt(&c.Port, v) },
	"USERNAME":              func(c *domain.Client, v string) { c.Username = v },
	"PASSWORD":              func(c *domain.Client, v string) { c.Password = v },
	"APIKEY":                func(c *domain.Client, v string) { c.APIKey = v },
	"IMPORT_SAVE_PATH":      func(c *domain.Client, v string) { c.Import.SavePath = v },
	"IMPORT_CATEGORY":       func(c *domain.Client, v string) { c.Import.Category = v },
	"IMPORT_DOWNLOAD_PATH":  func(c *domain.Client, v string) { c.Import.DownloadPath = v },
	"IMPORT_CONTENT_LAYOUT": func(c *domain.Client, v string) { c.Import.ContentLayout = strings.ToLower(v) },
	"IMPORT_TAGS":           func(c *domain.Client, v string) { c.Import.Tags = splitList(c.Import.Tags[:0], v) },
	"INVENTORY_INCLUDE_CATEGORIES": func(c *domain.Client, v string) {
		c.Inventory.IncludeCategories = splitList(c.Inventory.IncludeCategories[:0], v)
	},
	"INVENTORY_INCLUDE_TAGS": func(c *domain.Client, v string) { c.Inventory.IncludeTags = splitList(c.Inventory.IncludeTags[:0], v) },
	"INVENTORY_EXCLUDE_CATEGORIES": func(c *domain.Client, v string) {
		c.Inventory.ExcludeCategories = splitList(c.Inventory.ExcludeCategories[:0], v)
	},
	"INVENTORY_EXCLUDE_TAGS": func(c *domain.Client, v string) { c.Inventory.ExcludeTags = splitList(c.Inventory.ExcludeTags[:0], v) },
}

// splitList appends the non-empty, trimmed comma-separated values of v to dst.
func splitList(dst []string, v string) []string {
	for value := range strings.SplitSeq(v, ",") {
		if value = strings.TrimSpace(value); value != "" {
			dst = append(dst, value)
		}
	}
	return dst
}

// cliEnvVariables are read by CLI commands in cmd, not by the service config.
var cliEnvVariables = []string{"URL", "CLIENT", "AUTOBRR_URL", "AUTOBRR_API_KEY"}

// cutClientEnvVariable splits CLIENTS_<NAME>_<SETTING> on the first underscore
// after the prefix, so multi-word settings such as IMPORT_SAVE_PATH stay whole.
func cutClientEnvVariable(name string) (clientName, setting string, ok bool) {
	after, ok := strings.CutPrefix(name, "CLIENTS_")
	if !ok {
		return "", "", false
	}
	return strings.Cut(after, "_")
}

func applyEnvironment(cfg *domain.Config) {
	for _, env := range os.Environ() {
		key, value, _ := strings.Cut(env, "=")
		name, ok := strings.CutPrefix(key, envPrefix)
		if !ok || value == "" {
			continue
		}
		if set, ok := envSettings[name]; ok {
			set(cfg, value)
		}

		clientName, setting, ok := cutClientEnvVariable(name)
		set, known := clientEnvSettings[setting]
		// An unknown variable only logs a warning, so it must not add an empty client that fails validation.
		if !ok || clientName == "" || !known {
			continue
		}
		// The client is created here when absent, so clients defined only
		// through environment variables work too.
		clientName = strings.ToLower(clientName)
		if cfg.Clients[clientName] == nil {
			cfg.Clients[clientName] = &domain.Client{}
		}
		set(cfg.Clients[clientName], value)
	}
}

// validateInventoryEnvironment rejects an inventory list variable whose value
// has no entries, such as " " or ",". It would replace the scope of config.yaml
// with an empty list, and an empty list reads every torrent.
func validateInventoryEnvironment(environ []string) error {
	for _, env := range environ {
		key, value, _ := strings.Cut(env, "=")
		name, ok := strings.CutPrefix(key, envPrefix)
		if !ok || value == "" {
			continue
		}
		clientName, setting, ok := cutClientEnvVariable(name)
		if _, known := clientEnvSettings[setting]; ok && clientName != "" && known &&
			strings.HasPrefix(setting, "INVENTORY_") && len(splitList(nil, value)) == 0 {
			return fmt.Errorf("environment variable %s has no entries; set at least one value or remove the variable", key)
		}
	}
	return nil
}

// UnknownEnvironmentVariables returns the sorted SEASONPACKARR__ variables in
// environ ("KEY=value" entries) that neither the service config nor a CLI command
// reads. It ignores the values, so an empty unknown variable is reported too.
func UnknownEnvironmentVariables(environ []string) []string {
	var unknown []string
	for _, env := range environ {
		key, _, _ := strings.Cut(env, "=")
		name, ok := strings.CutPrefix(key, envPrefix)
		if ok && !knownEnvironmentVariable(name) {
			unknown = append(unknown, key)
		}
	}
	slices.Sort(unknown)
	return unknown
}

// knownEnvironmentVariable includes the removed variables, because
// validateDeprecatedConfigInputs stops startup with a migration error for them.
func knownEnvironmentVariable(name string) bool {
	if _, ok := envSettings[name]; ok || slices.Contains(cliEnvVariables, name) {
		return true
	}
	if slices.Contains([]string{deprecatedParseTorrentFileEnv, deprecatedMetadataTVDBAPIKeyEnv, deprecatedMetadataTVDBPINEnv}, envPrefix+name) {
		return true
	}
	clientName, setting, ok := cutClientEnvVariable(name)
	_, known := clientEnvSettings[setting]
	return ok && clientName != "" && (known || setting == deprecatedPreImportPathEnv)
}

func (c *AppConfig) resolveConfigFile(configPath string) (string, error) {
	if configPath != "" {
		configPath = filepath.Clean(configPath)
		if err := c.writeConfig(configPath, "config.yaml"); err != nil {
			return "", fmt.Errorf("writing config file: %w", err)
		}
		return filepath.Join(configPath, "config.yaml"), nil
	}
	return findConfigFile()
}

func findConfigFile() (string, error) {
	locations := []string{
		"./config.yaml",
		"$HOME/.config/seasonpackarr/config.yaml",
		"$HOME/.seasonpackarr/config.yaml",
	}
	for _, location := range locations {
		configFile := os.ExpandEnv(location)
		info, err := os.Stat(configFile)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if err == nil && !info.IsDir() {
			return configFile, nil
		}
	}

	return "", fmt.Errorf("could not find config file: %w", fs.ErrNotExist)
}

func (c *AppConfig) loadSnapshot() (*domain.Config, error) {
	cfg := defaultConfig(c.version, c.configFile, c.disableConfigFile)

	// The defaults live only in cfg. As keys next to the file keys, a default and a
	// case variant from the file would both match one field, and map order would pick.
	fileK := koanf.New(".")
	if !c.disableConfigFile {
		if err := fileK.Load(file.Provider(c.configFile), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("reading config file %s: %w", c.configFile, err)
		}
	}
	if err := validateDeprecatedConfigInputs(fileK, os.LookupEnv); err != nil {
		return nil, fmt.Errorf("validating config file %s: %w", c.configFile, err)
	}
	if err := fileK.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("decoding config file %s: %w", c.configFile, err)
	}
	unknown, err := unknownConfigKeys(fileK)
	if err != nil {
		return nil, fmt.Errorf("checking config keys in %s: %w", c.configFile, err)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("validating config file %s: unknown config keys: %s; remove or rename them (see the configuration guide)",
			c.configFile, strings.Join(unknown, ", "))
	}

	if err := validateInventoryEnvironment(os.Environ()); err != nil {
		return nil, err
	}
	applyEnvironment(&cfg)
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("validating config file %s: %w", c.configFile, err)
	}

	snapshot := cloneConfig(cfg)
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

func validateConfig(cfg domain.Config) error {
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

func cloneConfig(cfg domain.Config) domain.Config {
	clone := cfg
	clone.Search.IndexerIDs = slices.Clone(cfg.Search.IndexerIDs)
	clone.Clients = make(map[string]*domain.Client, len(cfg.Clients))
	for name, client := range cfg.Clients {
		if client == nil {
			clone.Clients[name] = nil
			continue
		}
		clientClone := *client
		clientClone.Import.Tags = slices.Clone(client.Import.Tags)
		clientClone.Inventory = client.Inventory.Clone()
		clone.Clients[name] = &clientClone
	}
	clone.Notifications.NotificationLevel = slices.Clone(cfg.Notifications.NotificationLevel)
	clone.Notifications.Shoutrrr = slices.Clone(cfg.Notifications.Shoutrrr)
	return clone
}

func (c *AppConfig) Snapshot() domain.Config {
	snapshot := c.current.Load()
	if snapshot == nil {
		return domain.Config{}
	}
	return cloneConfig(*snapshot)
}

func (c *AppConfig) reload() (*domain.Config, error) {
	if !c.disableConfigFile {
		contents, err := os.ReadFile(c.configFile)
		if err != nil {
			return nil, fmt.Errorf("reading config file %s: %w", c.configFile, err)
		}
		if len(bytes.TrimSpace(contents)) == 0 {
			return nil, fmt.Errorf("config file %s is empty", c.configFile)
		}
	}

	snapshot, err := c.loadSnapshot()
	if err != nil {
		return nil, err
	}
	c.current.Store(snapshot)
	return snapshot, nil
}

const configReloadDebounceDelay = 100 * time.Millisecond

func (c *AppConfig) watchConfigFile(log logger.Logger, onChange func()) error {
	watchedPath := filepath.Clean(c.configFile)
	realPath, err := filepath.EvalSymlinks(watchedPath)
	if err != nil {
		return err
	}
	realPath = filepath.Clean(realPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := watcher.Add(filepath.Dir(watchedPath)); err != nil {
		_ = watcher.Close()
		return err
	}
	c.watcher = watcher

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				eventPath := filepath.Clean(event.Name)
				currentRealPath, err := filepath.EvalSymlinks(watchedPath)
				if err != nil {
					if os.IsNotExist(err) {
						onWatchedFile := eventPath == watchedPath || eventPath == realPath
						if onWatchedFile && event.Has(fsnotify.Remove|fsnotify.Rename) {
							onChange()
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
					onChange()
				}
			case watchErr, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error().Err(watchErr).Msg("error watching config file")
			}
		}
	}()

	return nil
}

func (c *AppConfig) DynamicReload(log logger.Logger) (<-chan struct{}, error) {
	if c.disableConfigFile {
		return nil, nil
	}

	reloaded := make(chan struct{}, 1)
	var (
		reloadGeneration atomic.Uint64
		reloadMu         sync.Mutex
	)

	reloadIfLatest := func(generation uint64) {
		if generation != reloadGeneration.Load() {
			return
		}

		reloadMu.Lock()
		defer reloadMu.Unlock()
		if generation != reloadGeneration.Load() {
			return
		}

		snapshot, err := c.reload()
		if err != nil {
			log.Error().Err(err).Msg("config reload rejected; keeping previous config")
			return
		}

		log.SetLogLevel(snapshot.LogLevel)
		LogInventoryScopeWarnings(log, *snapshot)
		select {
		case reloaded <- struct{}{}:
		default:
		}
		log.Debug().Msg("config file reloaded")
	}

	scheduleReload := func() {
		generation := reloadGeneration.Add(1)
		time.AfterFunc(configReloadDebounceDelay, func() {
			reloadIfLatest(generation)
		})
	}

	err := c.watchConfigFile(log, func() {
		// Editors and bind mounts can emit several write events for one save.
		// Reload after the event burst becomes quiet so truncate-first writes do
		// not produce a transient rejection or duplicate successful reloads.
		scheduleReload()
	})
	if err != nil {
		return nil, fmt.Errorf("watching config file %s: %w", c.configFile, err)
	}
	return reloaded, nil
}
