// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/api"

	"github.com/spf13/cobra"
)

func newGenTokenCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "gen-token",
		GroupID: "tools",
		Short:   "Generate an API token for the apiToken config setting",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "API Token: %s\nSet apiToken in config.yaml to this value.\n", api.GenerateToken())
			return err
		},
	}
}
