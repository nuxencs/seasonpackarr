// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package autobrr

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Client calls the autobrr REST API.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient returns a client for the autobrr instance at baseURL, a validated
// HTTP or HTTPS URL.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http: &http.Client{
			Timeout: 30 * time.Second,
			// A redirect could forward the API key to another service.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// StoredFilter is a filter as autobrr stores it.
type StoredFilter struct {
	ID      int
	Name    string
	Actions []StoredAction
	// Endpoints are the seasonpackarr endpoints that the External entries and
	// actions call, in the order of seasonpackarrEndpoints.
	Endpoints []string
	// fields is the complete stored filter, because an update overwrites every field.
	fields map[string]any
}

// StoredAction is an action of a stored filter.
type StoredAction struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// FilterSummary is one filter of the autobrr filter list.
type FilterSummary struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// StaleFilter is a filter that calls seasonpackarr, for example a v0.16
// filter that calls /api/pack.
type StaleFilter struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Endpoints []string `json:"endpoints"`
}

// seasonpackarrEndpoints are the endpoints that a filter for v0.16 or later calls.
var seasonpackarrEndpoints = []string{"/api/pack", "/api/parse", "/api/candidate", "/api/match", "/api/import"}

// Filters lists the filters in autobrr.
func (c *Client) Filters(ctx context.Context) ([]FilterSummary, error) {
	var filters []FilterSummary
	if err := c.do(ctx, http.MethodGet, "/api/filters", nil, http.StatusOK, &filters, ""); err != nil {
		return nil, err
	}
	return filters, nil
}

// StaleFilters reads each filter of filters and returns those that call
// seasonpackarr. It only reads, so it never changes those filters. secret is
// a seasonpackarr token to redact from error messages.
func (c *Client) StaleFilters(ctx context.Context, filters []FilterSummary, secret string) ([]StaleFilter, error) {
	var stale []StaleFilter
	for _, summary := range filters {
		// The autobrr filter list has no External entries and actions, so read each filter.
		stored, err := c.filter(ctx, summary.ID, secret)
		if err != nil {
			return nil, fmt.Errorf("read filter %d: %w", summary.ID, err)
		}
		if len(stored.Endpoints) > 0 {
			stale = append(stale, StaleFilter{ID: stored.ID, Name: stored.Name, Endpoints: stored.Endpoints})
		}
	}
	return stale, nil
}

