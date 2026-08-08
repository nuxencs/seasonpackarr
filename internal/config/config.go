// Copyright (c) 2021 - 2025, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
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
    # Supported values: "qbittorrent" (default), "transmission"
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
    # qBittorrent listens on 8080 by default, Transmission on 9091
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
    # Controls how /api/parse re-imports the matched season pack back into the client
    # after the local episodes have been hardlinked into place.
    #
    import:
      # Save Path
      # Final import destination. Used both as the hardlink root and as the client's save directory.
      # Optional for qBittorrent (falls back to the category save path, then the default save path)
      # and for transmission (falls back to the session download dir). When set it must already exist.
      # A qBittorrent client must configure either savePath or category.
      #
      # Default: ""
      #
      savePath: ""

      # Tags
      # Tags (qBittorrent) / labels (transmission) applied to the imported torrent.
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
# Toggles smart mode to only download season packs that have a certain amount of episodes from a release group
# already in the client
#
# Default: false
#
# smartMode: false

# Smart Mode Threshold
# Sets the threshold for the percentage of episodes out of a season that must be present in the client
# In this example 75% of the episodes in a season must be present in the client for it to be downloaded
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

# Metadata
# Here you can provide credentials for additional metadata providers
#
metadata:
  # TVDB API Key
  # Your TVDB API Key. If you have a user-subscription key, also provide your subscriber PIN in tvdbPIN.
  #
  # Optional
  #
  tvdbAPIKey: ""

  # TVDB PIN
  # Your TVDB Subscriber PIN.
  #
  # Optional
  #
  tvdbPIN: ""

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
		} else if pd, _ := os.Open("/proc/1/cgroup"); pd != nil {
			defer pd.Close()
			b := make([]byte, 4096)
			pd.Read(b)
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
		defer f.Close()

		// setup text template to inject variables into
		tmpl, err := template.New("config").Parse(configTemplate)
		if err != nil {
			return errors.Wrap(err, "could not create config template")
		}

		tmplVars := map[string]string{
			"host": host,
		}

		var buffer bytes.Buffer
		if err = tmpl.Execute(&buffer, &tmplVars); err != nil {
			return errors.Wrap(err, "could not write template output")
		}

		if _, err = f.WriteString(buffer.String()); err != nil {
			log.Printf("error writing contents to file: %v %q", configPath, err)
			return err
		}

		return f.Sync()
	}

	return nil
}

type Config interface {
	DynamicReload(log logger.Logger)
}

type AppConfig struct {
	Config *domain.Config
	m      *sync.Mutex
	k      *koanf.Koanf
}

func New(configPath string, version string) *AppConfig {
	if _, err := os.Stat(filepath.Join(configPath, "config.toml")); err == nil {
		log.Fatalf("A legacy 'config.toml' file has been detected. " +
			"To continue, please migrate your configuration to the new 'config.yaml' format. " +
			"You can easily do this by copying the settings from 'config.toml' to 'config.yaml' and then renaming 'config.toml' to 'config.toml.old'. " +
			"The only difference between the old and the new config is, that the qbit client info is now stored in an array to allow for multiple clients to be configured.")
	}

	// init app config
	c := &AppConfig{
		Config: &domain.Config{
			Version:    version,
			ConfigPath: configPath,
		},
		m: new(sync.Mutex),
		k: koanf.New("."),
	}

	c.defaults()
	c.Config.DisableConfigFile = os.Getenv("SEASONPACKARR__DISABLE_CONFIG_FILE") == "true"

	if !c.Config.DisableConfigFile {
		c.load()
	}

	if err := validateDeprecatedConfigInputs(c.k, os.LookupEnv); err != nil {
		log.Fatal(err)
	}

	c.loadFromEnv()

	for clientName, client := range c.Config.Clients {
		if err := validateClientConfig(clientName, client); err != nil {
			log.Fatal(err)
		}
	}

	return c
}

