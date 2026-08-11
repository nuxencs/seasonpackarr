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

type processingResponse struct {
	Outcome domain.OutcomeKind `json:"outcome"`
	Reason  domain.Reason      `json:"reason"`
	Message string             `json:"message"`
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
			Str("reason", string(outcome.Reason())).
			Str("fault_class", string(outcome.FaultClass())).
			Msg("processing returned an invalid outcome")
		c.AbortWithStatusJSON(stdhttp.StatusInternalServerError, processingResponse{
			Outcome: domain.OutcomeFailure,
			Reason:  domain.ReasonInternalError,
			Message: domain.ReasonInternalError.Message(),
		})
		return
	}

	httpStatus := httpStatusForOutcome(outcome)
	log := p.log.With().
		Str("outcome", string(outcome.Kind())).
		Str("reason", string(outcome.Reason())).
		Str("fault_class", string(outcome.FaultClass())).
		Int("http_status", httpStatus).
		Logger()

	if operation.sendNotification {
		p.sendNotification(outcome, operation.notificationAction)
	}

	switch outcome.Kind() {
	case domain.OutcomeSuccess:
		log.Info().Msg(operation.successMessage)
		c.JSON(httpStatus, responseForOutcome(outcome))
	case domain.OutcomeRejection:
		log.Info().Msg(operation.rejectionMessage)
		c.AbortWithStatusJSON(httpStatus, responseForOutcome(outcome))
	case domain.OutcomeFailure:
		log.Error().Err(outcome.Cause()).Msg(operation.failureMessage)
		c.AbortWithStatusJSON(httpStatus, responseForOutcome(outcome))
	}
}

// httpStatusForOutcome is the HTTP adapter's complete transport policy. It
// must not inspect the semantic reason.
func httpStatusForOutcome(outcome domain.Outcome) int {
	switch outcome.Kind() {
	case domain.OutcomeSuccess:
		return stdhttp.StatusOK
	case domain.OutcomeRejection:
		return stdhttp.StatusUnprocessableEntity
	case domain.OutcomeFailure:
		switch outcome.FaultClass() {
		case domain.FaultRequest:
			return stdhttp.StatusBadRequest
		case domain.FaultDependency:
			return stdhttp.StatusBadGateway
		case domain.FaultInternal:
			return stdhttp.StatusInternalServerError
		}
	}

	return stdhttp.StatusInternalServerError
}

func responseForOutcome(outcome domain.Outcome) processingResponse {
	return processingResponse{
		Outcome: outcome.Kind(),
		Reason:  outcome.Reason(),
		Message: outcome.Reason().Message(),
	}
}
