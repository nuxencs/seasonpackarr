// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var configPath string

var rootCmd = &cobra.Command{
	Use:   "seasonpackarr",
	Short: "Automagically hardlink already downloaded episode files into a season folder when a matching season pack announce hits autobrr.",
	Long: `Automagically hardlink already downloaded episode files into a season folder when a matching season pack announce hits autobrr.

Provide a configuration file using one of the following methods:
1. Use the --config <path> or -c <path> flag.
2. Place a config.yaml file in the default user configuration directory (e.g., ~/.config/seasonpackarr/).
3. Place a config.yaml file a folder inside your home directory (e.g., ~/.seasonpackarr/).
4. Place a config.yaml file in the directory of the binary.

For more information and examples, visit https://github.com/nuxencs/seasonpackarr`,
}

func init() {
	startCmd.Flags().StringVarP(&configPath, "config", "c", "", "path to configuration directory")

	rootCmd.AddCommand(genTokenCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(versionCmd)
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
