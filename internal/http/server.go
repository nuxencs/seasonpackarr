// Copyright (c) 2021 - 2026, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
	"github.com/nuxencs/seasonpackarr/internal/logger"
)

var ErrServerClosed = http.ErrServerClosed

type Server struct {
	log   logger.Logger
	cfg   config.Provider
	noti  domain.Sender
	tasks *taskGroup

	httpServer http.Server
}

func NewServer(log logger.Logger, config config.Provider, notification domain.Sender) *Server {
	return &Server{
		log:   log,
		cfg:   config,
		noti:  notification,
		tasks: newTaskGroup(),
	}
}

func (s *Server) Open(ctx context.Context) error {
	var err error
	snapshot := s.cfg.Snapshot()
	addr := fmt.Sprintf("%s:%d", snapshot.Host, snapshot.Port)

	for _, proto := range []string{"tcp", "tcp4", "tcp6"} {
		if err = s.tryToServe(ctx, addr, proto); err == nil {
			return nil
		}
		s.log.Error().Err(err).Msgf("Failed to start %s server on %s", proto, addr)
	}

	return fmt.Errorf("unable to start server on any protocol")
}

func (s *Server) tryToServe(ctx context.Context, addr, proto string) error {
	listener, err := new(net.ListenConfig).Listen(ctx, proto, addr)
	if err != nil {
		return errtrace.WithStack(err)
	}

	s.log.Info().Msgf("Starting server on %s with %s", listener.Addr().String(), proto)

	s.httpServer = http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errtrace.WithStack(err)
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info().Msg("Shutting down the server gracefully...")
	var shutdownErrors []error
	if err := s.httpServer.Shutdown(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, errtrace.WithStack(fmt.Errorf("failed to shutdown server: %w", err)))
	}
	if err := s.tasks.Wait(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, errtrace.WithStack(fmt.Errorf("failed to finish background tasks: %w", err)))
	}
	return errors.Join(shutdownErrors...)
}

func (s *Server) Handler() http.Handler {
	// disable debug mode
	gin.SetMode(gin.ReleaseMode)

	g := gin.New()

	g.Use(gin.Recovery())
	g.Use(requestid.New())
	g.Use(CorsMiddleware())
	g.Use(LoggerMiddleware(s.log))

	api := g.Group("/api")
	{
		newHealthHandler().Routes(api.Group("/healthz"))

		api.Use(s.AuthMiddleware())
		{
			newWebhookHandler(s.log, s.cfg, s.noti, s.tasks).Routes(api.Group("/"))
		}
	}

	return g
}
