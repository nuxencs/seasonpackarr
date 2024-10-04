// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import (
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// BuildTitle constructs the title of the notification message.
func BuildTitle(statusCode domain.StatusCode) string {
	return strings.ToUpper(string(statusCode.Message()[0])) + statusCode.Message()[1:]
}
