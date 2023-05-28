package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	userConfigDir, _ = os.UserConfigDir()
	configPath       = filepath.Join(userConfigDir, "seasonpackarr")
)

func init() {
	pflag.StringVarP(&configPath, "config", "c", configPath, "config file location (default is ~/.config/seasonpackarr/config.toml)")

	pflag.Parse()

	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Stack().Logger()
}

func InitConfig() {
	viper.AddConfigPath(configPath)
	viper.SetConfigName("config")
	viper.SetConfigType("toml")

	viper.SetDefault("Host", "127.0.0.1")
	viper.SetDefault("Port", 42069)
	viper.SetDefault("PreImportPath", "")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			err = viper.SafeWriteConfig()
			if err != nil {
				log.Fatal().Err(err).Msg("failed to write to config file")
			}
		} else {
			log.Fatal().Err(err).Msg("failed to read config file")
		}
	}

	if viper.GetString("PreImportPath") == "" {
		log.Fatal().Msg("PreImportPath can't be empty, please provide a valid path to your pre import torrent directory")
	}

	if _, err := os.Stat(viper.GetString("PreImportPath")); os.IsNotExist(err) {
		log.Fatal().Err(err).Msg("PreImportPath doesn't exist, please make sure you entered the correct path")
	}
}
