// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	configPath  string
	rlsName     string
	torrentFile string
	clientName  string
	host        string
	port        int
	apiKey      string
)

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
	startCmd.Flags().StringVarP(&configPath, "config", "c", "", "path to the configuration directory")

	testCmd.PersistentFlags().StringVarP(&clientName, "client", "n", "", "name of the client you want to test")
	testCmd.PersistentFlags().StringVarP(&host, "host", "i", "127.0.0.1", "host used by seasonpackarr")
	testCmd.PersistentFlags().IntVarP(&port, "port", "p", 42069, "port used by seasonpackarr")
	testCmd.PersistentFlags().StringVarP(&apiKey, "api", "a", "", "api key used by seasonpackarr")

	rootCmd.AddCommand(genTokenCmd, startCmd, testCmd, versionCmd)
	testCmd.AddCommand(packCmd, parseCmd)
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
