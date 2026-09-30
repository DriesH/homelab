package settingsfile

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"homelab/internal/agent"
	"homelab/internal/backups"
	"homelab/internal/health"
	"homelab/internal/jellyfin"
	"homelab/internal/selfupdate"
	"homelab/internal/tailscale"
	"homelab/internal/updates"
)

type UpdatesService interface {
	Settings() updates.Settings
	SaveSettings(input updates.SettingsInput) error
}

type HealthService interface {
	Checks() []health.Check
	SetChecks(inputs []health.CheckInput) error
}

type BackupsService interface {
	Status(ctx context.Context) (backups.View, error)
	SaveJob(ctx context.Context, job agent.BackupJob) error
}

type TailscaleService interface {
	Status(ctx context.Context) tailscale.View
	SaveSettings(ctx context.Context, settings tailscale.Settings) error
	SetServe(ctx context.Context, enabled bool) error
}

type JellyfinService interface {
	Settings() jellyfin.Settings
	ThemeEnabled(ctx context.Context) (bool, error)
	SaveSettings(ctx context.Context, input jellyfin.Settings) error
	SetTheme(ctx context.Context, enabled bool) error
}

type SelfUpdateService interface {
	Settings() selfupdate.Settings
	SaveSettings(input selfupdate.SettingsInput) error
}

type Service struct {
	Updates    UpdatesService
	Health     HealthService
	Backups    BackupsService
	Tailscale  TailscaleService
	Jellyfin   JellyfinService
	SelfUpdate SelfUpdateService

	// mu stops two imports from running at the same time.
	mu sync.Mutex
}

type Status string

const (
	StatusUnchanged Status = "unchanged"
	// StatusChanged is only used in a preview.
	StatusChanged Status = "changed"
	StatusApplied Status = "applied"
	StatusSkipped Status = "skipped"
	StatusFailed  Status = "failed"
)

