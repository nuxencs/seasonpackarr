// Copyright (c) 2023 - 2024, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"encoding/json"
	"fmt"
	netHTTP "net/http"
	"os"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/buildinfo"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display version info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version: %v\nCommit: %v\nBuild date: %v\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)

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
	},
}
