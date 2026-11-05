// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/autobrr"
	"github.com/nuxencs/seasonpackarr/internal/config"

	"github.com/spf13/cobra"
)

func newSetupCommand() *cobra.Command {
	setup := &cobra.Command{
		Use:     "setup",
		GroupID: "tools",
		Short:   "Set up other services to work with seasonpackarr",
		Args:    cobra.NoArgs,
	}
	setup.AddCommand(newSetupFilterCommand())
	return setup
}

type setupFilterOptions struct {
	configDir, client, token           string
	autobrrURL, autobrrKey, webhookURL string
	filterID                           int
	dryRun, json                       bool
}

// setupFilterResult is one filter that the command wrote or would write.
type setupFilterResult struct {
	Client string `json:"clientname"`
	ID     int    `json:"id,omitzero"`
	Name   string `json:"name"`
	Result string `json:"result"`
	// RemovedActions is nil for a new filter and empty for an update without old actions.
	RemovedActions []autobrr.StoredAction `json:"removed_actions,omitzero"`
	// StaleFilters are the other filters that call seasonpackarr. The command does not change them.
	StaleFilters []autobrr.StaleFilter `json:"stale_filters,omitempty"`
	// Filter is the payload of a dry run.
	Filter any `json:"filter,omitempty"`
}

// filterChanges is the part of a filter that an update replaces.
type filterChanges struct {
	External []autobrr.External `json:"external"`
	Actions  []autobrr.Action   `json:"actions"`
}

