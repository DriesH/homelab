package tailscale

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const runningStatus = `{
  "BackendState": "Running",
  "AuthURL": "",
  "Health": [],
  "Self": {"HostName": "homelab", "DNSName": "homelab.tail1234.ts.net.", "TailscaleIPs": ["100.64.0.1", "fd7a::1"], "PrimaryRoutes": ["192.168.1.0/24"]},
  "CurrentTailnet": {"Name": "me@example.com"},
  "Peer": {
    "a": {"HostName": "phone", "DNSName": "phone.tail1234.ts.net.", "OS": "iOS", "TailscaleIPs": ["100.64.0.2"], "Online": true},
    "b": {"HostName": "laptop", "DNSName": "laptop.tail1234.ts.net.", "OS": "macOS", "TailscaleIPs": ["100.64.0.3"], "Online": false}
  }
}`

type fakeCLI struct {
	mu      sync.Mutex
	calls   [][]string
	outputs map[string]string
	errs    map[string]error
	keys    []string
}

func (f *fakeCLI) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, args)
	for _, arg := range args {
		if path, ok := strings.CutPrefix(arg, "--auth-key=file:"); ok {
			key, _ := os.ReadFile(path)
			f.keys = append(f.keys, string(key))
		}
	}

	name := strings.Join(args[:min(2, len(args))], " ")
	return []byte(f.outputs[name]), f.errs[name]
}

func (f *fakeCLI) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, call := range f.calls {
		if strings.HasPrefix(strings.Join(call, " "), prefix) {
			return call
		}
	}
	return nil
}

func newTestService(t *testing.T, cli *fakeCLI) *Service {
	t.Helper()

	service, err := New(Options{
		DataDir:  t.TempDir(),
		Backend:  "https+insecure://127.0.0.1:443",
		Logger:   slog.New(slog.DiscardHandler),
		Run:      cli.run,
		Operator: "homelab",
	})
	if err != nil {
		t.Fatal(err)
	}

	return service
}

func TestStatusWhenRunning(t *testing.T) {
	cli := &fakeCLI{outputs: map[string]string{
		"status --json": runningStatus,
		"serve status":  `{"TCP":{"443":{"HTTPS":true}},"Web":{"homelab.tail1234.ts.net:443":{}}}`,
	}}
	service := newTestService(t, cli)
	service.settings = Settings{ShareSubnet: true, Subnet: "192.168.1.0/24"}

	view := service.Status(context.Background())
	if !view.Installed || view.State != "Running" || view.DNSName != "homelab.tail1234.ts.net" {
		t.Fatalf("view = %+v", view)
	}
	if !view.Serving || view.ServeURL != "https://homelab.tail1234.ts.net" || !view.SubnetApproved {
		t.Fatalf("serve and subnet: %+v", view)
	}
	if len(view.Peers) != 2 || view.Peers[0].Name != "laptop" || !view.Peers[1].Online {
		t.Fatalf("peers = %+v", view.Peers)
	}
}

func TestStatusWhileANewLoginWaits(t *testing.T) {
	cli := &fakeCLI{outputs: map[string]string{
		"status --json": `{"BackendState":"Running","AuthURL":"https://login.tailscale.com/a/tag",` +
			`"Health":["You are logged out. The last login error was: fetch control key: context canceled","Some other warning"]}`,
	}}
	view := newTestService(t, cli).Status(context.Background())

	if view.State != "Running" || view.AuthURL != "https://login.tailscale.com/a/tag" {
		t.Fatalf("view = %+v", view)
	}
	if !slices.Equal(view.Health, []string{"Some other warning"}) {
		t.Fatalf("health = %q", view.Health)
	}
}

func TestStatusWhenNotInstalled(t *testing.T) {
	cli := &fakeCLI{errs: map[string]error{"status --json": ErrNotInstalled}}
	view := newTestService(t, cli).Status(context.Background())

	if view.Installed || view.IPs == nil || view.Peers == nil {
		t.Fatalf("view = %+v", view)
	}
}

func TestStatusNeedsLogin(t *testing.T) {
	cli := &fakeCLI{
		outputs: map[string]string{"status --json": `{"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/abc","Health":["Tailscale is stopped."]}`},
		errs:    map[string]error{"status --json": errors.New("exit status 1")},
	}
	view := newTestService(t, cli).Status(context.Background())

	if view.State != "NeedsLogin" || view.AuthURL != "https://login.tailscale.com/a/abc" || view.Serving {
		t.Fatalf("view = %+v", view)
	}
	if cli.called("serve") != nil {
		t.Fatal("serve status should only be read while running")
	}
}

