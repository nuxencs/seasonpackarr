// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/payload"

	"github.com/spf13/cobra"
)

func newOperationCommand(operation string) *cobra.Command {
	options := &connectionOptions{}
	var releaseName string
	argument, short, details := "<file.torrent>", "", ""
	switch operation {
	case "candidate":
		argument = "<release>"
		short = "Check release-name compatibility without torrent metadata"
		details = "This is a quick candidate check. Use match with a real torrent file to check exact reuse."
	case "match":
		short = "Check exact reuse from a torrent file without importing"
		details = "Reads the release name and episode list from a real .torrent file. Does not create hardlinks or add torrents."
	case "import":
		short = "Create hardlinks and import a real season-pack torrent"
		details = "Creates hardlinks and adds the pack to the torrent client. Use match first to check exact reuse without importing."
	}
	input := "./pack.torrent"
	if operation == "candidate" {
		input = `"Series.S01.1080p.WEB-DL-GRP"`
	}
	cmd := &cobra.Command{
		Use:     operation + " " + argument,
		GroupID: "packs",
		Short:   short,
		Long: short + ".\n\n" + details + `

Requires a running seasonpackarr service. Reads host, port, and API token from
local config or environment settings. Selects the sole configured client;
use --client when several clients exist. Without local clients, tries "default".`,
		Example: fmt.Sprintf("  seasonpackarr %s %s\n  seasonpackarr %s %s --config ./config\n  seasonpackarr %s %s --url https://example.com/seasonpackarr --client tv", operation, input, operation, input, operation, input),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
				return fmt.Errorf("provide exactly one %s; see seasonpackarr %s --help", argument, operation)
			}
			if cmd.Flags().Changed("release") && strings.TrimSpace(releaseName) == "" {
				return fmt.Errorf("--release requires a non-empty release name")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			var torrentBytes []byte
			var err error
			if operation != "candidate" {
				name, torrentBytes, err = torrentInput(args[0])
				if err != nil {
					return err
				}
				if cmd.Flags().Changed("release") {
					name = releaseName
				}
			}
			api, client, err := options.resolve(cmd, false)
			if err != nil {
				return err
			}
			var body io.Reader
			switch operation {
			case "candidate":
				body, err = payload.CompileCandidate(name, client)
			case "match":
				body, err = payload.CompileMatch(name, torrentBytes, client)
			case "import":
				body, err = payload.CompileImport(name, torrentBytes, client)
			}
			if err != nil {
				return err
			}
			response, err := api.post(cmd.Context(), "/api/"+operation, body, 30*time.Second)
			if err != nil {
				return err
			}
			// A proxy login page can also return HTTP 200; require the API error envelope.
			if response.status == int(domain.StatusNoMatches) {
				var body struct {
					StatusCode int    `json:"statusCode"`
					Error      string `json:"error"`
				}
				if err := json.Unmarshal(response.body, &body); err != nil || body.StatusCode != response.status || strings.TrimSpace(body.Error) == "" {
					return fmt.Errorf("invalid %s response; check the service URL and version", operation)
				}
			}
			status, code := "failed", 1
			domainStatus := domain.StatusCode(response.status)
			switch {
			case domainStatus == domain.StatusSuccessfulMatch:
				status, code = "accepted", 0
			case domainStatus.String() != "" && (response.status < 400 || domainStatus == domain.StatusFailedMatchToTorrentEps):
				status, code = "rejected", 2
			}
			message := api.message(response)
			if code == 0 {
				switch operation {
				case "candidate":
					message = "release-name checks passed; exact reuse is not verified"
				case "match":
					message = "exact reuse checks passed"
				case "import":
					message = "hardlinks created and torrent imported"
				}
			}
			result := operationResult{Operation: operation, Status: status, StatusCode: response.status, Client: client, Release: name, Message: message}
			if options.json {
				err = writeJSON(cmd.OutOrStdout(), result)
			} else {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\nClient: %s\nRelease: %s\n", strings.ToUpper(operation[:1])+operation[1:], status, message, terminalText(client), terminalText(name))
			}
			if err != nil {
				return err
			}
			if code != 0 {
				return &resultError{code: code}
			}
			return nil
		},
	}
	if operation != "candidate" {
		cmd.Flags().StringVar(&releaseName, "release", "", "full tracker release name (default: name inside the torrent)")
		cmd.Long += "\n\nUse --release when the torrent's folder name lacks the full tracker release name."
		cmd.Example += fmt.Sprintf("\n  seasonpackarr %s ./pack.torrent --release \"Series.S01.1080p.WEB-DL-GRP\"", operation)
		cmd.ValidArgsFunction = cobra.FixedCompletions([]string{"torrent"}, cobra.ShellCompDirectiveFilterFileExt)
	}
	options.addFlags(cmd)
	return cmd
}

type operationResult struct {
	Operation  string `json:"operation"`
	Status     string `json:"status"`
	StatusCode int    `json:"statusCode"`
	Client     string `json:"clientname"`
	Release    string `json:"name"`
	Message    string `json:"message"`
}
