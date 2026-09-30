package server

import (
	"errors"
	"io"
	"net/http"

	"homelab/internal/settingsfile"
)

func (s *server) settingsExport(w http.ResponseWriter, r *http.Request) {
	data, err := s.SettingsFile.Export(r.Context())
	if err != nil {
		s.Logger.Error("settings export failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not export the settings")
		return
	}

	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="homelab.yaml"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

// settingsImport takes the YAML as the body. Without ?apply=1 it only shows what would change.
func (s *server) settingsImport(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, settingsfile.MaxSize+1))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "the file is too large")
		return
	}

	apply := r.URL.Query().Get("apply") == "1"
	result, err := s.SettingsFile.Import(r.Context(), data, apply)
	switch {
	case errors.Is(err, settingsfile.ErrInvalidFile):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("settings import failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not import the settings")
	default:
		if apply {
			s.Logger.Info("settings imported")
		}
		writeJSON(w, http.StatusOK, result)
	}
}
