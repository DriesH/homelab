package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"homelab/internal/agent"
	"homelab/internal/backups"
)

func (s *server) backupsStatus(w http.ResponseWriter, r *http.Request) {
	view, err := s.Backups.Status(r.Context())
	if err != nil {
		s.Logger.Error("backups status", "error", err)
		writeError(w, http.StatusBadGateway, "could not reach Proxmox")
		return
	}

	writeJSON(w, http.StatusOK, view)
}

func (s *server) backupsSaveJob(w http.ResponseWriter, r *http.Request) {
	var job agent.BackupJob
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&job); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := s.Backups.SaveJob(r.Context(), job)
	switch {
	case errors.Is(err, agent.ErrInvalidBackupJob):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("save backup job", "error", err)
		writeError(w, http.StatusBadGateway, "the host agent could not save the backup job")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) backupsBackUp(w http.ResponseWriter, r *http.Request) {
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil || vmid < 100 {
		writeError(w, http.StatusBadRequest, "invalid guest ID")
		return
	}

	s.writeBackupsResult(w, s.Backups.BackUp(s.Background, vmid))
}

func (s *server) backupsRestore(w http.ResponseWriter, r *http.Request) {
	var input struct {
		VMID  int    `json:"vmid"`
		VolID string `json:"volid"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeBackupsResult(w, s.Backups.Restore(s.Background, input.VMID, input.VolID))
}

func (s *server) backupsDelete(w http.ResponseWriter, r *http.Request) {
	var input struct {
		VolID string `json:"volid"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeBackupsResult(w, s.Backups.Delete(s.Background, input.VolID))
}

func (s *server) writeBackupsResult(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backups.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, backups.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, backups.ErrRefused):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("backups", "error", err)
		writeError(w, http.StatusBadGateway, "could not reach Proxmox")
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}
