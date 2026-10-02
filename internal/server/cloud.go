package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"homelab/internal/agent"
	"homelab/internal/auth"
	"homelab/internal/cloud"
)

func (s *server) cloudStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Cloud.Status(r.Context()))
}

func (s *server) cloudSaveSettings(w http.ResponseWriter, r *http.Request) {
	var settings cloud.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeCloudResult(w, s.Cloud.SaveSettings(r.Context(), settings))
}

func (s *server) cloudTest(w http.ResponseWriter, r *http.Request) {
	var settings cloud.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeCloudResult(w, s.Cloud.Test(r.Context(), settings))
}

func (s *server) cloudCreateKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.Cloud.CreateKey()
	if err != nil {
		s.writeCloudResult(w, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"key": key})
}

func (s *server) cloudConfirmKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Groups string `json:"groups"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeCloudResult(w, s.Cloud.ConfirmKey(input.Groups))
}

// cloudRevealKey shows the recovery key again. It asks for the password and
// an authenticator code, because the key opens all media in the cloud.
func (s *server) cloudRevealKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := s.Auth.CheckPasswordAndCode(input.Password, input.Code, clientIP(r))
	switch {
	case errors.Is(err, auth.ErrLockedOut):
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.Logger.Warn("wrong password or code for the recovery key", "ip", clientIP(r))
		// Not 401, because that would sign the user out.
		writeError(w, http.StatusForbidden, "wrong password or code")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not check the password")
		return
	}

	key, err := s.Cloud.RevealKey(r.Context())
	if err != nil {
		s.writeCloudResult(w, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"key": key})
}

func (s *server) cloudSaveRule(w http.ResponseWriter, r *http.Request) {
	var rule cloud.Rule
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeCloudResult(w, s.Cloud.SaveRule(rule))
}

func (s *server) cloudEnable(w http.ResponseWriter, r *http.Request) {
	s.writeCloudResult(w, s.Cloud.Enable(r.Context()))
}

func (s *server) cloudDisable(w http.ResponseWriter, r *http.Request) {
	s.writeCloudResult(w, s.Cloud.Disable(r.Context()))
}

func (s *server) writeCloudResult(w http.ResponseWriter, err error) {
	var refused *agent.RefusedError
	switch {
	case errors.Is(err, agent.ErrInvalidAnswers):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), agent.ErrInvalidAnswers.Error()+": "))
	case errors.Is(err, cloud.ErrInvalidKey), errors.Is(err, cloud.ErrWrongKeyEnd):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, cloud.ErrNotSetUp), errors.Is(err, cloud.ErrNoKey), errors.Is(err, cloud.ErrKeyExists),
		errors.Is(err, cloud.ErrKeyNotConfirmed), errors.Is(err, cloud.ErrEnabled):
		writeError(w, http.StatusConflict, err.Error())
	case errors.As(err, &refused):
		writeError(w, http.StatusUnprocessableEntity, refused.Message)
	case err != nil:
		s.Logger.Error("cloud storage", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
