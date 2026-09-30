// Package arr wires the media stack together after `docker compose up`:
// logins, root folders, download client, indexer sync, subtitles and Jellyfin.
// Every step is safe to run again.
package arr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// How the apps reach each other on the compose network. Services behind the
// VPN share gluetun's network, so they're reached as gluetun:<port>.
const (
	radarrInternalURL   = "http://radarr:7878"
	sonarrInternalURL   = "http://sonarr:8989"
	prowlarrInternalURL = "http://gluetun:9696"
	qbittorrentHost     = "gluetun"
	qbittorrentPort     = 8080
	// Prowlarr shares gluetun's network with FlareSolverr.
	flaresolverrURL = "http://localhost:8191/"
)

type Config struct {
	Username string
	Password string

	RadarrAPIKey   string
	SonarrAPIKey   string
	ProwlarrAPIKey string

	// Optional. JellyfinURL is how this tool reaches Jellyfin.
	// JellyfinInternalURL is how Radarr and Sonarr reach it (defaults to JellyfinURL).
	JellyfinURL         string
	JellyfinInternalURL string
	JellyfinAPIKey      string

	SubtitleLanguages []string

	// DataDir is /data on this machine, which the containers see as /data.
	DataDir          string
	BazarrConfigPath string
	Endpoints        Endpoints

	// QBittorrentTempPassword returns the one-time password qBittorrent logs on first start.
	QBittorrentTempPassword func(context.Context) (string, error)
	Logf                    func(format string, args ...any)
}

// Endpoints are the published ports this tool uses to reach the apps.
type Endpoints struct {
	Radarr      string
	Sonarr      string
	Prowlarr    string
	QBittorrent string
	Bazarr      string
}

var DefaultEndpoints = Endpoints{
	Radarr:      "http://localhost:7878",
	Sonarr:      "http://localhost:8989",
	Prowlarr:    "http://localhost:9696",
	QBittorrent: "http://localhost:8080",
	Bazarr:      "http://localhost:6767",
}

func Configure(ctx context.Context, cfg Config) error {
	client := &http.Client{Timeout: 30 * time.Second}

	radarr := newServarr("radarr", cfg.Endpoints.Radarr, "v3", cfg.RadarrAPIKey, client)
	sonarr := newServarr("sonarr", cfg.Endpoints.Sonarr, "v3", cfg.SonarrAPIKey, client)
	prowlarr := newServarr("prowlarr", cfg.Endpoints.Prowlarr, "v1", cfg.ProwlarrAPIKey, client)
	qbit := newQBittorrent(cfg.Endpoints.QBittorrent, client)

	for _, dir := range []string{"media/movies", "media/tv", "downloads/torrents"} {
		if err := makeDataDir(filepath.Join(cfg.DataDir, dir)); err != nil {
			return err
		}
	}

	cfg.Logf("Waiting for the apps to start")
	for _, app := range []interface{ ready(context.Context) error }{radarr, sonarr, prowlarr, qbit} {
		if err := waitFor(ctx, app.ready); err != nil {
			return err
		}
	}

	steps := []struct {
		name string
		run  func(context.Context) error
	}{
		{"qBittorrent", func(ctx context.Context) error { return configureQBittorrent(ctx, cfg, qbit) }},
		{"Radarr", func(ctx context.Context) error {
			return configureServarr(ctx, cfg, radarr, "movies", "movieCategory", "radarr")
		}},
		{"Sonarr", func(ctx context.Context) error {
			return configureServarr(ctx, cfg, sonarr, "tv", "tvCategory", "tv-sonarr")
		}},
		{"Prowlarr", func(ctx context.Context) error { return configureProwlarr(ctx, cfg, prowlarr) }},
		{"Bazarr", func(ctx context.Context) error { return configureBazarr(ctx, cfg, client) }},
	}
	if cfg.JellyfinURL != "" && cfg.JellyfinAPIKey != "" {
		steps = append(steps, struct {
			name string
			run  func(context.Context) error
		}{"Jellyfin libraries", func(ctx context.Context) error { return configureJellyfin(ctx, cfg, client) }})
	}

	for _, step := range steps {
		cfg.Logf("Configuring %s", step.name)
		if err := step.run(ctx); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}

	return nil
}

