package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"homelab/internal/agent"
	"homelab/internal/auth"
	"homelab/internal/health"
	"homelab/internal/proxmox"
	"homelab/internal/updates"
)

const testSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

type fakeProxmox struct {
	actions []string
}

func (f *fakeProxmox) Resources(context.Context) ([]proxmox.Resource, error) {
	return []proxmox.Resource{
		{ID: "node/pve", Type: "node", Node: "pve", Status: "online"},
		{ID: "lxc/200", Type: "lxc", Node: "pve", VMID: 200, Name: "jellyfin", Status: "running"},
		{ID: "lxc/100", Type: "lxc", Node: "pve", VMID: 100, Name: "homelab", Status: "running"},
		{ID: "lxc/900", Type: "lxc", Node: "pve", VMID: 900, Name: "template", Template: 1},
	}, nil
}

func (f *fakeProxmox) NodeStatus(context.Context, string) (proxmox.NodeStatus, error) {
	return proxmox.NodeStatus{PVEVersion: "pve-manager/9.2.0"}, nil
}

func (f *fakeProxmox) RunGuestAction(_ context.Context, node string, guestType proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error) {
	f.actions = append(f.actions, fmt.Sprintf("%s/%s/%d/%s", node, guestType, vmid, action))
	return "UPID:1", nil
}

type offlineAgent struct{}

func (offlineAgent) Health(context.Context) (agent.Health, error) {
	return agent.Health{}, errors.New("offline")
}

func newTestServer(t *testing.T) (*httptest.Server, *fakeProxmox) {
	t.Helper()

	return newTestServerWith(t, nil)
}

func newTestServerWith(t *testing.T, fakeUpdates Updates, fakeJellyfin ...Jellyfin) (*httptest.Server, *fakeProxmox) {
	t.Helper()

	return newTestServerWithOptions(t, func(options *Options) {
		options.Updates = fakeUpdates
		if len(fakeJellyfin) > 0 {
			options.Jellyfin = fakeJellyfin[0]
		}
	})
}

func newTestServerWithOptions(t *testing.T, configure func(*Options)) (*httptest.Server, *fakeProxmox) {
	t.Helper()

	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}

	pve := &fakeProxmox{}
	options := Options{
		Auth:    auth.NewService(auth.Admin{Username: "admin", PasswordHash: hash, TOTPSecret: testSecret}),
		Proxmox: pve,
		Agent:   offlineAgent{},
		Web:     fstest.MapFS{"index.html": {Data: []byte("<h1>app</h1>")}},
		Logger:  slog.New(slog.DiscardHandler),
	}
	configure(&options)
	handler := New(options)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server, pve
}

func currentCode(t *testing.T) string {
	t.Helper()

	key, err := base32.StdEncoding.DecodeString(testSecret)
	if err != nil {
		t.Fatal(err)
	}

	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(message)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f

	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1_000_000)
}

func request(t *testing.T, method, url, body string, cookie *http.Cookie) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Homelab-Request", "1")
	if cookie != nil {
		req.AddCookie(cookie)
	}

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })

	return response
}

func login(t *testing.T, server *httptest.Server) *http.Cookie {
	t.Helper()

	body := fmt.Sprintf(`{"username":"admin","password":"secret","code":%q}`, currentCode(t))
	response := request(t, http.MethodPost, server.URL+"/api/auth/login", body, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %s", response.Status)
	}

	cookie := response.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("insecure cookie: %+v", cookie)
	}

	return cookie
}

func TestAPIRequiresSession(t *testing.T) {
	server, _ := newTestServer(t)

	response := request(t, http.MethodGet, server.URL+"/api/overview", "", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %s", response.Status)
	}
}

func TestPostWithoutCSRFHeaderIsRejected(t *testing.T) {
	server, _ := newTestServer(t)

	response, err := http.Post(server.URL+"/api/auth/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %s", response.Status)
	}
}

func TestOverviewHidesTemplatesAndSortsGuests(t *testing.T) {
	server, _ := newTestServer(t)
	cookie := login(t, server)

	response := request(t, http.MethodGet, server.URL+"/api/overview", "", cookie)
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %s: %s", response.Status, body)
	}
	text := string(body)
	if strings.Contains(text, `"template"`) {
		t.Error("template guest in overview")
	}
	if strings.Index(text, `"homelab"`) > strings.Index(text, `"jellyfin"`) {
		t.Error("guests not sorted by vmid")
	}
	if !strings.Contains(text, `"connected":false`) {
		t.Error("agent should be reported offline")
	}
}

func TestGuestActionValidatesInput(t *testing.T) {
	server, pve := newTestServer(t)
	cookie := login(t, server)

	if response := request(t, http.MethodPost, server.URL+"/api/guests/pve/lxc/200/destroy", "", cookie); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown action, got %s", response.Status)
	}
	if response := request(t, http.MethodPost, server.URL+"/api/guests/pve/lxc/200/reboot", "", cookie); response.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %s", response.Status)
	}

	if len(pve.actions) != 1 || pve.actions[0] != "pve/lxc/200/reboot" {
		t.Fatalf("unexpected actions: %v", pve.actions)
	}
}

