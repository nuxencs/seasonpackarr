// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// BuildTitle constructs the title of the notification message.
func BuildTitle(reason domain.Reason) string {
	message := reason.Message()
	if message == "" {
		return ""
	}
	return strings.ToUpper(message[:1]) + message[1:]
}
