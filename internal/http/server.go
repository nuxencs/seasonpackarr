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

	"github.com/nuxencs/seasonpackarr/internal/config"
	"github.com/nuxencs/seasonpackarr/internal/discovery"
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/errtrace"
	"github.com/nuxencs/seasonpackarr/internal/inventory"
	"github.com/nuxencs/seasonpackarr/internal/logger"
	"github.com/nuxencs/seasonpackarr/internal/seasonpack"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

var ErrServerClosed = http.ErrServerClosed

type Server struct {
	log   logger.Logger
	cfg   config.Provider
	inv   *inventory.Store
	tasks *taskGroup
	noti  *notifier
	// processor runs the stages for the webhook handlers and for search, so
	// both share its import plans and its endpoint import locks.
	processor *seasonpack.Processor
	search    *searchRunner
	// lifecycleTasks holds the search schedule, the startup inventory read, and the source folder scan schedule.
	// Shutdown cancels them first, so they cannot hold the HTTP drain.
	lifecycleTasks *taskGroup

	httpServer http.Server
}

// NewServer creates the server. inv holds the client inventories that the
// webhooks and search share. source is the Prowlarr discovery source of search.
func NewServer(log logger.Logger, config config.Provider, notification domain.Sender, source *discovery.Source, inv *inventory.Store) *Server {
	tasks := newTaskGroup()
	noti := &notifier{sender: notification, tasks: tasks}
	processor := seasonpack.New(log, inv)
	return &Server{
		log:            log,
		cfg:            config,
		inv:            inv,
		tasks:          tasks,
		noti:           noti,
		processor:      processor,
		search:         &searchRunner{log: log, cfg: config, inv: inv, processor: processor, noti: noti, source: source},
		lifecycleTasks: newTaskGroup(),
	}
}

func (s *Server) Open(ctx context.Context) error {
	s.lifecycleTasks.Go(func(searchCtx context.Context) {
		stop := context.AfterFunc(ctx, s.lifecycleTasks.cancel)
		defer stop()
		s.search.schedule(searchCtx)
	})
	s.lifecycleTasks.Go(func(warmCtx context.Context) {
		s.inv.ReadAtStartup(warmCtx, s.cfg)
	})
	s.lifecycleTasks.Go(func(scanCtx context.Context) {
		s.inv.ScheduleSourceFolderScans(scanCtx, s.cfg)
	})
	defer s.lifecycleTasks.cancel()
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
	s.lifecycleTasks.cancel()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, errtrace.WithStack(fmt.Errorf("failed to shutdown server: %w", err)))
	}
	if err := s.lifecycleTasks.Wait(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop search and startup tasks: %w", err))
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
			newWebhookHandler(s.log, s.cfg, s.inv, s.processor, s.noti).Routes(api.Group("/"))
			api.POST("/search", s.search.handler)
		}
	}

	return g
}
