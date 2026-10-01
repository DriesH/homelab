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
	var request agent.InstallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeAppsResult(w, s.Apps.Install(r.Context(), s.Background, r.PathValue("id"), request), http.StatusAccepted)
}

func (s *server) appsRetry(w http.ResponseWriter, r *http.Request) {
	s.writeAppsResult(w, s.Apps.Retry(r.Context(), s.Background, r.PathValue("id")), http.StatusAccepted)
}

func (s *server) appsForget(w http.ResponseWriter, r *http.Request) {
	s.writeAppsResult(w, s.Apps.Forget(r.Context(), r.PathValue("id")), http.StatusNoContent)
}

func (s *server) appsUpdate(w http.ResponseWriter, r *http.Request) {
	s.writeAppsResult(w, s.Apps.Update(r.Context(), s.Background, r.PathValue("id")), http.StatusAccepted)
}

func (s *server) appsRemove(w http.ResponseWriter, r *http.Request) {
	s.writeAppsResult(w, s.Apps.Remove(r.Context(), s.Background, r.PathValue("id")), http.StatusAccepted)
}

func (s *server) appsVPN(w http.ResponseWriter, r *http.Request) {
	countries, err := s.Apps.VPNCountries(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeAppsResult(w, err, 0)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"countries": countries})
}

func (s *server) appsChangeVPN(w http.ResponseWriter, r *http.Request) {
	var settings agent.VPNSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeAppsResult(w, s.Apps.ChangeVPN(r.Context(), s.Background, r.PathValue("id"), settings), http.StatusAccepted)
}

func (s *server) writeAppsResult(w http.ResponseWriter, err error, success int) {
	switch {
	case errors.Is(err, agent.ErrInvalidAnswers):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, apps.ErrUnknownApp):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, apps.ErrInstalled), errors.Is(err, apps.ErrBusy), errors.Is(err, apps.ErrNotInstalled),
		errors.Is(err, apps.ErrNotManaged), errors.Is(err, apps.ErrNotRunning):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.Logger.Error("app operation", "error", err)
		// The agent's message says what to do, like "update Homelab first".
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		w.WriteHeader(success)
	}
}
