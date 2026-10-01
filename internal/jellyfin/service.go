package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrNotConfigured   = errors.New("Jellyfin is not set up")
	ErrInvalidSettings = errors.New("invalid settings")
)

type Settings struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

// Service keeps the Jellyfin settings in the data dir (0600, it holds the API key).
type Service struct {
	path string

	mu       sync.Mutex
	settings Settings
}

func NewService(dataDir string) (*Service, error) {
	service := &Service{path: filepath.Join(dataDir, "jellyfin.json")}

	data, err := os.ReadFile(service.path)
	if errors.Is(err, os.ErrNotExist) {
		return service, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &service.settings); err != nil {
		return nil, err
	}

	return service, nil
}

func (s *Service) client() (*Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.settings.URL == "" || s.settings.APIKey == "" {
		return nil, ErrNotConfigured
	}

	return NewClient(s.settings.URL, s.settings.APIKey), nil
}

// SaveSettings checks the URL and key against Jellyfin before it saves them.
// An empty key keeps the saved one.
func (s *Service) SaveSettings(ctx context.Context, input Settings) error {
	if err := ValidateURL(input.URL); err != nil {
		return err
	}

	s.mu.Lock()
	if input.APIKey == "" {
		input.APIKey = s.settings.APIKey
	}
	s.mu.Unlock()

	if input.APIKey == "" {
		return fmt.Errorf("%w: an API key is required", ErrInvalidSettings)
	}
	if _, err := NewClient(input.URL, input.APIKey).Info(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSettings, err)
	}

	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, s.path); err != nil {
		return err
	}
	s.settings = input

	return nil
}

// Disconnect forgets the URL and the API key when they are for the Jellyfin at host,
// like after its container was removed. It reports whether it forgot them.
func (s *Service) Disconnect(host string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	parsed, err := url.Parse(s.settings.URL)
	if err != nil || host == "" || parsed.Hostname() != host {
		return false, nil
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	s.settings = Settings{}

	return true, nil
}

type NowPlaying struct {
	User       string `json:"user"`
	Device     string `json:"device"`
	ItemID     string `json:"itemId"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle"`
	Progress   int    `json:"progress"`
	Paused     bool   `json:"paused"`
	Transcode  bool   `json:"transcode"`
	PlayMethod string `json:"playMethod"`
	// Reasons explain why Jellyfin transcodes, for example "ContainerNotSupported".
	Reasons []string `json:"reasons"`
}

type RecentItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Year int    `json:"year,omitempty"`
}

type View struct {
	Configured   bool         `json:"configured"`
	URL          string       `json:"url"`
	Error        string       `json:"error,omitempty"`
	ServerName   string       `json:"serverName,omitempty"`
	Version      string       `json:"version,omitempty"`
	ThemeEnabled bool         `json:"themeEnabled"`
	Movies       int          `json:"movies"`
	Series       int          `json:"series"`
	Episodes     int          `json:"episodes"`
	NowPlaying   []NowPlaying `json:"nowPlaying"`
	Recent       []RecentItem `json:"recent"`
}

// Status collects everything for the dashboard. When Jellyfin is down, it
// returns what it knows with an error message instead of failing.
func (s *Service) Status(ctx context.Context) View {
	s.mu.Lock()
	view := View{URL: s.settings.URL, NowPlaying: []NowPlaying{}, Recent: []RecentItem{}}
	s.mu.Unlock()

	client, err := s.client()
	if err != nil {
		return view
	}
	view.Configured = true

	info, err := client.Info(ctx)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.ServerName, view.Version = info.ServerName, info.Version

	// The rest is best effort: one failing call should not hide the others.
	if enabled, err := client.ThemeEnabled(ctx); err == nil {
		view.ThemeEnabled = enabled
	}
	if counts, err := client.Counts(ctx); err == nil {
		view.Movies, view.Series, view.Episodes = counts.MovieCount, counts.SeriesCount, counts.EpisodeCount
	}
	if sessions, err := client.Playing(ctx); err == nil {
		for _, session := range sessions {
			view.NowPlaying = append(view.NowPlaying, nowPlaying(session))
		}
	}
	if items, err := client.RecentlyAdded(ctx, 12); err == nil {
		for _, item := range items {
			view.Recent = append(view.Recent, RecentItem{ID: item.ID, Name: item.Name, Type: item.Type, Year: item.ProductionYear})
		}
	}

	return view
}

func nowPlaying(session Session) NowPlaying {
	item := session.NowPlayingItem
	playing := NowPlaying{
		User:       session.UserName,
		Device:     session.DeviceName,
		ItemID:     item.ID,
		Title:      item.Name,
		Paused:     session.PlayState.IsPaused,
		PlayMethod: session.PlayState.PlayMethod,
		Reasons:    []string{},
	}

	if item.Type == "Episode" {
		// Show the series as the title, and the episode below it.
		playing.Title = item.SeriesName
		playing.Subtitle = fmt.Sprintf("S%d:E%d · %s", item.SeasonNumber, item.EpisodeNumber, item.Name)
		if item.SeriesID != "" {
			playing.ItemID = item.SeriesID
		}
	} else if item.ProductionYear > 0 {
		playing.Subtitle = fmt.Sprint(item.ProductionYear)
	}

	if item.RunTimeTicks > 0 {
		playing.Progress = int(session.PlayState.PositionTicks * 100 / item.RunTimeTicks)
	}

	if session.TranscodingInfo != nil && !(session.TranscodingInfo.IsVideoDirect && session.TranscodingInfo.IsAudioDirect) {
		playing.Transcode = true
		playing.Reasons = session.TranscodingInfo.TranscodeReasons
	}
	if playing.PlayMethod == "Transcode" {
		playing.Transcode = true
	}

	return playing
}

func ValidateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%w: the URL must look like http://192.168.1.20:8096", ErrInvalidSettings)
	}

	return nil
}

// Settings returns the saved settings, with the API key.
func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.settings
}

func (s *Service) ThemeEnabled(ctx context.Context) (bool, error) {
	client, err := s.client()
	if err != nil {
		return false, err
	}

	return client.ThemeEnabled(ctx)
}

func (s *Service) SetTheme(ctx context.Context, enabled bool) error {
	client, err := s.client()
	if err != nil {
		return err
	}

	return client.SetTheme(ctx, enabled)
}

func (s *Service) Image(ctx context.Context, itemID, imageType string, maxWidth int) (*http.Response, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}

	return client.Image(ctx, itemID, imageType, maxWidth)
}