func newSetupFilterCommand() *cobra.Command {
	o := &setupFilterOptions{}
	cmd := &cobra.Command{
		Use:   "filter",
		Short: "Create or update the seasonpackarr filter in autobrr",
		Long: `Create or update the seasonpackarr filter in autobrr through the autobrr REST API.

Creates one filter for each configured client, or for the client from --client.
A new filter is named seasonpackarr-<client>, is disabled, and has no indexers.
In autobrr, select the indexers, set the priority, and enable the filter. If a
filter with that name exists, the command writes nothing and prints its ID.

--filter-id updates an existing filter for one client. The update replaces the
External entries and all actions, prints each removed action, and keeps all
other settings, such as the indexers, the priority, and the enabled state.

The command warns about each other filter that calls seasonpackarr, for example
an old filter that calls /api/pack, and prints its ID. It never changes those
filters.

--webhook-url is the seasonpackarr URL that autobrr uses, for example
http://seasonpackarr:42069 in Docker. Give the autobrr URL and API key with
--autobrr-url and --autobrr-api-key, or with SEASONPACKARR__AUTOBRR_URL and
SEASONPACKARR__AUTOBRR_API_KEY. Reads the API token and the client names from
config; without a config, use --client and --api.

Use --dry-run to print each filter without a write to autobrr. A dry run with
--filter-id reads the filter to show the actions that the update removes.`,
		Args: cobra.NoArgs,
		Example: `  seasonpackarr setup filter --webhook-url http://seasonpackarr:42069 --autobrr-url http://autobrr:7474 --autobrr-api-key <key>
  seasonpackarr setup filter --webhook-url http://seasonpackarr:42069 --client tv --filter-id 12
  seasonpackarr setup filter --webhook-url http://127.0.0.1:42069 --client tv --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error { return o.run(cmd) },
	}
	f := cmd.Flags()
	f.StringVarP(&o.configDir, "config", "c", "", "read config.yaml from this directory")
	f.StringVarP(&o.client, "client", "n", "", "write the filter for this client only (default: each configured client)")
	f.StringVarP(&o.token, "api", "a", "", "seasonpackarr API token (default: environment or config)")
	f.StringVar(&o.webhookURL, "webhook-url", "", "seasonpackarr base URL that autobrr uses, including any proxy path")
	f.StringVar(&o.autobrrURL, "autobrr-url", "", "autobrr base URL (default: SEASONPACKARR__AUTOBRR_URL)")
	f.StringVar(&o.autobrrKey, "autobrr-api-key", "", "autobrr API key (default: SEASONPACKARR__AUTOBRR_API_KEY)")
	f.IntVar(&o.filterID, "filter-id", 0, "update this existing filter in place; needs exactly one client")
	f.BoolVar(&o.dryRun, "dry-run", false, "print each filter with the tokens redacted and write nothing to autobrr")
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
	if cmd.Flags().Changed("filter-id") {
		if o.filterID <= 0 {
			return fmt.Errorf("--filter-id must be a positive filter ID")
		}
		// One filter has one clientname, so an update must not guess the client.
		if len(clients) != 1 {
			return fmt.Errorf("--filter-id updates one filter, but %d clients are configured; select one with --client", len(clients))
		}
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
	// A dry run of an update reads the filter to show the actions that the update removes.
	if !o.dryRun || o.filterID != 0 {
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

	var stale []autobrr.StaleFilter
	if api != nil {
		existing, err := api.Filters(cmd.Context())
		if err != nil {
			return fmt.Errorf("list the autobrr filters: %w", err)
		}
		if o.filterID == 0 {
			if err := checkFilterNames(existing, clients, filters); err != nil {
				return err
			}
		}
		// After the name check, only the filter from --filter-id can be a target.
		others := slices.DeleteFunc(existing, func(other autobrr.FilterSummary) bool { return other.ID == o.filterID })
		if stale, err = warnStaleFilters(cmd.Context(), cmd.ErrOrStderr(), api, others, token); err != nil {
			return err
		}
	}

	if o.filterID != 0 {
		result, err := o.update(cmd.Context(), api, clients[0], filters[0], token)
		if err != nil {
			return err
		}
		result.StaleFilters = stale
		return o.print(cmd.OutOrStdout(), []setupFilterResult{result})
	}

	results := make([]setupFilterResult, 0, len(filters))
	var createErr error
	for i, filter := range filters {
		result := setupFilterResult{Client: clients[i], Name: filter.Name, StaleFilters: stale}
		if o.dryRun {
			result.Result, result.Filter = "dry-run", filter
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

// checkFilterNames fails when a filter with the name of a new filter already
// exists, before any write, so a rerun never gives a client two filters.
func checkFilterNames(existing []autobrr.FilterSummary, clients []string, filters []autobrr.Filter) error {
	var errs []error
	for i, filter := range filters {
		for _, other := range existing {
			if other.Name == filter.Name {
				errs = append(errs, fmt.Errorf("filter %d (%s) already exists for client %s; rerun with --filter-id %d --client %s",
					other.ID, terminalText(other.Name), terminalText(clients[i]), other.ID, terminalText(clients[i])))
			}
		}
	}
	return errors.Join(errs...)
}

// warnStaleFilters warns on stderr about each filter of filters that calls
// seasonpackarr, so the user can update it with --filter-id. The hint names no
// client, because the filter can belong to a client that the run did not select.
func warnStaleFilters(ctx context.Context, stderr io.Writer, api *autobrr.Client, filters []autobrr.FilterSummary, token string) ([]autobrr.StaleFilter, error) {
	stale, err := api.StaleFilters(ctx, filters, token)
	if err != nil {
		return nil, fmt.Errorf("search the autobrr filters that call seasonpackarr: %w", err)
	}
	for _, filter := range stale {
		fmt.Fprintf(stderr, "Warning: filter %d (%s) calls seasonpackarr at %s; update it with --filter-id %d --client <client>\n",
			filter.ID, terminalText(filter.Name), strings.Join(filter.Endpoints, ", "), filter.ID)
	}
	return stale, nil
}

// update replaces the External entries and the actions of the filter from
// --filter-id, or with --dry-run reads the filter and shows the changes.
func (o *setupFilterOptions) update(ctx context.Context, api *autobrr.Client, client string, filter autobrr.Filter, token string) (setupFilterResult, error) {
	stored, err := api.Filter(ctx, o.filterID, token)
	if err != nil {
		return setupFilterResult{}, fmt.Errorf("read filter %d: %w", o.filterID, err)
	}
	result := setupFilterResult{Client: client, ID: stored.ID, Name: stored.Name, RemovedActions: stored.Actions}
	if result.RemovedActions == nil {
		result.RemovedActions = []autobrr.StoredAction{}
	}
	if o.dryRun {
		result.Result, result.Filter = "dry-run", filterChanges{filter.External, filter.Actions}
		return result, nil
	}
	if err := api.UpdateFilter(ctx, stored, filter); err != nil {
		return setupFilterResult{}, fmt.Errorf("update filter %d for client %s: %w", stored.ID, terminalText(client), err)
	}
	result.Result = "updated"
	return result, nil
}

func (o *setupFilterOptions) print(w io.Writer, results []setupFilterResult) error {
	if o.json {
		return writeJSON(w, results)
	}
	created := false
	for _, result := range results {
		switch {
		case result.Filter != nil && result.RemovedActions != nil:
			fmt.Fprintf(w, "Dry run: update filter %d (%s) for client %s, not sent to autobrr\n", result.ID, terminalText(result.Name), terminalText(result.Client))
			printActions(w, "Would remove", result.RemovedActions)
		case result.Filter != nil:
			fmt.Fprintf(w, "Dry run: filter %s for client %s, not sent to autobrr\n", terminalText(result.Name), terminalText(result.Client))
		case result.Result == "updated":
			fmt.Fprintf(w, "Updated filter %d (%s) for client %s\n", result.ID, terminalText(result.Name), terminalText(result.Client))
			printActions(w, "Removed", result.RemovedActions)
			continue
		default:
			fmt.Fprintf(w, "Created filter %d (%s) for client %s\n", result.ID, terminalText(result.Name), terminalText(result.Client))
			created = true
			continue
		}
		if err := writeJSON(w, result.Filter); err != nil {
			return err
		}
	}
	if created {
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

// printActions prints each action of an update with verb, such as Removed.
func printActions(w io.Writer, verb string, actions []autobrr.StoredAction) {
	for _, action := range actions {
		fmt.Fprintf(w, "  %s action %d (%s, %s)\n", verb, action.ID, terminalText(action.Name), terminalText(action.Type))
	}
}
