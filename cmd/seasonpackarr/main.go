package main

import (
	"encoding/json"
	"fmt"
	netHTTP "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"seasonpackarr/internal/api"
	"seasonpackarr/internal/config"
	"seasonpackarr/internal/http"
	"seasonpackarr/internal/logger"
	"seasonpackarr/pkg/errors"

	"github.com/spf13/pflag"
)

var (
	version = "dev"
	commit  = ""
	date    = ""
)

const usage = `seasonpackarr - Automagically hardlink already downloaded episode files into a season folder when a matching season pack announce hits autobrr.

Usage:
  seasonpackarr [command] [flags]

Commands:
  start          Start seasonpackarr
  gen-token      Generate an API Token
  version        Print version info
  help           Show this help message

Flags:
  -c, --config <path>  Path to configuration file (default is in the default user config directory)

Provide a configuration file using one of the following methods:
1. Use the --config <path> or -c <path> flag.
2. Place a config.toml file in the default user configuration directory (e.g., ~/.config/seasonpackarr/).
3. Place a config.toml file a folder inside your home directory (e.g., ~/.seasonpackarr/).
4. Place a config.toml file in the directory of the binary.

For more information and examples, visit https://github.com/nuxencs/seasonpackarr
` + "\n"

func init() {
	pflag.Usage = func() {
		fmt.Print(usage)
	}
}

func main() {
	var configPath string

	pflag.StringVarP(&configPath, "config", "c", "", "path to configuration file")
	pflag.Parse()

	switch cmd := pflag.Arg(0); cmd {
	case "version":
		fmt.Printf("Version: %v\nCommit: %v\n", version, commit)

		// get the latest release tag from api
		client := netHTTP.Client{
			Timeout: 10 * time.Second,
		}

		resp, err := client.Get("https://api.github.com/repos/nuxencs/seasonpackarr/releases/latest")
		if err != nil {
			if errors.Is(err, netHTTP.ErrHandlerTimeout) {
				fmt.Println("Server timed out while fetching latest release from api")
			} else {
				fmt.Printf("Failed to fetch latest release from api: %v\n", err)
			}
			os.Exit(1)
		}
		defer resp.Body.Close()

		// api returns 500 instead of 404 here
		if resp.StatusCode == netHTTP.StatusNotFound || resp.StatusCode == netHTTP.StatusInternalServerError {
			fmt.Print("No release found")
			os.Exit(1)
		}

		var rel struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			fmt.Printf("Failed to decode response from api: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Latest release: %v\n", rel.TagName)

	case "gen-token":
		key := api.GenerateToken()
		fmt.Printf("API Token: %v\nJust copy and paste it into your config file!\n", key)

	case "start":
		// read config
		cfg := config.New(configPath, version)

		// init new logger
		log := logger.New(cfg.Config)

		if err := cfg.UpdateConfig(); err != nil {
			log.Error().Err(err).Msgf("error updating config")
		}

		// init dynamic config
		cfg.DynamicReload(log)

		srv := http.NewServer(log, cfg)

		log.Info().Msgf("Starting seasonpackarr")
		log.Info().Msgf("Version: %s", version)
		log.Info().Msgf("Commit: %s", commit)
		log.Info().Msgf("Build date: %s", date)
		log.Info().Msgf("Log-level: %s", cfg.Config.LogLevel)

		errorChannel := make(chan error)
		go func() {
			errorChannel <- srv.Open()
		}()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

		for sig := range sigCh {
			log.Info().Msgf("received signal: %v, shutting down server.", sig)
			os.Exit(0)
		}

	default:
		pflag.Usage()
		if cmd != "help" {
			os.Exit(0)
		}
	}
}
