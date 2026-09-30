package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"homelab/internal/agent"
	"homelab/internal/proxmox"
)

type fakeProxmox struct {
	resources []proxmox.Resource
	disks     []proxmox.Disk
	pools     []proxmox.ZFSPool
	err       error
}

func (f *fakeProxmox) Resources(context.Context) ([]proxmox.Resource, error) {
	return f.resources, f.err
}

func (f *fakeProxmox) Disks(context.Context, string) ([]proxmox.Disk, error) {
	return f.disks, nil
}

func (f *fakeProxmox) ZFSPools(context.Context, string) ([]proxmox.ZFSPool, error) {
	return f.pools, nil
}

type fakeAgent struct {
	mounts []agent.Mount
	err    error
}

func (f *fakeAgent) Mounts(context.Context) ([]agent.Mount, error) {
	return f.mounts, f.err
}

type harness struct {
	service  *Service
	pve      *fakeProxmox
	agent    *fakeAgent
	messages []string
	failing  map[string]bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		pve: &fakeProxmox{resources: []proxmox.Resource{
			{Type: "node", Node: "pve", Status: "online"},
			{Type: "storage", Node: "pve", Storage: "local", Status: "available", Disk: 10, MaxDisk: 100},
		}},
		agent:   &fakeAgent{},
		failing: map[string]bool{},
	}

	service, err := New(Options{
		DataDir: t.TempDir(),
		Proxmox: h.pve,
		Agent:   h.agent,
		Notify:  func(_ context.Context, text string) { h.messages = append(h.messages, text) },
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(_ context.Context, check Check) error {
			if h.failing[check.Name] {
				return errors.New("connection refused")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.service = service

	return h
}

func TestServiceGoesDownAfterTwoFailures(t *testing.T) {
	h := newHarness(t)
	check, err := h.service.AddCheck(CheckInput{Name: "Jellyfin", Kind: HTTPCheck, Target: "http://10.0.0.5:8096"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	h.service.CheckServices(ctx)
	if status := h.service.Status().Services[0].Status; status != StatusUp {
		t.Fatalf("status = %s, want up", status)
	}

	h.failing["Jellyfin"] = true
	h.service.CheckServices(ctx)
	if status := h.service.Status().Services[0].Status; status != StatusUp || len(h.messages) != 0 {
		t.Fatalf("one failure: status = %s, messages = %v", status, h.messages)
	}

	h.service.CheckServices(ctx)
	h.service.CheckServices(ctx)
	if status := h.service.Status().Services[0].Status; status != StatusDown {
		t.Fatalf("status = %s, want down", status)
	}
	if len(h.messages) != 1 || !strings.Contains(h.messages[0], "Jellyfin is down: connection refused") {
		t.Fatalf("messages = %v", h.messages)
	}

	h.failing["Jellyfin"] = false
	h.service.CheckServices(ctx)
	if len(h.messages) != 2 || h.messages[1] != "✅ Service Jellyfin is up again" {
		t.Fatalf("messages = %v", h.messages)
	}

	if err := h.service.DeleteCheck(check.ID); err != nil {
		t.Fatal(err)
	}
	if len(h.service.Status().Services) != 0 {
		t.Fatal("check was not deleted")
	}
}

func TestChecksAreSaved(t *testing.T) {
	h := newHarness(t)
	if _, err := h.service.AddCheck(CheckInput{Name: " SSH ", Kind: TCPCheck, Target: "10.0.0.2:22"}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := New(Options{DataDir: h.service.DataDir, Proxmox: h.pve})
	if err != nil {
		t.Fatal(err)
	}

	services := reloaded.Status().Services
	if len(services) != 1 || services[0].Name != "SSH" || services[0].Status != StatusPending {
		t.Fatalf("services = %+v", services)
	}
}

func TestCheckInputValidation(t *testing.T) {
	invalid := []CheckInput{
		{Name: "", Kind: HTTPCheck, Target: "http://10.0.0.5"},
		{Name: "App", Kind: HTTPCheck, Target: "ftp://10.0.0.5"},
		{Name: "App", Kind: HTTPCheck, Target: "10.0.0.5:8096"},
		{Name: "App", Kind: TCPCheck, Target: "10.0.0.5"},
		{Name: "App", Kind: TCPCheck, Target: "10.0.0.5:70000"},
		{Name: "App", Kind: "ping", Target: "10.0.0.5"},
	}
	for _, input := range invalid {
		if err := input.Validate(); !errors.Is(err, ErrInvalidCheck) {
			t.Errorf("%+v: err = %v, want ErrInvalidCheck", input, err)
		}
	}
}

func TestSystemAlerts(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.pve.disks = []proxmox.Disk{
		{DevPath: "/dev/sda", Model: "Samsung SSD", Health: "PASSED", Wearout: json.RawMessage(`3`)},
		{DevPath: "/dev/sdb", Model: "USB stick", Health: "UNKNOWN", Wearout: json.RawMessage(`"N/A"`)},
	}
	h.pve.pools = []proxmox.ZFSPool{{Name: "rpool", Health: "ONLINE"}}

	h.service.CheckSystem(ctx)
	view := h.service.Status()
	if len(h.messages) != 0 || len(view.Disks) != 2 || *view.Disks[0].Wearout != 3 || view.Disks[1].Wearout != nil {
		t.Fatalf("messages = %v, disks = %+v", h.messages, view.Disks)
	}

	h.pve.disks[0].Health = "FAILED"
	h.pve.pools[0].Health = "DEGRADED"
	h.pve.resources[1].Disk = 95
	h.service.CheckSystem(ctx)
	if len(h.messages) != 1 {
		t.Fatalf("messages = %v", h.messages)
	}
	for _, want := range []string{"SMART says FAILED", "rpool on pve is DEGRADED", "local on pve is 95% full"} {
		if !strings.Contains(h.messages[0], want) {
			t.Errorf("message %q does not contain %q", h.messages[0], want)
		}
	}

	// A failed Proxmox call must not resolve the open alerts.
	h.pve.err = errors.New("offline")
	h.service.CheckSystem(ctx)
	h.pve.err = nil
	if len(h.messages) != 1 {
		t.Fatalf("messages = %v", h.messages)
	}

	h.pve.disks[0].Health = "PASSED"
	h.service.CheckSystem(ctx)
	if len(h.messages) != 2 || !strings.Contains(h.messages[1], "✅ Fixed: Disk /dev/sda Samsung SSD on pve") {
		t.Fatalf("messages = %v", h.messages)
	}
}

func TestProbe(t *testing.T) {
	ctx := context.Background()
	statuses := map[string]int{"/ok": 200, "/login": 401, "/broken": 503}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/nowhere", http.StatusFound)
			return
		}
		w.WriteHeader(statuses[r.URL.Path])
	}))
	defer server.Close()

	for path, wantUp := range map[string]bool{"/ok": true, "/login": true, "/redirect": true, "/broken": false} {
		err := probe(ctx, Check{Kind: HTTPCheck, Target: server.URL + path})
		if (err == nil) != wantUp {
			t.Errorf("%s: err = %v, want up = %v", path, err, wantUp)
		}
	}

	address := server.Listener.Addr().String()
	if err := probe(ctx, Check{Kind: TCPCheck, Target: address}); err != nil {
		t.Errorf("tcp: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := listener.Addr().String()
	listener.Close()
	if err := probe(ctx, Check{Kind: TCPCheck, Target: closed}); err == nil || err.Error() != "connection refused" {
		t.Errorf("tcp to a closed port: err = %v, want connection refused", err)
	}
}

func TestShareAlerts(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	media := agent.Mount{Path: "/mnt/homelab/media", Source: "nas:/volume1/media", FSType: "nfs", Mounted: true, Size: 100, Used: 50}
	h.agent.mounts = []agent.Mount{media}

	h.service.CheckSystem(ctx)
	if len(h.messages) != 0 || len(h.service.Status().Shares) != 1 {
		t.Fatalf("messages = %v, shares = %+v", h.messages, h.service.Status().Shares)
	}

	h.agent.mounts[0].Mounted = false
	h.service.CheckSystem(ctx)
	if len(h.messages) != 1 || h.messages[0] != "⚠️ Share nas:/volume1/media is not mounted at /mnt/homelab/media" {
		t.Fatalf("messages = %v", h.messages)
	}

	// When the agent is offline, the open alert stays open.
	h.agent.err = errors.New("offline")
	h.agent.mounts = nil
	h.service.CheckSystem(ctx)
	if len(h.messages) != 1 || len(h.service.Status().Errors) != 1 {
		t.Fatalf("messages = %v, errors = %v", h.messages, h.service.Status().Errors)
	}

	h.agent.err = nil
	h.agent.mounts = []agent.Mount{media}
	h.agent.mounts[0].Used = 95
	h.service.CheckSystem(ctx)
	want := "⚠️ Share nas:/volume1/media is 95% full\n✅ Fixed: Share nas:/volume1/media is not mounted at /mnt/homelab/media"
	if len(h.messages) != 2 || h.messages[1] != want {
		t.Fatalf("messages = %q", h.messages)
	}
}

func TestSetChecksKeepsUnchangedChecks(t *testing.T) {
	h := newHarness(t)
	kept, err := h.service.AddCheck(CheckInput{Name: "Jellyfin", Kind: HTTPCheck, Target: "http://10.0.0.5:8096"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.AddCheck(CheckInput{Name: "Old", Kind: TCPCheck, Target: "10.0.0.9:22"}); err != nil {
		t.Fatal(err)
	}
	h.service.CheckServices(context.Background())

	err = h.service.SetChecks([]CheckInput{
		{Name: "SSH", Kind: TCPCheck, Target: "10.0.0.2:22"},
		{Name: " Jellyfin ", Kind: HTTPCheck, Target: "http://10.0.0.5:8096"},
	})
	if err != nil {
		t.Fatal(err)
	}

	services := h.service.Status().Services
	if len(services) != 2 {
		t.Fatalf("services = %+v", services)
	}
	checks := h.service.Checks()
	if checks[1].ID != kept.ID || checks[0].ID == kept.ID {
		t.Fatalf("checks = %+v, kept %s", checks, kept.ID)
	}
	for _, service := range services {
		want := StatusPending
		if service.ID == kept.ID {
			want = StatusUp
		}
		if service.Status != want {
			t.Fatalf("%s status = %s, want %s", service.Name, service.Status, want)
		}
	}

	if err := h.service.SetChecks([]CheckInput{{Name: "Bad", Kind: "ping", Target: "x"}}); !errors.Is(err, ErrInvalidCheck) {
		t.Fatalf("err = %v", err)
	}
	if len(h.service.Checks()) != 2 {
		t.Fatal("an invalid list changed the checks")
	}
}