func configureQBittorrent(ctx context.Context, cfg Config, qbit *qbittorrent) error {
	// After the first run the password is ours. Before that, use the temporary one.
	if err := qbit.login(ctx, cfg.Username, cfg.Password); err != nil {
		tempPassword, tempErr := cfg.QBittorrentTempPassword(ctx)
		if tempErr != nil {
			return errors.Join(err, tempErr)
		}
		if err := qbit.login(ctx, "admin", tempPassword); err != nil {
			return err
		}
	}

	err := qbit.setPreferences(ctx, map[string]any{
		"web_ui_username":   cfg.Username,
		"web_ui_password":   cfg.Password,
		"save_path":         "/data/downloads/torrents",
		"temp_path_enabled": false,
		// Gluetun's port-forward hook calls qBittorrent on 127.0.0.1 without a login.
		"bypass_local_auth": true,
	})
	if err != nil {
		return err
	}

	for _, category := range []string{"radarr", "tv-sonarr"} {
		if err := qbit.ensureCategory(ctx, category); err != nil {
			return err
		}
	}

	return nil
}

func configureServarr(ctx context.Context, cfg Config, app *servarr, mediaFolder, categoryField, category string) error {
	if err := app.setLogin(ctx, cfg.Username, cfg.Password); err != nil {
		return err
	}

	if err := app.ensureRootFolder(ctx, "/data/media/"+mediaFolder); err != nil {
		return err
	}

	err := app.ensure(ctx, provider{
		resource:       "downloadclient",
		implementation: "QBittorrent",
		name:           "qBittorrent",
		fields: map[string]any{
			"host":        qbittorrentHost,
			"port":        qbittorrentPort,
			"username":    cfg.Username,
			"password":    cfg.Password,
			categoryField: category,
		},
		settings: map[string]any{"enable": true},
	})
	if err != nil {
		return err
	}

	if cfg.JellyfinURL == "" || cfg.JellyfinAPIKey == "" {
		return nil
	}

	jellyfin, err := jellyfinHostFields(cfg)
	if err != nil {
		return err
	}
	jellyfin["apiKey"] = cfg.JellyfinAPIKey
	jellyfin["updateLibrary"] = true

	return app.ensure(ctx, provider{
		resource:       "notification",
		implementation: "MediaBrowser",
		name:           "Jellyfin",
		fields:         jellyfin,
		settings: map[string]any{
			"onDownload":          true,
			"onUpgrade":           true,
			"onRename":            true,
			"onMovieDelete":       true,
			"onMovieFileDelete":   true,
			"onSeriesDelete":      true,
			"onEpisodeFileDelete": true,
		},
	})
}

func jellyfinHostFields(cfg Config) (map[string]any, error) {
	raw := cfg.JellyfinInternalURL
	if raw == "" {
		raw = cfg.JellyfinURL
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}

	port := 80
	if parsed.Scheme == "https" {
		port = 443
	}
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			return nil, err
		}
	}

	return map[string]any{
		"host":   parsed.Hostname(),
		"port":   port,
		"useSsl": parsed.Scheme == "https",
	}, nil
}

