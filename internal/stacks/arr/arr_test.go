package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeServarr stores download clients in memory and serves a QBittorrent schema.
type fakeServarr struct {
	items []map[string]any
}

func (f *fakeServarr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/downloadclient/schema":
		json.NewEncoder(w).Encode([]map[string]any{{
			"implementation": "QBittorrent",
			"enable":         false,
			"fields":         []map[string]any{{"name": "host", "value": "localhost"}, {"name": "port", "value": 8080}},
		}})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/downloadclient":
		json.NewEncoder(w).Encode(f.items)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/downloadclient":
		var item map[string]any
		json.NewDecoder(r.Body).Decode(&item)
		item["id"] = len(f.items) + 1
		f.items = append(f.items, item)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v3/downloadclient/"):
		var item map[string]any
		json.NewDecoder(r.Body).Decode(&item)
		f.items[0] = item
	default:
		http.NotFound(w, r)
	}
}

func TestEnsureCreatesFromSchemaThenUpdatesByName(t *testing.T) {
	fake := &fakeServarr{}
	server := httptest.NewServer(fake)
	defer server.Close()

	app := newServarr("radarr", server.URL, "v3", "key", server.Client())
	client := func(host string) provider {
		return provider{
			resource: "downloadclient", implementation: "QBittorrent", name: "qBittorrent",
			fields:   map[string]any{"host": host},
			settings: map[string]any{"enable": true, "unknownSetting": true},
		}
	}

	if err := app.ensure(context.Background(), client("gluetun")); err != nil {
		t.Fatal(err)
	}
	if err := app.ensure(context.Background(), client("vpn")); err != nil {
		t.Fatal(err)
	}

	if len(fake.items) != 1 {
		t.Fatalf("expected 1 download client, got %d", len(fake.items))
	}

	item := fake.items[0]
	if item["enable"] != true {
		t.Error("enable not set")
	}
	if _, ok := item["unknownSetting"]; ok {
		t.Error("setting missing from the schema was added")
	}

	fields := item["fields"].([]any)
	if host := fields[0].(map[string]any)["value"]; host != "vpn" {
		t.Errorf("host not updated, got %v", host)
	}
	if port := fields[1].(map[string]any)["value"]; port != float64(8080) {
		t.Errorf("schema default lost, got %v", port)
	}
}

func TestQBittorrentLoginAcceptsNewCookieName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("password") == "right" {
			http.SetCookie(w, &http.Cookie{Name: "QBT_SID_8080", Value: "session", Path: "/"})
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := newQBittorrent(server.URL, server.Client()).login(context.Background(), "admin", "right"); err != nil {
		t.Fatalf("login with right password failed: %v", err)
	}
	if err := newQBittorrent(server.URL, server.Client()).login(context.Background(), "admin", "wrong"); err == nil {
		t.Fatal("login with wrong password succeeded")
	}
}

func TestBazarrAPIKeyReadsAuthSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := "analytics:\n  enabled: true\nauth:\n  apikey: abc123\n  type: null\nradarr:\n  apikey: ''\n"
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	key, err := bazarrAPIKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if key != "abc123" {
		t.Fatalf("expected abc123, got %q", key)
	}
}

func TestJellyfinHostFieldsPreferInternalURL(t *testing.T) {
	fields, err := jellyfinHostFields(Config{
		JellyfinURL:         "http://192.168.1.20:8096",
		JellyfinInternalURL: "https://jellyfin.lan",
	})
	if err != nil {
		t.Fatal(err)
	}

	if fields["host"] != "jellyfin.lan" || fields["port"] != 443 || fields["useSsl"] != true {
		t.Fatalf("unexpected fields: %v", fields)
	}

	fields, _ = jellyfinHostFields(Config{JellyfinURL: "http://192.168.1.20:8096"})
	if fields["host"] != "192.168.1.20" || fields["port"] != 8096 || fields["useSsl"] != false {
		t.Fatalf("unexpected fields: %v", fields)
	}
}

func TestValidFolder(t *testing.T) {
	for _, name := range []string{"movies", "Series", "TV Shows", "films_4k", "a"} {
		if !ValidFolder(name) {
			t.Errorf("%q should be valid", name)
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", "../movies", " movies", "movies ", ".hidden", "a\nb", "a..b"} {
		if ValidFolder(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

func TestJellyfinLibrariesUseTheFolders(t *testing.T) {
	libraries := jellyfinLibraries(Config{MoviesFolder: "Films", SeriesFolder: "TV Shows"})
	if libraries[0].path != "/data/media/Films" || libraries[1].path != "/data/media/TV Shows" || libraries[1].name != "Series" {
		t.Fatalf("libraries = %+v", libraries)
	}

	// Installs from before the folders could be chosen keep movies and tv.
	libraries = jellyfinLibraries(Config{})
	if libraries[0].path != "/data/media/movies" || libraries[1].path != "/data/media/tv" {
		t.Fatalf("default libraries = %+v", libraries)
	}
}
