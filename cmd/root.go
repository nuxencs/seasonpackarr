// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "seasonpackarr",
		Short: "Reuse downloaded episodes in season packs",
		Long: `Reuse downloaded episodes in season packs.

Start the service, then use candidate or match to check a pack.
Import creates hardlinks and adds the pack to your torrent client.
Search discovers packs through Prowlarr and imports them unless --dry-run is set.

API commands read connection settings from config.yaml and environment variables.
Use --config for another config directory or --url for a remote service.`,
		Example: `  seasonpackarr start --config ~/.config/seasonpackarr
  seasonpackarr candidate "Series.S01.1080p.WEB-DL-GRP"
  seasonpackarr match ./pack.torrent
  seasonpackarr search --dry-run`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddGroup(
		&cobra.Group{ID: "service", Title: "Service:"},
		&cobra.Group{ID: "packs", Title: "Season packs (requires a running service):"},
		&cobra.Group{ID: "tools", Title: "Tools:"},
	)
	root.AddCommand(newStartCommand(), newSearchCommand(), newGenTokenCommand(), newVersionCommand())
	for _, name := range []string{"candidate", "match", "import"} {
		root.AddCommand(newOperationCommand(name))
	}
	return root
}

// resultError signals an unsuccessful outcome that has already been printed.
type resultError struct{ code int }

func (e *resultError) Error() string { return "operation did not pass" }

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cmd := newRootCommand()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	if err := cmd.ExecuteContext(ctx); err != nil {
		if result, ok := errors.AsType[*resultError](err); ok {
			return result.code
		}
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}

func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}
