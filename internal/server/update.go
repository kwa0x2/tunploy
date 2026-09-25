package server

import (
	"errors"
	"net/http"

	"github.com/kwa0x2/tunploy/internal/httpx"
	"github.com/kwa0x2/tunploy/internal/update"
)

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) error {
	return httpx.JSON(w, http.StatusOK, s.updates.Status(r.Context()))
}

// A failed check is part of the status, not an error of this request.
func (s *Server) handleCheckUpdate(w http.ResponseWriter, r *http.Request) error {
	st, _ := s.updates.Check(r.Context())
	return httpx.JSON(w, http.StatusOK, st)
}

// The panel goes away for a moment once the new image is pulled; the page
// polls the status until the new version answers.
func (s *Server) handleStartUpdate(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.updates.Start(r.Context()); err != nil {
		switch {
		case errors.Is(err, update.ErrBusy), errors.Is(err, update.ErrNoUpdate), errors.Is(err, update.ErrUnsupported):
			return httpx.Conflict("%s", err.Error())
		}
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, s.updates.Status(r.Context()))
}
