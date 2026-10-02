package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"homelab/internal/agent"
)

// fakeCloud plays turning the cloud storage on and off. .dev/cloud-enabled
// keeps it on, and with .dev/fail-cloud the next job fails.
type fakeCloud struct {
	mu  sync.Mutex
	job *agent.CloudJob
}

var (
	enableLog = []string{
		"Installing rclone 1.75.1 and mergerfs 2.42.0",
		"Checking the storage",
		"Writing the marker and the canary in the cloud",
		"Mounting the cloud at /mnt/homelab/cloud",
		"Moving the media mount to /mnt/homelab/local",
		"Shutting down the containers that use the media: 130 140",
		"Joining the local media and the cloud at /mnt/homelab/media",
		"Starting the containers again",
		"Done",
	}
	disableLog = []string{
		"Checking the free space: 1.2 TB in the cloud, 2.6 TB free here",
		"Copying everything back from the cloud",
		"Checking the copies",
		"Shutting down the containers that use the media: 130 140",
		"Putting the media mount back at /mnt/homelab/media",
		"Starting the containers again",
		"Emptying the cloud",
		"Done",
	}
)

func addCloudRoutes(mux *http.ServeMux) {
	fake := &fakeCloud{}

	mux.HandleFunc("GET /v1/cloud", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fake.status())
	})
	mux.HandleFunc("POST /v1/cloud/test", func(w http.ResponseWriter, r *http.Request) {
		var config agent.CloudConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.ValidateStorage(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		time.Sleep(time.Second)
		// A secret with "wrong" in it fails, to try the error in the form.
		if strings.Contains(config.SecretKey+config.Password, "wrong") {
			http.Error(w, "the storage refused the keys: AccessDenied", http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/cloud/enable", func(w http.ResponseWriter, r *http.Request) {
		var config agent.CloudConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		os.WriteFile(statePath("cloud-label"), []byte(config.Label()), 0o600)
		fake.start(w, agent.CloudEnable, enableLog)
	})
	mux.HandleFunc("POST /v1/cloud/disable", func(w http.ResponseWriter, r *http.Request) {
		fake.start(w, agent.CloudDisable, disableLog)
	})
}

func cloudEnabled() bool {
	_, err := os.Stat(statePath("cloud-enabled"))
	return err == nil
}

func cloudLabel() string {
	label, _ := os.ReadFile(statePath("cloud-label"))
	return string(label)
}

func (f *fakeCloud) status() agent.CloudStatus {
	f.mu.Lock()
	defer f.mu.Unlock()

	status := agent.CloudStatus{Enabled: cloudEnabled(), Job: f.job}
	if status.Enabled {
		status.Label = cloudLabel()
	}

	return status
}

func (f *fakeCloud) start(w http.ResponseWriter, action agent.CloudAction, lines []string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.job != nil && f.job.State == agent.UpgradeRunning {
		http.Error(w, "the cloud storage is busy", http.StatusConflict)
		return
	}
	if (action == agent.CloudEnable) == cloudEnabled() {
		http.Error(w, "the cloud storage is "+string(action)+"d already", http.StatusConflict)
		return
	}

	fail := false
	if _, err := os.Stat(statePath("fail-cloud")); err == nil {
		fail = true
	}
	f.job = &agent.CloudJob{Action: action, State: agent.UpgradeRunning, StartedAt: time.Now()}
	go f.play(action, lines, fail)

	w.WriteHeader(http.StatusAccepted)
}

func (f *fakeCloud) play(action agent.CloudAction, lines []string, fail bool) {
	for index, line := range lines {
		time.Sleep(1500 * time.Millisecond)

		f.mu.Lock()
		if fail && index == 3 {
			f.job.Log += "Error: the cloud did not mount in time\n"
			f.job.State, f.job.Message, f.job.FinishedAt = agent.UpgradeFailed, "the cloud did not mount in time, nothing changed", time.Now()
			f.mu.Unlock()
			return
		}
		f.job.Log += line + "\n"
		f.mu.Unlock()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if action == agent.CloudEnable {
		os.WriteFile(statePath("cloud-enabled"), nil, 0o600)
	} else {
		os.Remove(statePath("cloud-enabled"))
	}
	f.job.State, f.job.FinishedAt = agent.UpgradeSucceeded, time.Now()
}
