package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"homelab/internal/tailscale"
)

func (s *server) tailscaleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Tailscale.Status(r.Context()))
}

func (s *server) tailscaleConnect(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AuthKey string `json:"authKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeTailscaleResult(w, s.Tailscale.Connect(s.Background, input.AuthKey))
}

func (s *server) tailscaleLogout(w http.ResponseWriter, r *http.Request) {
	s.writeTailscaleResult(w, s.Tailscale.Logout(r.Context()))
}

func (s *server) tailscaleServe(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeTailscaleResult(w, s.Tailscale.SetServe(r.Context(), input.Enabled))
}

func (s *server) tailscaleSettings(w http.ResponseWriter, r *http.Request) {
	var input tailscale.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeTailscaleResult(w, s.Tailscale.SaveSettings(r.Context(), input))
}

func (s *server) tailscaleUseTag(w http.ResponseWriter, r *http.Request) {
	s.writeTailscaleResult(w, s.Tailscale.UseTag(s.Background))
}

func (s *server) tailscaleService(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Published bool `json:"published"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeTailscaleResult(w, s.Tailscale.SetService(r.Context(), r.PathValue("name"), input.Published))
}

// writeTailscaleResult passes Tailscale's own message on, because it tells the user what to do.
func (s *server) writeTailscaleResult(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tailscale.ErrInvalidSettings):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, tailscale.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.Logger.Warn("tailscale", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
