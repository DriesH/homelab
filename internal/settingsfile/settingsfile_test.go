package settingsfile

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"homelab/internal/agent"
	"homelab/internal/backups"
	"homelab/internal/health"
	"homelab/internal/jellyfin"
	"homelab/internal/selfupdate"
	"homelab/internal/tailscale"
	"homelab/internal/updates"
)

type fakeUpdates struct {
	settings updates.Settings
	saved    []updates.SettingsInput
}

func (f *fakeUpdates) Settings() updates.Settings { return f.settings }
func (f *fakeUpdates) SaveSettings(input updates.SettingsInput) error {
	f.saved = append(f.saved, input)
	return nil
}

type fakeHealth struct {
	checks []health.Check
	set    [][]health.CheckInput
}

func (f *fakeHealth) Checks() []health.Check { return f.checks }
func (f *fakeHealth) SetChecks(inputs []health.CheckInput) error {
	f.set = append(f.set, inputs)
	return nil
}

type fakeBackups struct {
	view  backups.View
	err   error
	saved []agent.BackupJob
}

func (f *fakeBackups) Status(context.Context) (backups.View, error) { return f.view, f.err }
func (f *fakeBackups) SaveJob(_ context.Context, job agent.BackupJob) error {
	f.saved = append(f.saved, job)
	return nil
}

type fakeTailscale struct {
	view  tailscale.View
	saved []tailscale.Settings
	serve []bool
}

func (f *fakeTailscale) Status(context.Context) tailscale.View { return f.view }
func (f *fakeTailscale) SaveSettings(_ context.Context, settings tailscale.Settings) error {
	f.saved = append(f.saved, settings)
	return nil
}
func (f *fakeTailscale) SetServe(_ context.Context, enabled bool) error {
	f.serve = append(f.serve, enabled)
	return nil
}

type fakeJellyfin struct {
	settings jellyfin.Settings
	theme    bool
	themeErr error
	saved    []jellyfin.Settings
	themes   []bool
}

func (f *fakeJellyfin) Settings() jellyfin.Settings { return f.settings }
func (f *fakeJellyfin) ThemeEnabled(context.Context) (bool, error) {
	return f.theme, f.themeErr
}
func (f *fakeJellyfin) SaveSettings(_ context.Context, input jellyfin.Settings) error {
	f.saved = append(f.saved, input)
	return nil
}
func (f *fakeJellyfin) SetTheme(_ context.Context, enabled bool) error {
	f.themes = append(f.themes, enabled)
	return nil
}

type fakeSelfUpdate struct {
	settings selfupdate.Settings
	saved    []selfupdate.SettingsInput
}

func (f *fakeSelfUpdate) Settings() selfupdate.Settings { return f.settings }
func (f *fakeSelfUpdate) SaveSettings(input selfupdate.SettingsInput) error {
	f.saved = append(f.saved, input)
	return nil
}

type fakes struct {
	updates    *fakeUpdates
	health     *fakeHealth
	backups    *fakeBackups
	tailscale  *fakeTailscale
	jellyfin   *fakeJellyfin
	selfUpdate *fakeSelfUpdate
}

func newService() (*Service, fakes) {
	f := fakes{
		updates: &fakeUpdates{settings: updates.Settings{
			Schedule: updates.Schedule{Enabled: true, Weekday: 0, Hour: 4},
			Excluded: []int{105},
			Telegram: updates.TelegramSettings{BotToken: "123:secret-token-abcdefghijklmnop", ChatID: "42"},
		}},
		health: &fakeHealth{checks: []health.Check{
			{ID: "a", Name: "Jellyfin", Kind: health.HTTPCheck, Target: "http://192.168.1.20:8096"},
		}},
		backups: &fakeBackups{view: backups.View{Job: backups.JobView{
			BackupJob: agent.BackupJob{
				Enabled: true, Days: []string{}, Hour: 3, Storage: "local", Exclude: []int{},
				KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 3,
			},
			Exists: true,
		}}},
		tailscale: &fakeTailscale{view: tailscale.View{
			State: "Running", Serving: true,
			Settings: tailscale.Settings{ShareSubnet: true, Subnet: "192.168.1.0/24"},
		}},
		jellyfin:   &fakeJellyfin{settings: jellyfin.Settings{URL: "http://192.168.1.20:8096", APIKey: "key"}, theme: true},
		selfUpdate: &fakeSelfUpdate{settings: selfupdate.Settings{Repo: "DriesH/homelab", Token: "ghp_secret"}},
	}

	return &Service{
		Updates: f.updates, Health: f.health, Backups: f.backups,
		Tailscale: f.tailscale, Jellyfin: f.jellyfin, SelfUpdate: f.selfUpdate,
	}, f
}

