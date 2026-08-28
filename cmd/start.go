// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/buildinfo"
	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/http"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/notification"
	"github.com/spf13/cobra"
)

const serverShutdownTimeout = 15 * time.Second

type managedServer interface {
	Open(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start seasonpackarr",
	RunE: func(cmd *cobra.Command, args []string) error {
		// read config
		cfg := config.New(configPath, buildinfo.Version)
		snapshot := cfg.Snapshot()

		// init new logger
		log := logger.New(&snapshot)

		// init dynamic config
		if _, err := cfg.DynamicReload(log); err != nil {
			return fmt.Errorf("failed to start config reload watcher: %w", err)
		}

		// init notification sender
		noti := notification.NewDiscordSender(log, cfg)

		srv := http.NewServer(log, cfg, noti)

		log.Info().Msgf("Starting seasonpackarr")
		log.Info().Msgf("Version: %s", buildinfo.Version)
		log.Info().Msgf("Commit: %s", buildinfo.Commit)
		log.Info().Msgf("Build date: %s", buildinfo.Date)
		log.Info().Msgf("Log-level: %s", snapshot.LogLevel)

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
		defer stop()

		if err := runServer(ctx, srv); err != nil {
			return err
		}
		return nil
	},
}

func runServer(ctx context.Context, srv managedServer) error {
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Open(ctx)
	}()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server stopped unexpectedly: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), serverShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("could not shut down server: %w", err)
	}

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server stopped during shutdown: %w", err)
		}
	default:
	}
	return nil
}
