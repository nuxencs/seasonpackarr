// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is heavily modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

type NotificationPayload struct {
	Message     string
	Event       NotificationEvent
	ReleaseName string
}

type NotificationEvent string

const (
	NotificationEventAppUpdateAvailable NotificationEvent = "APP_UPDATE_AVAILABLE"
	NotificationEventSuccessfulMatch    NotificationEvent = "SUCCESSFUL_MATCH"
	NotificationEventNoMatch            NotificationEvent = "NO_MATCH"
	NotificationEventSuccessfulHardlink NotificationEvent = "SUCCESSFUL_HARDLINK"
	NotificationEventFailedHardlink     NotificationEvent = "FAILED_HARDLINK"
)
