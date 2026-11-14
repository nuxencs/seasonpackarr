// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/shoutrrrurl"

	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"github.com/rs/zerolog"
)

// sendTimeout bounds one send to one notification destination.
const sendTimeout = 30 * time.Second

// message is one notification, before it gets the format of a destination.
type message struct {
	title      string
	statusCode domain.StatusCode
	payload    domain.NotificationPayload
}

// Result is the outcome of one send to one notification destination.
type Result struct {
	// Destination names the destination without its URL, for example "discord" or "shoutrrr[0] (gotify)".
	Destination string
	// Err holds no part of a configured URL that can hold a secret.
	Err error
}

type notificationSender struct {
	log        zerolog.Logger
	cfg        config.Provider
	httpClient *http.Client
	// shoutrrrClient replaces the HTTP client of each Shoutrrr service when it is set.
	// It is nil outside tests, because a service applies URL settings such as skiptlsverify to its own client.
	shoutrrrClient *http.Client
	timeout        time.Duration
}

// NewSender returns the sender that sends each notification to every
// notification destination of the current config: the Discord webhook and
// each Shoutrrr URL.
func NewSender(log logger.Logger, cfg config.Provider) domain.Sender {
	return &notificationSender{
		log:        log.With().Str("sender", "notification").Logger(),
		cfg:        cfg,
		httpClient: &http.Client{},
		timeout:    sendTimeout,
	}
}

func (s *notificationSender) Name() string {
	return "notification"
}

// Send sends the notification to each destination that the notification level
// allows. The error joins one error for each failed destination, and each
// error starts with the destination name.
func (s *notificationSender) Send(ctx context.Context, statusCode domain.StatusCode, payload domain.NotificationPayload) error {
	notifications := s.cfg.Snapshot().Notifications
	if !levelAllows(notifications.NotificationLevel, statusCode) {
		s.log.Debug().Msg("no notification wanted for this status, skipping notification")
		return nil
	}

	results := sendAll(ctx, s.httpClient, s.shoutrrrClient, s.timeout, notifications, message{
		title:      BuildTitle(statusCode),
		statusCode: statusCode,
		payload:    payload,
	})
	if len(results) == 0 {
		s.log.Debug().Msg("no notification destination configured, skipping notification")
		return nil
	}

	var errs []error
	for _, result := range results {
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", result.Destination, result.Err))
			continue
		}
		s.log.Debug().Str("destination", result.Destination).Msg("notification sent")
	}
	return errors.Join(errs...)
}

// SendTest sends one fixed test notification to each destination in
// notifications, whatever the notification level. It returns one result for
// each destination, in config order, and no result when none is configured.
func SendTest(ctx context.Context, notifications domain.Notifications) []Result {
	return sendAll(ctx, &http.Client{}, nil, sendTimeout, notifications, message{
		title:      "Test notification",
		statusCode: domain.StatusSuccessfulMatch,
		payload: domain.NotificationPayload{
			ReleaseName: "Series.S01.1080p.WEB-DL-GROUP",
			Client:      "default",
			Action:      "Test",
		},
	})
}

// sendAll sends m to each destination at the same time, each bounded by
// timeout, and returns one result for each destination, in config order. A
// failed destination does not stop the others.
func sendAll(ctx context.Context, httpClient, shoutrrrClient *http.Client, timeout time.Duration, notifications domain.Notifications, m message) []Result {
	type destination struct {
		name string
		send func(context.Context) error
	}
	var destinations []destination
	if notifications.Discord != "" {
		destinations = append(destinations, destination{"discord", func(ctx context.Context) error {
			return sendDiscord(ctx, httpClient, notifications.Discord, buildEmbed(m))
		}})
	}
	for i, rawURL := range notifications.Shoutrrr {
		destinations = append(destinations, destination{shoutrrrurl.Name(i, rawURL), func(ctx context.Context) error {
			err := sendShoutrrr(ctx, rawURL, shoutrrrClient, timeout, m)
			if err == nil {
				return nil
			}
			// Service errors can quote the URL, so remove the parts of every configured URL.
			return errors.New(shoutrrrurl.Redact(err.Error(), notifications.Shoutrrr))
		}})
	}

	results := make([]Result, len(destinations))
	var wg sync.WaitGroup
	for i, d := range destinations {
		wg.Go(func() {
			sendCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			results[i] = Result{Destination: d.name, Err: d.send(sendCtx)}
		})
	}
	wg.Wait()
	return results
}

func sendShoutrrr(ctx context.Context, rawURL string, httpClient *http.Client, timeout time.Duration, m message) error {
	options := types.SenderOptions{Timeout: timeout}
	if httpClient != nil {
		options.HTTPClient = httpClient
	}
	// A nil logger discards the service output, which can hold the URL.
	serviceRouter, err := router.NewWithOptions(nil, options, rawURL)
	if err != nil {
		return err
	}
	defer func() { _ = serviceRouter.Close() }()

	params := types.Params{types.TitleKey: m.title}
	return serviceRouter.SendContext(ctx, plainText(m), &params)[0]
}

// plainText is the body of a Shoutrrr notification: the fields of the Discord
// embed, one line each, without markdown.
func plainText(m message) string {
	var lines []string
	for _, field := range messageFields(m) {
		lines = append(lines, field.name+": "+field.value)
	}
	return strings.Join(lines, "\n")
}

type messageField struct {
	name, value string
	// isError marks the error, which the Discord embed shows as a code block.
	isError bool
}

// messageFields returns the fields that are set. Error is only set for a status
// code of the ERROR level.
func messageFields(m message) []messageField {
	var fields []messageField
	if m.payload.ReleaseName != "" {
		fields = append(fields, messageField{name: "Release Name", value: m.payload.ReleaseName})
	}
	if m.payload.Client != "" {
		fields = append(fields, messageField{name: "Client", value: m.payload.Client})
	}
	if m.payload.Action != "" {
		fields = append(fields, messageField{name: "Action", value: m.payload.Action})
	}
	if m.payload.Error != nil && isError(m.statusCode) {
		fields = append(fields, messageField{name: "Error", value: m.payload.Error.Error(), isError: true})
	}
	return fields
}

func isError(statusCode domain.StatusCode) bool {
	return slices.Contains(domain.NotificationStatusMap[domain.NotificationLevelError], statusCode)
}

func levelAllows(levels []string, statusCode domain.StatusCode) bool {
	for _, level := range levels {
		if slices.Contains(domain.NotificationStatusMap[level], statusCode) {
			return true
		}
	}
	return false
}
