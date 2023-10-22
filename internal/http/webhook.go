package http

import (
	"net/http"

	"seasonpackarr/internal/config"
	"seasonpackarr/internal/logger"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type webhookHandler struct {
	log logger.Logger
	cfg *config.AppConfig
}

func newWebhookHandler(log logger.Logger, cfg *config.AppConfig) *webhookHandler {
	return &webhookHandler{
		log: log,
		cfg: cfg,
	}
}

func (h webhookHandler) Routes(r chi.Router) {
	r.Post("/pack", h.pack)
}

func (h webhookHandler) pack(w http.ResponseWriter, r *http.Request) {
	newProcessor(h.log, h.cfg).ProcessSeasonPack(w, r)
	render.Status(r, http.StatusOK)
}
