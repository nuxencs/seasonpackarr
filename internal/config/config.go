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
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
)

var configTemplate = `# yaml-language-server: $schema=https://raw.githubusercontent.com/nuxencs/seasonpackarr/develop/schemas/config-schema.json
# config.yaml

# Hostname / IP
#
# Default: "0.0.0.0"
#
host: "{{ .host }}"

# Port
#
# Default: 42069
#
port: 42069

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
		log.Fatal(err)
	}
	c.current.Store(snapshot)

	return c
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
		Notifications: domain.Notifications{
			NotificationLevel: []string{"MATCH", "ERROR"},
		},
	}
}

func applyEnvironment(cfg *domain.Config) {
	const prefix = "SEASONPACKARR__"

	envs := os.Environ()
	for _, env := range envs {
		if strings.HasPrefix(env, prefix) {
			envPair := strings.SplitN(env, "=", 2)
			envKey, envValue := envPair[0], envPair[1]

			if envValue != "" {
				switch envKey {
				// disable config file
				case prefix + "DISABLE_CONFIG_FILE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						cfg.DisableConfigFile = b
					}

				// server settings
				case prefix + "HOST":
					cfg.Host = envValue
				case prefix + "PORT":
					if i, _ := strconv.ParseInt(envValue, 10, 32); i > 0 {
						cfg.Port = int(i)
					}

				// log settings
				case prefix + "LOG_LEVEL":
					cfg.LogLevel = envValue
				case prefix + "LOG_PATH":
					cfg.LogPath = envValue
				case prefix + "LOG_MAX_SIZE":
					if i, _ := strconv.ParseInt(envValue, 10, 32); i > 0 {
						cfg.LogMaxSize = int(i)
					}
				case prefix + "LOG_MAX_BACKUPS":
					if i, _ := strconv.ParseInt(envValue, 10, 32); i > 0 {
						cfg.LogMaxBackups = int(i)
					}

				// smart mode settings
				case prefix + "SMART_MODE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						cfg.SmartMode = b
					}
				case prefix + "SMART_MODE_THRESHOLD":
					if f, _ := strconv.ParseFloat(envValue, 32); f > 0 {
						cfg.SmartModeThreshold = float32(f)
					}

				// api token
				case prefix + "API_TOKEN":
					cfg.APIToken = envValue

				// fuzzy matching settings
				case prefix + "FUZZY_MATCHING_SKIP_REPACK_COMPARE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						cfg.FuzzyMatching.SkipRepackCompare = b
					}
				case prefix + "FUZZY_MATCHING_SIMPLIFY_HDR_COMPARE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						cfg.FuzzyMatching.SimplifyHdrCompare = b
					}
				case prefix + "FUZZY_MATCHING_SIMPLIFY_WEB_COMPARE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						cfg.FuzzyMatching.SimplifyWebCompare = b
					}

				// notifications settings
				case prefix + "NOTIFICATIONS_DISCORD":
					cfg.Notifications.Discord = envValue
				case prefix + "NOTIFICATIONS_NOTIFICATION_LEVEL":
					levels := strings.Split(envValue, ",")
					for i, level := range levels {
						levels[i] = strings.TrimSpace(level)
					}
					cfg.Notifications.NotificationLevel = levels
				}

				// client settings
				// Cut on the first underscore so multi-word settings such as
				// IMPORT_SAVE_PATH survive; the old len==2 Split silently dropped
				// them. Applies to clients defined purely via env vars too, since
				// the client is lazily created here when absent.
				if after, ok := strings.CutPrefix(envKey, prefix+"CLIENTS_"); ok {
					clientName, setting, ok := strings.Cut(after, "_")
					if !ok {
						continue
					}
					clientName = strings.ToLower(clientName)

					// initialize client if it doesn't exist
					if cfg.Clients[clientName] == nil {
						cfg.Clients[clientName] = &domain.Client{}
					}

					switch setting {
					case "TYPE":
						cfg.Clients[clientName].Type = envValue
					case "HOST":
						cfg.Clients[clientName].Host = envValue
					case "PORT":
						if i, _ := strconv.ParseInt(envValue, 10, 32); i > 0 {
							cfg.Clients[clientName].Port = int(i)
						}
					case "USERNAME":
						cfg.Clients[clientName].Username = envValue
					case "PASSWORD":
						cfg.Clients[clientName].Password = envValue
					case "APIKEY":
						cfg.Clients[clientName].APIKey = envValue
					case "IMPORT_SAVE_PATH":
						cfg.Clients[clientName].Import.SavePath = envValue
					case "IMPORT_CATEGORY":
						cfg.Clients[clientName].Import.Category = envValue
					case "IMPORT_DOWNLOAD_PATH":
						cfg.Clients[clientName].Import.DownloadPath = envValue
					case "IMPORT_CONTENT_LAYOUT":
						cfg.Clients[clientName].Import.ContentLayout = strings.ToLower(envValue)
					case "IMPORT_TAGS":
						cfg.Clients[clientName].Import.Tags = cfg.Clients[clientName].Import.Tags[:0]
						for tag := range strings.SplitSeq(envValue, ",") {
							if tag = strings.TrimSpace(tag); tag != "" {
								cfg.Clients[clientName].Import.Tags = append(cfg.Clients[clientName].Import.Tags, tag)
							}
						}
					}
				}
			}
		}
	}
}

func (c *AppConfig) resolveConfigFile(configPath string) (string, error) {
	if configPath != "" {
		configPath = filepath.Clean(configPath)
		if err := c.writeConfig(configPath, "config.yaml"); err != nil {
			return "", fmt.Errorf("writing config file: %w", err)
		}
		return filepath.Join(configPath, "config.yaml"), nil
	}

	locations := []string{
		"./config.yaml",
		"$HOME/.config/seasonpackarr/config.yaml",
		"$HOME/.seasonpackarr/config.yaml",
	}
	for _, location := range locations {
		configFile := os.ExpandEnv(location)
		if info, err := os.Stat(configFile); err == nil && !info.IsDir() {
			return configFile, nil
		}
	}

	return "", fmt.Errorf("could not find config file")
}

func (c *AppConfig) loadSnapshot() (*domain.Config, error) {
	cfg := defaultConfig(c.version, c.configFile, c.disableConfigFile)
	k := koanf.New(".")
	if err := k.Load(structs.Provider(&cfg, "yaml"), nil); err != nil {
		return nil, fmt.Errorf("loading config defaults: %w", err)
	}

	if !c.disableConfigFile {
		if err := k.Load(file.Provider(c.configFile), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("reading config file %s: %w", c.configFile, err)
		}
	}
	if err := validateDeprecatedConfigInputs(k, os.LookupEnv); err != nil {
		return nil, fmt.Errorf("validating config file %s: %w", c.configFile, err)
	}
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("decoding config file %s: %w", c.configFile, err)
	}

	applyEnvironment(&cfg)
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("validating config file %s: %w", c.configFile, err)
	}

	snapshot := cloneConfig(cfg)
	return &snapshot, nil
}

func validateConfig(cfg domain.Config) error {
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

func cloneConfig(cfg domain.Config) domain.Config {
	clone := cfg
	clone.Clients = make(map[string]*domain.Client, len(cfg.Clients))
	for name, client := range cfg.Clients {
		if client == nil {
			clone.Clients[name] = nil
			continue
		}
		clientClone := *client
		clientClone.Import.Tags = slices.Clone(client.Import.Tags)
		clone.Clients[name] = &clientClone
	}
	clone.Notifications.NotificationLevel = slices.Clone(cfg.Notifications.NotificationLevel)
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
