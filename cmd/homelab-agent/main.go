// homelab-agent runs on the Proxmox host and does the few things the Proxmox
// API can't, like apt upgrades and running commands inside containers.
// It only listens on a Unix socket that is shared with the manager LXC.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"homelab/internal/agent"
)

var version = "dev"

func main() {
	socketPath := flag.String("socket", "/var/lib/homelab-agent/socket/agent.sock", "Unix socket to listen on")
	socketGID := flag.Int("socket-gid", -1, "host group ID that may use the socket (the manager's group, shifted by the LXC ID map)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := run(*socketPath, *socketGID, logger); err != nil {
		logger.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(socketPath string, socketGID int, logger *slog.Logger) error {
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()

	if socketGID >= 0 {
		if err := os.Chown(socketPath, 0, socketGID); err != nil {
			return err
		}
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		return err
	}

	runner := agent.NewRunner()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", health)
	mux.HandleFunc("POST /v1/jobs", startJob(runner, logger))
	mux.HandleFunc("GET /v1/jobs/{id}", getJob(runner))
	mux.HandleFunc("GET /v1/mounts", listMounts(agent.NewMounts()))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	logger.Info("listening", "socket", socketPath, "version", version)

	if err := srv.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func health(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()

	writeJSON(w, http.StatusOK, agent.Health{Status: "ok", Hostname: hostname, Version: version})
}

func startJob(runner *agent.Runner, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request agent.JobRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		job, err := runner.Start(request)
		switch {
		case errors.Is(err, agent.ErrInvalidJob):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		case errors.Is(err, agent.ErrBusy):
			http.Error(w, err.Error(), http.StatusConflict)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("job started", "id", job.ID, "kind", job.Kind, "vmid", job.VMID)
		writeJSON(w, http.StatusAccepted, job)
	}
}

func getJob(runner *agent.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job, err := runner.Get(r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		writeJSON(w, http.StatusOK, job)
	}
}

func listMounts(mounts *agent.Mounts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, mounts.List())
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
