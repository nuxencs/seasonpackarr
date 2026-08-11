// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	stdhttp "net/http"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/gin-gonic/gin"
)

type processingOperation struct {
	startMessage       string
	notificationAction string
	sendNotification   bool
	successMessage     string
	rejectionMessage   string
	failureMessage     string
}

func (p *processor) writeProcessingOutcome(
	c *gin.Context,
	outcome domain.Outcome,
	operation processingOperation,
) {
	if err := outcome.Validate(); err != nil {
		p.log.Error().
			Err(err).
			Str("outcome", string(outcome.Kind())).
			Int("status_code", outcome.StatusCode().Code()).
			Msg("processing returned an invalid outcome")
		c.AbortWithStatusJSON(stdhttp.StatusInternalServerError, gin.H{
			"statusCode": stdhttp.StatusInternalServerError,
			"error":      "internal processing error",
		})
		return
	}

	statusCode := outcome.StatusCode()
	log := p.log.With().
		Str("outcome", string(outcome.Kind())).
		Int("status_code", statusCode.Code()).
		Logger()

	if operation.sendNotification {
		p.sendNotification(outcome, operation.notificationAction)
	}

	switch outcome.Kind() {
	case domain.OutcomeSuccess:
		log.Info().Msg(operation.successMessage)
		c.String(statusCode.Code(), statusCode.String())
	case domain.OutcomeRejection:
		log.Info().Msg(operation.rejectionMessage)
		abortWithStatus(c, statusCode)
	case domain.OutcomeFailure:
		log.Error().Err(outcome.Cause()).Msg(operation.failureMessage)
		abortWithStatus(c, statusCode)
	}
}
