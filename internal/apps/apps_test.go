package apps

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"homelab/internal/agent"
	"homelab/internal/proxmox"
)

type fakeAgent struct {
	mu        sync.Mutex
	status    agent.AppInstallStatus
	installed []string
	retried   []string
	saved     *agent.SavedAnswers
	mounts    []agent.Mount
	requests  []agent.InstallRequest
}

func (f *fakeAgent) Mounts(context.Context) ([]agent.Mount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mounts, nil
}

func (f *fakeAgent) RetryApp(_ context.Context, app string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retried = append(f.retried, app)
	f.status = agent.AppInstallStatus{App: app, State: agent.UpgradeRunning}
	return nil
}

func (f *fakeAgent) SavedAppAnswers(context.Context, string) (*agent.SavedAnswers, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saved, nil
}

func (f *fakeAgent) ForgetAppAnswers(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = nil
	return nil
}

func (f *fakeAgent) InstallApp(_ context.Context, app string, request agent.InstallRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed = append(f.installed, app)
	f.requests = append(f.requests, request)
	f.status = agent.AppInstallStatus{App: app, State: agent.UpgradeRunning}
	return nil
}

func (f *fakeAgent) AppInstallStatus(context.Context) (agent.AppInstallStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *fakeAgent) set(status agent.AppInstallStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

type fakeProxmox struct {
	resources []proxmox.Resource
}

func (f *fakeProxmox) Resources(context.Context) ([]proxmox.Resource, error) {
	return f.resources, nil
}

func (f *fakeProxmox) ContainerStorages(context.Context, string) ([]proxmox.BackupStorage, error) {
	return []proxmox.BackupStorage{
		{Storage: "local-lvm", Active: true},
		{Storage: "fast", Active: true},
		{Storage: "offline", Active: false},
	}, nil
}

func (f *fakeProxmox) ContainerInterfaces(_ context.Context, _ string, vmid int) ([]proxmox.Interface, error) {
	return []proxmox.Interface{{Name: "lo", Inet: "127.0.0.1/8"}, {Name: "eth0", Inet: "192.168.1.50/24"}}, nil
}

func validAnswers() agent.MediaStackAnswers {
	return agent.MediaStackAnswers{
		NASServer: "192.168.1.5", NASExport: "/volume1/media", MoviesFolder: "movies", SeriesFolder: "series",
		WireGuardPrivateKey: "cGVyZmVjdGx5IHZhbGlkIGtleSBvZiAzMiBieXRlcyE=",
		VPNCountries:        "Netherlands", SubtitleLanguages: "en",
		Username: "homelab", Password: "correct horse battery",
		Storage: "local-lvm", DownloadsSize: 200,
	}
}

func appByID(t *testing.T, view View, id string) AppView {
	t.Helper()
	for _, app := range view.Apps {
		if app.ID == id {
			return app
		}
	}
	t.Fatalf("no app %q in %+v", id, view.Apps)
	return AppView{}
}

func baseResources() []proxmox.Resource {
	return []proxmox.Resource{
		{Type: "node", Node: "pve", Status: "online"},
		{Type: "lxc", Node: "pve", VMID: 100, Name: "homelab", Status: "running", Tags: "homelab"},
		{Type: "lxc", Node: "pve", VMID: 110, Name: "jellyfin", Status: "running"},
	}
}

func TestStatusBeforeInstall(t *testing.T) {
	service := New(Options{Agent: &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100})

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	media := appByID(t, view, "media")
	if media.ID != "media" || media.Installed || media.Install != nil || media.Links[0].URL != "" {
		t.Fatalf("media = %+v", media)
	}
	defaults := view.Defaults
	if defaults.Storage != "local-lvm" || len(defaults.Storages) != 2 || defaults.JellyfinVMID != 110 {
		t.Fatalf("defaults = %+v", defaults)
	}
}

func TestStatusAfterInstall(t *testing.T) {
	resources := append(baseResources(), proxmox.Resource{Type: "lxc", Node: "pve", VMID: 130, Name: "media", Status: "running", Tags: "homelab;media"})
	fake := &fakeAgent{status: agent.AppInstallStatus{App: "media", State: agent.UpgradeSucceeded, VMID: 130}}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100, Hostname: "homelab.local"})

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	media := appByID(t, view, "media")
	if !media.Installed || media.VMID != 130 || media.Install != nil || media.HostURL != "https://seerr.homelab.local" {
		t.Fatalf("media = %+v", media)
	}
	if media.Links[0].URL != "http://192.168.1.50:5055" || media.Links[5].URL != "http://192.168.1.50:8080" {
		t.Fatalf("links = %+v", media.Links)
	}
	// The catalog itself must not change.
	if Catalog[0].Links[0].URL != "" {
		t.Fatal("the catalog got a URL")
	}

	if err := service.Install(context.Background(), context.Background(), "media", agent.InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrInstalled) {
		t.Fatalf("install again: %v", err)
	}
}

