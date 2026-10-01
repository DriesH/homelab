// Package jellyfinsetup does the first setup of a new Jellyfin server: the
// startup wizard, the libraries, hardware transcoding, the theme and an API
// key for Homelab.
package jellyfinsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"

	"homelab/internal/jellyfin"
)

// AppName is the name of the API key that Homelab uses.
const AppName = "Homelab"

// GPUDevice is the render node that the installer passes to the container.
const GPUDevice = "/dev/dri/renderD128"

type Config struct {
	URL           string
	AdminUsername string
	AdminPassword string
	MoviesPath    string
	SeriesPath    string
	// GPU turns on VA-API transcoding, for Intel and AMD.
	GPU   bool
	Theme bool
	Logf  func(format string, args ...any)
	// HTTP is replaced in tests.
	HTTP *http.Client
	// Wait is how long to wait for Jellyfin to start, also when it restarts during the setup.
	Wait time.Duration
}

type server struct {
	baseURL string
	token   string
	http    *http.Client
	wait    time.Duration
}

// retryEvery is the pause between two tries while Jellyfin starts.
var retryEvery = 2 * time.Second

// Setup returns an API key for Homelab. It skips the startup wizard when it
// was already done, so it is safe to run again.
func Setup(ctx context.Context, cfg Config) (string, error) {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Wait == 0 {
		cfg.Wait = 3 * time.Minute
	}
	jf := &server{baseURL: strings.TrimRight(cfg.URL, "/"), http: cfg.HTTP, wait: cfg.Wait}

	cfg.Logf("Waiting for Jellyfin to start")
	info, err := jf.waitForStart(ctx, cfg.Wait)
	if err != nil {
		return "", err
	}

	if !info.StartupWizardCompleted {
		cfg.Logf("Finishing the setup wizard")
		if err := jf.wizard(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
			return "", fmt.Errorf("setup wizard: %w", err)
		}
	}

	if err := jf.login(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		return "", fmt.Errorf("log in as %s: %w", cfg.AdminUsername, err)
	}

	cfg.Logf("Adding the Movies and Series libraries")
	for _, library := range []struct{ name, kind, path string }{
		{"Movies", "movies", cfg.MoviesPath},
		{"Series", "tvshows", cfg.SeriesPath},
	} {
		if err := jf.ensureLibrary(ctx, library.name, library.kind, library.path); err != nil {
			return "", err
		}
	}

	if cfg.GPU {
		cfg.Logf("Turning on hardware transcoding (VA-API)")
		if err := jf.enableVAAPI(ctx); err != nil {
			return "", err
		}
	}

	apiKey, err := jf.apiKey(ctx)
	if err != nil {
		return "", err
	}

	if cfg.Theme {
		cfg.Logf("Turning on the Netflix theme")
		if err := jellyfin.NewClient(jf.baseURL, apiKey).SetTheme(ctx, true); err != nil {
			return "", fmt.Errorf("theme: %w", err)
		}
	}

	return apiKey, nil
}

type publicInfo struct {
	StartupWizardCompleted bool `json:"StartupWizardCompleted"`
}

func (s *server) waitForStart(ctx context.Context, wait time.Duration) (publicInfo, error) {
	deadline := time.Now().Add(wait)
	for {
		var info publicInfo
		err := s.do(ctx, http.MethodGet, "/System/Info/Public", nil, &info)
		if err == nil {
			return info, nil
		}
		if time.Now().After(deadline) {
			return publicInfo{}, fmt.Errorf("jellyfin did not start: %w", err)
		}
		select {
		case <-ctx.Done():
			return publicInfo{}, ctx.Err()
		case <-time.After(retryEvery):
		}
	}
}

// wizard does the steps of the first-run wizard of the web page.
func (s *server) wizard(ctx context.Context, username, password string) error {
	steps := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/Startup/Configuration", map[string]string{
			"UICulture": "en-US", "MetadataCountryCode": "US", "PreferredMetadataLanguage": "en",
		}},
		// Getting the user makes Jellyfin create the first user.
		{http.MethodGet, "/Startup/User", nil},
		{http.MethodPost, "/Startup/User", map[string]string{"Name": username, "Password": password}},
		{http.MethodPost, "/Startup/RemoteAccess", map[string]bool{"EnableRemoteAccess": true}},
		{http.MethodPost, "/Startup/Complete", nil},
	}
	for _, step := range steps {
		if err := s.do(ctx, step.method, step.path, step.body, nil); err != nil {
			return err
		}
	}

	return nil
}

