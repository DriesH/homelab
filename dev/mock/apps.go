package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"homelab/internal/agent"
)

// failPassword makes a fake install fail, to see the error on the Apps page.
const failPassword = "failfailfail12"

// fakeInstalls plays the installers. The real installer of the agent does
// everything else: checking answers, saving them for a retry, and the status.
type fakeInstalls struct {
	mu      sync.Mutex
	running bool
}

func newAppInstaller() (*agent.AppInstaller, error) {
	stacks := statePath("stacks")
	for _, dir := range []string{"arr", "jellyfin"} {
		if err := os.MkdirAll(filepath.Join(stacks, dir), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(stacks, dir, "install.sh"), nil, 0o600); err != nil {
			return nil, err
		}
	}

	fake := &fakeInstalls{}
	return agent.NewAppInstallerWith(statePath("apps"), stacks, fake.start, fake.isRunning), nil
}

func (f *fakeInstalls) isRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *fakeInstalls) start(script string, args []string, cleanup, logPath, exitPath string) error {
	stack := filepath.Base(filepath.Dir(script))
	play := func() { f.playOnContainer(stack, args[0], logPath, exitPath) }
	if args[0] == "--vpn" {
		answers, err := os.ReadFile(cleanup)
		if err != nil {
			return err
		}
		os.Remove(cleanup)
		play = func() { f.playVPN(string(answers), logPath, exitPath) }
	}
	if args[0] == "--answers" {
		answers, err := os.ReadFile(cleanup)
		if err != nil {
			return err
		}
		os.Remove(cleanup)
		play = func() { f.play(stack, string(answers), logPath, exitPath) }
	}

	f.mu.Lock()
	f.running = true
	f.mu.Unlock()

	go play()
	return nil
}

// playVPN plays a change of the VPN settings. Countries with "Mars" do not connect.
func (f *fakeInstalls) playVPN(answers, logPath, exitPath string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logFile.Close()
	write := func(line string) {
		fmt.Fprintln(logFile, line)
		time.Sleep(time.Second)
	}

	countries := ""
	for line := range strings.Lines(answers) {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "VPN_COUNTRIES="); found {
			countries = value
		}
	}

	write("==> Saving the new VPN settings")
	write("==> Restarting the VPN and the apps behind it")
	write("==> Waiting for the VPN")
	if strings.Contains(countries, "Mars") {
		write("Last lines of the VPN log (gluetun):")
		write("ERROR [vpn] no server found for countries: " + countries)
		write("==> The VPN did not connect, going back to the old settings")
		write("==> Restarting the VPN and the apps behind it")
		write("error: the VPN did not connect with the new settings, so the old settings are back")
		os.WriteFile(exitPath, []byte("1\n"), 0o600)
		return
	}
	os.WriteFile(statePath("vpn-countries"), []byte(countries), 0o600)
	write("==> Done")
	os.WriteFile(exitPath, []byte("0\n"), 0o600)
}

// playOnContainer plays an update or a removal. With .dev/fail-update, an update fails.
func (f *fakeInstalls) playOnContainer(stack, action, logPath, exitPath string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logFile.Close()
	write := func(line string) {
		fmt.Fprintln(logFile, line)
		time.Sleep(time.Second)
	}
	finish := func(code int) { os.WriteFile(exitPath, []byte(fmt.Sprintln(code)), 0o600) }

	installed := map[string]string{"arr": "media-installed", "jellyfin": "jellyfin-installed"}[stack]
	if action == "--remove" {
		write("==> Shutting down the container")
		write("==> Removing the container with its disks and snapshots. Its backups stay")
		os.Remove(statePath(installed))
		write("==> Done")
		finish(0)
		return
	}

	write("==> Updating the packages in the container")
	if stack == "arr" {
		write("==> Copying the stack of Homelab dev into the container")
		write("==> Downloading the new images")
		write("==> Starting the stack")
		write("==> Waiting for the VPN")
	} else {
		write("==> Waiting for Jellyfin")
	}
	if _, err := os.Stat(statePath("fail-update")); err == nil {
		write("error: the fake update failed, because .dev/fail-update exists")
		finish(1)
		return
	}
	write("==> Done")
	finish(0)
}

