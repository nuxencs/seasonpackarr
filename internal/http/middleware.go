// Copyright (c) 2021 - 2024, Ludvig Lundgren and the autobrr contributors.
// Code is modified for use with seasonpackarr
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"net/http"
	"runtime/debug"
	"time"

	"seasonpackarr/internal/logger"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func (s *Server) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Allow access if apiToken value is set to an empty string
		if s.cfg.Config.APIToken == "" {
			c.Next()
			return
		}

		// Check the X-API-Token header
		if token := c.GetHeader("X-API-Token"); token != "" {
			if token != s.cfg.Config.APIToken {
				s.log.Error().Msgf("unauthorized access attempt with incorrect API token in header from IP: %s", c.ClientIP())
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
		} else if key := c.Query("apikey"); key != "" {
			// Check the query parameter ?apikey=TOKEN
			if key != s.cfg.Config.APIToken {
				s.log.Error().Msgf("unauthorized access attempt with incorrect API token in query parameters from IP: %s", c.ClientIP())
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
		} else {
			// Neither header nor query parameter provided a token
			s.log.Error().Msgf("unauthorized access attempt without API token from IP: %s", c.ClientIP())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Next()
	}
}

func CorsMiddleware() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowMethods:     []string{"GET", "POST"},
		AllowCredentials: true,
		AllowAllOrigins:  true,
		MaxAge:           12 * time.Hour,
	})
}

func LoggerMiddleware(logger logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		log := logger.With().Logger()

		start := time.Now()

		defer func() {
			if rec := recover(); rec != nil {
				log.Error().
					Str("type", "error").
					Interface("recover_info", rec).
					Bytes("debug_stack", debug.Stack()).
					Msg("log system error")
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}

			if c.Request.URL.Path != "/api/healthz/liveness" && c.Request.URL.Path != "/api/healthz/readiness" {
				latency := float64(time.Since(start).Nanoseconds()) / 1e6
				log.Trace().
					Str("type", "access").
					Timestamp().
					Fields(map[string]interface{}{
						"remote_ip":  c.ClientIP(),
						"url":        c.Request.URL.Path,
						"proto":      c.Request.Proto,
						"method":     c.Request.Method,
						"user_agent": c.Request.UserAgent(),
						"status":     c.Writer.Status(),
						"latency_ms": latency,
						"bytes_in":   c.GetHeader("Content-Length"),
						"bytes_out":  c.Writer.Size(),
					}).
					Msg("incoming_request")
			}
		}()

		c.Next()
	}
}