// CreateFilter creates filter in autobrr and returns its ID. A create stores
// only the base settings, so CreateFilter reads the stored filter, sets the
// External entries and actions, and sends it back.
func (c *Client) CreateFilter(ctx context.Context, filter Filter) (int, error) {
	var created struct {
		ID int `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/filters", filter.Base, http.StatusCreated, &created, filter.token); err != nil {
		return 0, err
	}
	if created.ID == 0 {
		return 0, fmt.Errorf("autobrr returned no filter ID for POST /api/filters; check --autobrr-url")
	}
	stored, err := c.filter(ctx, created.ID, filter.token)
	if err != nil {
		return created.ID, err
	}
	return created.ID, c.UpdateFilter(ctx, stored, filter)
}

// Filter reads the filter with id from --filter-id. secret is a seasonpackarr
// token to redact from error messages.
func (c *Client) Filter(ctx context.Context, id int, secret string) (StoredFilter, error) {
	stored, err := c.filter(ctx, id, secret)
	if status, ok := errors.AsType[*statusError](err); ok && status.status == http.StatusNotFound {
		status.hint = "check --filter-id"
	}
	return stored, err
}

func (c *Client) filter(ctx context.Context, id int, secret string) (StoredFilter, error) {
	path := "/api/filters/" + strconv.Itoa(id)
	var data jsontext.Value
	if err := c.do(ctx, http.MethodGet, path, nil, http.StatusOK, &data, secret); err != nil {
		return StoredFilter{}, err
	}
	var stored StoredFilter
	var known struct {
		ID      int            `json:"id"`
		Name    string         `json:"name"`
		Actions []StoredAction `json:"actions"`
	}
	type webhook struct {
		Host string `json:"webhook_host"`
	}
	// A separate decode, so the hosts and their tokens never reach the printed actions.
	var webhooks struct {
		External []webhook `json:"external"`
		Actions  []webhook `json:"actions"`
	}
	if err := errors.Join(json.Unmarshal(data, &known), json.Unmarshal(data, &webhooks), json.Unmarshal(data, &stored.fields)); err != nil {
		return StoredFilter{}, fmt.Errorf("invalid autobrr response for GET %s; check --autobrr-url: %w", path, err)
	}
	if known.ID != id {
		return StoredFilter{}, fmt.Errorf("autobrr returned filter %d for GET %s; check --autobrr-url", known.ID, path)
	}
	stored.ID, stored.Name, stored.Actions = known.ID, known.Name, known.Actions
	hooks := slices.Concat(webhooks.External, webhooks.Actions)
	for _, endpoint := range seasonpackarrEndpoints {
		if slices.ContainsFunc(hooks, func(w webhook) bool { return strings.Contains(w.Host, endpoint) }) {
			stored.Endpoints = append(stored.Endpoints, endpoint)
		}
	}
	return stored, nil
}

// UpdateFilter replaces the External entries and all actions of stored with
// those of filter, and keeps every other field.
func (c *Client) UpdateFilter(ctx context.Context, stored StoredFilter, filter Filter) error {
	// An update keeps the stored actions that it does not send, so delete them
	// first. A later failure leaves a filter without actions, which downloads
	// nothing, and never a filter with two download actions.
	for i, action := range stored.Actions {
		if err := c.do(ctx, http.MethodDelete, "/api/actions/"+strconv.Itoa(action.ID), nil, http.StatusNoContent, nil, filter.token); err != nil {
			if i > 0 {
				return fmt.Errorf("%w; autobrr removed some old actions, so rerun the command", err)
			}
			return err
		}
	}
	fields := maps.Clone(stored.fields)
	// autobrr reads omit empty arrays, but its update requires the NOT NULL arrays.
	for _, key := range []string{"resolutions", "sources", "codecs", "containers"} {
		if fields[key] == nil {
			fields[key] = []string{}
		}
	}
	fields["id"] = stored.ID
	fields["external"] = filter.External
	fields["actions"] = filter.Actions
	if err := c.do(ctx, http.MethodPut, "/api/filters/"+strconv.Itoa(stored.ID), fields, http.StatusOK, nil, filter.token); err != nil {
		if len(stored.Actions) > 0 {
			return fmt.Errorf("%w; autobrr removed the old actions, so the filter has no action until you rerun the command", err)
		}
		return err
	}
	// autobrr reads a filter without actions when its action lookup fails, so an old download action can stay.
	updated, err := c.filter(ctx, stored.ID, filter.token)
	if err != nil {
		return fmt.Errorf("read the updated filter: %w", err)
	}
	if len(updated.Actions) != len(filter.Actions) {
		actions := make([]string, 0, len(updated.Actions))
		for _, action := range updated.Actions {
			actions = append(actions, fmt.Sprintf("action %d (%s, %s)", action.ID, c.safe(action.Name, filter.token), c.safe(action.Type, filter.token)))
		}
		return fmt.Errorf("filter %d has %d actions after the update, but the update sends %d: %s; delete the old actions in autobrr",
			stored.ID, len(updated.Actions), len(filter.Actions), strings.Join(actions, ", "))
	}
	return nil
}

// do sends one request and decodes the response into out. secret is a
// seasonpackarr token to redact from error messages.
func (c *Client) do(ctx context.Context, method, path string, in any, want int, out any, secret string) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode the request for %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create the request for %s %s; check --autobrr-url: %w", method, path, err)
	}
	req.Header.Set("X-API-Token", c.apiKey)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("autobrr request stopped: %w", ctx.Err())
		}
		if requestError, ok := errors.AsType[*url.Error](err); ok {
			err = requestError.Err
		}
		// The text only, so the redaction cannot be bypassed through the wrapped error.
		return fmt.Errorf("could not reach autobrr for %s %s: %s; check --autobrr-url", method, path, c.safe(err.Error(), secret))
	}
	defer resp.Body.Close()
	const maxResponseBytes = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("could not read the autobrr response for %s %s: %w", method, path, err)
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("the autobrr response for %s %s exceeds 8 MiB", method, path)
	}
	if resp.StatusCode != want {
		err := &statusError{status: resp.StatusCode, request: method + " " + path}
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			err.hint = "check --autobrr-api-key"
		case resp.StatusCode == http.StatusNotFound || (resp.StatusCode >= 300 && resp.StatusCode < 400):
			err.hint = "check --autobrr-url"
		}
		if err.detail = c.safe(string(data), secret); len(err.detail) > maxDetail {
			err.detail = strings.ToValidUTF8(err.detail[:maxDetail], "") + "..."
		}
		return err
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("invalid autobrr response for %s %s; check --autobrr-url: %w", method, path, err)
		}
	}
	return nil
}

const maxDetail = 200

// statusError is an autobrr response with an unexpected HTTP status.
type statusError struct {
	status       int
	request      string
	hint, detail string
}

func (e *statusError) Error() string {
	message := fmt.Sprintf("autobrr returned HTTP %d for %s", e.status, e.request)
	if e.hint != "" {
		message += "; " + e.hint
	}
	if e.detail != "" {
		message += ": " + e.detail
	}
	return message
}

// safe removes secrets and control characters from text that the command prints.
func (c *Client) safe(text, secret string) string {
	for _, value := range []string{c.apiKey, secret, url.QueryEscape(secret)} {
		if value != "" {
			text = strings.ReplaceAll(text, value, redacted)
		}
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text))
}