func configureProwlarr(ctx context.Context, cfg Config, prowlarr *servarr) error {
	if err := prowlarr.setLogin(ctx, cfg.Username, cfg.Password); err != nil {
		return err
	}

	apps := []struct {
		implementation string
		url            string
		apiKey         string
	}{
		{"Radarr", radarrInternalURL, cfg.RadarrAPIKey},
		{"Sonarr", sonarrInternalURL, cfg.SonarrAPIKey},
	}
	for _, app := range apps {
		err := prowlarr.ensure(ctx, provider{
			resource:       "applications",
			implementation: app.implementation,
			name:           app.implementation,
			fields: map[string]any{
				"prowlarrUrl": prowlarrInternalURL,
				"baseUrl":     app.url,
				"apiKey":      app.apiKey,
			},
			settings: map[string]any{"syncLevel": "fullSync"},
		})
		if err != nil {
			return err
		}
	}

	// Prowlarr only sends indexers with this tag through FlareSolverr.
	tag, err := prowlarr.ensureTag(ctx, "flaresolverr")
	if err != nil {
		return err
	}

	return prowlarr.ensure(ctx, provider{
		resource:       "indexerProxy",
		implementation: "FlareSolverr",
		name:           "FlareSolverr",
		fields:         map[string]any{"host": flaresolverrURL},
		settings:       map[string]any{"tags": []int{tag}},
	})
}

func configureBazarr(ctx context.Context, cfg Config, client *http.Client) error {
	var apiKey string
	err := waitFor(ctx, func(context.Context) error {
		key, err := bazarrAPIKey(cfg.BazarrConfigPath)
		apiKey = key
		return err
	})
	if err != nil {
		return err
	}

	app := &bazarr{baseURL: cfg.Endpoints.Bazarr, apiKey: apiKey, http: client}
	if err := waitFor(ctx, app.ready); err != nil {
		return err
	}

	profiles, err := bazarrLanguageProfile(cfg.SubtitleLanguages)
	if err != nil {
		return err
	}

	form := url.Values{
		"settings-auth-type":     {"form"},
		"settings-auth-username": {cfg.Username},
		"settings-auth-password": {cfg.Password},

		"settings-general-use_radarr": {"true"},
		"settings-radarr-ip":          {"radarr"},
		"settings-radarr-port":        {"7878"},
		"settings-radarr-apikey":      {cfg.RadarrAPIKey},

		"settings-general-use_sonarr": {"true"},
		"settings-sonarr-ip":          {"sonarr"},
		"settings-sonarr-port":        {"8989"},
		"settings-sonarr-apikey":      {cfg.SonarrAPIKey},

		"languages-enabled":                      cfg.SubtitleLanguages,
		"languages-profiles":                     {profiles},
		"settings-general-movie_default_enabled": {"true"},
		"settings-general-movie_default_profile": {"1"},
		"settings-general-serie_default_enabled": {"true"},
		"settings-general-serie_default_profile": {"1"},
		// Providers that work without an account.
		"settings-general-enabled_providers": {"embeddedsubtitles", "podnapisi"},
	}

	return app.saveSettings(ctx, form)
}

func configureJellyfin(ctx context.Context, cfg Config, client *http.Client) error {
	jellyfin := newJellyfin(cfg.JellyfinURL, cfg.JellyfinAPIKey, client)

	for _, library := range []jellyfinLibrary{
		{name: "Movies", collectionType: "movies", path: "/data/media/movies"},
		{name: "Shows", collectionType: "tvshows", path: "/data/media/tv"},
	} {
		if err := ensureJellyfinLibrary(ctx, jellyfin, library); err != nil {
			return err
		}
	}

	return nil
}

// containerUser is the PUID/PGID the containers run as.
const containerUser = 1000

func makeDataDir(path string) error {
	if err := os.MkdirAll(path, 0o775); err != nil {
		return err
	}

	// A NAS share that maps all users to one account refuses chown, which is fine.
	if os.Geteuid() == 0 {
		if err := os.Chown(path, containerUser, containerUser); err != nil && !errors.Is(err, os.ErrPermission) {
			return err
		}
	}

	return nil
}

// waitFor retries check every 3 seconds for up to 3 minutes.
func waitFor(ctx context.Context, check func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	for {
		err := check(ctx)
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up waiting: %w", err)
		case <-time.After(3 * time.Second):
		}
	}
}
