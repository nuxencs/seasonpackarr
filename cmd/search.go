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
	options := &connectionOptions{}
	var dryRun, verify bool
	cmd := &cobra.Command{
		Use:     "search",
		GroupID: "packs",
		Short:   "Find and import packs through Prowlarr; --dry-run previews",
		Long: `Find season packs through the running service's Prowlarr connection.

By default, search creates hardlinks and imports accepted packs for all clients.
Use --dry-run to check release names without downloading torrent metadata.
Add --verify to a dry run to check exact reuse without importing.

Reads connection settings from config and environment. Use --client to select
one client. Search stays connected until it finishes; Ctrl+C cancels the run.`,
		Args: cobra.NoArgs,
		Example: `  seasonpackarr search --dry-run
  seasonpackarr search --dry-run --verify --client tv
  seasonpackarr search --config ./config
  seasonpackarr search --dry-run --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if verify && !dryRun {
				return fmt.Errorf("--verify requires --dry-run")
			}
			api, client, err := options.resolve(cmd, true)
			if err != nil {
				return err
			}
			body, err := json.Marshal(struct {
				ClientName string `json:"clientname"`
				DryRun     bool   `json:"dryRun"`
				Verify     bool   `json:"verify"`
			}{client, dryRun, verify})
			if err != nil {
				return err
			}
			if !options.json {
				fmt.Fprintln(cmd.ErrOrStderr(), searchMode(dryRun, verify)+" started. Press Ctrl+C to cancel.")
			}
			// Search can span many tracker requests; cancellation controls its lifetime.
			response, err := api.post(cmd.Context(), "/api/search", bytes.NewReader(body), 0)
			if err != nil {
				return err
			}
			if response.status != http.StatusOK {
				return fmt.Errorf("search failed: %s", api.message(response))
			}
			var report *searchReport
			if err := json.Unmarshal(response.body, &report); err != nil || report == nil || report.Outcomes == nil || report.Failures == nil {
				return fmt.Errorf("invalid search response; check the service URL and version")
			}
			if options.json {
				// Keep the complete API report, including fields from newer service versions.
				err = writeJSON(cmd.OutOrStdout(), json.RawMessage(response.body))
			} else {
				err = report.print(cmd.OutOrStdout(), api)
			}
			if err != nil {
				return err
			}
			if report.hasFailures() {
				return &resultError{code: 1}
			}
			return nil
		},
	}
	options.addFlags(cmd)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview candidates without downloading torrent metadata or importing")
	cmd.Flags().BoolVar(&verify, "verify", false, "with --dry-run, retrieve torrent metadata and check exact reuse")
	return cmd
}

type searchReport struct {
	DryRun                 bool `json:"dryRun"`
	Verify                 bool `json:"verify"`
	ScannedTorrents        int  `json:"scannedTorrents"`
	EpisodeTorrents        int  `json:"episodeTorrents"`
	CoveredEpisodeTorrents int  `json:"coveredEpisodeTorrents"`
	Groups                 int  `json:"groups"`
	Requests               int  `json:"requests"`
	TorrentDownloads       int  `json:"torrentDownloads"`
	TorrentCacheHits       int  `json:"torrentCacheHits"`
	Outcomes               []struct {
		ClientName       string `json:"clientname"`
		Title            string `json:"title"`
		IndexerID        int    `json:"indexerId"`
		Status           string `json:"status"`
		Reason           string `json:"reason"`
		ReusableEpisodes *int   `json:"reusableEpisodes"`
		TotalEpisodes    *int   `json:"totalEpisodes"`
	} `json:"outcomes"`
	Failures []struct {
		ClientName string `json:"clientname"`
		Query      string `json:"query"`
		IndexerID  int    `json:"indexerId"`
		Reason     string `json:"reason"`
	} `json:"failures"`
}

func searchMode(dryRun, verify bool) string {
	if !dryRun {
		return "Search and import"
	}
	if verify {
		return "Exact preview"
	}
	return "Candidate preview"
}

func (r *searchReport) print(w io.Writer, api *apiClient) error {
	var output bytes.Buffer
	completion := "complete"
	if r.hasFailures() {
		completion = "finished with failures"
	}
	fmt.Fprintf(&output, "%s %s\n", searchMode(r.DryRun, r.Verify), completion)
	fmt.Fprintf(&output, "Scanned: %d torrents, %d episode torrents, %d already covered\n", r.ScannedTorrents, r.EpisodeTorrents, r.CoveredEpisodeTorrents)
	fmt.Fprintf(&output, "Search: %d groups, %d requests\nMetadata: %d downloads, %d cache hits\n", r.Groups, r.Requests, r.TorrentDownloads, r.TorrentCacheHits)
	fmt.Fprintf(&output, "Results: %d\n", len(r.Outcomes))
	for _, outcome := range r.Outcomes {
		label := strings.ReplaceAll(terminalText(outcome.Status), "_", " ")
		fmt.Fprintf(&output, "\n%s: %s\n  Client: %s | Indexer: %d", label, terminalText(outcome.Title), terminalText(outcome.ClientName), outcome.IndexerID)
		if outcome.ReusableEpisodes != nil && outcome.TotalEpisodes != nil {
			fmt.Fprintf(&output, " | Reuse: %d/%d episodes", *outcome.ReusableEpisodes, *outcome.TotalEpisodes)
		}
		fmt.Fprintln(&output)
		if outcome.Reason != "" {
			fmt.Fprintf(&output, "  %s\n", api.safeMessage(outcome.Reason))
		}
	}
	if len(r.Outcomes) == 0 {
		fmt.Fprintln(&output, "No season-pack results.")
	}
	if len(r.Failures) > 0 {
		fmt.Fprintf(&output, "\nOperation failures: %d\n", len(r.Failures))
		for _, failure := range r.Failures {
			fmt.Fprintf(&output, "  %s", api.safeMessage(failure.Reason))
			if failure.ClientName != "" {
				fmt.Fprintf(&output, " | Client: %s", terminalText(failure.ClientName))
			}
			if failure.IndexerID != 0 {
				fmt.Fprintf(&output, " | Indexer: %d", failure.IndexerID)
			}
			if failure.Query != "" {
				fmt.Fprintf(&output, " | Query: %s", terminalText(failure.Query))
			}
			fmt.Fprintln(&output)
		}
	}
	_, err := w.Write(output.Bytes())
	return err
}

func (r *searchReport) hasFailures() bool {
	if len(r.Failures) > 0 {
		return true
	}
	for _, outcome := range r.Outcomes {
		if outcome.Status == "failed" {
			return true
		}
	}
	return false
}
