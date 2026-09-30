// Package settingsfile exports the manager settings to homelab.yaml and
// imports them again. Secrets, like tokens and API keys, never go in the file.
package settingsfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"homelab/internal/agent"
	"homelab/internal/health"
	"homelab/internal/jellyfin"
	"homelab/internal/selfupdate"
	"homelab/internal/tailscale"
	"homelab/internal/updates"
)

const (
	Version = 1
	MaxSize = 64 * 1024
)

var ErrInvalidFile = errors.New("invalid settings file")

var clockPattern = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)

// File is homelab.yaml. A section that is left out stays as it is.
type File struct {
	Version       int            `yaml:"version"`
	Notifications *Notifications `yaml:"notifications,omitempty"`
	Updates       *Updates       `yaml:"updates,omitempty"`
	Backups       *Backups       `yaml:"backups,omitempty"`
	Health        *Health        `yaml:"health,omitempty"`
	Tailscale     *Tailscale     `yaml:"tailscale,omitempty"`
	Jellyfin      *Jellyfin      `yaml:"jellyfin,omitempty"`
	SelfUpdate    *SelfUpdate    `yaml:"selfUpdate,omitempty"`
}

type Notifications struct {
	Telegram Telegram `yaml:"telegram"`
}

// Telegram is off when ChatID is empty. The bot token stays on the manager.
type Telegram struct {
	ChatID string `yaml:"chatId"`
}

type Updates struct {
	Enabled bool `yaml:"enabled"`
	// Day is mon to sun.
	Day     string `yaml:"day"`
	Time    string `yaml:"time"`
	Exclude []int  `yaml:"exclude"`
}

type Backups struct {
	Enabled bool `yaml:"enabled"`
	// Days are mon to sun. No days means every day.
	Days    []string `yaml:"days"`
	Time    string   `yaml:"time"`
	Storage string   `yaml:"storage"`
	Exclude []int    `yaml:"exclude"`
	Keep    Keep     `yaml:"keep"`
}

type Keep struct {
	Daily   int `yaml:"daily"`
	Weekly  int `yaml:"weekly"`
	Monthly int `yaml:"monthly"`
}

type Health struct {
	Checks []Check `yaml:"checks"`
}

type Check struct {
	Name string `yaml:"name"`
	// Type is http or tcp.
	Type   string `yaml:"type"`
	Target string `yaml:"target"`
}

type Tailscale struct {
	// Serve is left out when Tailscale is not connected.
	Serve       *bool  `yaml:"serve,omitempty"`
	ShareSubnet bool   `yaml:"shareSubnet"`
	Subnet      string `yaml:"subnet,omitempty"`
}

type Jellyfin struct {
	URL   string `yaml:"url"`
	Theme *bool  `yaml:"theme,omitempty"`
}

type SelfUpdate struct {
	Repo        string `yaml:"repo"`
	AutoInstall bool   `yaml:"autoInstall"`
}

// Parse reads a file strictly: unknown keys are an error, so a typo is not ignored.
func Parse(data []byte) (File, error) {
	if len(data) > MaxSize {
		return File{}, fmt.Errorf("%w: the file is larger than %d KB", ErrInvalidFile, MaxSize/1024)
	}

	var file File
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		if errors.Is(err, io.EOF) {
			return File{}, fmt.Errorf("%w: the file is empty", ErrInvalidFile)
		}
		return File{}, fmt.Errorf("%w: %s", ErrInvalidFile, yamlError(err))
	}
	if file.Version != Version {
		return File{}, fmt.Errorf("%w: version must be %d", ErrInvalidFile, Version)
	}
	if err := file.Validate(); err != nil {
		return File{}, err
	}

	return file, nil
}

var (
	unknownField = regexp.MustCompile(`field (\S+) not found in type \S+`)
	wrongType    = regexp.MustCompile("cannot unmarshal !!\\w+(?: (`[^`]*`))? into \\S+")
)

// yamlError keeps the line numbers but leaves out Go type names.
func yamlError(err error) string {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return strings.TrimPrefix(err.Error(), "yaml: ")
	}

	messages := make([]string, 0, len(typeErr.Errors))
	for _, message := range typeErr.Errors {
		message = unknownField.ReplaceAllString(message, "unknown key $1")
		message = wrongType.ReplaceAllStringFunc(message, func(match string) string {
			if value := wrongType.FindStringSubmatch(match)[1]; value != "" {
				return "the value " + value + " has the wrong type"
			}
			return "a value has the wrong type"
		})
		messages = append(messages, message)
	}

	return strings.Join(messages, ", ")
}

