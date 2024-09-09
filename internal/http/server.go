// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

var ErrServerClosed = http.ErrServerClosed

type Server struct {
	log  logger.Logger
	cfg  *config.AppConfig
	noti domain.Sender

	httpServer http.Server
}

func NewServer(log logger.Logger, config *config.AppConfig, notification domain.Sender) *Server {
	return &Server{
		log:  log,
		cfg:  config,
		noti: notification,
	}
}

func (s *Server) Open() error {
	var err error
	addr := fmt.Sprintf("%s:%d", s.cfg.Config.Host, s.cfg.Config.Port)

	for _, proto := range []string{"tcp", "tcp4", "tcp6"} {
		if err = s.tryToServe(addr, proto); err == nil {
			return nil
		}
		s.log.Error().Err(err).Msgf("Failed to start %s server on %s", proto, addr)
	}

	return fmt.Errorf("unable to start server on any protocol")
}

func (s *Server) tryToServe(addr, proto string) error {
	listener, err := net.Listen(proto, addr)
	if err != nil {
		return err
	}

	s.log.Info().Msgf("Starting server on %s with %s", listener.Addr().String(), proto)

	s.httpServer = http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info().Msg("Shutting down the server gracefully...")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	return nil
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
			newWebhookHandler(s.log, s.cfg, s.noti).Routes(api.Group("/"))
		}
	}

	return g
}
