package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"homelab/internal/auth"
	"homelab/internal/databackup"
)

// checkPassword asks for the password again, because a data backup holds the
// admin account and all tokens.
func (s *server) checkPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	err := s.Auth.CheckPassword(password, clientIP(r))
	switch {
	case errors.Is(err, auth.ErrLockedOut):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.Logger.Warn("wrong password for a data backup", "ip", clientIP(r))
		// Not 401, because that would sign the user out.
		writeError(w, http.StatusForbidden, "wrong password")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not check the password")
	default:
		return true
	}

	return false
}

func (s *server) dataBackupDownload(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password   string `json:"password"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !s.checkPassword(w, r, input.Password) {
		return
	}

	data, err := databackup.Create(s.DataDir, input.Passphrase)
	switch {
	case errors.Is(err, databackup.ErrWeakPassphrase):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.Logger.Error("data backup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not make the backup")
		return
	}

	s.Logger.Info("data backup downloaded", "ip", clientIP(r))
	s.Notify(r.Context(), "📦 Someone downloaded a data backup of Homelab.")

	name := fmt.Sprintf("homelab-data-%s.hlbackup", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

// dataBackupRestore takes a multipart form with password, passphrase and file.
// The manager restarts to load the restored data.
func (s *server) dataBackupRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, databackup.MaxSize+64*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "send the backup as a form")
		return
	}

	fields := map[string][]byte{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "the file is too large")
			return
		}
		name := part.FormName()
		if name != "password" && name != "passphrase" && name != "file" {
			continue
		}
		fields[name], err = io.ReadAll(part)
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "the file is too large")
			return
		}
	}

	if !s.checkPassword(w, r, string(fields["password"])) {
		return
	}

	err = databackup.Stage(s.DataDir, fields["file"], string(fields["passphrase"]))
	switch {
	case errors.Is(err, databackup.ErrInvalidBackup), errors.Is(err, databackup.ErrWrongPassphrase):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.Logger.Error("data restore failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not restore the backup")
		return
	}

	s.Logger.Info("data backup restored, restarting", "ip", clientIP(r))
	s.Notify(r.Context(), "♻️ Someone restored a data backup of Homelab. It restarts now.")
	writeJSON(w, http.StatusOK, map[string]bool{"restarting": true})

	// Give the response time to reach the browser.
	time.AfterFunc(time.Second, s.Restart)
}
