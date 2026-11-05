// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/nuxencs/seasonpackarr/internal/autobrr"
	"github.com/nuxencs/seasonpackarr/internal/config"

	"github.com/spf13/cobra"
)

func newSetupCommand() *cobra.Command {
	setup := &cobra.Command{
		Use:     "setup",
		GroupID: "tools",
		Short:   "Set up the integration with other services",
		Args:    cobra.NoArgs,
	}
	setup.AddCommand(newSetupFilterCommand())
	return setup
}

type setupFilterOptions struct {
	configDir, client, token           string
	autobrrURL, autobrrKey, webhookURL string
	dryRun, json                       bool
}

// setupFilterResult is one filter that the command created or would create.
type setupFilterResult struct {
	Client string          `json:"clientname"`
	ID     int             `json:"id,omitzero"`
	Name   string          `json:"name"`
	Result string          `json:"result"`
	Filter *autobrr.Filter `json:"filter,omitempty"`
}

func newSetupFilterCommand() *cobra.Command {
	o := &setupFilterOptions{}
	cmd := &cobra.Command{
		Use:   "filter",
		Short: "Create the seasonpackarr filter in autobrr",
		Long: `Create the seasonpackarr filter in autobrr through the autobrr REST API.

Creates one filter for each configured client, or for the client from --client.
A new filter is named seasonpackarr-<client>, is disabled, and has no indexers.
In autobrr, select the indexers, set the priority, and enable the filter.

--webhook-url is the seasonpackarr URL that autobrr uses, for example
http://seasonpackarr:42069 in Docker. Give the autobrr URL and API key with
--autobrr-url and --autobrr-api-key, or with SEASONPACKARR__AUTOBRR_URL and
SEASONPACKARR__AUTOBRR_API_KEY. Reads the API token and the client names from
config; without a config, use --client and --api.

Use --dry-run to print each filter without a call to autobrr.`,
		Args: cobra.NoArgs,
		Example: `  seasonpackarr setup filter --webhook-url http://seasonpackarr:42069 --autobrr-url http://autobrr:7474
  seasonpackarr setup filter --webhook-url http://127.0.0.1:42069 --client tv --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error { return o.run(cmd) },
	}
	f := cmd.Flags()
	f.StringVarP(&o.configDir, "config", "c", "", "read config.yaml from this directory")
	f.StringVarP(&o.client, "client", "n", "", "create the filter for this client only (default: each configured client)")
	f.StringVarP(&o.token, "api", "a", "", "seasonpackarr API token (default: environment or config)")
	f.StringVar(&o.webhookURL, "webhook-url", "", "seasonpackarr base URL that autobrr uses, including any proxy path")
	f.StringVar(&o.autobrrURL, "autobrr-url", "", "autobrr base URL (default: SEASONPACKARR__AUTOBRR_URL)")
	f.StringVar(&o.autobrrKey, "autobrr-api-key", "", "autobrr API key (default: SEASONPACKARR__AUTOBRR_API_KEY)")
	f.BoolVar(&o.dryRun, "dry-run", false, "print each filter with the tokens redacted and make no autobrr calls")
	f.BoolVar(&o.json, "json", false, "print results as JSON")
	_ = cmd.MarkFlagRequired("webhook-url")
	return cmd
}

func (o *setupFilterOptions) run(cmd *cobra.Command) error {
	webhookURL, err := parseBaseURL("--webhook-url", o.webhookURL)
	if err != nil {
		return err
	}
	settings, err := config.ReadConnectionSettings(o.configDir)
	if err != nil {
		return err
	}
	token := settings.APIToken
	if cmd.Flags().Changed("api") {
		token = o.token
	}
	client := os.Getenv("SEASONPACKARR__CLIENT")
	if cmd.Flags().Changed("client") {
		client = o.client
	}
	clients := settings.Clients
	if client != "" {
		clients = []string{client}
	}
	if len(clients) == 0 {
		return fmt.Errorf("no clients configured; select a client with --client")
	}
	newFilter := autobrr.NewFilter
	if o.dryRun {
		newFilter = autobrr.NewRedactedFilter
	}
	filters := make([]autobrr.Filter, 0, len(clients))
	for _, name := range clients {
		filter, err := newFilter(name, webhookURL, token)
		if err != nil {
			return err
		}
		filters = append(filters, filter)
	}

	var api *autobrr.Client
	if !o.dryRun {
		autobrrURL := os.Getenv("SEASONPACKARR__AUTOBRR_URL")
		if cmd.Flags().Changed("autobrr-url") {
			autobrrURL = o.autobrrURL
		}
		if autobrrURL == "" {
			return fmt.Errorf("set the autobrr URL with --autobrr-url or SEASONPACKARR__AUTOBRR_URL")
		}
		if autobrrURL, err = parseBaseURL("autobrr URL", autobrrURL); err != nil {
			return err
		}
		key := os.Getenv("SEASONPACKARR__AUTOBRR_API_KEY")
		if cmd.Flags().Changed("autobrr-api-key") {
			key = o.autobrrKey
		}
		if key == "" {
			return fmt.Errorf("set the autobrr API key with --autobrr-api-key or SEASONPACKARR__AUTOBRR_API_KEY")
		}
		api = autobrr.NewClient(autobrrURL, key)
	}
	if token == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Warning: the seasonpackarr API token is empty, so the filters send no token. Set apiToken in config, SEASONPACKARR__API_TOKEN, or --api.")
	}

	results := make([]setupFilterResult, 0, len(filters))
	var createErr error
	for i, filter := range filters {
		result := setupFilterResult{Client: clients[i], Name: filter.Name}
		if o.dryRun {
			result.Result, result.Filter = "dry-run", &filter
		} else {
			id, err := api.CreateFilter(cmd.Context(), filter)
			if err != nil {
				createErr = fmt.Errorf("create the filter for client %s: %w", terminalText(clients[i]), err)
				if id != 0 {
					createErr = fmt.Errorf("%w; autobrr has the incomplete filter %d, delete it before you rerun", createErr, id)
				}
				break
			}
			result.Result, result.ID = "created", id
		}
		results = append(results, result)
	}
	// Print the filters written before a failure, so the user knows which exist.
	if err := o.print(cmd.OutOrStdout(), results); err != nil {
		return err
	}
	return createErr
}

func (o *setupFilterOptions) print(w io.Writer, results []setupFilterResult) error {
	if o.json {
		return writeJSON(w, results)
	}
	for _, result := range results {
		if result.Filter != nil {
			fmt.Fprintf(w, "Dry run: filter %s for client %s, not sent to autobrr\n", terminalText(result.Name), terminalText(result.Client))
			if err := writeJSON(w, result.Filter); err != nil {
				return err
			}
			continue
		}
		fmt.Fprintf(w, "Created filter %d (%s) for client %s\n", result.ID, terminalText(result.Name), terminalText(result.Client))
	}
	if len(results) > 0 && !o.dryRun {
		_, err := fmt.Fprint(w, `
New filters are disabled and have no indexers. In autobrr, for each new filter:
  1. Select the indexers in the General tab.
  2. Set the priority higher than your other TV filters.
  3. Enable the filter.
`)
		return err
	}
	return nil
}
