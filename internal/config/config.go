// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is modified for use with seasonpackarr
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

	"seasonpackarr/internal/domain"
	"seasonpackarr/internal/logger"
	"seasonpackarr/pkg/errors"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var configTemplate = `# config.yaml

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

# API Token
# If not defined, removes api authentication
#
# Optional
#
# apiToken: ""
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
}

func New(configPath string, version string) *AppConfig {
	if _, err := os.Stat(filepath.Join(configPath, "config.toml")); err == nil {
		log.Fatalf("A legacy 'config.toml' file has been detected. " +
			"To continue, please migrate your configuration to the new 'config.yaml' format. " +
			"You can easily do this by copying the settings from 'config.toml' to 'config.yaml' and then renaming 'config.toml' to 'config.toml.old'. " +
			"The only difference between the old and the new config is, that the qbit client info is now stored in an array to allow for multiple clients to be configured.")
	}

	c := &AppConfig{
		m: new(sync.Mutex),
	}
	c.defaults()
	c.Config.Version = version
	c.Config.ConfigPath = configPath

	c.load(configPath)
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
	c.Config = &domain.Config{
		Version:            "dev",
		Host:               "0.0.0.0",
		Port:               42069,
		Clients:            make(map[string]*domain.Client),
		LogLevel:           "DEBUG",
		LogPath:            "",
		LogMaxSize:         50,
		LogMaxBackups:      3,
		SmartMode:          false,
		SmartModeThreshold: 0.75,
		ParseTorrentFile:   false,
		FuzzyMatching: domain.FuzzyMatching{
			SkipRepackCompare:  false,
			SimplifyHdrCompare: false,
		},
		APIToken: "",
	}
}

func (c *AppConfig) loadFromEnv() {
	prefix := "SEASONPACKARR__"

	envs := os.Environ()
	for _, env := range envs {
		if strings.HasPrefix(env, prefix) {
			envPair := strings.SplitN(env, "=", 2)

			if envPair[1] != "" {
				switch envPair[0] {
				case prefix + "HOST":
					c.Config.Host = envPair[1]
				case prefix + "PORT":
					if i, _ := strconv.ParseInt(envPair[1], 10, 32); i > 0 {
						c.Config.Port = int(i)
					}
				case prefix + "LOG_LEVEL":
					c.Config.LogLevel = envPair[1]
				case prefix + "LOG_PATH":
					c.Config.LogPath = envPair[1]
				case prefix + "LOG_MAX_SIZE":
					if i, _ := strconv.ParseInt(envPair[1], 10, 32); i > 0 {
						c.Config.LogMaxSize = int(i)
					}
				case prefix + "LOG_MAX_BACKUPS":
					if i, _ := strconv.ParseInt(envPair[1], 10, 32); i > 0 {
						c.Config.LogMaxBackups = int(i)
					}
				case prefix + "SMART_MODE":
					if b, err := strconv.ParseBool(envPair[1]); err == nil {
						c.Config.SmartMode = b
					}
				case prefix + "SMART_MODE_THRESHOLD":
					if f, _ := strconv.ParseFloat(envPair[1], 32); f > 0 {
						c.Config.SmartModeThreshold = float32(f)
					}
				case prefix + "PARSE_TORRENT_FILE":
					if b, err := strconv.ParseBool(envPair[1]); err == nil {
						c.Config.ParseTorrentFile = b
					}
				case prefix + "API_TOKEN":
					c.Config.APIToken = envPair[1]
				}
			}
		}
	}
}

func (c *AppConfig) load(configPath string) {
	viper.SetConfigType("yaml")

	// clean trailing slash from configPath
	configPath = path.Clean(configPath)
	if configPath != "" {
		// check if path and file exists
		// if not, create path and file
		if err := c.writeConfig(configPath, "config.yaml"); err != nil {
			log.Printf("config write error: %q", err)
		}

		viper.SetConfigFile(path.Join(configPath, "config.yaml"))
	} else {
		viper.SetConfigName("config")

		// Search config in directories
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/seasonpackarr")
		viper.AddConfigPath("$HOME/.seasonpackarr")
	}

	// read config
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("config read error: %q", err)
	}

	if err := viper.Unmarshal(c.Config); err != nil {
		log.Fatalf("Could not unmarshal config file: %v: err %q", viper.ConfigFileUsed(), err)
	}
}

func (c *AppConfig) DynamicReload(log logger.Logger) {
	viper.OnConfigChange(func(e fsnotify.Event) {
		c.m.Lock()

		logLevel := viper.GetString("logLevel")
		c.Config.LogLevel = logLevel
		log.SetLogLevel(c.Config.LogLevel)

		logPath := viper.GetString("logPath")
		c.Config.LogPath = logPath

		smartMode := viper.GetBool("smartMode")
		c.Config.SmartMode = smartMode

		smartModeThreshold := viper.GetFloat64("smartModeThreshold")
		c.Config.SmartModeThreshold = float32(smartModeThreshold)

		parseTorrentFile := viper.GetBool("parseTorrentFile")
		c.Config.ParseTorrentFile = parseTorrentFile

		log.Debug().Msg("config file reloaded!")

		c.m.Unlock()
	})
	viper.WatchConfig()
}

func (c *AppConfig) UpdateConfig() error {
	filePath := path.Join(c.Config.ConfigPath, "config.yaml")

	f, err := os.ReadFile(filePath)
	if err != nil {
		return errors.Wrap(err, "could not read config filePath: %s", filePath)
	}

	lines := strings.Split(string(f), "\n")
	lines = c.processLines(lines)

	output := strings.Join(lines, "\n")
	if err := os.WriteFile(filePath, []byte(output), 0644); err != nil {
		return errors.Wrap(err, "could not write config file: %s", filePath)
	}

	return nil
}

func (c *AppConfig) processLines(lines []string) []string {
	// keep track of not found values to append at bottom
	var (
		foundLineLogLevel           = false
		foundLineLogPath            = false
		foundLineSmartMode          = false
		foundLineSmartModeThreshold = false
		foundLineParseTorrentFile   = false
		foundLineFuzzyMatching      = false
		foundLineSkipRepackCompare  = false
		foundLineSimplifyHdrCompare = false
		foundLineApiToken           = false
	)

	for i, line := range lines {
		if !foundLineLogLevel && strings.Contains(line, "logLevel:") {
			lines[i] = fmt.Sprintf("logLevel: \"%s\"", c.Config.LogLevel)
			foundLineLogLevel = true
		}
		if !foundLineLogPath && strings.Contains(line, "logPath:") {
			if c.Config.LogPath == "" {
				lines[i] = "#logPath: \"\""
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
			lines[i] = fmt.Sprintf("parseTorrentFile: %t", c.Config.SmartMode)
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
		if !foundLineApiToken && strings.Contains(line, "apiToken:") {
			lines[i] = fmt.Sprintf("apiToken: \"%s\"", c.Config.APIToken)
			foundLineApiToken = true
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
	}

	if !foundLineApiToken {
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

	return lines
}