const (
	deprecatedParseTorrentFileKey = "parseTorrentFile"
	deprecatedParseTorrentFileEnv = "SEASONPACKARR__PARSE_TORRENT_FILE"
	deprecatedPreImportPathKey    = "preImportPath"
	deprecatedPreImportPathEnv    = "PREIMPORTPATH"
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
		if value, ok := lookupEnv(deprecatedParseTorrentFileEnv); ok && strings.TrimSpace(value) != "" {
			return fmt.Errorf("deprecated environment variable detected: %s was removed; torrent parsing is always enabled now - remove this env var, add the new per-client import section (see the example config / README) and update your autobrr filter to use a single Webhook action",
				deprecatedParseTorrentFileEnv)
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
			envKey, envValue, ok := strings.Cut(env, "=")
			if !ok || strings.TrimSpace(envValue) == "" {
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

// validateClientConfig enforces the per-client import policy: qBittorrent
// clients need a category or savePath, transmission clients must not set the
// qBittorrent-only fields, and any configured local paths must exist.
func validateClientConfig(clientName string, client *domain.Client) error {
	if client.Type != "" && client.Type != "qbittorrent" && client.Type != "transmission" {
		return fmt.Errorf("type for client %q is invalid: %q - must be \"qbittorrent\" or \"transmission\"", clientName, client.Type)
	}

	imp := client.Import
	isQbit := client.Type == "" || client.Type == "qbittorrent"

	if isQbit {
		if imp.Category == "" && imp.SavePath == "" {
			return fmt.Errorf("client %q must configure import.category or import.savePath", clientName)
		}
		switch imp.ContentLayout {
		case "", "subfolder", "nosubfolder", "original":
		default:
			return fmt.Errorf("import.contentLayout for client %q is invalid: %q; use subfolder, nosubfolder or original", clientName, imp.ContentLayout)
		}
	} else if imp.Category != "" || imp.DownloadPath != "" || imp.ContentLayout != "" {
		return fmt.Errorf("client %q (transmission) must not set import.category, import.downloadPath or import.contentLayout - those are qBittorrent only", clientName)
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

func (c *AppConfig) defaults() {
	// Set default values
	c.Config.DisableConfigFile = false
	c.Config.Host = "0.0.0.0"
	c.Config.Port = 42069
	c.Config.Clients = make(map[string]*domain.Client)
	c.Config.LogPath = ""
	c.Config.LogLevel = "DEBUG"
	c.Config.LogMaxSize = 50
	c.Config.LogMaxBackups = 3
	c.Config.SmartMode = false
	c.Config.SmartModeThreshold = 0.75
	c.Config.FuzzyMatching = domain.FuzzyMatching{
		SkipRepackCompare:  false,
		SimplifyHdrCompare: false,
		SimplifyWebCompare: false,
	}
	c.Config.Metadata = domain.Metadata{
		TVDBAPIKey: "",
		TVDBPIN:    "",
		// SonarrHost:   "",
		// SonarrPort:   0,
		// SonarrAPIKey: "",
	}
	c.Config.APIToken = ""
	c.Config.Notifications = domain.Notifications{
		NotificationLevel: []string{"MATCH", "ERROR"},
		Discord:           "",
	}

	// load default values into koanf
	if err := c.k.Load(structs.Provider(c.Config, "yaml"), nil); err != nil {
		log.Fatalf("could not load default values into config: %q", err)
	}
}

func (c *AppConfig) loadFromEnv() {
	prefix := "SEASONPACKARR__"

	logLevel := c.Config.LogLevel
	if envLogLevel := os.Getenv(prefix + "LOG_LEVEL"); envLogLevel != "" {
		logLevel = envLogLevel
	}

	// create a temporary logger with the detected log level
	zLog := logger.New(&domain.Config{
		LogLevel: logLevel,
		Version:  c.Config.Version,
	})

	envs := os.Environ()
	for _, env := range envs {
		if strings.HasPrefix(env, prefix) {
			envPair := strings.SplitN(env, "=", 2)
			envKey, envValue := envPair[0], envPair[1]

			// Determine if this is a sensitive value that should be redacted in logs
			sensitiveKeys := []string{"PASSWORD", "API_TOKEN", "APIKEY", "DISCORD"}
			logValue := envValue

			for _, sensitive := range sensitiveKeys {
				if strings.Contains(envKey, sensitive) {
					logValue = "**REDACTED**"
					break
				}
			}

			zLog.Trace().Msgf("processing environment variable: %s=%s", envKey, logValue)

			if envValue != "" {
				switch envKey {
				// disable config file
				case prefix + "DISABLE_CONFIG_FILE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						c.Config.DisableConfigFile = b
					}

				// server settings
				case prefix + "HOST":
					c.Config.Host = envValue
				case prefix + "PORT":
					if i, _ := strconv.ParseInt(envValue, 10, 32); i > 0 {
						c.Config.Port = int(i)
					}

				// log settings
				case prefix + "LOG_LEVEL":
					c.Config.LogLevel = envValue
				case prefix + "LOG_PATH":
					c.Config.LogPath = envValue
				case prefix + "LOG_MAX_SIZE":
					if i, _ := strconv.ParseInt(envValue, 10, 32); i > 0 {
						c.Config.LogMaxSize = int(i)
					}
				case prefix + "LOG_MAX_BACKUPS":
					if i, _ := strconv.ParseInt(envValue, 10, 32); i > 0 {
						c.Config.LogMaxBackups = int(i)
					}

				// smart mode settings
				case prefix + "SMART_MODE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						c.Config.SmartMode = b
					}
				case prefix + "SMART_MODE_THRESHOLD":
					if f, _ := strconv.ParseFloat(envValue, 32); f > 0 {
						c.Config.SmartModeThreshold = float32(f)
					}

				// api token
				case prefix + "API_TOKEN":
					c.Config.APIToken = envValue

				// fuzzy matching settings
				case prefix + "FUZZY_MATCHING_SKIP_REPACK_COMPARE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						c.Config.FuzzyMatching.SkipRepackCompare = b
					}
				case prefix + "FUZZY_MATCHING_SIMPLIFY_HDR_COMPARE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						c.Config.FuzzyMatching.SimplifyHdrCompare = b
					}
				case prefix + "FUZZY_MATCHING_SIMPLIFY_WEB_COMPARE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						c.Config.FuzzyMatching.SimplifyWebCompare = b
					}

				// metadata settings
				case prefix + "METADATA_TVDB_API_KEY":
					c.Config.Metadata.TVDBAPIKey = envValue
				case prefix + "METADATA_TVDB_PIN":
					c.Config.Metadata.TVDBPIN = envValue

				// notifications settings
				case prefix + "NOTIFICATIONS_DISCORD":
					c.Config.Notifications.Discord = envValue
				case prefix + "NOTIFICATIONS_NOTIFICATION_LEVEL":
					levels := strings.Split(envValue, ",")
					for i, level := range levels {
						levels[i] = strings.TrimSpace(level)
					}
					c.Config.Notifications.NotificationLevel = levels
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
					if c.Config.Clients[clientName] == nil {
						c.Config.Clients[clientName] = &domain.Client{}
					}

					switch setting {
					case "TYPE":
						c.Config.Clients[clientName].Type = envValue
					case "HOST":
						c.Config.Clients[clientName].Host = envValue
					case "PORT":
						if i, _ := strconv.ParseInt(envValue, 10, 32); i > 0 {
							c.Config.Clients[clientName].Port = int(i)
						}
					case "USERNAME":
						c.Config.Clients[clientName].Username = envValue
					case "PASSWORD":
						c.Config.Clients[clientName].Password = envValue
					case "APIKEY":
						c.Config.Clients[clientName].APIKey = envValue
					case "IMPORT_SAVE_PATH":
						c.Config.Clients[clientName].Import.SavePath = envValue
					case "IMPORT_CATEGORY":
						c.Config.Clients[clientName].Import.Category = envValue
					case "IMPORT_DOWNLOAD_PATH":
						c.Config.Clients[clientName].Import.DownloadPath = envValue
					case "IMPORT_CONTENT_LAYOUT":
						c.Config.Clients[clientName].Import.ContentLayout = strings.ToLower(envValue)
					case "IMPORT_TAGS":
						c.Config.Clients[clientName].Import.Tags = c.Config.Clients[clientName].Import.Tags[:0]
						for tag := range strings.SplitSeq(envValue, ",") {
							if tag = strings.TrimSpace(tag); tag != "" {
								c.Config.Clients[clientName].Import.Tags = append(c.Config.Clients[clientName].Import.Tags, tag)
							}
						}
					}
				}
			}
		}
	}

	// load parsed env variables into koanf
	if err := c.k.Load(structs.Provider(c.Config, "yaml"), nil); err != nil {
		log.Fatalf("could not load env vars into config: %q", err)
	}
}

