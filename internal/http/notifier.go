// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"errors"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/rs/zerolog"
)

// notifier sends the notifications of the webhook handlers and of discovery
// in the server task group, so no request waits for a notification destination.
type notifier struct {
	sender domain.Sender
	tasks  *taskGroup
}

// send queues one notification. log gets one error line for each notification
// destination that fails.
func (n *notifier) send(log zerolog.Logger, statusCode domain.StatusCode, payload domain.NotificationPayload) {
	if n.sender == nil || n.tasks == nil {
		return
	}

	sender := n.sender
	n.tasks.Go(func(ctx context.Context) {
		sendErr := sender.Send(ctx, statusCode, payload)
		if sendErr == nil {
			return
		}
		// The sender joins one error for each failed notification destination. Log each on its own line.
		errs := []error{sendErr}
		if joined, ok := errors.AsType[interface {
			error
			Unwrap() []error
		}](sendErr); ok {
			errs = joined.Unwrap()
		}
		for _, err := range errs {
			log.Error().Err(err).Msgf("error sending %s for %d", sender.Name(), statusCode)
		}
	})
}