func TestUnknownPathsServeTheApp(t *testing.T) {
	server, _ := newTestServer(t)

	response := request(t, http.MethodGet, server.URL+"/some/page", "", nil)
	body, _ := io.ReadAll(response.Body)

	if !strings.Contains(string(body), "app") {
		t.Fatalf("expected index.html, got %q", body)
	}
	if response.Header.Get("Content-Security-Policy") == "" {
		t.Error("missing CSP header")
	}
}

type fakeUpdates struct {
	Updates
	started []int
}

func (f *fakeUpdates) StartCheck(context.Context) error { return updates.ErrBusy }

func (f *fakeUpdates) StartGuestUpdate(_ context.Context, vmid int) error {
	f.started = append(f.started, vmid)
	return nil
}

func TestUpdateEndpoints(t *testing.T) {
	fake := &fakeUpdates{}
	server, _ := newTestServerWith(t, fake)
	cookie := login(t, server)

	if response := request(t, http.MethodPost, server.URL+"/api/updates/check", "", cookie); response.StatusCode != http.StatusConflict {
		t.Errorf("busy check: expected 409, got %s", response.Status)
	}
	if response := request(t, http.MethodPost, server.URL+"/api/updates/guests/12", "", cookie); response.StatusCode != http.StatusBadRequest {
		t.Errorf("bad vmid: expected 400, got %s", response.Status)
	}
	if response := request(t, http.MethodPost, server.URL+"/api/updates/guests/101", "", cookie); response.StatusCode != http.StatusAccepted {
		t.Errorf("guest update: expected 202, got %s", response.Status)
	}
	if len(fake.started) != 1 || fake.started[0] != 101 {
		t.Errorf("unexpected started updates: %v", fake.started)
	}
	if response := request(t, http.MethodPost, server.URL+"/api/updates/host", "", nil); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("host update without session: expected 401, got %s", response.Status)
	}
}

type fakeJellyfin struct {
	Jellyfin
	images []string
}

func (f *fakeJellyfin) Image(_ context.Context, itemID, imageType string, maxWidth int) (*http.Response, error) {
	f.images = append(f.images, fmt.Sprintf("%s/%s/%d", itemID, imageType, maxWidth))
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("jpeg"))}, nil
}

func TestJellyfinImageProxyValidatesInput(t *testing.T) {
	fake := &fakeJellyfin{}
	server, _ := newTestServerWith(t, nil, fake)
	cookie := login(t, server)
	id := "d9d265a510bd6d93239192285fc4bbed"

	for _, path := range []string{
		"/api/jellyfin/items/../../System/Info/image?type=Primary",
		"/api/jellyfin/items/" + id + "/image?type=Logo",
		"/api/jellyfin/items/not-an-id/image?type=Primary",
	} {
		if response := request(t, http.MethodGet, server.URL+path, "", cookie); response.StatusCode == http.StatusOK {
			t.Errorf("%s: expected rejection, got 200", path)
		}
	}

	response := request(t, http.MethodGet, server.URL+"/api/jellyfin/items/"+id+"/image?type=Primary", "", cookie)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("expected proxied image, got %s", response.Status)
	}
	if len(fake.images) != 1 || fake.images[0] != id+"/Primary/300" {
		t.Fatalf("unexpected image requests: %v", fake.images)
	}
}

type fakeHealth struct {
	Health
	added []health.CheckInput
}

func (f *fakeHealth) AddCheck(input health.CheckInput) (health.Check, error) {
	if input.Name == "" {
		return health.Check{}, health.ErrInvalidCheck
	}
	f.added = append(f.added, input)
	return health.Check{ID: "1", Name: input.Name}, nil
}

func (f *fakeHealth) CheckServices(context.Context) {}

func (f *fakeHealth) DeleteCheck(string) error { return health.ErrCheckNotFound }

func TestHealthCheckEndpoints(t *testing.T) {
	fake := &fakeHealth{}
	server, _ := newTestServerWithOptions(t, func(options *Options) { options.Health = fake })
	cookie := login(t, server)
	body := `{"name":"Jellyfin","kind":"http","target":"http://10.0.0.5:8096"}`

	if response := request(t, http.MethodPost, server.URL+"/api/health/checks", body, nil); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("without session: expected 401, got %s", response.Status)
	}
	if response := request(t, http.MethodPost, server.URL+"/api/health/checks", body, cookie); response.StatusCode != http.StatusCreated {
		t.Errorf("add: expected 201, got %s", response.Status)
	}
	if response := request(t, http.MethodPost, server.URL+"/api/health/checks", `{"name":""}`, cookie); response.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid: expected 400, got %s", response.Status)
	}
	if response := request(t, http.MethodDelete, server.URL+"/api/health/checks/nope", "", cookie); response.StatusCode != http.StatusNotFound {
		t.Errorf("delete unknown: expected 404, got %s", response.Status)
	}
	if len(fake.added) != 1 || fake.added[0].Target != "http://10.0.0.5:8096" {
		t.Errorf("unexpected checks: %+v", fake.added)
	}
}
