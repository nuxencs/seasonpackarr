// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

func newSearchCommand() *cobra.Command {
	var serverURL, token, client string
	var dryRun, verify bool
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Backfill season packs through Prowlarr",
		Args:  cobra.NoArgs,
		Example: `  seasonpackarr search --dry-run --client default --api your-api-token
  seasonpackarr search --url http://127.0.0.1:42069 --api your-api-token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if verify && !dryRun {
				return fmt.Errorf("--verify requires --dry-run")
			}
			body, err := json.Marshal(struct {
				ClientName string `json:"clientname"`
				DryRun     bool   `json:"dryRun"`
				Verify     bool   `json:"verify"`
			}{client, dryRun, verify})
			if err != nil {
				return err
			}
			req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, strings.TrimRight(serverURL, "/")+"/api/search", bytes.NewReader(body))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Token", token)
			// Search may take longer than webhook checks. Request cancellation controls
			// its lifetime, rather than the test helper's 30-second timeout.
			transport := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := transport.Do(req)
			if err != nil {
				return fmt.Errorf("backfill request failed: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("backfill request returned HTTP %d", resp.StatusCode)
			}
			data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
			if err != nil {
				return err
			}
			var report struct {
				Failures []json.RawMessage `json:"failures"`
				Outcomes []struct {
					Status string `json:"status"`
				} `json:"outcomes"`
			}
			if err := json.Unmarshal(data, &report); err != nil {
				return fmt.Errorf("invalid backfill response: %w", err)
			}
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, data, "", "  "); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), pretty.String())
			if len(report.Failures) > 0 {
				return fmt.Errorf("backfill completed with %d operation failures", len(report.Failures))
			}
			for _, outcome := range report.Outcomes {
				if outcome.Status == "failed" {
					return fmt.Errorf("backfill completed with failed results")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&serverURL, "url", "http://127.0.0.1:42069", "seasonpackarr base URL")
	cmd.Flags().StringVar(&token, "api", "", "seasonpackarr API token")
	cmd.Flags().StringVar(&client, "client", "", "configured client name (default: all clients)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "search candidates without downloading torrent metadata")
	cmd.Flags().BoolVar(&verify, "verify", false, "with --dry-run, retrieve torrent metadata and verify exact reuse")
	return cmd
}
