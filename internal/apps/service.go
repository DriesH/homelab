// Package apps is the app catalog: stacks that the host agent installs in
// their own container, from installers in the release bundle.
package apps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"homelab/internal/agent"
	"homelab/internal/proxmox"
)

var (
	ErrUnknownApp = errors.New("unknown app")
	ErrInstalled  = errors.New("this app is already installed")
	ErrBusy       = errors.New("an app is already being installed")
)

type Agent interface {
	InstallApp(ctx context.Context, app string, request agent.InstallRequest) error
	RetryApp(ctx context.Context, app string) error
	SavedAppAnswers(ctx context.Context, app string) (*agent.SavedAnswers, error)
	ForgetAppAnswers(ctx context.Context, app string) error
	AppInstallStatus(ctx context.Context) (agent.AppInstallStatus, error)
}

type Proxmox interface {
	Resources(ctx context.Context) ([]proxmox.Resource, error)
	ContainerStorages(ctx context.Context, node string) ([]proxmox.BackupStorage, error)
	ContainerInterfaces(ctx context.Context, node string, vmid int) ([]proxmox.Interface, error)
}

type Link struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Port        int    `json:"-"`
	URL         string `json:"url,omitempty"`
}

type App struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Tag marks the container of the app in Proxmox.
	Tag   string `json:"-"`
	Links []Link `json:"links"`
}

var Catalog = []App{{
	ID:          agent.MediaStackApp,
	Name:        "Media stack",
	Description: "Request, find and download movies and series for Jellyfin. Downloads go through ProtonVPN.",
	Tag:         "media",
	Links: []Link{
		{Name: "Seerr", Description: "Requests", Port: 5055},
		{Name: "Radarr", Description: "Movies", Port: 7878},
		{Name: "Sonarr", Description: "Series", Port: 8989},
		{Name: "Prowlarr", Description: "Indexers", Port: 9696},
		{Name: "Bazarr", Description: "Subtitles", Port: 6767},
		{Name: "qBittorrent", Description: "Downloads", Port: 8080},
	},
}}

type AppView struct {
	App
	Installed bool   `json:"installed"`
	VMID      int    `json:"vmid,omitempty"`
	Status    string `json:"status,omitempty"`
	// Install is the last install of this app, while it runs or when it failed.
	Install *agent.AppInstallStatus `json:"install"`
	// Saved are the answers of a failed install, without secrets, for a retry.
	Saved *agent.SavedAnswers `json:"saved"`
}

type Defaults struct {
	Storages          []string `json:"storages"`
	Storage           string   `json:"storage"`
	JellyfinVMID      int      `json:"jellyfinVmid,omitempty"`
	MoviesFolder      string   `json:"moviesFolder"`
	SeriesFolder      string   `json:"seriesFolder"`
	VPNCountries      string   `json:"vpnCountries"`
	SubtitleLanguages string   `json:"subtitleLanguages"`
	Username          string   `json:"username"`
	DownloadsSize     int      `json:"downloadsSize"`
}

type View struct {
	Apps     []AppView `json:"apps"`
	Defaults Defaults  `json:"defaults"`
	// Error is set when the host agent did not answer.
	Error string `json:"error,omitempty"`
}

type Options struct {
	Agent   Agent
	Proxmox Proxmox
	// SelfVMID is the manager's container. The agent, and so the new app, runs on its node.
	SelfVMID int
	Notify   func(ctx context.Context, text string)
	Logger   *slog.Logger
	// PollInterval is how often a running install is checked. Tests shorten it.
	PollInterval time.Duration
}

type Service struct {
	Options

	mu         sync.Mutex
	monitoring bool
}

func New(options Options) *Service {
	if options.Notify == nil {
		options.Notify = func(context.Context, string) {}
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.PollInterval == 0 {
		options.PollInterval = 10 * time.Second
	}

	return &Service{Options: options}
}

func (s *Service) Status(ctx context.Context) (View, error) {
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return View{}, err
	}
	node := s.node(resources)

	view := View{Apps: []AppView{}, Defaults: s.defaults(ctx, node, resources)}
	install, err := s.Agent.AppInstallStatus(ctx)
	if err != nil {
		view.Error = err.Error()
	}

	for _, app := range Catalog {
		appView := AppView{App: app}
		appView.Links = slices.Clone(app.Links)

		if guest, found := findGuest(resources, app.Tag); found {
			appView.Installed, appView.VMID, appView.Status = true, guest.VMID, guest.Status
			if ip := s.guestIP(ctx, guest); ip != "" {
				for i := range appView.Links {
					appView.Links[i].URL = fmt.Sprintf("http://%s:%d", ip, appView.Links[i].Port)
				}
			}
		}
		if install.App == app.ID && install.State != agent.UpgradeIdle && !(appView.Installed && install.State == agent.UpgradeSucceeded) {
			appView.Install = &install
		}
		if !appView.Installed && install.State != agent.UpgradeRunning {
			if saved, err := s.Agent.SavedAppAnswers(ctx, app.ID); err == nil {
				appView.Saved = saved
			}
		}

		view.Apps = append(view.Apps, appView)
	}

	return view, nil
}

