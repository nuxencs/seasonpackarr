// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"seasonpackarr/internal/buildinfo"
	"seasonpackarr/internal/config"
	"seasonpackarr/internal/http"
	"seasonpackarr/internal/logger"
	"seasonpackarr/internal/notification"
	"seasonpackarr/pkg/errors"

	"github.com/spf13/cobra"
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start seasonpackarr",
	Run: func(cmd *cobra.Command, args []string) {
		// read config
		cfg := config.New(configPath, buildinfo.Version)

		// init new logger
		log := logger.New(cfg.Config)

		if err := cfg.UpdateConfig(); err != nil {
			log.Error().Err(err).Msgf("error updating config")
		}

		// init dynamic config
		cfg.DynamicReload(log)

		// init notification sender
		noti := notification.NewDiscordSender(log, cfg)

		srv := http.NewServer(log, cfg, noti)

		log.Info().Msgf("Starting seasonpackarr")
		log.Info().Msgf("Version: %s", buildinfo.Version)
		log.Info().Msgf("Commit: %s", buildinfo.Commit)
		log.Info().Msgf("Build date: %s", buildinfo.Date)
		log.Info().Msgf("Log-level: %s", cfg.Config.LogLevel)

		errorChannel := make(chan error)
		go func() {
			err := srv.Open()
			if err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					errorChannel <- err
				}
			}
		}()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

		select {
		case sig := <-sigCh:
			log.Info().Msgf("received signal: %q, shutting down server.", sig.String())
			os.Exit(0)

		case err := <-errorChannel:
			log.Error().Err(err).Msg("unexpected error from server")
		}
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Error().Err(err).Msg("error during http shutdown")
			os.Exit(1)
		}

		os.Exit(0)
	},
}