func (c *AppConfig) load() {
	// clean trailing slash from c.Config.ConfigPath
	configPath := path.Clean(c.Config.ConfigPath)

	var configFile string

	if configPath != "" {
		// check if path and file exists
		// if not, create path and file
		if err := c.writeConfig(configPath, "config.yaml"); err != nil {
			log.Printf("config write error: %q", err)
		}

		configFile = path.Join(configPath, "config.yaml")
	} else {
		// try to find config in standard locations
		locations := []string{
			"./config.yaml",
			"$HOME/.config/seasonpackarr/config.yaml",
			"$HOME/.seasonpackarr/config.yaml",
		}

		for _, loc := range locations {
			expandedLoc := os.ExpandEnv(loc)
			if _, err := os.Stat(expandedLoc); err == nil {
				configFile = expandedLoc
				break
			}
		}

		if configFile == "" {
			log.Fatalf("could not find config file")
		}
	}

	// load the config file
	if err := c.k.Load(file.Provider(configFile), yaml.Parser()); err != nil {
		log.Fatalf("config read error: %q", err)
	}

	// unmarshal the config into the Config struct
	if err := c.k.Unmarshal("", c.Config); err != nil {
		log.Fatalf("could not unmarshal config file: %v: err %q", configFile, err)
	}
}

