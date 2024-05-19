// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"fmt"
	"strings"

	"seasonpackarr/internal/domain"
)

type MessageBuilder interface {
	BuildBody(payload domain.NotificationPayload) string
}

type ConditionMessagePart struct {
	Condition bool
	Format    string
	Bits      []interface{}
}

// MessageBuilderPlainText constructs the body of the notification message in plain text format.
type MessageBuilderPlainText struct{}

// BuildBody constructs the body of the notification message.
func (b *MessageBuilderPlainText) BuildBody(payload domain.NotificationPayload) string {
	messageParts := []ConditionMessagePart{
		{payload.Message != "", "%v\n", []interface{}{payload.Message}},
		{payload.ReleaseName != "", "Release name: %v\n", []interface{}{payload.ReleaseName}},
	}

	return formatMessageContent(messageParts)
}

func formatMessageContent(messageParts []ConditionMessagePart) string {
	var builder strings.Builder
	for _, part := range messageParts {
		if part.Condition {
			builder.WriteString(fmt.Sprintf(part.Format, part.Bits...))
		}
	}
	return builder.String()
}

// BuildTitle constructs the title of the notification message.
func BuildTitle(event domain.NotificationEvent) string {
	titles := map[domain.NotificationEvent]string{
		domain.NotificationEventAppUpdateAvailable: "seasonpackarr update available",
		domain.NotificationEventSuccessfulMatch:    "Successful Match",
		domain.NotificationEventNoMatch:            "No Match",
		domain.NotificationEventSuccessfulHardlink: "Successful Hardlink",
		domain.NotificationEventFailedHardlink:     "Failed Hardlink",
	}

	if title, ok := titles[event]; ok {
		return title
	}

	return "New Event"
}
