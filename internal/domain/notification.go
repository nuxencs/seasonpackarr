// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

type Sender interface {
	Send(statusCode int, payload NotificationPayload) error
}

type NotificationPayload struct {
	Subject     string
	Message     string
	ReleaseName string
	Client      string
	Action      string
	Error       error
}
