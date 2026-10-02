// Package cloud keeps the cloud storage for the media: the storage, its
// recovery key and the rule for what moves to the cloud. The host agent does
// the work on the host.
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"homelab/internal/agent"
)

var (
	ErrNotSetUp        = errors.New("set up the cloud storage first")
	ErrNoKey           = errors.New("make a recovery key first")
	ErrKeyExists       = errors.New("the cloud storage has a recovery key already")
	ErrKeyNotConfirmed = errors.New("save the recovery key first, and type its last two groups")
	ErrWrongKeyEnd     = errors.New("those are not the last two groups of the recovery key")
	ErrEnabled         = errors.New("turn the cloud storage off first")
)

type Agent interface {
	CloudStatus(ctx context.Context) (agent.CloudStatus, error)
	TestCloud(ctx context.Context, config agent.CloudConfig) error
	EnableCloud(ctx context.Context, config agent.CloudConfig) error
	DisableCloud(ctx context.Context) error
}

type Options struct {
	DataDir string
	Agent   Agent
	Notify  func(ctx context.Context, text string)
	Logger  *slog.Logger
}

type state struct {
	Settings *Settings `json:"settings,omitempty"`
	// Key is the recovery key. Like everything in the data folder, it is in
	// the data backup.
	Key          string `json:"key,omitempty"`
	KeyConfirmed bool   `json:"keyConfirmed"`
	Rule         Rule   `json:"rule"`
}

type KeyState string

const (
	KeyNone        KeyState = "none"
	KeyUnconfirmed KeyState = "unconfirmed"
	KeyConfirmed   KeyState = "confirmed"
)

type View struct {
	Settings *SettingsView `json:"settings"`
	Label    string        `json:"label,omitempty"`
	Key      KeyState      `json:"key"`
	Rule     Rule          `json:"rule"`
	// Host is nil when the agent did not answer.
	Host      *agent.CloudStatus `json:"host"`
	HostError string             `json:"hostError,omitempty"`
}

type Service struct {
	Options
	path string

	mu    sync.Mutex
	state state
}

func New(options Options) (*Service, error) {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Notify == nil {
		options.Notify = func(context.Context, string) {}
	}

	service := &Service{
		Options: options,
		path:    filepath.Join(options.DataDir, "cloud.json"),
		state:   state{Rule: DefaultRule},
	}

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

func (s *Service) Status(ctx context.Context) View {
	host, err := s.Agent.CloudStatus(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	view := View{Key: s.keyState(), Rule: s.state.Rule}
	if settings := s.state.Settings; settings != nil {
		settingsView := settings.view()
		view.Settings = &settingsView
		if config, err := settings.config(); err == nil {
			view.Label = config.Label()
		}
	}
	if err != nil {
		s.Logger.Error("cloud status", "error", err)
		view.HostError = "the host agent did not answer"
	} else {
		view.Host = &host
	}

	return view
}

func (s *Service) keyState() KeyState {
	switch {
	case s.state.Key == "":
		return KeyNone
	case !s.state.KeyConfirmed:
		return KeyUnconfirmed
	}

	return KeyConfirmed
}

// SaveSettings keeps the storage. It can't change while the media is in it.
func (s *Service) SaveSettings(ctx context.Context, input Settings) error {
	if err := s.refuseWhenEnabled(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	settings := input.withSecretsFrom(s.state.Settings)
	if _, err := settings.config(); err != nil {
		return err
	}
	s.state.Settings = &settings

	return s.save()
}

// Test connects to the storage of the form, with the saved secrets when the
// form leaves them empty.
func (s *Service) Test(ctx context.Context, input Settings) error {
	s.mu.Lock()
	config, err := input.withSecretsFrom(s.state.Settings).config()
	s.mu.Unlock()
	if err != nil {
		return err
	}

	return s.Agent.TestCloud(ctx, config)
}

// CreateKey makes the recovery key and returns it. Until it is confirmed, it
// can be made again, for example when the first one got lost.
func (s *Service) CreateKey() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.KeyConfirmed {
		return "", ErrKeyExists
	}
	key, err := NewRecoveryKey()
	if err != nil {
		return "", err
	}
	s.state.Key = key.String()
	if err := s.save(); err != nil {
		return "", err
	}

	return s.state.Key, nil
}

// ConfirmKey checks that the user saved the key: they type its last two groups.
func (s *Service) ConfirmKey(typed string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, err := s.key()
	if err != nil {
		return err
	}
	if !key.endsWith(typed) {
		return ErrWrongKeyEnd
	}
	s.state.KeyConfirmed = true

	return s.save()
}

// RevealKey returns the key again. The server checks the password and the
// authenticator code first.
func (s *Service) RevealKey(ctx context.Context) (string, error) {
	s.mu.Lock()
	key := s.state.Key
	s.mu.Unlock()
	if key == "" {
		return "", ErrNoKey
	}

	s.Logger.Info("recovery key of the cloud storage shown")
	s.Notify(ctx, "🔑 Someone showed the recovery key of the cloud storage in Homelab.")

	return key, nil
}

func (s *Service) SaveRule(rule Rule) error {
	if err := rule.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Rule = rule

	return s.save()
}

// Enable starts to put the media folder on the cloud storage.
func (s *Service) Enable(ctx context.Context) error {
	s.mu.Lock()
	config, err := s.fullConfig()
	s.mu.Unlock()
	if err != nil {
		return err
	}

	return s.Agent.EnableCloud(ctx, config)
}

// Disable starts to bring all media back from the cloud.
func (s *Service) Disable(ctx context.Context) error {
	return s.Agent.DisableCloud(ctx)
}

// fullConfig is the storage with the recovery key. It needs s.mu.
func (s *Service) fullConfig() (agent.CloudConfig, error) {
	if s.state.Settings == nil {
		return agent.CloudConfig{}, ErrNotSetUp
	}
	key, err := s.key()
	if err != nil {
		return agent.CloudConfig{}, err
	}
	if !s.state.KeyConfirmed {
		return agent.CloudConfig{}, ErrKeyNotConfirmed
	}

	config, err := s.state.Settings.config()
	if err != nil {
		return config, err
	}
	config.CryptPassword, config.CryptSalt = key.CryptPasswords()

	return config, config.Validate()
}

// key parses the saved key. It needs s.mu.
func (s *Service) key() (RecoveryKey, error) {
	if s.state.Key == "" {
		return RecoveryKey{}, ErrNoKey
	}

	return ParseRecoveryKey(s.state.Key)
}

func (s *Service) refuseWhenEnabled(ctx context.Context) error {
	host, err := s.Agent.CloudStatus(ctx)
	if err != nil {
		return err
	}
	if host.Enabled || (host.Job != nil && host.Job.State == agent.UpgradeRunning) {
		return ErrEnabled
	}

	return nil
}

// save writes the state. It needs s.mu.
func (s *Service) save() error {
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