func (c *AppConfig) DynamicReload(log logger.Logger) {
	if c.Config.DisableConfigFile {
		return
	}

	configFile := path.Join(c.Config.ConfigPath, "config.yaml")

	// use koanf's built-in file watcher
	f := file.Provider(configFile)

	// watch the config file for changes
	f.Watch(func(event any, err error) {
		if err != nil {
			log.Error().Err(err).Msg("error watching config file")
			return
		}

		c.m.Lock()
		defer c.m.Unlock()

		// create a new koanf instance for reloading
		k := koanf.New(".")

		// load the config file
		if err := k.Load(f, yaml.Parser()); err != nil {
			log.Error().Err(err).Msg("failed to reload config file")
			return
		}

		// refuse to apply a reload that still uses removed settings
		if err := validateDeprecatedConfigInputs(k, os.LookupEnv); err != nil {
			log.Error().Err(err).Msg("reloaded config uses a removed setting; keeping previous config")
			return
		}

		// unmarshal the updated config into the Config struct
		if err := k.Unmarshal("", c.Config); err != nil {
			log.Error().Err(err).Msg("failed to unmarshal updated config")
			return
		}

		for clientName, client := range c.Config.Clients {
			if err := validateClientConfig(clientName, client); err != nil {
				log.Error().Err(err).Msg("reloaded config is invalid; check the client import policy")
			}
		}

		log.SetLogLevel(c.Config.LogLevel)

		log.Debug().Msg("config file reloaded!")
	})
}
