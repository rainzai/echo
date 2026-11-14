package bluebubbles

import (
	"encoding/json"
	"errors"
	"net/http"
)

type event struct {
	Type string  `json:"type"`
	Data Message `json:"data"`
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	var ev event
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&ev); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "REQUEST_SIZE", http.StatusRequestEntityTooLarge)
			return
		}

		http.Error(w, "INVALID_JSON", http.StatusBadRequest)
		return
	}

	if ev.Type != "new-message" {
		return
	}

	h.svc.HandleMessage(ev.Data, "webhook")
}