func TestConnectPassesAllSettings(t *testing.T) {
	cli := &fakeCLI{}
	service := newTestService(t, cli)
	service.settings = Settings{ShareSubnet: true, Subnet: "192.168.1.0/24"}

	if err := service.Connect(context.Background(), " tskey-auth-secret "); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !service.Status(context.Background()).Connecting })

	call := cli.called("up")
	for _, want := range []string{"--reset", "--operator=homelab", "--accept-dns=false", "--advertise-routes=192.168.1.0/24"} {
		if !slices.Contains(call, want) {
			t.Errorf("up call %v misses %s", call, want)
		}
	}
	if strings.Contains(strings.Join(call, " "), "tskey-auth-secret") {
		t.Error("the auth key is in the command line")
	}
	if len(cli.keys) != 1 || cli.keys[0] != "tskey-auth-secret" {
		t.Errorf("key file held %q", cli.keys)
	}

	files, _ := os.ReadDir(service.DataDir)
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".tailscale-key") {
			t.Error("the key file was not removed")
		}
	}
}

func TestSetServeShowsEnableLink(t *testing.T) {
	cli := &fakeCLI{
		outputs: map[string]string{"serve --bg": "Serve is not enabled on your tailnet.\nTo enable, visit:\n\n         https://login.tailscale.com/f/serve?node=abc\n"},
		errs:    map[string]error{"serve --bg": errors.New("exit status 1")},
	}
	err := newTestService(t, cli).SetServe(context.Background(), true)

	if err == nil || !strings.Contains(err.Error(), "https://login.tailscale.com/f/serve?node=abc") {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveSettings(t *testing.T) {
	cli := &fakeCLI{outputs: map[string]string{"status --json": runningStatus}}
	service := newTestService(t, cli)
	ctx := context.Background()

	for _, subnet := range []string{"", "192.168.1.5/24", "fd00::/64", "home"} {
		if err := service.SaveSettings(ctx, Settings{ShareSubnet: true, Subnet: subnet}); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("subnet %q: err = %v", subnet, err)
		}
	}

	if err := service.SaveSettings(ctx, Settings{ShareSubnet: true, Subnet: "192.168.1.0/24"}); err != nil {
		t.Fatal(err)
	}
	if call := cli.called("up"); !slices.Contains(call, "--advertise-routes=192.168.1.0/24") {
		t.Fatalf("settings not applied: %v", call)
	}

	reloaded, _ := New(Options{DataDir: service.DataDir, Run: cli.run})
	if !reloaded.settings.ShareSubnet || reloaded.settings.Subnet != "192.168.1.0/24" {
		t.Fatalf("settings not saved: %+v", reloaded.settings)
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUseTagLogsInAgainWithTheTag(t *testing.T) {
	cli := &fakeCLI{outputs: map[string]string{}, errs: map[string]error{}}
	service := newTestService(t, cli)

	if err := service.UseTag(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return cli.called("up") != nil })

	call := strings.Join(cli.called("up"), " ")
	if !strings.Contains(call, "--advertise-tags=tag:homelab") || !strings.Contains(call, "--force-reauth") {
		t.Fatalf("up = %s", call)
	}

	// Saving the subnet keeps the tag.
	if err := service.SaveSettings(context.Background(), Settings{ShareSubnet: false}); err != nil {
		t.Fatal(err)
	}
	if !service.Status(context.Background()).Settings.UseTag {
		t.Fatal("saving the settings dropped the tag")
	}
}

func TestServices(t *testing.T) {
	tagged := strings.Replace(runningStatus, `"PrimaryRoutes": ["192.168.1.0/24"]`, `"PrimaryRoutes": [], "Tags": ["tag:homelab"]`, 1)
	cli := &fakeCLI{outputs: map[string]string{
		"status --json": tagged,
		"serve status":  `{"Services": {"svc:seerr": {"TCP": {"443": {"HTTPS": true}}}}}`,
	}, errs: map[string]error{}}
	service := newTestService(t, cli)
	service.Services = []AppService{
		{Name: "seerr", Title: "Seerr", Backend: "http://127.0.0.1:18081"},
		{Name: "jellyfin", Title: "Jellyfin", Backend: "http://127.0.0.1:18082"},
	}

	view := service.Status(context.Background())
	if len(view.Tags) != 1 || view.Tags[0] != "tag:homelab" {
		t.Fatalf("tags = %v", view.Tags)
	}
	if len(view.Services) != 2 || !view.Services[0].Published || view.Services[0].URL != "https://seerr.tail1234.ts.net" || view.Services[1].Published {
		t.Fatalf("services = %+v", view.Services)
	}
	// A Service is not the manager itself.
	if view.Serving {
		t.Fatal("a Service counted as serving the manager")
	}

	if err := service.SetService(context.Background(), "jellyfin", true); err != nil {
		t.Fatal(err)
	}
	if call := strings.Join(cli.called("serve --service"), " "); call != "serve --service=svc:jellyfin --https=443 --yes http://127.0.0.1:18082" {
		t.Fatalf("publish = %q", call)
	}
	if err := service.SetService(context.Background(), "seerr", false); err != nil {
		t.Fatal(err)
	}
	if call := strings.Join(cli.called("serve clear"), " "); call != "serve clear svc:seerr" {
		t.Fatalf("remove = %q", call)
	}
	if err := service.SetService(context.Background(), "radarr", true); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("unknown app: %v", err)
	}
}
