package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Seerr runs in the same Docker network as Radarr and Sonarr, so it reaches
// them by container name.
const (
	seerrRadarrHost = "radarr"
	seerrSonarrHost = "sonarr"
	// mediaServerJellyfin is MediaServerType.JELLYFIN in Seerr.
	mediaServerJellyfin = 2
)

// The profiles Recyclarr makes, then the defaults of Radarr and Sonarr.
var (
	radarrProfiles = []string{"HD Bluray + WEB", "HD-1080p"}
	sonarrProfiles = []string{"WEB-1080p", "HD-1080p"}
)

func newSeerr(baseURL string, timeout time.Duration) *apiClient {
	// Seerr keeps the login in a session cookie.
	jar, _ := cookiejar.New(nil)

	return &apiClient{
		name:    "seerr",
		baseURL: strings.TrimRight(baseURL, "/") + "/api/v1",
		http:    &http.Client{Timeout: timeout, Jar: jar},
	}
}

type seerrService struct {
	Hostname string `json:"hostname"`
}

type seerrProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// configureSeerr does the setup wizard of Seerr: it logs in with the Jellyfin
// admin, turns on the movie and series libraries, adds Radarr and Sonarr, and
// finishes the setup. It does nothing when Seerr was already set up.
func configureSeerr(ctx context.Context, cfg Config, seerr *apiClient) error {
	var public struct {
		Initialized bool `json:"initialized"`
	}
	if err := seerr.do(ctx, http.MethodGet, "/settings/public", nil, &public); err != nil {
		return err
	}
	if public.Initialized {
		cfg.Logf("Seerr is already set up, skipping it")
		return nil
	}

	jellyfinURL := cfg.JellyfinInternalURL
	if jellyfinURL == "" {
		jellyfinURL = cfg.JellyfinURL
	}
	host, port, useSSL, err := splitURL(jellyfinURL)
	if err != nil {
		return err
	}

	login := map[string]any{
		"username":   cfg.JellyfinAdminUsername,
		"password":   cfg.JellyfinAdminPassword,
		"hostname":   host,
		"port":       port,
		"useSsl":     useSSL,
		"urlBase":    "",
		"email":      "",
		"serverType": mediaServerJellyfin,
	}
	if err := seerr.do(ctx, http.MethodPost, "/auth/jellyfin", login, nil); err != nil {
		return fmt.Errorf("log in to Seerr with the Jellyfin admin: %w", err)
	}

	var libraries []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := seerr.do(ctx, http.MethodPost, "/settings/jellyfin/library/sync", nil, &libraries); err != nil {
		return err
	}
	for _, library := range libraries {
		if library.Type != "movie" && library.Type != "show" {
			continue
		}
		path := "/settings/jellyfin/library/" + url.PathEscape(library.ID)
		if err := seerr.do(ctx, http.MethodPut, path, map[string]bool{"enabled": true}, nil); err != nil {
			return err
		}
	}

	movies, series := cfg.folders()
	radarr := map[string]any{
		"name": "Radarr", "hostname": seerrRadarrHost, "port": 7878, "apiKey": cfg.RadarrAPIKey,
		"useSsl": false, "baseUrl": "", "activeDirectory": "/data/media/" + movies,
		"minimumAvailability": "released", "is4k": false, "isDefault": true,
		"tags": []int{}, "syncEnabled": true, "preventSearch": false, "tagRequests": false, "overrideRule": []int{},
	}
	if err := addSeerrService(ctx, seerr, "radarr", radarr, radarrProfiles); err != nil {
		return err
	}

	sonarr := map[string]any{
		"name": "Sonarr", "hostname": seerrSonarrHost, "port": 8989, "apiKey": cfg.SonarrAPIKey,
		"useSsl": false, "baseUrl": "", "activeDirectory": "/data/media/" + series,
		"seriesType": "standard", "animeSeriesType": "anime", "enableSeasonFolders": true, "monitorNewItems": "all",
		"is4k": false, "isDefault": true,
		"tags": []int{}, "syncEnabled": true, "preventSearch": false, "tagRequests": false, "overrideRule": []int{},
	}
	if err := addSeerrService(ctx, seerr, "sonarr", sonarr, sonarrProfiles); err != nil {
		return err
	}

	return seerr.do(ctx, http.MethodPost, "/settings/initialize", nil, nil)
}

// addSeerrService adds Radarr or Sonarr, with the first profile that exists.
// It skips a service that Seerr already has.
func addSeerrService(ctx context.Context, seerr *apiClient, kind string, settings map[string]any, wanted []string) error {
	var existing []seerrService
	if err := seerr.do(ctx, http.MethodGet, "/settings/"+kind, nil, &existing); err != nil {
		return err
	}
	if slices.ContainsFunc(existing, func(service seerrService) bool { return service.Hostname == settings["hostname"] }) {
		return nil
	}

	test := map[string]any{"hostname": settings["hostname"], "port": settings["port"], "apiKey": settings["apiKey"], "useSsl": false, "baseUrl": ""}
	var found struct {
		Profiles []seerrProfile `json:"profiles"`
	}
	if err := seerr.do(ctx, http.MethodPost, "/settings/"+kind+"/test", test, &found); err != nil {
		return err
	}

	profile, ok := pickProfile(found.Profiles, wanted)
	if !ok {
		return fmt.Errorf("%s has no quality profiles", kind)
	}
	settings["activeProfileId"] = profile.ID
	settings["activeProfileName"] = profile.Name

	return seerr.do(ctx, http.MethodPost, "/settings/"+kind, settings, nil)
}

func pickProfile(profiles []seerrProfile, wanted []string) (seerrProfile, bool) {
	for _, name := range wanted {
		if index := slices.IndexFunc(profiles, func(profile seerrProfile) bool { return profile.Name == name }); index >= 0 {
			return profiles[index], true
		}
	}
	if len(profiles) > 0 {
		return profiles[0], true
	}

	return seerrProfile{}, false
}

// splitURL turns http://192.168.1.20:8096 into the parts Seerr asks for.
func splitURL(raw string) (host string, port int, useSSL bool, err error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", 0, false, fmt.Errorf("invalid Jellyfin URL %q", raw)
	}

	useSSL = parsed.Scheme == "https"
	port = 8096
	if useSSL {
		port = 443
	}
	if parsed.Port() != "" {
		port, _ = strconv.Atoi(parsed.Port())
	}

	return parsed.Hostname(), port, useSSL, nil
}
