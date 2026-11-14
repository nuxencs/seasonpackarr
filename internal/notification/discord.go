// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
)

type DiscordMessage struct {
	Content any             `json:"content"`
	Embeds  []DiscordEmbeds `json:"embeds,omitempty"`
}

type DiscordEmbeds struct {
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Color       int                   `json:"color"`
	Fields      []DiscordEmbedsFields `json:"fields,omitempty"`
	Timestamp   time.Time             `json:"timestamp"`
}

type DiscordEmbedsFields struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitzero"`
}

type EmbedColors int

const (
	RED   EmbedColors = 0xed4245
	GREEN EmbedColors = 0x57f287
	GRAY  EmbedColors = 0x99aab5
)

// sendDiscord posts embed to the Discord webhook. The error holds no part of the webhook URL.
func sendDiscord(ctx context.Context, httpClient *http.Client, webhook string, embed DiscordEmbeds) error {
	m := DiscordMessage{
		Content: nil,
		Embeds:  []DiscordEmbeds{embed},
	}

	jsonData, err := json.Marshal(m)
	if err != nil {
		return errtrace.WithStack(fmt.Errorf("could not marshal JSON request: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(jsonData))
	if err != nil {
		return errtrace.WithStack(fmt.Errorf("could not create request: %w", withoutURL(err)))
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := httpClient.Do(req)
	if err != nil {
		return errtrace.WithStack(fmt.Errorf("client request error: %w", withoutURL(err)))
	}

	defer func() { _ = res.Body.Close() }()

	// discord responds with 204, Notifiarr with 204 so lets take all 200 as ok
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		body, err := io.ReadAll(bufio.NewReader(res.Body))
		if err != nil {
			return errtrace.WithStack(fmt.Errorf("could not read response body: %w", err))
		}

		return errtrace.WithStack(fmt.Errorf("unexpected status %v with body %v", res.StatusCode, string(body)))
	}

	return nil
}

// withoutURL returns the cause of a request error without the request URL. The
// webhook URL holds the token, and the caller logs the error.
func withoutURL(err error) error {
	if requestErr, ok := errors.AsType[*url.Error](err); ok {
		return requestErr.Err
	}
	return err
}

func buildEmbed(m message) DiscordEmbeds {
	var color EmbedColors

	if slices.Contains(domain.NotificationStatusMap[domain.NotificationLevelInfo], m.statusCode) { // not matching
		color = GRAY
	} else if isError(m.statusCode) { // error processing
		color = RED
	} else { // success
		color = GREEN
	}

	var fields []DiscordEmbedsFields
	for _, field := range messageFields(m) {
		if field.isError {
			fields = append(fields, DiscordEmbedsFields{Name: field.name, Value: fmt.Sprintf("```%s```", field.value)})
			continue
		}
		fields = append(fields, DiscordEmbedsFields{Name: field.name, Value: field.value, Inline: true})
	}

	return DiscordEmbeds{
		Title:     m.title,
		Color:     int(color),
		Fields:    fields,
		Timestamp: time.Now(),
	}
}