func (s *server) login(ctx context.Context, username, password string) error {
	var result struct {
		AccessToken string `json:"AccessToken"`
	}
	body := map[string]string{"Username": username, "Pw": password}
	if err := s.do(ctx, http.MethodPost, "/Users/AuthenticateByName", body, &result); err != nil {
		return err
	}
	if result.AccessToken == "" {
		return errors.New("no access token")
	}
	s.token = result.AccessToken

	return nil
}

type virtualFolder struct {
	Locations []string `json:"Locations"`
}

// ensureLibrary adds a library unless one already uses the path.
func (s *server) ensureLibrary(ctx context.Context, name, kind, path string) error {
	var folders []virtualFolder
	if err := s.do(ctx, http.MethodGet, "/Library/VirtualFolders", nil, &folders); err != nil {
		return err
	}
	if slices.ContainsFunc(folders, func(folder virtualFolder) bool { return slices.Contains(folder.Locations, path) }) {
		return nil
	}

	query := url.Values{"name": {name}, "collectionType": {kind}, "refreshLibrary": {"true"}}
	body := map[string]any{"LibraryOptions": map[string]any{"PathInfos": []map[string]string{{"Path": path}}}}

	return s.do(ctx, http.MethodPost, "/Library/VirtualFolders?"+query.Encode(), body, nil)
}

// enableVAAPI turns on VA-API for H.264 and HEVC, which every GPU since about
// 2015 decodes. Newer codecs can be turned on in Jellyfin itself.
func (s *server) enableVAAPI(ctx context.Context) error {
	var encoding map[string]any
	if err := s.do(ctx, http.MethodGet, "/System/Configuration/encoding", nil, &encoding); err != nil {
		return err
	}
	encoding["HardwareAccelerationType"] = "vaapi"
	encoding["VaapiDevice"] = GPUDevice
	encoding["EnableHardwareEncoding"] = true
	encoding["HardwareDecodingCodecs"] = []string{"h264", "hevc"}

	return s.do(ctx, http.MethodPost, "/System/Configuration/encoding", encoding, nil)
}

// apiKey makes an API key for Homelab, or returns the one it made before.
func (s *server) apiKey(ctx context.Context) (string, error) {
	find := func() (string, error) {
		var keys struct {
			Items []struct {
				AccessToken string `json:"AccessToken"`
				AppName     string `json:"AppName"`
			} `json:"Items"`
		}
		if err := s.do(ctx, http.MethodGet, "/Auth/Keys", nil, &keys); err != nil {
			return "", err
		}
		for _, key := range keys.Items {
			if key.AppName == AppName && key.AccessToken != "" {
				return key.AccessToken, nil
			}
		}
		return "", nil
	}

	if key, err := find(); err != nil || key != "" {
		return key, err
	}
	if err := s.do(ctx, http.MethodPost, "/Auth/Keys?app="+AppName, nil, nil); err != nil {
		return "", err
	}
	key, err := find()
	if err == nil && key == "" {
		err = errors.New("jellyfin made no API key")
	}

	return key, err
}

// do sends a request. While Jellyfin (re)starts, it refuses connections or
// answers 503, and then do tries again until s.wait is over. Jellyfin did
// not handle such a request, so a second try is safe.
func (s *server) do(ctx context.Context, method, path string, body, out any) error {
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return err
		}
	}

	deadline := time.Now().Add(s.wait)
	for {
		err := s.send(ctx, method, path, data, out)
		if !starting(err) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryEvery):
		}
	}
}

type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string { return e.message }

func starting(err error) bool {
	var status *statusError
	return errors.Is(err, syscall.ECONNREFUSED) || (errors.As(err, &status) && status.status == http.StatusServiceUnavailable)
}

func (s *server) send(ctx context.Context, method, path string, data []byte, out any) error {
	var reader io.Reader
	if data != nil {
		reader = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return err
	}
	auth := `MediaBrowser Client="Homelab", Device="Homelab installer", DeviceId="homelab-installer", Version="1"`
	if s.token != "" {
		auth += `, Token="` + s.token + `"`
	}
	request.Header.Set("Authorization", auth)
	if data != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := s.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return &statusError{
			status:  response.StatusCode,
			message: fmt.Sprintf("%s %s: HTTP %d %s", method, path, response.StatusCode, strings.TrimSpace(string(message))),
		}
	}
	if out == nil {
		return nil
	}

	return json.NewDecoder(response.Body).Decode(out)
}