// Change is the result for one section of the file.
type Change struct {
	Section string `json:"section"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

type Result struct {
	Applied bool     `json:"applied"`
	Changes []Change `json:"changes"`
}

func (s *Service) Export(ctx context.Context) ([]byte, error) {
	file, notes := s.current(ctx)

	return Marshal(file, notes)
}

// current reads the settings of every section. A section that can't be read
// is left out, with a note that says why.
func (s *Service) current(ctx context.Context) (File, []string) {
	file := File{Version: Version}
	notes := []string{}

	settings := s.Updates.Settings()
	file.Notifications = &Notifications{Telegram: Telegram{ChatID: settings.Telegram.ChatID}}
	file.Updates = &Updates{
		Enabled: settings.Schedule.Enabled,
		Day:     weekdays[settings.Schedule.Weekday],
		Time:    formatClock(settings.Schedule.Hour, settings.Schedule.Minute),
		Exclude: nonNil(settings.Excluded),
	}

	switch view, err := s.Backups.Status(ctx); {
	case err != nil:
		notes = append(notes, fmt.Sprintf("backups: left out, because the job could not be read (%v).", err))
	case !view.Job.Exists:
		notes = append(notes, "backups: left out, because there is no backup job yet.")
	case view.Job.Custom != "":
		notes = append(notes, fmt.Sprintf("backups: left out, because the schedule %q was changed in Proxmox.", view.Job.Custom))
	default:
		job := view.Job
		file.Backups = &Backups{
			Enabled: job.Enabled,
			Days:    nonNil(job.Days),
			Time:    formatClock(job.Hour, job.Minute),
			Storage: job.Storage,
			Exclude: nonNil(job.Exclude),
			Keep:    Keep{Daily: job.KeepDaily, Weekly: job.KeepWeekly, Monthly: job.KeepMonthly},
		}
	}

	file.Health = &Health{Checks: []Check{}}
	for _, check := range s.Health.Checks() {
		file.Health.Checks = append(file.Health.Checks, Check{Name: check.Name, Type: string(check.Kind), Target: check.Target})
	}

	view := s.Tailscale.Status(ctx)
	file.Tailscale = &Tailscale{ShareSubnet: view.Settings.ShareSubnet, Subnet: view.Settings.Subnet}
	if view.State == "Running" {
		file.Tailscale.Serve = &view.Serving
	}

	if url := s.Jellyfin.Settings().URL; url != "" {
		file.Jellyfin = &Jellyfin{URL: url}
		if enabled, err := s.Jellyfin.ThemeEnabled(ctx); err == nil {
			file.Jellyfin.Theme = &enabled
		} else {
			notes = append(notes, fmt.Sprintf("jellyfin: theme left out, because Jellyfin did not answer (%v).", err))
		}
	}

	if self := s.SelfUpdate.Settings(); self.Repo != "" {
		file.SelfUpdate = &SelfUpdate{Repo: self.Repo, AutoInstall: self.AutoInstall}
	} else {
		notes = append(notes, "selfUpdate: left out, because no GitHub repo is set.")
	}

	return file, notes
}

// Import compares the file with the current settings. With apply, it also
// saves the sections that changed. It checks the whole file first, so a
// mistake changes nothing.
func (s *Service) Import(ctx context.Context, data []byte, apply bool) (Result, error) {
	file, err := Parse(data)
	if err != nil {
		return Result{}, err
	}
	normalize(&file)

	s.mu.Lock()
	defer s.mu.Unlock()

	current, _ := s.current(ctx)
	normalize(&current)

	result := Result{Applied: apply, Changes: []Change{}}
	add := func(section string, status Status, message string) {
		result.Changes = append(result.Changes, Change{Section: section, Status: status, Message: message})
	}
	// finish reports a change: in a preview it is only "changed".
	finish := func(section string, err error, message string) {
		switch {
		case !apply:
			add(section, StatusChanged, message)
		case err != nil:
			add(section, StatusFailed, err.Error())
		default:
			add(section, StatusApplied, message)
		}
	}
	run := func(operation func() error) error {
		if !apply {
			return nil
		}
		return operation()
	}

	if file.SelfUpdate != nil {
		if reflect.DeepEqual(file.SelfUpdate, current.SelfUpdate) {
			add("selfUpdate", StatusUnchanged, "")
		} else {
			err := run(func() error {
				return s.SelfUpdate.SaveSettings(selfupdate.SettingsInput{Repo: file.SelfUpdate.Repo, AutoInstall: file.SelfUpdate.AutoInstall})
			})
			finish("selfUpdate", err, "")
		}
	}

	s.importUpdates(file, current, add, finish, run)

	if file.Health != nil {
		if reflect.DeepEqual(file.Health, current.Health) {
			add("health", StatusUnchanged, "")
		} else {
			inputs := make([]health.CheckInput, 0, len(file.Health.Checks))
			for _, check := range file.Health.Checks {
				inputs = append(inputs, check.input())
			}
			finish("health", run(func() error { return s.Health.SetChecks(inputs) }), "")
		}
	}

	if file.Backups != nil {
		if reflect.DeepEqual(file.Backups, current.Backups) {
			add("backups", StatusUnchanged, "")
		} else {
			job, _ := file.Backups.job()
			finish("backups", run(func() error { return s.Backups.SaveJob(ctx, job) }), "")
		}
	}

	s.importTailscale(ctx, file, current, add, finish, run)
	s.importJellyfin(ctx, file, current, add, finish, run)

	return result, nil
}

type (
	addFunc    func(section string, status Status, message string)
	finishFunc func(section string, err error, message string)
	runFunc    func(operation func() error) error
)

// importUpdates handles updates and notifications together, because both are
// saved as the update settings.
func (s *Service) importUpdates(file, current File, add addFunc, finish finishFunc, run runFunc) {
	settings := s.Updates.Settings()
	input := updates.SettingsInput{Schedule: settings.Schedule, Excluded: settings.Excluded, Telegram: settings.Telegram}
	changed := []string{}

	if file.Notifications != nil {
		chatID := file.Notifications.Telegram.ChatID
		switch {
		case reflect.DeepEqual(file.Notifications, current.Notifications):
			add("notifications", StatusUnchanged, "")
		case chatID != "" && settings.Telegram.BotToken == "":
			add("notifications", StatusSkipped, "Add the Telegram bot token on the Updates page first.")
		default:
			input.Telegram = updates.TelegramSettings{ChatID: chatID}
			changed = append(changed, "notifications")
		}
	}

	if file.Updates != nil {
		if reflect.DeepEqual(file.Updates, current.Updates) {
			add("updates", StatusUnchanged, "")
		} else {
			input.Schedule, _ = file.Updates.schedule()
			input.Excluded = file.Updates.Exclude
			changed = append(changed, "updates")
		}
	}

	if len(changed) == 0 {
		return
	}
	err := run(func() error { return s.Updates.SaveSettings(input) })
	for _, section := range changed {
		finish(section, err, "")
	}
}

func (s *Service) importTailscale(ctx context.Context, file, current File, add addFunc, finish finishFunc, run runFunc) {
	if file.Tailscale == nil {
		return
	}

	wanted := *file.Tailscale
	if wanted.Serve == nil {
		wanted.Serve = current.Tailscale.Serve
	}
	settingsChanged := wanted.settings() != current.Tailscale.settings()
	serveChanged := wanted.Serve != nil && (current.Tailscale.Serve == nil || *wanted.Serve != *current.Tailscale.Serve)
	// Serve can only change while Tailscale is connected.
	serveBlocked := serveChanged && current.Tailscale.Serve == nil
	message := ""
	if serveBlocked {
		message = "Serve is left as it is, because Tailscale is not connected."
	}

	switch {
	case !settingsChanged && !serveChanged:
		add("tailscale", StatusUnchanged, "")
	case !settingsChanged && serveBlocked:
		add("tailscale", StatusSkipped, message)
	default:
		err := run(func() error {
			if settingsChanged {
				if err := s.Tailscale.SaveSettings(ctx, wanted.settings()); err != nil {
					return err
				}
			}
			if serveChanged && !serveBlocked {
				return s.Tailscale.SetServe(ctx, *wanted.Serve)
			}
			return nil
		})
		finish("tailscale", err, message)
	}
}

func (s *Service) importJellyfin(ctx context.Context, file, current File, add addFunc, finish finishFunc, run runFunc) {
	if file.Jellyfin == nil {
		return
	}
	if s.Jellyfin.Settings().APIKey == "" {
		add("jellyfin", StatusSkipped, "Add the API key on the Jellyfin page first.")
		return
	}

	urlChanged := current.Jellyfin == nil || file.Jellyfin.URL != current.Jellyfin.URL
	themeChanged := file.Jellyfin.Theme != nil &&
		(current.Jellyfin == nil || current.Jellyfin.Theme == nil || *file.Jellyfin.Theme != *current.Jellyfin.Theme)
	if !urlChanged && !themeChanged {
		add("jellyfin", StatusUnchanged, "")
		return
	}

	err := run(func() error {
		if urlChanged {
			// An empty API key keeps the saved one.
			if err := s.Jellyfin.SaveSettings(ctx, jellyfin.Settings{URL: file.Jellyfin.URL}); err != nil {
				return err
			}
		}
		if themeChanged {
			return s.Jellyfin.SetTheme(ctx, *file.Jellyfin.Theme)
		}
		return nil
	})
	finish("jellyfin", err, "")
}

// normalize makes equal settings compare equal: no nil lists, and sorted days and guests.
func normalize(file *File) {
	if file.Updates != nil {
		file.Updates.Exclude = sortedUnique(file.Updates.Exclude)
	}
	if file.Backups != nil {
		days := []string{}
		for _, day := range agent.Weekdays {
			if slices.Contains(file.Backups.Days, day) {
				days = append(days, day)
			}
		}
		// All days is the same as no days.
		if len(days) == len(agent.Weekdays) {
			days = []string{}
		}
		file.Backups.Days = days
		file.Backups.Exclude = sortedUnique(file.Backups.Exclude)
		hour, minute, _ := parseClock(file.Backups.Time)
		file.Backups.Time = formatClock(hour, minute)
	}
	if file.Updates != nil {
		hour, minute, _ := parseClock(file.Updates.Time)
		file.Updates.Time = formatClock(hour, minute)
	}
	if file.Health != nil {
		checks := make([]Check, 0, len(file.Health.Checks))
		for _, check := range file.Health.Checks {
			input := check.input()
			checks = append(checks, Check{Name: input.Name, Type: string(input.Kind), Target: input.Target})
		}
		file.Health.Checks = checks
	}
	if file.Tailscale != nil {
		file.Tailscale.Subnet = file.Tailscale.settings().Subnet
		if !file.Tailscale.ShareSubnet {
			file.Tailscale.Subnet = ""
		}
	}
}

func sortedUnique(values []int) []int {
	values = slices.Clone(nonNil(values))
	slices.Sort(values)

	return slices.Compact(values)
}
