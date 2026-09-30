package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"homelab/internal/updates"
)

func (s *server) updatesStatus(w http.ResponseWriter, r *http.Request) {
	view, err := s.Updates.Status(r.Context())
	if err != nil {
		s.Logger.Error("updates status", "error", err)
		writeError(w, http.StatusBadGateway, "could not reach Proxmox")
		return
	}

	writeJSON(w, http.StatusOK, view)
}

func (s *server) updatesRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.Updates.Run(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, run)
}

func (s *server) updatesCheck(w http.ResponseWriter, r *http.Request) {
	s.startUpdate(w, s.Updates.StartCheck(s.Background))
}

func (s *server) updatesHost(w http.ResponseWriter, r *http.Request) {
	s.startUpdate(w, s.Updates.StartHostUpdate(s.Background))
}

func (s *server) updatesGuest(w http.ResponseWriter, r *http.Request) {
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil || vmid < 100 {
		writeError(w, http.StatusBadRequest, "invalid container ID")
		return
	}

	s.startUpdate(w, s.Updates.StartGuestUpdate(s.Background, vmid))
}

func (s *server) startUpdate(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, updates.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

func (s *server) updatesSettings(w http.ResponseWriter, r *http.Request) {
	var input updates.SettingsInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := s.Updates.SaveSettings(input)
	switch {
	case errors.Is(err, updates.ErrInvalidSettings):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("save update settings", "error", err)
		writeError(w, http.StatusInternalServerError, "could not save the settings")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) updatesTestNotification(w http.ResponseWriter, r *http.Request) {
	err := s.Updates.SendTestNotification(r.Context())
	switch {
	case errors.Is(err, updates.ErrInvalidSettings):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
