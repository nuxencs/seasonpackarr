// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"errors"
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/buildinfo"
	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/notification"

	"github.com/spf13/cobra"
)

func newNotificationCommand() *cobra.Command {
	group := &cobra.Command{
		Use:     "notification",
		GroupID: "tools",
		Short:   "Check the notification destinations",
		Args:    cobra.NoArgs,
	}
	group.AddCommand(newNotificationTestCommand())
	return group
}

func newNotificationTestCommand() *cobra.Command {
	var configDir string
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Send a test notification to each notification destination",
		Long: `Send a test notification to each notification destination: the Discord webhook and each Shoutrrr URL.

Reads config.yaml and the environment variables as start does, and needs no running service.
Ignores notificationLevel. Prints one line for each destination and exits 1 when a send
fails or when no destination is configured.`,
		Example: "  seasonpackarr notification test --config ./config",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configDir, buildinfo.Version)
			if err != nil {
				return err
			}
			results := notification.SendTest(cmd.Context(), cfg.Notifications)
			if len(results) == 0 {
				return errors.New("no notification destination configured; set notifications.discord or notifications.shoutrrr")
			}

			failed := false
			for _, result := range results {
				status := "ok"
				if result.Err != nil {
					status, failed = result.Err.Error(), true
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", result.Destination, status); err != nil {
					return err
				}
			}
			if failed {
				return &resultError{code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&configDir, "config", "c", "", "configuration directory (default: search the default locations)")
	return cmd
}
