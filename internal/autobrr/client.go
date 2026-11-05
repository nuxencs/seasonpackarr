// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package autobrr

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	path := "/api/filters/" + strconv.Itoa(created.ID)
	// The update overwrites every field, so it sends back the complete stored filter.
	var stored map[string]any
	if err := c.do(ctx, http.MethodGet, path, nil, http.StatusOK, &stored, filter.token); err != nil {
		return created.ID, err
	}
	// autobrr reads omit empty arrays, but its update requires the NOT NULL arrays.
	for _, key := range []string{"resolutions", "sources", "codecs", "containers"} {
		if stored[key] == nil {
			stored[key] = []string{}
		}
	}
	stored["external"] = filter.External
	stored["actions"] = filter.Actions
	if err := c.do(ctx, http.MethodPut, path, stored, http.StatusOK, nil, filter.token); err != nil {
		return created.ID, err
	}
	return created.ID, nil
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
		return fmt.Errorf("create the request for %s %s; check --autobrr-url", method, path)
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
		return fmt.Errorf("could not reach autobrr for %s %s: %s; check --autobrr-url", method, path, c.safe(err.Error(), secret))
	}
	defer resp.Body.Close()
	const maxResponseBytes = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return fmt.Errorf("could not read the autobrr response for %s %s", method, path)
	}
	if resp.StatusCode != want {
		message := fmt.Sprintf("autobrr returned HTTP %d for %s %s", resp.StatusCode, method, path)
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			message += "; check --autobrr-api-key"
		case resp.StatusCode == http.StatusNotFound || (resp.StatusCode >= 300 && resp.StatusCode < 400):
			message += "; check --autobrr-url"
		}
		if detail := c.safe(string(data), secret); detail != "" {
			const maxDetail = 200
			if len(detail) > maxDetail {
				detail = strings.ToValidUTF8(detail[:maxDetail], "") + "..."
			}
			message += ": " + detail
		}
		return errors.New(message)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("invalid autobrr response for %s %s; check --autobrr-url", method, path)
		}
	}
	return nil
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