// node is where the host agent runs: the node of the manager's container.
func (s *Service) node(resources []proxmox.Resource) string {
	online := ""
	for _, resource := range resources {
		if resource.Type == "lxc" && resource.VMID == s.SelfVMID {
			return resource.Node
		}
		if resource.Type == "node" && resource.Status == "online" && online == "" {
			online = resource.Node
		}
	}

	return online
}

func (s *Service) defaults(ctx context.Context, node string, resources []proxmox.Resource) Defaults {
	defaults := Defaults{
		Storages:          []string{},
		MoviesFolder:      "movies",
		SeriesFolder:      "series",
		VPNCountries:      "Netherlands",
		SubtitleLanguages: "en",
		Username:          "homelab",
		DownloadsSize:     200,
	}

	if storages, err := s.Proxmox.ContainerStorages(ctx, node); err == nil {
		for _, storage := range storages {
			if storage.Active {
				defaults.Storages = append(defaults.Storages, storage.Storage)
			}
		}
	}
	slices.Sort(defaults.Storages)
	// local-lvm is the default of a new Proxmox install.
	if slices.Contains(defaults.Storages, "local-lvm") || len(defaults.Storages) == 0 {
		defaults.Storage = "local-lvm"
	} else {
		defaults.Storage = defaults.Storages[0]
	}

	for _, resource := range resources {
		if resource.Type == "lxc" && resource.Name == "jellyfin" {
			defaults.JellyfinVMID = resource.VMID
		}
	}

	return defaults
}

func findGuest(resources []proxmox.Resource, tag string) (proxmox.Resource, bool) {
	for _, resource := range resources {
		if resource.Type != "lxc" || resource.Template == 1 {
			continue
		}
		tags := strings.Split(resource.Tags, ";")
		if slices.Contains(tags, "homelab") && slices.Contains(tags, tag) {
			return resource, true
		}
	}

	return proxmox.Resource{}, false
}

// guestIP returns the first IPv4 address of a running container.
func (s *Service) guestIP(ctx context.Context, guest proxmox.Resource) string {
	if guest.Status != "running" {
		return ""
	}
	interfaces, err := s.Proxmox.ContainerInterfaces(ctx, guest.Node, guest.VMID)
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		prefix, err := netip.ParsePrefix(iface.Inet)
		if err == nil && iface.Name != "lo" && prefix.Addr().Is4() && !prefix.Addr().IsLoopback() {
			return prefix.Addr().String()
		}
	}

	return ""
}

func (s *Service) Install(ctx context.Context, background context.Context, id string, request agent.InstallRequest) error {
	// With KeepSecrets, the agent checks the answers after it adds the saved secrets.
	if !request.KeepSecrets {
		if err := request.Validate(); err != nil {
			return err
		}
	}

	return s.start(ctx, background, id, func(app App) error {
		return s.Agent.InstallApp(ctx, app.ID, request)
	})
}

// Retry runs the install again with the answers the agent saved.
func (s *Service) Retry(ctx context.Context, background context.Context, id string) error {
	return s.start(ctx, background, id, func(app App) error {
		return s.Agent.RetryApp(ctx, app.ID)
	})
}

func (s *Service) Forget(ctx context.Context, id string) error {
	if !slices.ContainsFunc(Catalog, func(app App) bool { return app.ID == id }) {
		return ErrUnknownApp
	}

	return s.Agent.ForgetAppAnswers(ctx, id)
}

// start checks that the app can be installed, runs install, and watches it.
func (s *Service) start(ctx context.Context, background context.Context, id string, install func(App) error) error {
	index := slices.IndexFunc(Catalog, func(app App) bool { return app.ID == id })
	if index < 0 {
		return ErrUnknownApp
	}
	app := Catalog[index]

	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return err
	}
	if _, found := findGuest(resources, app.Tag); found {
		return ErrInstalled
	}
	if status, err := s.Agent.AppInstallStatus(ctx); err == nil && status.State == agent.UpgradeRunning {
		return ErrBusy
	}

	if err := install(app); err != nil {
		return err
	}
	s.Logger.Info("app install started", "app", id)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.monitoring {
		s.monitoring = true
		go s.monitor(background, app)
	}

	return nil
}

// monitor waits for the install to end and sends the result to Telegram.
func (s *Service) monitor(ctx context.Context, app App) {
	defer func() {
		s.mu.Lock()
		s.monitoring = false
		s.mu.Unlock()
	}()

	ticker := time.NewTicker(s.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		status, err := s.Agent.AppInstallStatus(ctx)
		if err != nil || status.State == agent.UpgradeRunning {
			continue
		}

		if status.State == agent.UpgradeSucceeded {
			s.Logger.Info("app installed", "app", app.ID, "vmid", status.VMID)
			s.Notify(ctx, fmt.Sprintf("✅ %s is installed in container %d.", app.Name, status.VMID))
		} else {
			s.Logger.Warn("app install failed", "app", app.ID, "message", status.Message)
			s.Notify(ctx, fmt.Sprintf("❌ Installing %s failed: %s", app.Name, status.Message))
		}
		return
	}
}
