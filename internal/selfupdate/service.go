// Package selfupdate checks GitHub Releases for a new version of the manager
// and hands the signed bundle to the host agent, which installs it.
package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"homelab/internal/agent"
	"homelab/internal/release"
)

// DefaultRepo is set at build time from the git remote, like "owner/homelab".
var DefaultRepo string

const (
	checkInterval = 6 * time.Hour
	apiVersion    = "2026-03-10"
	maxSignature  = 4096
)

var (
	ErrInvalidSettings = errors.New("invalid settings")
	ErrNoUpdate        = errors.New("there is no newer version to install")
	ErrBusy            = errors.New("an update is already being installed")
)

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

type Agent interface {
	StartUpgrade(ctx context.Context, bundle io.Reader, signature release.Signature) error
	UpgradeStatus(ctx context.Context) (agent.UpgradeStatus, error)
}

type Options struct {
	DataDir string
	// Version is the running version.
	Version string
	Agent   Agent
	Notify  func(ctx context.Context, text string)
	Logger  *slog.Logger
	// APIURL and HTTP are replaced in tests.
	APIURL string
	HTTP   *http.Client
}

type Settings struct {
	Repo string `json:"repo"`
	// Token is only needed for a private repo: a fine-grained token with read access to its contents.
	Token       string `json:"token"`
	AutoInstall bool   `json:"autoInstall"`
}

type SettingsInput struct {
	Repo        string `json:"repo"`
	Token       string `json:"token"`
	ClearToken  bool   `json:"clearToken"`
	AutoInstall bool   `json:"autoInstall"`
}

type Release struct {
	Version      string    `json:"version"`
	Notes        string    `json:"notes"`
	URL          string    `json:"url"`
	PublishedAt  time.Time `json:"publishedAt"`
	bundleURL    string
	signatureURL string
}

type View struct {
	Version         string               `json:"version"`
	Repo            string               `json:"repo"`
	TokenSet        bool                 `json:"tokenSet"`
	AutoInstall     bool                 `json:"autoInstall"`
	Latest          *Release             `json:"latest"`
	UpdateAvailable bool                 `json:"updateAvailable"`
	CheckedAt       time.Time            `json:"checkedAt,omitzero"`
	Error           string               `json:"error,omitempty"`
	Installing      bool                 `json:"installing"`
	Upgrade         *agent.UpgradeStatus `json:"upgrade"`
}

type state struct {
	Settings Settings `json:"settings"`
	// Notified is the last version we sent a message about.
	Notified string `json:"notified"`
}

type Service struct {
	Options
	path string

	mu         sync.Mutex
	state      state
	latest     *Release
	checkedAt  time.Time
	checkErr   string
	installing bool
}

func New(options Options) (*Service, error) {
	if options.APIURL == "" {
		options.APIURL = "https://api.github.com"
	}
	if options.HTTP == nil {
		options.HTTP = &http.Client{Timeout: 5 * time.Minute}
	}
	// Downloads redirect to a signed URL that needs no token, so never send it there.
	options.HTTP.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		request.Header.Del("Authorization")
		return nil
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Notify == nil {
		options.Notify = func(context.Context, string) {}
	}

	service := &Service{Options: options, path: filepath.Join(options.DataDir, "self-update.json")}
	service.state.Settings.Repo = DefaultRepo

	data, err := os.ReadFile(service.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &service.state); err != nil {
			return nil, err
		}
	}

	return service, nil
}

// Run checks for a new version now and every 6 hours. It blocks until ctx ends.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		s.scheduledCheck(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) scheduledCheck(ctx context.Context) {
	if err := s.Check(ctx); err != nil {
		s.Logger.Warn("could not check for a new version", "error", err)
		return
	}

	s.mu.Lock()
	latest, available := s.latest, s.updateAvailable()
	autoInstall := s.state.Settings.AutoInstall
	notify := available && latest.Version != s.state.Notified
	if notify {
		s.state.Notified = latest.Version
		s.saveLocked()
	}
	s.mu.Unlock()

	if !available {
		return
	}

	if autoInstall {
		s.Notify(ctx, fmt.Sprintf("⬆️ Installing Homelab %s", latest.Version))
		if err := s.Install(ctx); err != nil {
			s.Notify(ctx, fmt.Sprintf("❌ Homelab %s was not installed: %v", latest.Version, err))
		}
		return
	}

	if notify {
		s.Notify(ctx, fmt.Sprintf("🆕 Homelab %s is available. Install it on the Updates page.", latest.Version))
	}
}

// Check asks GitHub for the latest release.
func (s *Service) Check(ctx context.Context) error {
	s.mu.Lock()
	settings := s.state.Settings
	s.mu.Unlock()

	latest, err := s.fetchLatest(ctx, settings)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.checkedAt = time.Now()
	s.checkErr = ""
	if err != nil {
		s.checkErr = err.Error()
		return err
	}
	s.latest = latest

	return nil
}

