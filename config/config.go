package config

import (
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Host          string
	Port          int
	PreImportPath string
}

var (
	userConfigDir, _ = os.UserConfigDir()
	configPath       = filepath.Join(userConfigDir, "seasonpackarr", "config.toml")
)

var configTemplate = `# config.toml

# Hostname / IP
#
# Default: "0.0.0.0"
#
host = "{{ .Host }}"

# Port
#
# Default: 42069
#
port = {{ .Port }}

# Pre Import Path of qBittorrent for Sonarr
# Needs to be filled out correctly, e.g. "/data/torrents/tv-hd"
#
# Default: ""
#
preImportPath = "{{ .PreImportPath }}"
`

func init() {
	pflag.StringVarP(&configPath, "config", "c", configPath, "config file (default is ~/.config/seasonpackarr/config.toml)")

	pflag.Parse()

	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Stack().Logger()
}

func InitConfig() {
	defaultConfig := Config{
		Host:          "0.0.0.0",
		Port:          42069,
		PreImportPath: "",
	}

	tmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create config template")
	}

	if _, err = os.Stat(configPath); os.IsNotExist(err) {
		configFile, err2 := os.Create(configPath)
		if err2 != nil {
			log.Fatal().Err(err2).Msg("failed to create config configFile")
		}
		defer func(configFile *os.File) {
			err2 = configFile.Close()
			if err2 != nil {
				log.Fatal().Err(err2).Msg("failed to close config configFile")
			}
		}(configFile)

		err2 = tmpl.Execute(configFile, defaultConfig)
		if err2 != nil {
			log.Fatal().Err(err2).Msg("failed to apply template to config configFile")
		}
	}

	viper.SetConfigFile(configPath)
	if err = viper.ReadInConfig(); err != nil {
		log.Fatal().Err(err).Msg("failed to read from config file")
	}

	if viper.GetString("PreImportPath") == "" {
		log.Fatal().Msg("PreImportPath can't be empty, please provide a valid path to your pre import torrent directory")
	}

	if _, err := os.Stat(viper.GetString("PreImportPath")); os.IsNotExist(err) {
		log.Fatal().Err(err).Msg("PreImportPath doesn't exist, please make sure you entered the correct path")
	}
}
