package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"homelab/internal/agent"
	"homelab/internal/apps"
)

func (s *server) appsStatus(w http.ResponseWriter, r *http.Request) {
	view, err := s.Apps.Status(r.Context())
	if err != nil {
		s.Logger.Error("apps status", "error", err)
		writeError(w, http.StatusBadGateway, "could not reach Proxmox")
		return
	}

	writeJSON(w, http.StatusOK, view)
}

func (s *server) appsInstall(w http.ResponseWriter, r *http.Request) {
	var answers agent.MediaStackAnswers
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&answers); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := s.Apps.Install(r.Context(), s.Background, r.PathValue("id"), answers)
	switch {
	case errors.Is(err, agent.ErrInvalidAnswers):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, apps.ErrUnknownApp):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, apps.ErrInstalled), errors.Is(err, apps.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.Logger.Error("app install", "error", err)
		// The agent's message says what to do, like "update Homelab first".
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}
