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

// failPassword makes the fake media stack install fail, to see the error on the Apps page.
const failPassword = "failfailfail12"

// fakeAgent answers like the host agent, without touching the host.
type fakeAgent struct {
	mu      sync.Mutex
	jobs    map[string]agent.Job
	nextJob int
	install agent.AppInstallStatus
}

func serveAgent(socket string) error {
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}

	fake := &fakeAgent{jobs: map[string]agent.Job{}, install: agent.AppInstallStatus{State: agent.UpgradeIdle}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, agent.Health{Status: "ok", Hostname: "pve", Version: "dev"})
	})
	mux.HandleFunc("GET /v1/mounts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []agent.Mount{
			{Path: "/mnt/backup", Source: "//nas/backup", FSType: "cifs", Mounted: false},
			{Path: "/mnt/homelab/media", Source: "192.168.1.10:/volume1/media", FSType: "nfs", Mounted: true, Size: 7_900_000_000_000, Used: 5_300_000_000_000},
		})
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
	mux.HandleFunc("GET /v1/apps/install", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		writeJSON(w, fake.install)
	})
	mux.HandleFunc("POST /v1/apps/{app}/install", fake.installApp)
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

// installApp plays a media stack install of a few seconds.
func (f *fakeAgent) installApp(w http.ResponseWriter, r *http.Request) {
	var answers agent.MediaStackAnswers
	if err := json.NewDecoder(r.Body).Decode(&answers); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := answers.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	if f.install.State == agent.UpgradeRunning {
		f.mu.Unlock()
		http.Error(w, agent.ErrAppInstallRunning.Error(), http.StatusConflict)
		return
	}
	f.install = agent.AppInstallStatus{App: r.PathValue("app"), State: agent.UpgradeRunning, StartedAt: time.Now()}
	f.mu.Unlock()

	go f.playInstall(answers)
	w.WriteHeader(http.StatusAccepted)
}

func (f *fakeAgent) playInstall(answers agent.MediaStackAnswers) {
	steps := []string{
		"==> Mounting " + answers.NASServer + ":" + answers.NASExport + " at /mnt/homelab/media",
		"==> Downloading Debian 13 template",
		"==> Creating container 130 (media)",
		"==> Installing Docker",
		"==> Starting the stack (the first image download takes a few minutes)",
		"==> Waiting for the VPN",
	}
	for _, step := range steps {
		time.Sleep(time.Second)
		f.mu.Lock()
		f.install.Log += step + "\n"
		f.mu.Unlock()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.install.FinishedAt = time.Now()
	if answers.Password == failPassword {
		f.install.Log += "error: the VPN did not connect, check the WireGuard key\n==> The install failed, removing container 130\n"
		f.install.State, f.install.Message = agent.UpgradeFailed, "the installer stopped with exit code 1"
		return
	}
	f.install.Log += "==> Connecting the apps\n==> Done\n"
	f.install.State, f.install.VMID, f.install.IP = agent.UpgradeSucceeded, 130, "192.168.1.150"
	os.WriteFile(statePath("media-installed"), nil, 0o600)
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

func dockerLogs(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	writeJSON(w, agent.DockerLogs{
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
