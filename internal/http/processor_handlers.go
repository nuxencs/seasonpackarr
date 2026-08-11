// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"encoding/json"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/notification"

	"github.com/gin-gonic/gin"
)

var (
	candidateOperation = processingOperation{
		startMessage:     "starting season pack candidate check",
		sendNotification: false,
		successMessage:   "season pack candidate matched client releases",
		rejectionMessage: "season pack candidate rejected",
		failureMessage:   "error checking season pack candidate",
	}
	packOperation = processingOperation{
		startMessage:       "starting to process season pack request",
		notificationAction: "Pack",
		sendNotification:   true,
		successMessage:     "successfully matched season pack to episodes in client",
		rejectionMessage:   "season pack rejected",
		failureMessage:     "error processing season pack",
	}
	parseOperation = processingOperation{
		startMessage:       "starting to parse season pack torrent",
		notificationAction: "Parse",
		sendNotification:   true,
		successMessage:     "successfully parsed torrent, hardlinked episodes, and imported the season pack",
		rejectionMessage:   "season pack import rejected",
		failureMessage:     "error parsing torrent",
	}
	decodeOperation = processingOperation{
		failureMessage: domain.StatusDecodingError.String(),
	}
)

func (p *processor) CandidateSeasonPackHandler(c *gin.Context) {
	p.handleProcessing(c, candidateOperation, p.candidateSeasonPack)
}

func (p *processor) ProcessSeasonPackHandler(c *gin.Context) {
	p.handleProcessing(c, packOperation, p.processSeasonPack)
}

func (p *processor) ParseTorrentHandler(c *gin.Context) {
	p.handleProcessing(c, parseOperation, p.parseTorrent)
}

func (p *processor) handleProcessing(c *gin.Context, operation processingOperation, process func() domain.Outcome) {
	p.log.Info().Msg(operation.startMessage)
	if !p.decodeRequest(c) {
		return
	}

	p.writeProcessingOutcome(c, process(), operation)
}

func (p *processor) decodeRequest(c *gin.Context) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(&p.req); err != nil {
		p.writeProcessingOutcome(c, domain.FailedBecause(domain.StatusDecodingError, err), decodeOperation)
		return false
	}
	return true
}

func abortWithStatus(c *gin.Context, statusCode domain.StatusCode) {
	c.AbortWithStatusJSON(statusCode.Code(), gin.H{
		"statusCode": statusCode.Code(),
		"error":      statusCode.String(),
	})
}

func (p *processor) sendNotification(outcome domain.Outcome, action string) {
	if p.noti == nil {
		return
	}

	sender := p.noti
	payload := notification.Payload{
		ReleaseName: p.req.Name,
		Client:      p.req.ClientName,
		Action:      action,
	}
	log := p.log
	go func() {
		if sendErr := sender.Send(outcome, payload); sendErr != nil {
			log.Error().Err(sendErr).Msgf("error sending %s notification for %d", sender.Name(), outcome.StatusCode())
		}
	}()
}
