// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package shoutrrrurl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestName(t *testing.T) {
	tests := map[string]string{
		"gotify://gotify.example/token":         "shoutrrr[2] (gotify)",
		"GOTIFY://gotify.example/token":         "shoutrrr[2] (gotify)",
		"generic+https://example.com/hook":      "shoutrrr[2] (generic+https)",
		"sentinel-no-scheme":                    "shoutrrr[2] (invalid scheme)",
		"://sentinel-host/path":                 "shoutrrr[2] (invalid scheme)",
		"sentinel token://host/path":            "shoutrrr[2] (invalid scheme)",
		"1gotify://gotify.example/sentinel-tok": "shoutrrr[2] (invalid scheme)",
	}
	for rawURL, want := range tests {
		assert.Equal(t, want, Name(2, rawURL), rawURL)
	}
}

func TestRedact(t *testing.T) {
	tests := map[string]struct {
		url, text, want string
	}{
		"userinfo": {
			url:  "telegram://123456:sentinel-secret@telegram?chats=1",
			text: `POST "https://api.telegram.org/bot123456:sentinel-secret/sendMessage": 401`,
			want: `POST "https://api.telegram.org/bot[redacted]/sendMessage": 401`,
		},
		"password alone": {
			url:  "smtp://user:sentinel-password@mail.example:587",
			text: "auth failed for sentinel-password",
			want: "auth failed for [redacted]",
		},
		"path and segments": {
			url:  "gotify://gotify.example/sub/sentinel-token",
			text: "GET /sub/sentinel-token and sentinel-token",
			want: "GET [redacted] and [redacted]",
		},
		"encoded query value": {
			url:  "generic://example.com/hook?@Authorization=Bearer%20sentinel-token",
			text: "header Bearer sentinel-token, escaped Bearer+sentinel-token",
			want: "header [redacted], escaped [redacted]",
		},
		"short parts stay": {
			url:  "generic://example.com/a?disabletls=yes&chats=1",
			text: "status 401: yes, 1 chat, a path",
			want: "status 401: yes, 1 chat, a path",
		},
		"list items of a query value": {
			url:  "telegram://123:abc@telegram?chats=sentinel-chat,-1001",
			text: `sending to chat "sentinel-chat": 400`,
			want: `sending to chat "[redacted]": 400`,
		},
		"whole URL": {
			url:  "ntfy://ntfy.sh/sentinel-topic",
			text: "request to ntfy://ntfy.sh/sentinel-topic failed",
			want: "request to [redacted] failed",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, Redact(tt.text, []string{"ntfy://ntfy.sh/other", tt.url}))
		})
	}
}
