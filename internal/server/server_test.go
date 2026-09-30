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
	"homelab/internal/proxmox"
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

	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}

	pve := &fakeProxmox{}
	handler := New(Options{
		Auth:    auth.NewService(auth.Admin{Username: "admin", PasswordHash: hash, TOTPSecret: testSecret}),
		Proxmox: pve,
		Agent:   offlineAgent{},
		Web:     fstest.MapFS{"index.html": {Data: []byte("<h1>app</h1>")}},
		Logger:  slog.New(slog.DiscardHandler),
	})

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
