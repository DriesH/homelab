package updates

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"homelab/internal/agent"
)

type Schedule struct {
	Enabled bool `json:"enabled"`
	// Weekday is 0 for Sunday, like time.Weekday.
	Weekday int `json:"weekday"`
	Hour    int `json:"hour"`
	Minute  int `json:"minute"`
}

type TelegramSettings struct {
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
}

type Settings struct {
	Schedule Schedule `json:"schedule"`
	// Excluded containers are never updated by the schedule.
	Excluded []int            `json:"excluded"`
	Telegram TelegramSettings `json:"telegram"`
}

var defaultSettings = Settings{
	Schedule: Schedule{Enabled: true, Weekday: int(time.Sunday), Hour: 4, Minute: 0},
	Excluded: []int{},
}

// Target is what we know about the updates for the host or one container.
type Target struct {
	VMID           int             `json:"vmid,omitempty"`
	Name           string          `json:"name"`
	CheckedAt      time.Time       `json:"checkedAt,omitzero"`
	Packages       []agent.Package `json:"packages"`
	Images         []agent.Image   `json:"images"`
	Unsupported    bool            `json:"unsupported,omitempty"`
	RebootRequired bool            `json:"rebootRequired,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// withLists makes empty lists [] instead of null in JSON.
func (t Target) withLists() Target {
	if t.Packages == nil {
		t.Packages = []agent.Package{}
	}
	if t.Images == nil {
		t.Images = []agent.Image{}
	}

	return t
}

func (t Target) updateCount() int {
	return len(t.Packages) + len(t.Images)
}

type RunKind string

const (
	RunCheck       RunKind = "check"
	RunHostUpdate  RunKind = "host-update"
	RunGuestUpdate RunKind = "guest-update"
)

type RunStatus string

const (
	RunSucceeded  RunStatus = "succeeded"
	RunFailed     RunStatus = "failed"
	RunRolledBack RunStatus = "rolled-back"
)

type Run struct {
	ID         string    `json:"id"`
	Kind       RunKind   `json:"kind"`
	Target     string    `json:"target"`
	VMID       int       `json:"vmid,omitempty"`
	Scheduled  bool      `json:"scheduled"`
	Status     RunStatus `json:"status"`
	Snapshot   string    `json:"snapshot,omitempty"`
	Message    string    `json:"message"`
	Log        string    `json:"log,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

const (
	maxHistory = 50
	maxRunLog  = 32 * 1024
)

type state struct {
	Settings      Settings       `json:"settings"`
	Host          Target         `json:"host"`
	Guests        map[int]Target `json:"guests"`
	History       []Run          `json:"history"`
	LastScheduled time.Time      `json:"lastScheduled,omitzero"`
}

func loadState(path string) (state, error) {
	loaded := state{Settings: defaultSettings, Guests: map[int]Target{}, History: []Run{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return loaded, nil
	}
	if err != nil {
		return state{}, err
	}

	if err := json.Unmarshal(data, &loaded); err != nil {
		return state{}, err
	}
	if loaded.Guests == nil {
		loaded.Guests = map[int]Target{}
	}

	return loaded, nil
}

// save writes the file with 0600, because it holds the Telegram bot token.
func (s state) save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temp, path)
}
