package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"homelab/internal/selfupdate"
)

func (s *server) selfUpdateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.SelfUpdate.Status(r.Context()))
}

func (s *server) selfUpdateCheck(w http.ResponseWriter, r *http.Request) {
	// The error is part of the status, so the page can show it.
	s.SelfUpdate.Check(r.Context())
	writeJSON(w, http.StatusOK, s.SelfUpdate.Status(r.Context()))
}

// selfUpdateInstall waits until the agent has the bundle, so errors like a bad
// signature reach the page. The agent then restarts the manager.
func (s *server) selfUpdateInstall(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	err := s.SelfUpdate.Install(ctx)
	switch {
	case errors.Is(err, selfupdate.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, selfupdate.ErrNoUpdate):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("self update", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

func (s *server) selfUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var input selfupdate.SettingsInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := s.SelfUpdate.SaveSettings(input)
	switch {
	case errors.Is(err, selfupdate.ErrInvalidSettings):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("save self update settings", "error", err)
		writeError(w, http.StatusInternalServerError, "could not save the settings")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