func (s *Service) fetchLatest(ctx context.Context, settings Settings) (*Release, error) {
	if settings.Repo == "" {
		return nil, errors.New("no GitHub repo set")
	}

	response, err := s.get(ctx, settings, fmt.Sprintf("%s/repos/%s/releases/latest", s.APIURL, settings.Repo), "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	var body struct {
		TagName     string    `json:"tag_name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("GitHub sent an unexpected answer: %w", err)
	}

	latest := &Release{Version: body.TagName, Notes: body.Body, URL: body.HTMLURL, PublishedAt: body.PublishedAt}
	bundleName := fmt.Sprintf("homelab-%s-linux-amd64.tar.gz", body.TagName)
	for _, asset := range body.Assets {
		switch asset.Name {
		case bundleName:
			latest.bundleURL = asset.URL
		case bundleName + ".sig":
			latest.signatureURL = asset.URL
		}
	}

	return latest, nil
}

// get calls the GitHub API with the token, if there is one.
func (s *Service) get(ctx context.Context, settings Settings, url, accept string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	if settings.Token != "" {
		request.Header.Set("Authorization", "Bearer "+settings.Token)
	}

	response, err := s.HTTP.Do(request)
	if err != nil {
		return nil, errors.New("could not reach GitHub")
	}

	switch response.StatusCode {
	case http.StatusOK:
		return response, nil
	case http.StatusNotFound:
		response.Body.Close()
		if settings.Token == "" {
			return nil, fmt.Errorf("no release found in %s. If the repo is private, add a token", settings.Repo)
		}
		return nil, fmt.Errorf("no release found in %s, or the token can't read it", settings.Repo)
	case http.StatusUnauthorized:
		response.Body.Close()
		return nil, errors.New("GitHub refused the token")
	default:
		response.Body.Close()
		return nil, fmt.Errorf("GitHub answered %s", response.Status)
	}
}

// Install downloads the latest bundle and hands it to the host agent. The
// agent checks the signature before it installs anything.
func (s *Service) Install(ctx context.Context) error {
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return ErrBusy
	}
	if !s.updateAvailable() {
		s.mu.Unlock()
		return ErrNoUpdate
	}
	latest, settings := *s.latest, s.state.Settings
	s.installing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.installing = false
		s.mu.Unlock()
	}()

	if latest.bundleURL == "" || latest.signatureURL == "" {
		return fmt.Errorf("release %s has no signed bundle", latest.Version)
	}

	signatureResponse, err := s.get(ctx, settings, latest.signatureURL, "application/octet-stream")
	if err != nil {
		return err
	}
	var signature release.Signature
	err = json.NewDecoder(io.LimitReader(signatureResponse.Body, maxSignature)).Decode(&signature)
	signatureResponse.Body.Close()
	if err != nil || signature.Version != latest.Version {
		return errors.New("the signature file of the release is not valid")
	}

	bundle, err := s.get(ctx, settings, latest.bundleURL, "application/octet-stream")
	if err != nil {
		return err
	}
	defer bundle.Body.Close()

	s.Logger.Info("installing new version", "version", latest.Version)

	return s.Agent.StartUpgrade(ctx, bundle.Body, signature)
}

// updateAvailable needs s.mu.
func (s *Service) updateAvailable() bool {
	return s.latest != nil && release.Newer(s.latest.Version, s.Version)
}

func (s *Service) Status(ctx context.Context) View {
	s.mu.Lock()
	view := View{
		Version:         s.Version,
		Repo:            s.state.Settings.Repo,
		TokenSet:        s.state.Settings.Token != "",
		AutoInstall:     s.state.Settings.AutoInstall,
		Latest:          s.latest,
		UpdateAvailable: s.updateAvailable(),
		CheckedAt:       s.checkedAt,
		Error:           s.checkErr,
		Installing:      s.installing,
	}
	s.mu.Unlock()

	if upgrade, err := s.Agent.UpgradeStatus(ctx); err == nil && upgrade.State != agent.UpgradeIdle {
		upgrade.Log = strings.TrimSpace(upgrade.Log)
		view.Upgrade = &upgrade
	}

	return view
}

func (s *Service) SaveSettings(input SettingsInput) error {
	input.Repo = strings.TrimSpace(input.Repo)
	input.Token = strings.TrimSpace(input.Token)
	if !repoPattern.MatchString(input.Repo) {
		return fmt.Errorf("%w: the repo must look like owner/name", ErrInvalidSettings)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	settings := Settings{Repo: input.Repo, Token: s.state.Settings.Token, AutoInstall: input.AutoInstall}
	switch {
	case input.ClearToken:
		settings.Token = ""
	case input.Token != "":
		settings.Token = input.Token
	}

	// A new repo or token makes the last check meaningless.
	if settings.Repo != s.state.Settings.Repo || settings.Token != s.state.Settings.Token {
		s.latest, s.checkErr, s.checkedAt = nil, "", time.Time{}
	}

	previous := s.state.Settings
	s.state.Settings = settings
	if err := s.saveLocked(); err != nil {
		s.state.Settings = previous
		return err
	}

	return nil
}

// saveLocked writes the state with 0600, because it holds the token. It needs s.mu.
func (s *Service) saveLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp")
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temp, s.path)
}
