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
    # qBittorrent Hostname / IP
    #
    # Default: "127.0.0.1"
    #
    host: "127.0.0.1"

    # qBittorrent Port
    #
    # Default: 8080
    #
    port: 8080

    # qBittorrent Username
    #
    # Default: "admin"
    #
    username: "admin"

    # qBittorrent Password
    #
    # Default: "adminadmin"
    #
    password: "adminadmin"

    # Pre Import Path of qBittorrent for Sonarr
    # Needs to be filled out correctly, e.g. "/data/torrents/tv-hd"
    #
    # Default: ""
    #
    preImportPath: ""

  # Below you can find an example on how to define a second qBittorrent client
  # If you want to define even more clients just copy this segment and adjust the values accordingly
  #
  #multi_client_example:
  #  host: "127.0.0.1"
  #
  #  port: 9090
  #
  #  username: "example"
  #
  #  password: "example"
  #
  #  preImportPath: ""

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

# Parse Torrent File
# Toggles torrent file parsing to get the correct folder name
#
# Default: false
#
# parseTorrentFile: false

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
	UpdateConfig() error
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

	c.loadFromEnv()

	for clientName, client := range c.Config.Clients {
		if client.PreImportPath == "" {
			log.Fatalf("preImportPath for client %q can't be empty, please provide a valid path to the directory you want seasonpacks to be hardlinked to", clientName)
		}

		if _, err := os.Stat(client.PreImportPath); errors.Is(err, fs.ErrNotExist) {
			log.Fatalf("preImportPath for client %q doesn't exist, please make sure you entered the correct path", clientName)
		}
	}

	return c
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
	c.Config.ParseTorrentFile = false
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
			sensitiveKeys := []string{"PASSWORD", "API_TOKEN", "DISCORD"}
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

				// parse torrent file
				case prefix + "PARSE_TORRENT_FILE":
					if b, err := strconv.ParseBool(envValue); err == nil {
						c.Config.ParseTorrentFile = b
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
				if strings.HasPrefix(envKey, prefix+"CLIENTS_") {
					parts := strings.Split(strings.TrimPrefix(envKey, prefix+"CLIENTS_"), "_")
					if len(parts) == 2 {
						clientName := strings.ToLower(parts[0])
						setting := parts[1]

						// initialize client if it doesn't exist
						if c.Config.Clients[clientName] == nil {
							c.Config.Clients[clientName] = &domain.Client{}
						}

						switch setting {
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
						case "PREIMPORTPATH":
							c.Config.Clients[clientName].PreImportPath = envValue
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

		// unmarshal the updated config into the Config struct
		if err := k.Unmarshal("", c.Config); err != nil {
			log.Error().Err(err).Msg("failed to unmarshal updated config")
			return
		}

		log.SetLogLevel(c.Config.LogLevel)

		log.Debug().Msg("config file reloaded!")
	})
}

func (c *AppConfig) UpdateConfig() error {
	if c.Config.DisableConfigFile {
		return nil
	}

	configFile := path.Join(c.Config.ConfigPath, "config.yaml")

	f, err := os.ReadFile(configFile)
	if err != nil {
		return errors.Wrap(err, "could not read config configFile: %s", configFile)
	}

	lines := strings.Split(string(f), "\n")
	lines = c.processLines(lines)

	output := strings.Join(lines, "\n")
	if err := os.WriteFile(configFile, []byte(output), 0o644); err != nil {
		return errors.Wrap(err, "could not write config file: %s", configFile)
	}

	return nil
}

func (c *AppConfig) processLines(lines []string) []string {
	// keep track of not found values to append at the bottom
	var (
		foundLineLogLevel = false
		foundLineLogPath  = false

		foundLineSmartMode          = false
		foundLineSmartModeThreshold = false
		foundLineParseTorrentFile   = false

		foundLineFuzzyMatching      = false
		foundLineSkipRepackCompare  = false
		foundLineSimplifyHdrCompare = false
		foundLineSimplifyWebCompare = false
		foundLineSkipYearCompare    = false

		foundLineMetadata           = false
		foundLineMetadataTVDBAPIKey = false
		foundLineMetadataTVDBPIN    = false

		foundLineAPIToken = false

		foundLineNotifications     = false
		foundLineNotificationLevel = false
		foundLineDiscord           = false
	)

	for i, line := range lines {
		if !foundLineLogLevel && strings.Contains(line, "logLevel:") {
			lines[i] = fmt.Sprintf("logLevel: \"%s\"", c.Config.LogLevel)
			foundLineLogLevel = true
		}
		if !foundLineLogPath && strings.Contains(line, "logPath:") {
			if c.Config.LogPath == "" {
				lines[i] = "# logPath: \"\""
			} else {
				lines[i] = fmt.Sprintf("logPath: \"%s\"", c.Config.LogPath)
			}
			foundLineLogPath = true
		}

		if !foundLineSmartMode && strings.Contains(line, "smartMode:") {
			lines[i] = fmt.Sprintf("smartMode: %t", c.Config.SmartMode)
			foundLineSmartMode = true
		}
		if !foundLineSmartModeThreshold && strings.Contains(line, "smartModeThreshold:") {
			lines[i] = fmt.Sprintf("smartModeThreshold: %.2f", c.Config.SmartModeThreshold)
			foundLineSmartModeThreshold = true
		}
		if !foundLineParseTorrentFile && strings.Contains(line, "parseTorrentFile:") {
			lines[i] = fmt.Sprintf("parseTorrentFile: %t", c.Config.ParseTorrentFile)
			foundLineParseTorrentFile = true
		}

		if !foundLineFuzzyMatching && strings.Contains(line, "fuzzyMatching:") {
			foundLineFuzzyMatching = true
		}
		if foundLineFuzzyMatching && !foundLineSkipRepackCompare && strings.Contains(line, "skipRepackCompare:") {
			lines[i] = fmt.Sprintf("  skipRepackCompare: %t", c.Config.FuzzyMatching.SkipRepackCompare)
			foundLineSkipRepackCompare = true
		}
		if foundLineFuzzyMatching && !foundLineSimplifyHdrCompare && strings.Contains(line, "simplifyHdrCompare:") {
			lines[i] = fmt.Sprintf("  simplifyHdrCompare: %t", c.Config.FuzzyMatching.SimplifyHdrCompare)
			foundLineSimplifyHdrCompare = true
		}
		if foundLineFuzzyMatching && !foundLineSimplifyWebCompare && strings.Contains(line, "simplifyWebCompare:") {
			lines[i] = fmt.Sprintf("  simplifyWebCompare: %t", c.Config.FuzzyMatching.SimplifyWebCompare)
			foundLineSimplifyWebCompare = true
		}
		if foundLineFuzzyMatching && !foundLineSkipYearCompare && strings.Contains(line, "skipYearCompare:") {
			lines[i] = fmt.Sprintf("  skipYearCompare: %t", c.Config.FuzzyMatching.SkipYearCompare)
			foundLineSkipYearCompare = true
		}

		if !foundLineMetadata && strings.Contains(line, "metadata:") {
			foundLineMetadata = true
		}
		if foundLineMetadata && !foundLineMetadataTVDBAPIKey && strings.Contains(line, "tvdbAPIKey:") {
			lines[i] = fmt.Sprintf("  tvdbAPIKey: \"%s\"", c.Config.Metadata.TVDBAPIKey)
			foundLineMetadataTVDBAPIKey = true
		}
		if foundLineMetadata && !foundLineMetadataTVDBPIN && strings.Contains(line, "tvdbPIN:") {
			lines[i] = fmt.Sprintf("  tvdbPIN: \"%s\"", c.Config.Metadata.TVDBPIN)
			foundLineMetadataTVDBPIN = true
		}

		if !foundLineAPIToken && strings.Contains(line, "apiToken:") {
			if c.Config.APIToken == "" {
				lines[i] = "# apiToken: \"\""
			} else {
				lines[i] = fmt.Sprintf("apiToken: \"%s\"", c.Config.APIToken)
			}
			foundLineAPIToken = true
		}

		if !foundLineNotifications && strings.Contains(line, "notifications:") {
			foundLineNotifications = true
		}
		if foundLineNotifications && !foundLineNotificationLevel && strings.Contains(line, "notificationLevel:") {
			printLevels := make([]string, len(c.Config.Notifications.NotificationLevel))
			for i, level := range c.Config.Notifications.NotificationLevel {
				printLevels[i] = fmt.Sprintf("%q", strings.TrimSpace(level))
			}
			lines[i] = fmt.Sprintf("  notificationLevel: [%s]", strings.Join(printLevels, ", "))
			foundLineNotificationLevel = true
		}
		if foundLineNotifications && !foundLineDiscord && strings.Contains(line, "discord:") {
			lines[i] = fmt.Sprintf("  discord: \"%s\"", c.Config.Notifications.Discord)
			foundLineDiscord = true
		}
	}

	if !foundLineLogLevel {
		lines = append(lines, "# Log level")
		lines = append(lines, "#")
		lines = append(lines, `# Default: "DEBUG"`)
		lines = append(lines, "#")
		lines = append(lines, `# Options: "ERROR", "DEBUG", "INFO", "WARN", "TRACE"`)
		lines = append(lines, "#")
		lines = append(lines, fmt.Sprintf("logLevel: \"%s\"\n", c.Config.LogLevel))
	}

	if !foundLineLogPath {
		lines = append(lines, "# Log Path")
		lines = append(lines, "#")
		lines = append(lines, "# Optional")
		lines = append(lines, "#")
		if c.Config.LogPath == "" {
			lines = append(lines, "# logPath: \"\"")
			lines = append(lines, "")
		} else {
			lines = append(lines, fmt.Sprintf("logPath: \"%s\"\n", c.Config.LogPath))
			lines = append(lines, "")
		}
	}

	if !foundLineSmartMode {
		lines = append(lines, "# Smart Mode")
		lines = append(lines, "# Toggles smart mode to only download season packs that have a certain amount of episodes from a release group")
		lines = append(lines, "# already in the client")
		lines = append(lines, "#")
		lines = append(lines, "# Default: false")
		lines = append(lines, "#")
		lines = append(lines, fmt.Sprintf("# smartMode: %t\n", c.Config.SmartMode))
	}

	if !foundLineSmartMode {
		lines = append(lines, "# Smart Mode Threshold")
		lines = append(lines, "# Sets the threshold for the percentage of episodes out of a season that must be present in the client")
		lines = append(lines, "# In this example 75% of the episodes in a season must be present in the client for it to be downloaded")
		lines = append(lines, "#")
		lines = append(lines, "# Default: 0.75")
		lines = append(lines, "#")
		lines = append(lines, fmt.Sprintf("# smartModeThreshold: %.2f\n", c.Config.SmartModeThreshold))
	}

	if !foundLineParseTorrentFile {
		lines = append(lines, "# Parse Torrent File")
		lines = append(lines, "# Toggles torrent file parsing to get the correct folder name")
		lines = append(lines, "#")
		lines = append(lines, "# Default: false")
		lines = append(lines, "#")
		lines = append(lines, fmt.Sprintf("# parseTorrentFile: %t\n", c.Config.ParseTorrentFile))
	}

	if !foundLineFuzzyMatching {
		lines = append(lines, "# Fuzzy Matching")
		lines = append(lines, "# You can decide for which criteria the matching should be less strict, e.g. repack status and HDR format")
		lines = append(lines, "#")
		lines = append(lines, "fuzzyMatching:")
		if !foundLineSkipRepackCompare {
			lines = append(lines, "  # Skip Repack Compare")
			lines = append(lines, "  # Toggle comparing of the repack status of a release, e.g. repacked episodes will be treated the same as a non-repacked ones")
			lines = append(lines, "  #")
			lines = append(lines, "  # Default: false")
			lines = append(lines, "  #")
			lines = append(lines, fmt.Sprintf("  skipRepackCompare: %t\n", c.Config.FuzzyMatching.SkipRepackCompare))
		}
		if !foundLineSimplifyHdrCompare {
			lines = append(lines, "  # Simplify HDR Compare")
			lines = append(lines, "  # Toggle simplification of HDR formats for comparing, e.g. HDR10+ will be treated the same as HDR")
			lines = append(lines, "  #")
			lines = append(lines, "  # Default: false")
			lines = append(lines, "  #")
			lines = append(lines, fmt.Sprintf("  simplifyHdrCompare: %t\n", c.Config.FuzzyMatching.SimplifyHdrCompare))
		}
		if !foundLineSimplifyWebCompare {
			lines = append(lines, "  # Simplify WEB Compare")
			lines = append(lines, "  # Toggle simplification of WEB-DL for comparing, e.g. WEB-DL will be treated the same as WEB")
			lines = append(lines, "  #")
			lines = append(lines, "  # Default: false")
			lines = append(lines, "  #")
			lines = append(lines, fmt.Sprintf("  simplifyWebCompare: %t\n", c.Config.FuzzyMatching.SimplifyWebCompare))
		}
		if !foundLineSkipYearCompare {
			lines = append(lines, "  # Skip Year Compare")
			lines = append(lines, "  # Toggle comparing of the year of a release, e.g. a release without a year will be treated the same as a release with a year")
			lines = append(lines, "  #")
			lines = append(lines, "  # Default: false")
			lines = append(lines, "  #")
			lines = append(lines, fmt.Sprintf("  skipYearCompare: %t\n", c.Config.FuzzyMatching.SkipRepackCompare))
		}
	}

	if !foundLineMetadata {
		lines = append(lines, "# Metadata")
		lines = append(lines, "# Here you can provide credentials for additional metadata providers")
		lines = append(lines, "#")
		lines = append(lines, "metadata:")
		if !foundLineMetadataTVDBAPIKey {
			lines = append(lines, "  # TVDB API Key")
			lines = append(lines, "  # Your TVDB API Key. If you have a user-subscription key, also provide your subscriber PIN in tvdbPIN.")
			lines = append(lines, "  #")
			lines = append(lines, "  # Optional")
			lines = append(lines, "  #")
			lines = append(lines, fmt.Sprintf("  tvdbAPIKey: \"%s\"\n", c.Config.Metadata.TVDBAPIKey))
		}
		if !foundLineMetadataTVDBPIN {
			lines = append(lines, "  # TVDB PIN")
			lines = append(lines, "  # Your TVDB Subscriber PIN.")
			lines = append(lines, "  #")
			lines = append(lines, "  # Optional")
			lines = append(lines, "  #")
			lines = append(lines, fmt.Sprintf("  tvdbPIN: \"%s\"\n", c.Config.Metadata.TVDBPIN))
		}
	}

	if !foundLineAPIToken {
		lines = append(lines, "# API Token")
		lines = append(lines, "# If not defined, removes api authentication")
		lines = append(lines, "#")
		lines = append(lines, "# Optional")
		lines = append(lines, "#")
		if c.Config.APIToken == "" {
			lines = append(lines, "# apiToken: \"\"\n")
		} else {
			lines = append(lines, fmt.Sprintf("apiToken: \"%s\"\n", c.Config.APIToken))
		}
	}

	if !foundLineNotifications {
		lines = append(lines, "# Notifications")
		lines = append(lines, "# You can decide which notifications you want to receive")
		lines = append(lines, "#")
		lines = append(lines, "notifications:")
		if !foundLineNotificationLevel {
			lines = append(lines, "  # Notification Level")
			lines = append(lines, "  # Decides what notifications you want to receive")
			lines = append(lines, "  #")
			lines = append(lines, "  # Default: [ \"MATCH\", \"ERROR\" ]")
			lines = append(lines, "  #")
			lines = append(lines, "  # Options: \"MATCH\", \"INFO\", \"ERROR\"")
			lines = append(lines, "  #")
			lines = append(lines, "  # Examples:")
			lines = append(lines, "  # [ \"MATCH\", \"INFO\", \"ERROR\" ] would send everything")
			lines = append(lines, "  # [ \"MATCH\", \"INFO\" ] would send all matches and rejection infos")
			lines = append(lines, "  # [ \"MATCH\", \"ERROR\" ] would send all matches and errors")
			lines = append(lines, "  # [ \"ERROR\" ] would only send all errors")
			lines = append(lines, "  #")
			lines = append(lines, fmt.Sprintf("  notificationLevel: %s\n", c.Config.Notifications.NotificationLevel))
		}
		if !foundLineDiscord {
			lines = append(lines, "  # Discord")
			lines = append(lines, "  # Uses the given Discord webhook to send notifications for various events")
			lines = append(lines, "  #")
			lines = append(lines, "  # Optional")
			lines = append(lines, "  #")
			lines = append(lines, fmt.Sprintf("  discord: \"%s\"\n", c.Config.Notifications.Discord))
		}
	}

	return lines
}