func statuses(result Result) map[string]Status {
	found := map[string]Status{}
	for _, change := range result.Changes {
		found[change.Section] = change.Status
	}

	return found
}

func TestExportLeavesSecretsOutAndImportsUnchanged(t *testing.T) {
	service, _ := newService()

	data, err := service.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"secret-token", "ghp_secret", "apiKey", "key\n"} {
		if strings.Contains(text, secret) {
			t.Fatalf("export has a secret %q:\n%s", secret, text)
		}
	}
	for _, want := range []string{"day: sun", `time: "04:00"`, "chatId: \"42\"", "serve: true", "theme: true", "repo: DriesH/homelab"} {
		if !strings.Contains(text, want) {
			t.Fatalf("export misses %q:\n%s", want, text)
		}
	}

	result, err := service.Import(context.Background(), data, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 7 {
		t.Fatalf("changes = %+v", result.Changes)
	}
	for section, status := range statuses(result) {
		if status != StatusUnchanged {
			t.Fatalf("%s = %s, want unchanged", section, status)
		}
	}
}

func TestExportNotes(t *testing.T) {
	service, f := newService()
	f.backups.view.Job.Custom = "*/2:00"
	f.jellyfin.themeErr = errors.New("timeout")
	f.selfUpdate.settings.Repo = ""

	data, err := service.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "\nbackups:") || !strings.Contains(text, `# backups: left out, because the schedule "*/2:00"`) {
		t.Fatalf("custom schedule not left out:\n%s", text)
	}
	if strings.Contains(text, "theme:") || !strings.Contains(text, "# jellyfin: theme left out") {
		t.Fatalf("theme not left out:\n%s", text)
	}
	if strings.Contains(text, "\nselfUpdate:") {
		t.Fatalf("empty repo not left out:\n%s", text)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("the export can't be imported: %v", err)
	}
}

func TestPreviewChangesNothing(t *testing.T) {
	service, f := newService()

	result, err := service.Import(context.Background(), []byte(`
version: 1
health:
  checks:
    - {name: Radarr, type: http, target: "http://192.168.1.30:7878"}
updates: {enabled: false, day: mon, time: "5:30", exclude: []}
`), false)
	if err != nil {
		t.Fatal(err)
	}

	got := statuses(result)
	if got["health"] != StatusChanged || got["updates"] != StatusChanged || len(got) != 2 {
		t.Fatalf("statuses = %v", got)
	}
	if len(f.health.set) != 0 || len(f.updates.saved) != 0 {
		t.Fatal("a preview saved settings")
	}
}

func TestImportApplies(t *testing.T) {
	service, f := newService()

	result, err := service.Import(context.Background(), []byte(`
version: 1
notifications: {telegram: {chatId: "-1001"}}
updates: {enabled: false, day: mon, time: "5:30", exclude: [110, 105, 110]}
backups:
  enabled: true
  days: [thu, mon]
  time: "02:30"
  storage: nas
  exclude: [105]
  keep: {daily: 3, weekly: 0, monthly: 1}
health:
  checks:
    - {name: " Jellyfin ", type: http, target: "http://192.168.1.20:8096"}
    - {name: SSH, type: tcp, target: "192.168.1.2:22"}
tailscale: {serve: false, shareSubnet: false}
jellyfin: {url: "http://192.168.1.21:8096", theme: false}
selfUpdate: {repo: someone/fork, autoInstall: true}
`), true)
	if err != nil {
		t.Fatal(err)
	}

	for section, status := range statuses(result) {
		if status != StatusApplied {
			t.Fatalf("%s = %s", section, status)
		}
	}

	if len(f.updates.saved) != 1 {
		t.Fatalf("update settings saved %d times", len(f.updates.saved))
	}
	saved := f.updates.saved[0]
	if saved.Schedule != (updates.Schedule{Weekday: 1, Hour: 5, Minute: 30}) || len(saved.Excluded) != 2 {
		t.Fatalf("updates = %+v", saved)
	}
	// An empty token keeps the saved one.
	if saved.Telegram != (updates.TelegramSettings{ChatID: "-1001"}) {
		t.Fatalf("telegram = %+v", saved.Telegram)
	}

	job := f.backups.saved[0]
	if job.Storage != "nas" || job.Hour != 2 || job.Minute != 30 || job.KeepDaily != 3 || len(job.Days) != 2 {
		t.Fatalf("job = %+v", job)
	}
	if checks := f.health.set[0]; len(checks) != 2 || checks[0].Name != "Jellyfin" {
		t.Fatalf("checks = %+v", checks)
	}
	if len(f.tailscale.saved) != 1 || f.tailscale.saved[0].ShareSubnet || len(f.tailscale.serve) != 1 || f.tailscale.serve[0] {
		t.Fatalf("tailscale saved %+v, serve %v", f.tailscale.saved, f.tailscale.serve)
	}
	if f.jellyfin.saved[0] != (jellyfin.Settings{URL: "http://192.168.1.21:8096"}) || f.jellyfin.themes[0] {
		t.Fatalf("jellyfin saved %+v, themes %v", f.jellyfin.saved, f.jellyfin.themes)
	}
	if f.selfUpdate.saved[0] != (selfupdate.SettingsInput{Repo: "someone/fork", AutoInstall: true}) {
		t.Fatalf("self-update = %+v", f.selfUpdate.saved[0])
	}
}