func (f File) Validate() error {
	invalid := func(section string, err error) error {
		return fmt.Errorf("%w: %s: %v", ErrInvalidFile, section, err)
	}

	if f.Notifications != nil && f.Notifications.Telegram.ChatID != "" && !updates.ValidChatID(f.Notifications.Telegram.ChatID) {
		return invalid("notifications", errors.New("the Telegram chat ID must be a number or @channel"))
	}
	if f.Updates != nil {
		if _, err := f.Updates.schedule(); err != nil {
			return invalid("updates", err)
		}
		if err := validVMIDs(f.Updates.Exclude); err != nil {
			return invalid("updates", err)
		}
	}
	if f.Backups != nil {
		job, err := f.Backups.job()
		if err == nil {
			err = job.Validate()
		}
		if err != nil {
			return invalid("backups", err)
		}
	}
	if f.Health != nil {
		if len(f.Health.Checks) > health.MaxChecks {
			return invalid("health", fmt.Errorf("you can add up to %d checks", health.MaxChecks))
		}
		for _, check := range f.Health.Checks {
			if err := check.input().Validate(); err != nil {
				return invalid("health", fmt.Errorf("check %q: %v", check.Name, err))
			}
		}
	}
	if f.Tailscale != nil {
		if err := f.Tailscale.settings().Validate(); err != nil {
			return invalid("tailscale", err)
		}
	}
	if f.Jellyfin != nil {
		if err := jellyfin.ValidateURL(f.Jellyfin.URL); err != nil {
			return invalid("jellyfin", err)
		}
	}
	if f.SelfUpdate != nil && !selfupdate.ValidRepo(f.SelfUpdate.Repo) {
		return invalid("selfUpdate", errors.New("the repo must look like owner/name"))
	}

	return nil
}

func validVMIDs(vmids []int) error {
	for _, vmid := range vmids {
		if vmid < 100 || vmid > 999999999 {
			return fmt.Errorf("invalid guest %d", vmid)
		}
	}

	return nil
}

// Marshal writes the file with a short header. Notes explain what was left out.
func Marshal(file File, notes []string) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("# Homelab settings. Import this file on the Settings page.\n")
	buffer.WriteString("# Secrets, like tokens and API keys, are not in this file.\n")
	for _, note := range notes {
		buffer.WriteString("# " + note + "\n")
	}

	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(file); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func parseClock(value string) (hour, minute int, err error) {
	match := clockPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, 0, fmt.Errorf("the time %q must look like 03:00", value)
	}
	hour, _ = strconv.Atoi(match[1])
	minute, _ = strconv.Atoi(match[2])
	if hour > 23 || minute > 59 {
		return 0, 0, fmt.Errorf("the time %q is out of range", value)
	}

	return hour, minute, nil
}

func formatClock(hour, minute int) string {
	return fmt.Sprintf("%02d:%02d", hour, minute)
}

// updates.Schedule counts days from Sunday, like time.Weekday.
var weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func (u Updates) schedule() (updates.Schedule, error) {
	weekday := slices.Index(weekdays, u.Day)
	if weekday < 0 {
		return updates.Schedule{}, fmt.Errorf("the day %q must be one of mon, tue, wed, thu, fri, sat or sun", u.Day)
	}
	hour, minute, err := parseClock(u.Time)
	if err != nil {
		return updates.Schedule{}, err
	}

	return updates.Schedule{Enabled: u.Enabled, Weekday: weekday, Hour: hour, Minute: minute}, nil
}

func (b Backups) job() (agent.BackupJob, error) {
	hour, minute, err := parseClock(b.Time)
	if err != nil {
		return agent.BackupJob{}, err
	}

	return agent.BackupJob{
		Enabled:     b.Enabled,
		Days:        nonNil(b.Days),
		Hour:        hour,
		Minute:      minute,
		Storage:     b.Storage,
		Exclude:     nonNil(b.Exclude),
		KeepDaily:   b.Keep.Daily,
		KeepWeekly:  b.Keep.Weekly,
		KeepMonthly: b.Keep.Monthly,
	}, nil
}

func (c Check) input() health.CheckInput {
	return health.CheckInput{
		Name:   strings.TrimSpace(c.Name),
		Kind:   health.CheckKind(c.Type),
		Target: strings.TrimSpace(c.Target),
	}
}

func (t Tailscale) settings() tailscale.Settings {
	return tailscale.Settings{ShareSubnet: t.ShareSubnet, Subnet: strings.TrimSpace(t.Subnet)}
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}

	return values
}
