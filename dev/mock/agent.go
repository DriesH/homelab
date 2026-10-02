package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"homelab/internal/agent"
)

// fakeAgent answers like the host agent, without touching the host.
type fakeAgent struct {
	mu      sync.Mutex
	jobs    map[string]agent.Job
	nextJob int
}

func serveAgent(socket string) error {
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}

	fake := &fakeAgent{jobs: map[string]agent.Job{}}
	installer, err := newAppInstaller()
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, agent.Health{Status: "ok", Hostname: "pve", Version: "dev"})
	})
	mux.HandleFunc("GET /v1/mounts", func(w http.ResponseWriter, r *http.Request) {
		mounts := []agent.Mount{{Path: "/mnt/backup", Source: "//nas/backup", FSType: "cifs", Mounted: false}}
		writeJSON(w, append(mounts, mediaMount()...))
	})
	mux.HandleFunc("GET /v1/media-folders", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []agent.MediaFolder{{Storage: "media", Path: "/mnt/pve/media"}, {Storage: "usb", Path: "/mnt/pve/usb"}})
	})
	mux.HandleFunc("PUT /v1/backup-job", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
		os.WriteFile(statePath("backup-job.json"), data, 0o600)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/upgrade", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, agent.UpgradeStatus{State: agent.UpgradeIdle})
	})
	mux.HandleFunc("POST /v1/jobs", fake.startJob)
	mux.HandleFunc("GET /v1/jobs/{id}", fake.job)
	mux.HandleFunc("GET /v1/logs/journal", journal)
	mux.HandleFunc("GET /v1/logs/docker", dockerLogs)
	addAppRoutes(mux, installer)
	addCloudRoutes(mux)
	mux.HandleFunc("GET /v1/console/{vmid}", console)

	return http.Serve(listener, mux)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func packages(names ...string) []agent.Package {
	list := []agent.Package{}
	for _, name := range names {
		list = append(list, agent.Package{Name: name, From: "1.0-1", To: "1.0-2"})
	}
	return list
}

// startJob finishes every update job at once. Updating container 102 fails.
func (f *fakeAgent) startJob(w http.ResponseWriter, r *http.Request) {
	var request agent.JobRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.nextJob++
	job := agent.Job{
		ID: strconv.Itoa(f.nextJob), Kind: request.Kind, VMID: request.VMID, Status: agent.JobSucceeded,
		StartedAt: time.Now(), FinishedAt: time.Now().Add(time.Second),
		Packages: []agent.Package{}, Images: []agent.Image{}, Log: "Reading package lists...\nDone\n",
	}
	switch {
	case request.Kind == agent.HostCheck:
		job.Packages = packages("pve-manager", "proxmox-kernel-6.14", "libssl3t64", "openssh-server")
	case request.Kind == agent.HostUpgrade:
		job.RebootRequired = true
	case request.Kind == agent.GuestCheck && request.VMID == 101:
		job.Packages = packages("jellyfin-server", "jellyfin-web", "libssl3t64")
	case request.Kind == agent.GuestCheck && request.VMID == 102:
		job.Packages = packages("docker-ce")
		job.Images = []agent.Image{{Service: "radarr", Image: "lscr.io/linuxserver/radarr:latest"}}
	case request.Kind == agent.GuestUpdate && request.VMID == 102:
		job.Status, job.Error = agent.JobFailed, "exit status 1"
		job.Log = "Pulling radarr...\nError response from daemon: toomanyrequests\n"
	}
	f.jobs[job.ID] = job

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, agent.Job{ID: job.ID, Kind: job.Kind, VMID: job.VMID, Status: agent.JobRunning, StartedAt: job.StartedAt})
}

func (f *fakeAgent) job(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	job, ok := f.jobs[r.PathValue("id")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, job)
}

func journal(w http.ResponseWriter, r *http.Request) {
	priority, _ := strconv.Atoi(r.URL.Query().Get("priority"))
	now := time.Now()
	entries := []agent.LogEntry{
		{Time: now.Add(-50 * time.Minute), Level: 6, Source: "systemd", Message: "Started pvedaemon.service - PVE API Daemon."},
		{Time: now.Add(-30 * time.Minute), Level: 4, Source: "kernel", Message: "EXT4-fs (dm-1): warning: mounting fs with errors, running e2fsck is recommended"},
		{Time: now.Add(-20 * time.Minute), Level: 3, Source: "pvedaemon", Message: "authentication failure; rhost=::ffff:192.168.1.50 user=root@pam"},
		{Time: now.Add(-10 * time.Minute), Level: 7, Source: "homelab-agent", Message: "job started id=abc kind=host-check"},
		{Time: now.Add(-2 * time.Minute), Level: 6, Source: "pvestatd", Message: "status update time (5.123 seconds)"},
	}
	if r.URL.Query().Get("vmid") != "0" {
		entries = []agent.LogEntry{
			{Time: now.Add(-9 * time.Minute), Level: 6, Source: "systemd", Message: "Started jellyfin.service - Jellyfin Media Server."},
			{Time: now.Add(-8 * time.Minute), Level: 4, Source: "jellyfin", Message: "[WRN] Slow query executed in 1203ms"},
			{Time: now.Add(-7 * time.Minute), Level: 3, Source: "jellyfin", Message: "[ERR] Error processing request."},
		}
	}

	shown := []agent.LogEntry{}
	for _, entry := range entries {
		if entry.Level <= priority {
			shown = append(shown, entry)
		}
	}
	writeJSON(w, shown)
}

// dockerLogs has no Docker in the manager and Jellyfin, like on a real host.
func dockerLogs(w http.ResponseWriter, r *http.Request) {
	if vmid := r.URL.Query().Get("vmid"); vmid == "100" || vmid == "101" {
		writeJSON(w, agent.DockerLogs{Containers: []agent.DockerContainer{}, Entries: []agent.LogEntry{}})
		return
	}

	now := time.Now()
	writeJSON(w, agent.DockerLogs{
		Installed: true,
		Containers: []agent.DockerContainer{
			{Name: "radarr", State: "running", Image: "lscr.io/linuxserver/radarr"},
			{Name: "gluetun", State: "running", Image: "qmcgaw/gluetun"},
		},
		Entries: []agent.LogEntry{
			{Time: now.Add(-6 * time.Minute), Level: 6, Source: "gluetun", Message: "INFO [vpn] connected"},
			{Time: now.Add(-4 * time.Minute), Level: 4, Source: "radarr", Message: "[Warn] IndexerStatusService: Indexer 1337x is disabled for a while"},
			{Time: now.Add(-3 * time.Minute), Level: 3, Source: "radarr", Message: fmt.Sprintf("[Error] DownloadClient: could not connect to qbittorrent at %s", "gluetun:8080")},
		},
	})
}

func init() {
	log.SetFlags(log.Ltime)
}