func TestInstallChecksAndNotifies(t *testing.T) {
	fake := &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
	messages := make(chan string, 1)
	service := New(Options{
		Agent: fake, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100,
		Notify:       func(_ context.Context, text string) { messages <- text },
		PollInterval: 10 * time.Millisecond,
	})
	ctx := context.Background()

	if err := service.Install(ctx, ctx, "nextcloud", agent.InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrUnknownApp) {
		t.Fatalf("unknown: %v", err)
	}
	bad := validAnswers()
	bad.Password = "short"
	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: bad}); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("invalid: %v", err)
	}

	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: validAnswers()}); err != nil {
		t.Fatal(err)
	}
	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second install: %v", err)
	}

	view, _ := service.Status(ctx)
	if appByID(t, view, "media").Install == nil || appByID(t, view, "media").Install.State != agent.UpgradeRunning {
		t.Fatalf("install = %+v", appByID(t, view, "media").Install)
	}

	fake.set(agent.AppInstallStatus{App: "media", State: agent.UpgradeFailed, Message: "the VPN did not connect"})
	select {
	case message := <-messages:
		if !strings.Contains(message, "Installing Media stack failed: the VPN did not connect") {
			t.Fatalf("message = %q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
	}
}

func TestRetryAndChangedAnswers(t *testing.T) {
	saved := &agent.SavedAnswers{Answers: agent.MediaStackAnswers{NASServer: "192.168.1.5"}}
	fake := &fakeAgent{status: agent.AppInstallStatus{App: "media", State: agent.UpgradeFailed}, saved: saved}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100, PollInterval: time.Hour})
	ctx := context.Background()

	view, err := service.Status(ctx)
	if err != nil || appByID(t, view, "media").Saved != saved {
		t.Fatalf("saved = %+v, err = %v", appByID(t, view, "media").Saved, err)
	}

	if err := service.Retry(ctx, ctx, "media"); err != nil || len(fake.retried) != 1 {
		t.Fatalf("retry: %v, %v", err, fake.retried)
	}
	if err := service.Retry(ctx, ctx, "media"); !errors.Is(err, ErrBusy) {
		t.Fatalf("retry while running: %v", err)
	}

	// With KeepSecrets the empty secrets are fine: the agent fills them in.
	fake.set(agent.AppInstallStatus{App: "media", State: agent.UpgradeFailed})
	changed := validAnswers()
	changed.Password, changed.WireGuardPrivateKey = "", ""
	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: changed, KeepSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if !fake.requests[0].KeepSecrets {
		t.Fatal("KeepSecrets was not passed on")
	}
	// Without it, they are required.
	fake.set(agent.AppInstallStatus{App: "media", State: agent.UpgradeFailed})
	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: changed}); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("empty secrets: %v", err)
	}

	if err := service.Forget(ctx, "media"); err != nil || fake.saved != nil {
		t.Fatalf("forget: %v", err)
	}
	if err := service.Forget(ctx, "nextcloud"); !errors.Is(err, ErrUnknownApp) {
		t.Fatalf("forget unknown: %v", err)
	}
}

