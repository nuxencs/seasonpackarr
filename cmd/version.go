// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/buildinfo"
	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display version info",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Version: %v\nCommit: %v\nBuild date: %v\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)

		// get the latest release tag from api
		client := http.Client{
			Timeout: 10 * time.Second,
		}

		req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, "https://api.github.com/repos/nuxencs/seasonpackarr/releases/latest", nil)
		if err != nil {
			return fmt.Errorf("could not create latest-release request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(err, http.ErrHandlerTimeout) {
				return fmt.Errorf("server timed out while fetching the latest release: %w", err)
			}
			return fmt.Errorf("failed to fetch the latest release: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		// api returns 500 instead of 404 here
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusInternalServerError {
			return errors.New("no release found")
		}

		var rel struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			return fmt.Errorf("failed to decode the latest-release response: %w", err)
		}
		fmt.Printf("Latest release: %v\n", rel.TagName)
		return nil
	},
}
