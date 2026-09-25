// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

type apiClient struct {
	baseURL string
	token   string
}

type apiResponse struct {
	status int
	body   []byte
}

func (c *apiClient) post(ctx context.Context, endpoint string, body io.Reader, timeout time.Duration) (apiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, body)
	if err != nil {
		return apiResponse{}, fmt.Errorf("could not create request; check the service URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Token", c.token)
	client := &http.Client{
		Timeout: timeout,
		// Redirects can change the operation or forward credentials to another service.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return apiResponse{}, fmt.Errorf("request stopped: %w", ctx.Err())
		}
		if requestError, ok := errors.AsType[*url.Error](err); ok {
			err = requestError.Err
		}
		return apiResponse{}, fmt.Errorf("request failed: %s; check that seasonpackarr is running and the connection settings are correct", c.safeMessage(err.Error()))
	}
	defer resp.Body.Close()
	const maxResponseBytes = 32 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return apiResponse{}, fmt.Errorf("could not read the complete service response")
	}
	if len(data) > maxResponseBytes {
		return apiResponse{}, fmt.Errorf("service response exceeds 32 MiB")
	}
	return apiResponse{status: resp.StatusCode, body: data}, nil
}

func (c *apiClient) message(response apiResponse) string {
	switch response.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication failed; set apiToken in config, SEASONPACKARR__API_TOKEN, or --api"
	case http.StatusNotFound:
		return "API endpoint not found; check --url and the service version"
	case int(domain.StatusClientNotFound):
		return "client not found on the service; select a configured client with --client"
	}
	if response.status >= 300 && response.status < 400 {
		return "service returned a redirect; set --url to the final service URL"
	}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(response.body, &body) == nil && body.Error != "" {
		return c.safeMessage(body.Error)
	}
	if message := domain.StatusCode(response.status).String(); message != "" {
		return message
	}
	return fmt.Sprintf("unexpected service response: HTTP %d", response.status)
}

func (c *apiClient) safeMessage(message string) string {
	if c.token != "" {
		message = strings.ReplaceAll(message, c.token, "[redacted]")
	}
	return terminalText(message)
}

func terminalText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