func TestAddress(t *testing.T) {
	resources := append(baseResources(), proxmox.Resource{Type: "lxc", Node: "pve", VMID: 130, Name: "media", Status: "running", Tags: "homelab;media"})
	service := New(Options{Agent: &fakeAgent{}, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100})

	address, err := service.Address(context.Background(), "media", 5055)
	if err != nil || address != "http://192.168.1.50:5055" {
		t.Fatalf("address = %q, err = %v", address, err)
	}

	service = New(Options{Agent: &fakeAgent{}, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100})
	if _, err := service.Address(context.Background(), "media", 5055); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v", err)
	}
}

func TestJellyfinFromCommunityScriptsCountsAsInstalled(t *testing.T) {
	// baseResources has a container named jellyfin without Homelab tags.
	fake := &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100})

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	jellyfin := appByID(t, view, "jellyfin")
	if !jellyfin.Installed || jellyfin.VMID != 110 || jellyfin.Links[0].URL != "http://192.168.1.50:8096" {
		t.Fatalf("jellyfin = %+v", jellyfin)
	}
	ctx := context.Background()
	if err := service.Install(ctx, ctx, "jellyfin", agent.InstallRequest{Jellyfin: validJellyfin()}); !errors.Is(err, ErrInstalled) {
		t.Fatalf("second Jellyfin: %v", err)
	}
}

func validJellyfin() agent.JellyfinAnswers {
	return agent.JellyfinAnswers{
		MoviesFolder: "movies", SeriesFolder: "series",
		AdminUsername: "dries", AdminPassword: "jelly pass", Theme: true, Storage: "local-lvm",
	}
}

func TestJellyfinInstallConnectsHomelab(t *testing.T) {
	resources := []proxmox.Resource{
		{Type: "node", Node: "pve", Status: "online"},
		{Type: "lxc", Node: "pve", VMID: 100, Name: "homelab", Status: "running", Tags: "homelab"},
	}
	fake := &fakeAgent{
		status: agent.AppInstallStatus{State: agent.UpgradeIdle},
		mounts: []agent.Mount{{Path: MediaShareMount, Source: "192.168.1.5:/volume1/media", Mounted: true}},
	}
	connected := make(chan agent.AppInstallStatus, 1)
	service := New(Options{
		Agent: fake, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100, PollInterval: 10 * time.Millisecond,
		OnInstalled: func(_ context.Context, app string, status agent.AppInstallStatus) error {
			if app == "jellyfin" {
				connected <- status
			}
			return nil
		},
	})
	ctx := context.Background()

	view, _ := service.Status(ctx)
	if view.Defaults.MediaShare != "192.168.1.5:/volume1/media" {
		t.Fatalf("media share = %q", view.Defaults.MediaShare)
	}

	bad := validJellyfin()
	bad.AdminPassword = ""
	if err := service.Install(ctx, ctx, "jellyfin", agent.InstallRequest{Jellyfin: bad}); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("no password: %v", err)
	}
	if err := service.Install(ctx, ctx, "jellyfin", agent.InstallRequest{Jellyfin: validJellyfin()}); err != nil {
		t.Fatal(err)
	}

	done := agent.AppInstallStatus{App: "jellyfin", State: agent.UpgradeSucceeded, VMID: 140, IP: "192.168.1.60", APIKey: "secret-key"}
	fake.set(done)
	select {
	case status := <-connected:
		if status.APIKey != "secret-key" || status.IP != "192.168.1.60" {
			t.Fatalf("status = %+v", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnInstalled did not run")
	}

	// The key never goes to the browser.
	fake.set(agent.AppInstallStatus{App: "jellyfin", State: agent.UpgradeFailed, APIKey: "secret-key"})
	view, _ = service.Status(ctx)
	if install := appByID(t, view, "jellyfin").Install; install == nil || install.APIKey != "" {
		t.Fatalf("install = %+v", install)
	}
}
