// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"

	"seasonpackarr/internal/api"

	"github.com/spf13/cobra"
)

// genTokenCmd represents the gen-token command
var genTokenCmd = &cobra.Command{
	Use:   "gen-token",
	Short: "Generate an api token",
	Run: func(cmd *cobra.Command, args []string) {
		key := api.GenerateToken()
		fmt.Printf("API Token: %v\nJust copy and paste it into your config file!\n", key)
	},
}

func init() {
	rootCmd.AddCommand(genTokenCmd)
}
