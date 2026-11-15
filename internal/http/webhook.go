// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/seasonpack"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// webhookHandler serves the candidate check, the match, and the import. It
// owns the payload, the response, and the notification of a request. The
// season pack processor owns the stages.
type webhookHandler struct {
	log       zerolog.Logger
	cfg       config.Provider
	inv       *inventory.Store
	processor *seasonpack.Processor
	noti      *notifier
}

// webhookRequest is the payload of the three webhook endpoints.
type webhookRequest struct {
	Name       string
	Torrent    json.RawMessage
	ClientName string
}

func newWebhookHandler(log logger.Logger, cfg config.Provider, inv *inventory.Store, processor *seasonpack.Processor, noti *notifier) *webhookHandler {
	return &webhookHandler{
		// The handler lines have the module of the stage lines, so one log filter shows a request.
		log:       log.With().Str("module", "processor").Logger(),
		cfg:       cfg,
		inv:       inv,
		processor: processor,
		noti:      noti,
	}
}

func (h *webhookHandler) Routes(r *gin.RouterGroup) {
	r.POST("/candidate", h.candidate)
	r.POST("/match", h.match)
	r.POST("/import", h.importPack)
}

func (h *webhookHandler) candidate(c *gin.Context) {
	h.log.Info().Msg("starting season pack candidate check")

	req, log, ok := h.decodeRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	client, outcome := h.connect(ctx, req.ClientName)
	if outcome.Err == nil {
		outcome = h.processor.Candidate(ctx, client, req.Name)
	}
	if outcome.Err != nil {
		if outcome.Class() == seasonpack.Rejected {
			log.Info().Err(outcome.Err).Msg("season pack candidate rejected")
		} else {
			log.Error().Err(outcome.Err).Msg("error checking season pack candidate")
		}
		abortWithError(c, outcome)
		return
	}

	log.Info().Msg("season pack candidate matched client releases")
	c.String(outcome.Status.Code(), outcome.Status.String())
}

func (h *webhookHandler) match(c *gin.Context) {
	h.log.Info().Msg("starting season pack match check")

	req, log, ok := h.decodeRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	client, torrentBytes, outcome := h.connectAndDecodeTorrent(ctx, req)
	if outcome.Err == nil {
		outcome = h.processor.Match(ctx, client, req.Name, seasonpack.TorrentBytes(torrentBytes))
	}
	if outcome.Err != nil {
		h.noti.send(log, outcome.Status, req.notification("Match", outcome.Err))
		if outcome.Class() == seasonpack.Rejected {
			log.Info().Err(outcome.Err).Msg("season pack rejected")
		} else {
			log.Error().Err(outcome.Err).Msg("error matching season pack")
		}
		abortWithError(c, outcome)
		return
	}

	// The import sends the success notification, so an accepted pack notifies once.
	log.Info().Msg("successfully matched season pack to episodes in client")
	c.String(outcome.Status.Code(), outcome.Status.String())
}

func (h *webhookHandler) importPack(c *gin.Context) {
	h.log.Info().Msg("starting season pack import")

	req, log, ok := h.decodeRequest(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	client, torrentBytes, outcome := h.connectAndDecodeTorrent(ctx, req)
	if outcome.Err == nil {
		outcome = h.processor.Import(ctx, client, req.Name, torrentBytes)
	}
	h.noti.send(log, outcome.Status, req.notification("Import", outcome.Err))
	if outcome.Err != nil {
		log.Error().Err(outcome.Err).Msg("error importing season pack")
		abortWithError(c, outcome)
		return
	}

	log.Info().Msg("successfully hardlinked episodes and imported the season pack")
	c.String(outcome.Status.Code(), outcome.Status.String())
}

// decodeRequest reads the payload and returns it with the logger of the
// request. An empty client name becomes "default". It writes the response of a
// payload that it cannot decode.
func (h *webhookHandler) decodeRequest(c *gin.Context) (webhookRequest, zerolog.Logger, bool) {
	var req webhookRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		h.log.Error().Err(err).Msgf("%s", domain.StatusDecodingError)
		abortWithError(c, seasonpack.Outcome{Status: domain.StatusDecodingError, Err: err})
		return webhookRequest{}, zerolog.Logger{}, false
	}
	if len(req.ClientName) == 0 {
		req.ClientName = "default"
		h.log.Info().Msg("no clientname defined. trying to use default client")
	}
	log := h.log.With().Str("release", req.Name).Str("clientname", req.ClientName).Logger()
	return req, log, true
}

func (req webhookRequest) notification(action string, err error) domain.NotificationPayload {
	return domain.NotificationPayload{
		ReleaseName: req.Name,
		Client:      req.ClientName,
		Action:      action,
		Error:       err,
	}
}

// connect returns the handle of the client for one request, so a config reload
// during the request cannot switch the adapter between the read and the
// import. The outcome has status 472 for a client that the config does not
// have, and status 471 for a client that cannot connect.
func (h *webhookHandler) connect(ctx context.Context, clientName string) (*inventory.Client, seasonpack.Outcome) {
	client, err := h.inv.Client(ctx, clientName, h.cfg.Snapshot())
	switch {
	case errors.Is(err, inventory.ErrUnknownClient):
		return nil, seasonpack.Outcome{Status: domain.StatusClientNotFound, Err: domain.StatusClientNotFound.Error()}
	case err != nil:
		return nil, seasonpack.Outcome{Status: domain.StatusGetClientError, Err: fmt.Errorf("%s: %w", domain.StatusGetClientError, err)}
	}
	return client, seasonpack.Outcome{}
}

// connectAndDecodeTorrent connects the client, then decodes the torrent
// payload of the match and the import. Both come before the processor call, so
// a bad request costs no client read. An empty payload stays empty: the
// processor gives it status 467.
func (h *webhookHandler) connectAndDecodeTorrent(ctx context.Context, req webhookRequest) (*inventory.Client, []byte, seasonpack.Outcome) {
	client, outcome := h.connect(ctx, req.ClientName)
	if outcome.Err != nil || len(req.Torrent) == 0 {
		return client, nil, outcome
	}
	torrentBytes, err := torrents.DecodeTorrentBytes(req.Torrent)
	if err != nil {
		return nil, nil, seasonpack.Outcome{
			Status: domain.StatusDecodeTorrentBytesError,
			Err:    fmt.Errorf("%s: %w", domain.StatusDecodeTorrentBytesError, err),
		}
	}
	return client, torrentBytes, seasonpack.Outcome{}
}

func abortWithError(c *gin.Context, outcome seasonpack.Outcome) {
	c.AbortWithStatusJSON(outcome.Status.Code(), gin.H{
		"statusCode": outcome.Status.Code(),
		"error":      outcome.Err.Error(),
	})
}