func TestImportSkipsWhatNeedsSecretsOrTailscale(t *testing.T) {
	service, f := newService()
	f.updates.settings.Telegram = updates.TelegramSettings{}
	f.jellyfin.settings = jellyfin.Settings{}
	f.tailscale.view = tailscale.View{State: "NeedsLogin"}

	result, err := service.Import(context.Background(), []byte(`
version: 1
notifications: {telegram: {chatId: "42"}}
tailscale: {serve: true, shareSubnet: false}
jellyfin: {url: "http://192.168.1.20:8096"}
`), true)
	if err != nil {
		t.Fatal(err)
	}

	got := statuses(result)
	for _, section := range []string{"notifications", "tailscale", "jellyfin"} {
		if got[section] != StatusSkipped {
			t.Fatalf("%s = %s", section, got[section])
		}
	}
	if len(f.updates.saved)+len(f.tailscale.serve)+len(f.jellyfin.saved) != 0 {
		t.Fatal("a skipped section was saved")
	}
}

func TestParseRejectsBadFiles(t *testing.T) {
	for name, file := range map[string]string{
		"empty":         "",
		"no version":    "health: {checks: []}",
		"wrong version": "version: 2",
		"unknown key":   "version: 1\nhealth: {chekcs: []}",
		"bad day":       "version: 1\nupdates: {enabled: true, day: funday, time: \"04:00\"}",
		"bad time":      "version: 1\nbackups: {enabled: true, time: \"25:00\", storage: local}",
		"bad storage":   "version: 1\nbackups: {enabled: true, time: \"03:00\", storage: \"a b\"}",
		"bad check":     "version: 1\nhealth: {checks: [{name: x, type: ping, target: y}]}",
		"bad subnet":    "version: 1\ntailscale: {shareSubnet: true, subnet: 192.168.1.1/24}",
		"bad url":       "version: 1\njellyfin: {url: ftp://nas}",
		"bad repo":      "version: 1\nselfUpdate: {repo: nope}",
		"bad chat":      "version: 1\nnotifications: {telegram: {chatId: hello}}",
		"bad guest":     "version: 1\nupdates: {enabled: true, day: sun, time: \"04:00\", exclude: [5]}",
		"too large":     "version: 1\n#" + strings.Repeat("x", MaxSize),
	} {
		if _, err := Parse([]byte(file)); !errors.Is(err, ErrInvalidFile) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParseErrorsAreReadable(t *testing.T) {
	for file, want := range map[string]string{
		"version: 1\nhealth: {chekcs: []}":                     "invalid settings file: line 2: unknown key chekcs",
		"version: 1\nupdates: {enabled: yes please}":           "invalid settings file: line 2: the value `yes please` has the wrong type",
		"version: 1\nbackups: {keep: {daily: many}, time: x}":  "invalid settings file: line 2: the value `many` has the wrong type",
		"version: 1\nhealth: {checks: [{name: a, type: [x]}]}": "invalid settings file: line 2: a value has the wrong type",
		"version: 1\n  bad: indent":                            "invalid settings file: line 2: mapping values are not allowed in this context",
	} {
		_, err := Parse([]byte(file))
		if err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", file, err, want)
		}
	}
}

func TestReadmeExampleParses(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, _ := strings.Cut(string(readme), "## Settings file")
	_, example, _ := strings.Cut(section, "```yaml\n")
	example, _, _ = strings.Cut(example, "```")

	file, err := Parse([]byte(example))
	if err != nil {
		t.Fatal(err)
	}
	if file.Health == nil || len(file.Health.Checks) != 2 || file.SelfUpdate == nil {
		t.Fatalf("file = %+v", file)
	}
}
