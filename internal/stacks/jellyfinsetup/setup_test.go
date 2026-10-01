package jellyfinsetup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeJellyfin struct {
	mu        sync.Mutex
	completed bool
	user      map[string]string
	calls     []string
	libraries []map[string]any
	encoding  map[string]any
	keys      []string
	css       string
}

func (f *fakeJellyfin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Jellyfin (ASP.NET) matches paths without case.
	call := r.Method + " " + r.URL.Path
	if strings.EqualFold(r.URL.Path, "/System/Configuration/Branding") {
		call = r.Method + " /System/Configuration/branding"
	}
	f.calls = append(f.calls, call)
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)

	authorized := strings.Contains(r.Header.Get("Authorization"), `Token="user-token"`) ||
		strings.Contains(r.Header.Get("Authorization"), `Token="api-key"`)
	wizardStep := strings.HasPrefix(r.URL.Path, "/Startup/")
	public := call == "GET /System/Info/Public" || call == "POST /Users/AuthenticateByName"
	if !public && !(wizardStep && !f.completed) && !authorized {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch call {
	case "GET /System/Info/Public":
		json.NewEncoder(w).Encode(map[string]bool{"StartupWizardCompleted": f.completed})
	case "POST /Startup/User":
		f.user = map[string]string{"name": body["Name"].(string), "password": body["Password"].(string)}
	case "POST /Startup/Complete":
		f.completed = true
	case "POST /Users/AuthenticateByName":
		if f.user == nil || body["Username"] != f.user["name"] || body["Pw"] != f.user["password"] {
			http.Error(w, "wrong password", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"AccessToken":"user-token"}`))
	case "GET /Library/VirtualFolders":
		json.NewEncoder(w).Encode(f.libraries)
	case "POST /Library/VirtualFolders":
		path := body["LibraryOptions"].(map[string]any)["PathInfos"].([]any)[0].(map[string]any)["Path"]
		f.libraries = append(f.libraries, map[string]any{"Name": r.URL.Query().Get("name"), "CollectionType": r.URL.Query().Get("collectionType"), "Locations": []any{path}})
	case "GET /System/Configuration/encoding":
		json.NewEncoder(w).Encode(f.encoding)
	case "POST /System/Configuration/encoding":
		f.encoding = body
	case "GET /Auth/Keys":
		items := []map[string]string{}
		for _, app := range f.keys {
			items = append(items, map[string]string{"AppName": app, "AccessToken": "api-key"})
		}
		json.NewEncoder(w).Encode(map[string]any{"Items": items})
	case "POST /Auth/Keys":
		f.keys = append(f.keys, r.URL.Query().Get("app"))
	case "GET /System/Configuration/branding":
		json.NewEncoder(w).Encode(map[string]string{"CustomCss": f.css})
	case "POST /System/Configuration/branding":
		f.css, _ = body["CustomCss"].(string)
	}
}

func testConfig(url string) Config {
	return Config{
		URL: url, AdminUsername: "dries", AdminPassword: "secret",
		MoviesPath: "/data/media/movies", SeriesPath: "/data/media/series",
		GPU: true, Theme: true, Logf: func(string, ...any) {}, Wait: time.Second,
	}
}

func TestSetup(t *testing.T) {
	fake := &fakeJellyfin{encoding: map[string]any{"HardwareAccelerationType": "none", "EncodingThreadCount": -1}}
	server := httptest.NewServer(fake)
	defer server.Close()

	key, err := Setup(context.Background(), testConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if key != "api-key" {
		t.Fatalf("key = %q", key)
	}

	wizard := []string{"POST /Startup/Configuration", "GET /Startup/User", "POST /Startup/User", "POST /Startup/RemoteAccess", "POST /Startup/Complete"}
	last := -1
	for _, step := range wizard {
		index := slices.Index(fake.calls, step)
		if index <= last {
			t.Fatalf("wizard step %q out of order in %v", step, fake.calls)
		}
		last = index
	}
	if fake.user["name"] != "dries" {
		t.Fatalf("user = %v", fake.user)
	}
	if len(fake.libraries) != 2 || fake.libraries[1]["CollectionType"] != "tvshows" || fake.libraries[1]["Name"] != "Series" {
		t.Fatalf("libraries = %v", fake.libraries)
	}
	if fake.encoding["HardwareAccelerationType"] != "vaapi" || fake.encoding["VaapiDevice"] != GPUDevice || fake.encoding["EncodingThreadCount"] != float64(-1) {
		t.Fatalf("encoding = %v", fake.encoding)
	}
	if !strings.Contains(fake.css, "homelab-theme:start") {
		t.Fatal("the theme was not turned on")
	}

	// A second run skips the wizard and makes no second key or library.
	calls := len(fake.calls)
	if _, err := Setup(context.Background(), testConfig(server.URL)); err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.calls[calls:] {
		if strings.HasPrefix(call, "POST /Startup/") || call == "POST /Auth/Keys" || call == "POST /Library/VirtualFolders" {
			t.Fatalf("second run called %s", call)
		}
	}
}

func TestSetupWithoutGPUAndTheme(t *testing.T) {
	fake := &fakeJellyfin{encoding: map[string]any{"HardwareAccelerationType": "none"}}
	server := httptest.NewServer(fake)
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.GPU, cfg.Theme = false, false
	if _, err := Setup(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if fake.encoding["HardwareAccelerationType"] != "none" || fake.css != "" {
		t.Fatalf("encoding = %v, css = %q", fake.encoding, fake.css)
	}
}

func TestSetupWrongPasswordOnDoneWizard(t *testing.T) {
	fake := &fakeJellyfin{completed: true, user: map[string]string{"name": "dries", "password": "other"}}
	server := httptest.NewServer(fake)
	defer server.Close()

	if _, err := Setup(context.Background(), testConfig(server.URL)); err == nil || !strings.Contains(err.Error(), "log in as dries") {
		t.Fatalf("err = %v", err)
	}
}

// restartingTransport plays a Jellyfin that restarts right after it first
// answers: it refuses connections, then answers 503, then works.
type restartingTransport struct {
	mu      sync.Mutex
	answers int
	refused int
	busy    int
}

func (t *restartingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.answers > 0 {
		switch {
		case t.refused > 0:
			t.refused--
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
		case t.busy > 0:
			t.busy--
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("starting")), Request: request}, nil
		}
	}
	t.answers++

	return http.DefaultTransport.RoundTrip(request)
}

func TestSetupWaitsWhileJellyfinRestarts(t *testing.T) {
	defer func(previous time.Duration) { retryEvery = previous }(retryEvery)
	retryEvery = 10 * time.Millisecond

	fake := &fakeJellyfin{encoding: map[string]any{}}
	server := httptest.NewServer(fake)
	defer server.Close()

	config := testConfig(server.URL)
	config.HTTP = &http.Client{Transport: &restartingTransport{refused: 3, busy: 2}}
	if _, err := Setup(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if !fake.completed {
		t.Fatal("the wizard was not finished")
	}

	// Jellyfin that stays down still fails, after the wait.
	config.HTTP = &http.Client{Transport: &restartingTransport{refused: 1000}}
	config.Wait = 50 * time.Millisecond
	fake.completed = false
	_, err := Setup(context.Background(), config)
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("got %v, want connection refused", err)
	}
}
