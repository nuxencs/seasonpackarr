// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notification

import "github.com/nuxencs/seasonpackarr/internal/domain"

type Sender interface {
	Name() string
	Send(outcome domain.Outcome, payload Payload) error
}

const (
	LevelInfo  = "INFO"
	LevelError = "ERROR"
	LevelMatch = "MATCH"
)

type Payload struct {
	ReleaseName string
	Client      string
	Action      string
}
