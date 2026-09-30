package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSeerr struct {
	mu          sync.Mutex
	initialized bool
	calls       []string
	bodies      map[string]map[string]any
	radarr      []map[string]any
}

func (f *fakeSeerr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	call := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1")
	f.calls = append(f.calls, call)
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	f.bodies[call] = body

	// Everything after the login needs the session cookie.
	if call != "GET /settings/public" && call != "POST /auth/jellyfin" {
		if cookie, err := r.Cookie("connect.sid"); err != nil || cookie.Value != "admin" {
			http.Error(w, "not logged in", http.StatusForbidden)
			return
		}
	}

	switch call {
	case "GET /settings/public":
		json.NewEncoder(w).Encode(map[string]bool{"initialized": f.initialized})
	case "POST /auth/jellyfin":
		http.SetCookie(w, &http.Cookie{Name: "connect.sid", Value: "admin", Path: "/"})
		w.Write([]byte(`{"id":1}`))
	case "POST /settings/jellyfin/library/sync":
		w.Write([]byte(`[{"id":"m1","type":"movie"},{"id":"s1","type":"show"},{"id":"x1","type":"music"}]`))
	case "GET /settings/radarr":
		json.NewEncoder(w).Encode(f.radarr)
	case "GET /settings/sonarr":
		w.Write([]byte(`[]`))
	case "POST /settings/radarr/test":
		w.Write([]byte(`{"profiles":[{"id":1,"name":"Any"},{"id":7,"name":"HD Bluray + WEB"}]}`))
	case "POST /settings/sonarr/test":
		w.Write([]byte(`{"profiles":[{"id":1,"name":"Any"},{"id":4,"name":"HD-1080p"}]}`))
	default:
		w.Write([]byte(`{}`))
	}
}

func seerrConfig() Config {
	return Config{
		RadarrAPIKey: "radarr-key", SonarrAPIKey: "sonarr-key",
		JellyfinURL:           "http://192.168.1.20:8096",
		JellyfinAdminUsername: "dries", JellyfinAdminPassword: "secret",
		SeriesFolder: "TV Shows",
		Logf:         func(string, ...any) {},
	}
}

func TestConfigureSeerr(t *testing.T) {
	fake := &fakeSeerr{bodies: map[string]map[string]any{}}
	server := httptest.NewServer(fake)
	defer server.Close()

	if err := configureSeerr(context.Background(), seerrConfig(), newSeerr(server.URL, 5*time.Second)); err != nil {
		t.Fatal(err)
	}

	login := fake.bodies["POST /auth/jellyfin"]
	if login["username"] != "dries" || login["hostname"] != "192.168.1.20" || login["port"] != float64(8096) || login["serverType"] != float64(mediaServerJellyfin) {
		t.Fatalf("login = %v", login)
	}
	for _, call := range []string{"PUT /settings/jellyfin/library/m1", "PUT /settings/jellyfin/library/s1", "POST /settings/radarr", "POST /settings/sonarr", "POST /settings/initialize"} {
		if !slices.Contains(fake.calls, call) {
			t.Errorf("missing %s in %v", call, fake.calls)
		}
	}
	if slices.Contains(fake.calls, "PUT /settings/jellyfin/library/x1") {
		t.Error("turned on a music library")
	}

	radarr := fake.bodies["POST /settings/radarr"]
	if radarr["hostname"] != "radarr" || radarr["activeProfileId"] != float64(7) || radarr["activeDirectory"] != "/data/media/movies" || radarr["apiKey"] != "radarr-key" {
		t.Fatalf("radarr = %v", radarr)
	}
	// No WEB-1080p yet, so the default of Sonarr.
	sonarr := fake.bodies["POST /settings/sonarr"]
	if sonarr["activeProfileName"] != "HD-1080p" || sonarr["activeDirectory"] != "/data/media/TV Shows" || sonarr["enableSeasonFolders"] != true {
		t.Fatalf("sonarr = %v", sonarr)
	}
}

func TestConfigureSeerrSkipsWhatIsDone(t *testing.T) {
	fake := &fakeSeerr{bodies: map[string]map[string]any{}, initialized: true}
	server := httptest.NewServer(fake)
	defer server.Close()

	if err := configureSeerr(context.Background(), seerrConfig(), newSeerr(server.URL, 5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("an initialized Seerr got calls: %v", fake.calls)
	}

	// Not initialized, but Radarr is already there: only Sonarr is added.
	fake = &fakeSeerr{bodies: map[string]map[string]any{}, radarr: []map[string]any{{"hostname": "radarr"}}}
	server2 := httptest.NewServer(fake)
	defer server2.Close()
	if err := configureSeerr(context.Background(), seerrConfig(), newSeerr(server2.URL, 5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(fake.calls, "POST /settings/radarr") || !slices.Contains(fake.calls, "POST /settings/sonarr") {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestSplitURL(t *testing.T) {
	for raw, want := range map[string]struct {
		host string
		port int
		ssl  bool
	}{
		"http://192.168.1.20:8096":   {"192.168.1.20", 8096, false},
		"https://jellyfin.lan":       {"jellyfin.lan", 443, true},
		"http://jellyfin.lan/":       {"jellyfin.lan", 8096, false},
		"https://10.0.0.5:8920/path": {"10.0.0.5", 8920, true},
	} {
		host, port, ssl, err := splitURL(raw)
		if err != nil || host != want.host || port != want.port || ssl != want.ssl {
			t.Errorf("%s: %s %d %v %v", raw, host, port, ssl, err)
		}
	}
	if _, _, _, err := splitURL("not a url"); err == nil {
		t.Error("no error for a bad URL")
	}
}