func (f *fakeInstalls) play(stack, answers, logPath, exitPath string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logFile.Close()
	write := func(line string) {
		fmt.Fprintln(logFile, line)
		time.Sleep(time.Second)
	}
	finish := func(code int) { os.WriteFile(exitPath, []byte(fmt.Sprintln(code)), 0o600) }

	steps := []string{"==> Mounting the media", "==> Downloading Debian 13 template"}
	if stack == "jellyfin" {
		steps = append(steps, "==> Creating container 140 (jellyfin)", "==> Installing Jellyfin from repo.jellyfin.org",
			"==> Passing the GPU (/dev/dri/renderD128) to the container", "==> Setting up Jellyfin", "==> Finishing the setup wizard")
	} else {
		steps = append(steps, "==> Creating container 130 (media)", "==> Installing Docker",
			"==> Starting the stack (the first image download takes a few minutes)", "==> Waiting for the VPN")
		if !strings.Contains(answers, "JELLYFIN_ADMIN_USERNAME=\n") {
			steps = append(steps, "==> Configuring Seerr")
		}
	}
	for _, step := range steps {
		write(step)
	}

	if strings.Contains(answers, "PASSWORD="+failPassword+"\n") {
		write("error: the fake install failed, because of the password " + failPassword)
		write("==> The install failed, removing the container")
		finish(1)
		return
	}

	mountMedia(answers)
	if stack == "jellyfin" {
		write("HOMELAB jellyfin-key 0123456789abcdef0123456789abcdef")
		write("==> Done")
		write("HOMELAB app jellyfin 140 192.168.1.160")
		os.WriteFile(statePath("jellyfin-installed"), nil, 0o600)
	} else {
		write("==> Done")
		write("HOMELAB app media 130 192.168.1.150")
		os.WriteFile(statePath("media-installed"), nil, 0o600)
	}
	finish(0)
}

// mediaMount is the media of the apps: the NAS share, or what an install
// mounted. With .dev/no-media-mount, nothing is mounted yet.
func mediaMount() []agent.Mount {
	if _, err := os.Stat(statePath("no-media-mount")); err == nil {
		return nil
	}
	source, fsType := "192.168.1.10:/volume1/media", "nfs"
	if data, err := os.ReadFile(statePath("media-source")); err == nil {
		source = strings.TrimSpace(string(data))
	}
	if strings.HasPrefix(source, "/") {
		fsType = "folder"
	}

	return []agent.Mount{{Path: agent.MediaMount, Source: source, FSType: fsType, Mounted: true, Size: 7_900_000_000_000, Used: 5_300_000_000_000}}
}

// mountMedia keeps the media of a working install, like the installers do.
func mountMedia(answers string) {
	values := map[string]string{}
	for line := range strings.Lines(answers) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		values[key] = value
	}

	source := values["MEDIA_FOLDER"]
	if source == "" && values["NAS_SERVER"] != "" {
		source = values["NAS_SERVER"] + ":" + values["NAS_EXPORT"]
	}
	if source != "" {
		os.WriteFile(statePath("media-source"), []byte(source), 0o600)
		os.Remove(statePath("no-media-mount"))
	}
}

// addAppRoutes serves the app routes of the agent with the real installer.
func addAppRoutes(mux *http.ServeMux, installer *agent.AppInstaller) {
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, agent.ErrInvalidAnswers), errors.Is(err, agent.ErrUnknownApp), errors.Is(err, agent.ErrNoSavedAnswers):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, agent.ErrAppInstallRunning):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	mux.HandleFunc("GET /v1/apps/install", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, installer.Status())
	})
	mux.HandleFunc("POST /v1/apps/{app}/install", func(w http.ResponseWriter, r *http.Request) {
		var request agent.InstallRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := installer.Install(r.PathValue("app"), request); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /v1/apps/{app}/retry", func(w http.ResponseWriter, r *http.Request) {
		if err := installer.Retry(r.PathValue("app")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	for action, run := range map[string]func(string, int) error{"update": installer.Update, "remove": installer.Remove} {
		mux.HandleFunc("POST /v1/apps/{app}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var request agent.AppContainerRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			if err := run(r.PathValue("app"), request.VMID); err != nil {
				fail(w, err)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		})
	}
	mux.HandleFunc("GET /v1/apps/media/vpn", func(w http.ResponseWriter, r *http.Request) {
		countries := "Netherlands"
		if data, err := os.ReadFile(statePath("vpn-countries")); err == nil {
			countries = string(data)
		}
		writeJSON(w, agent.VPNSettings{Countries: countries})
	})
	mux.HandleFunc("PUT /v1/apps/media/vpn", func(w http.ResponseWriter, r *http.Request) {
		var request agent.VPNRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := installer.ChangeVPN(request.VMID, request.VPNSettings); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /v1/apps/{app}/answers", func(w http.ResponseWriter, r *http.Request) {
		saved, err := installer.Saved(r.PathValue("app"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, saved)
	})
	mux.HandleFunc("DELETE /v1/apps/{app}/answers", func(w http.ResponseWriter, r *http.Request) {
		if err := installer.Forget(r.PathValue("app")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
