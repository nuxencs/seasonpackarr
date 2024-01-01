// Copyright (c) 2021 - 2023, Ludvig Lundgren and the autobrr contributors.
// Code is slightly modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"seasonpackarr/internal/config"
	"seasonpackarr/internal/logger"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	log logger.Logger
	cfg *config.AppConfig
}

func NewServer(log logger.Logger, config *config.AppConfig) *Server {
	return &Server{
		log: log,
		cfg: config,
	}
}

func (s Server) Open() error {
	addr := fmt.Sprintf("%v:%v", s.cfg.Config.Host, s.cfg.Config.Port)

	var err error
	for _, proto := range []string{"tcp", "tcp4", "tcp6"} {
		if err = s.tryToServe(addr, proto); err == nil {
			break
		}

		s.log.Error().Err(err).Msgf("Failed to start %s server. Attempted to listen on %s", proto, addr)
	}

	return err
}

func (s Server) tryToServe(addr, proto string) error {
	listener, err := net.Listen(proto, addr)
	if err != nil {
		return err
	}

	s.log.Info().Msgf("Starting %s server. Listening on %s", proto, listener.Addr().String())

	server := http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: time.Second * 15,
	}

	return server.Serve(listener)
}

func (s Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Route("/api", func(r chi.Router) {
		r.Get("/healthz", s.handleHealth)

		r.Group(func(r chi.Router) {
			r.Use(s.isAuthenticated)

			r.Route("/", newWebhookHandler(s.log, s.cfg).Routes)
		})
	})

	return r
}
