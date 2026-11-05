// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package autobrr holds the seasonpackarr filter definition and a client for
// the autobrr REST API. docs/AUTOBRR.md describes the same filter for a manual
// setup, and a test in cmd compares the two.
package autobrr

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"strings"
)

const redacted = "[redacted]"

// Filter is the complete autobrr filter for one seasonpackarr client.
type Filter struct {
	Base
	External []External `json:"external"`
	Actions  []Action   `json:"actions"`

	// token is the seasonpackarr API token, kept for error redaction.
	token string
}

// Base holds the base settings of a new filter, the only part that a create
// stores in autobrr.
type Base struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Seasons  string `json:"seasons"`
	Episodes string `json:"episodes"`
	// autobrr stores these arrays as NOT NULL, so a create must send them.
	Resolutions []string `json:"resolutions"`
	Sources     []string `json:"sources"`
	Codecs      []string `json:"codecs"`
	Containers  []string `json:"containers"`
}

// External is an autobrr External webhook entry. autobrr runs the entry with
// the highest Index first.
type External struct {
	Name                string `json:"name"`
	Index               int    `json:"index"`
	Type                string `json:"type"`
	Enabled             bool   `json:"enabled"`
	OnError             string `json:"on_error"`
	WebhookHost         string `json:"webhook_host"`
	WebhookMethod       string `json:"webhook_method"`
	WebhookHeaders      string `json:"webhook_headers,omitempty"`
	WebhookExpectStatus int    `json:"webhook_expect_status"`
	WebhookData         string `json:"webhook_data"`
}

// Action is an autobrr Webhook action.
type Action struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Enabled       bool   `json:"enabled"`
	WebhookType   string `json:"webhook_type"`
	WebhookMethod string `json:"webhook_method"`
	WebhookHost   string `json:"webhook_host"`
	WebhookData   string `json:"webhook_data"`
}

// NewFilter returns the filter that sends announces for client to the
// seasonpackarr service at webhookURL. An empty token gives a filter without
// authentication.
func NewFilter(client, webhookURL, token string) (Filter, error) {
	return newFilter(client, webhookURL, token, false)
}

// NewRedactedFilter returns NewFilter with the token replaced by a marker, for
// display.
func NewRedactedFilter(client, webhookURL, token string) (Filter, error) {
	return newFilter(client, webhookURL, token, true)
}

func newFilter(client, webhookURL, token string, redact bool) (Filter, error) {
	webhookURL = strings.TrimRight(webhookURL, "/")
	candidateData, err := templateJSON(struct {
		Name       string `json:"name"`
		ClientName string `json:"clientname"`
	}{"{{ .TorrentName }}", client})
	if err != nil {
		return Filter{}, err
	}
	matchData, err := templateJSON(struct {
		Name       string `json:"name"`
		Torrent    string `json:"torrent"`
		ClientName string `json:"clientname"`
	}{"{{ .TorrentName }}", "{{ .TorrentDataRawBytes | js }}", client})
	if err != nil {
		return Filter{}, err
	}
	var headers, query string
	if token != "" {
		shown, escaped := token, url.QueryEscape(token)
		if redact {
			shown, escaped = redacted, redacted
		}
		headers = "X-API-Token=" + shown
		// The autobrr Webhook action has no header field, so the token goes in the query.
		query = "?apikey=" + escaped
	}
	external := func(name string, index int, endpoint, data string) External {
		return External{
			Name: name, Index: index, Type: "WEBHOOK", Enabled: true, OnError: "REJECT",
			WebhookHost: webhookURL + endpoint, WebhookMethod: "POST", WebhookHeaders: headers,
			WebhookExpectStatus: 250, WebhookData: data,
		}
	}
	return Filter{
		Name: "seasonpackarr-" + client, Seasons: "1-99", Episodes: "0",
		Resolutions: []string{}, Sources: []string{}, Codecs: []string{}, Containers: []string{},
		External: []External{
			// The candidate check gets the higher index, so autobrr runs it before it downloads the torrent.
			external("1. Candidate check", 1, "/api/candidate", candidateData),
			external("2. Match", 0, "/api/match", matchData),
		},
		Actions: []Action{{
			Name: "Import with seasonpackarr", Type: "WEBHOOK", Enabled: true, WebhookType: "JSON",
			WebhookMethod: "POST", WebhookHost: webhookURL + "/api/import" + query, WebhookData: matchData,
		}},
		token: token,
	}, nil
}

// templateJSON formats an autobrr data template like the blocks in docs/AUTOBRR.md.
func templateJSON(value any) (string, error) {
	data, err := json.Marshal(value, jsontext.WithIndent("  "))
	if err != nil {
		return "", fmt.Errorf("client name must be valid UTF-8: %w", err)
	}
	return string(data), nil
}
