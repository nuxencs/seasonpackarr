// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/gin-gonic/gin"
)

func (p *processor) CandidateSeasonPackHandler(c *gin.Context) {
	p.log.Info().Msg("starting season pack candidate check")

	if !p.decodeRequest(c) {
		return
	}

	statusCode, err := p.candidateSeasonPack(c.Request.Context())
	if err != nil {
		if isExpectedGateRejection(statusCode) {
			p.log.Info().Err(err).Msg("season pack candidate rejected")
		} else {
			p.log.Error().Err(err).Msg("error checking season pack candidate")
		}
		abortWithError(c, statusCode, err)
		return
	}

	p.log.Info().Msg("season pack candidate matched client releases")
	c.String(statusCode.Code(), statusCode.String())
}

func (p *processor) MatchSeasonPackHandler(c *gin.Context) {
	p.log.Info().Msg("starting season pack match check")

	if !p.decodeRequest(c) {
		return
	}

	statusCode, err := p.matchSeasonPack(c.Request.Context())
	if err != nil {
		p.sendNotification(statusCode, "Match", err)
		if isExpectedGateRejection(statusCode) {
			p.log.Info().Err(err).Msg("season pack rejected")
		} else {
			p.log.Error().Err(err).Msg("error matching season pack")
		}
		abortWithError(c, statusCode, err)
		return
	}

	p.sendNotification(statusCode, "Match", nil)
	p.log.Info().Msg("successfully matched season pack to episodes in client")
	c.String(statusCode.Code(), statusCode.String())
}

func isExpectedGateRejection(statusCode domain.StatusCode) bool {
	return statusCode.Code() < http.StatusBadRequest || statusCode == domain.StatusFailedMatchToTorrentEps
}

func (p *processor) ImportSeasonPackHandler(c *gin.Context) {
	p.log.Info().Msg("starting season pack import")

	if !p.decodeRequest(c) {
		return
	}

	statusCode, err := p.importSeasonPack(c.Request.Context())
	if err != nil {
		p.sendNotification(statusCode, "Import", err)
		p.log.Error().Err(err).Msg("error importing season pack")
		abortWithError(c, statusCode, err)
		return
	}

	p.sendNotification(statusCode, "Import", nil)
	p.log.Info().Msg("successfully hardlinked episodes and imported the season pack")
	c.String(statusCode.Code(), statusCode.String())
}

func (p *processor) decodeRequest(c *gin.Context) bool {
	if err := json.NewDecoder(c.Request.Body).Decode(&p.req); err != nil {
		p.log.Error().Err(err).Msgf("%s", domain.StatusDecodingError)
		abortWithError(c, domain.StatusDecodingError, err)
		return false
	}
	return true
}

func abortWithError(c *gin.Context, statusCode domain.StatusCode, err error) {
	c.AbortWithStatusJSON(statusCode.Code(), gin.H{
		"statusCode": statusCode.Code(),
		"error":      err.Error(),
	})
}

func (p *processor) sendNotification(statusCode domain.StatusCode, action string, err error) {
	if p.noti == nil || p.tasks == nil {
		return
	}

	sender := p.noti
	payload := domain.NotificationPayload{
		ReleaseName: p.req.Name,
		Client:      p.req.ClientName,
		Action:      action,
		Error:       err,
	}
	log := p.log
	p.tasks.Go(func(ctx context.Context) {
		if sendErr := sender.Send(ctx, statusCode, payload); sendErr != nil {
			log.Error().Err(sendErr).Msgf("error sending %s notification for %d", sender.Name(), statusCode)
		}
	})
}
