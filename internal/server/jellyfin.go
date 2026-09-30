package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"

	"homelab/internal/jellyfin"
)

var jellyfinItemID = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Image widths per type, so the browser can't ask Jellyfin for huge images.
var jellyfinImageWidths = map[string]int{"Primary": 300, "Backdrop": 780}

func (s *server) jellyfinStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Jellyfin.Status(r.Context()))
}

func (s *server) jellyfinSettings(w http.ResponseWriter, r *http.Request) {
	var input jellyfin.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := s.Jellyfin.SaveSettings(r.Context(), input)
	switch {
	case errors.Is(err, jellyfin.ErrInvalidSettings):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("save jellyfin settings", "error", err)
		writeError(w, http.StatusInternalServerError, "could not save the settings")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) jellyfinTheme(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := s.Jellyfin.SetTheme(r.Context(), input.Enabled)
	switch {
	case errors.Is(err, jellyfin.ErrNotConfigured):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("set jellyfin theme", "error", err)
		writeError(w, http.StatusBadGateway, "Jellyfin refused the change")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) jellyfinImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	imageType := r.URL.Query().Get("type")
	width, ok := jellyfinImageWidths[imageType]
	if !jellyfinItemID.MatchString(id) || !ok {
		writeError(w, http.StatusBadRequest, "invalid image request")
		return
	}

	response, err := s.Jellyfin.Image(r.Context(), id, imageType, width)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not load the image")
		return
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	io.Copy(w, response.Body)
}
