package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validAnswers() MediaStackAnswers {
	return MediaStackAnswers{
		NASServer:           "192.168.1.5",
		NASExport:           "/volume1/media",
		MoviesFolder:        "movies",
		SeriesFolder:        "TV Shows",
		WireGuardPrivateKey: "cGVyZmVjdGx5IHZhbGlkIGtleSBvZiAzMiBieXRlcyE=",
		VPNCountries:        "Netherlands,Switzerland",
		SubtitleLanguages:   "en,nl",
		Username:            "homelab",
		Password:            "correct horse battery",
		JellyfinAPIKey:      "0123456789abcdef0123456789abcdef",
		RestartJellyfin:     true,
		Storage:             "local-lvm",
		DownloadsSize:       200,
	}
}

func TestMediaStackAnswersValidate(t *testing.T) {
	if err := validAnswers().Validate(); err != nil {
		t.Fatal(err)
	}

	for name, change := range map[string]func(*MediaStackAnswers){
		"NAS with space":      func(a *MediaStackAnswers) { a.NASServer = "nas local" },
		"export not absolute": func(a *MediaStackAnswers) { a.NASExport = "volume1" },
		"export with newline": func(a *MediaStackAnswers) { a.NASExport = "/media\nARR_PASSWORD=x" },
		"short key":           func(a *MediaStackAnswers) { a.WireGuardPrivateKey = "abc=" },
		"folder with slash":   func(a *MediaStackAnswers) { a.MoviesFolder = "media/movies" },
		"folder up":           func(a *MediaStackAnswers) { a.SeriesFolder = ".." },
		"folder newline":      func(a *MediaStackAnswers) { a.MoviesFolder = "movies\nARR_PASSWORD=x" },
		"same folders":        func(a *MediaStackAnswers) { a.SeriesFolder = "Movies" },
		"empty folder":        func(a *MediaStackAnswers) { a.MoviesFolder = "" },
		"country with digits": func(a *MediaStackAnswers) { a.VPNCountries = "NL1" },
		"language too long":   func(a *MediaStackAnswers) { a.SubtitleLanguages = "eng" },
		"username with space": func(a *MediaStackAnswers) { a.Username = "my user" },
		"short password":      func(a *MediaStackAnswers) { a.Password = "short" },
		"password newline":    func(a *MediaStackAnswers) { a.Password = "correct horse\nbattery" },
		"bad api key":         func(a *MediaStackAnswers) { a.JellyfinAPIKey = "not-a-key" },
		"bad storage":         func(a *MediaStackAnswers) { a.Storage = "a;b" },
		"tiny downloads":      func(a *MediaStackAnswers) { a.DownloadsSize = 1 },
	} {
		answers := validAnswers()
		change(&answers)
		if err := answers.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	answers := validAnswers()
	answers.JellyfinAPIKey = ""
	if err := answers.Validate(); err != nil {
		t.Errorf("no Jellyfin key: %v", err)
	}
}

type fakeAppUnit struct {
	started []string
	running bool
}

func newTestInstaller(t *testing.T) (*AppInstaller, *fakeAppUnit) {
	t.Helper()

	stacks := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stacks, "arr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stacks, "arr", "install.sh"), nil, 0o755); err != nil {
		t.Fatal(err)
	}

	unit := &fakeAppUnit{}
	installer := &AppInstaller{
		Dir:       filepath.Join(t.TempDir(), "apps"),
		StacksDir: stacks,
		start: func(script, answersPath, logPath, exitPath string) error {
			unit.started = append(unit.started, script, answersPath)
			unit.running = true
			return nil
		},
		running: func() bool { return unit.running },
		now:     time.Now,
	}

	return installer, unit
}

func TestAppInstall(t *testing.T) {
	installer, unit := newTestInstaller(t)

	if status := installer.Status(); status.State != UpgradeIdle {
		t.Fatalf("state = %s", status.State)
	}
	if err := installer.Install("nextcloud", InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrUnknownApp) {
		t.Fatalf("unknown app: %v", err)
	}
	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: validAnswers()}); err != nil {
		t.Fatal(err)
	}

	answers, err := os.ReadFile(unit.started[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"NAS_EXPORT=/volume1/media\n", "SERIES_FOLDER=TV Shows\n", "ARR_PASSWORD=correct horse battery\n", "RESTART_JELLYFIN=y\n", "DOWNLOADS_SIZE=200\n"} {
		if !strings.Contains(string(answers), line) {
			t.Errorf("answers miss %q:\n%s", line, answers)
		}
	}
	if info, _ := os.Stat(unit.started[1]); info.Mode().Perm() != 0o600 {
		t.Errorf("answers mode = %v", info.Mode())
	}

	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrAppInstallRunning) {
		t.Fatalf("second install: %v", err)
	}

	os.WriteFile(installer.logPath(), []byte("\x1b[1;32m==>\x1b[0m Creating container 130\n"), 0o600)
	status := installer.Status()
	if status.State != UpgradeRunning || status.App != MediaStackApp || status.Log != "==> Creating container 130\n" {
		t.Fatalf("running status = %+v", status)
	}

	os.WriteFile(installer.logPath(), []byte("==> Done\nHOMELAB app media 130 192.168.1.50\n"), 0o600)
	os.WriteFile(installer.exitPath(), []byte("0\n"), 0o600)
	unit.running = false
	status = installer.Status()
	if status.State != UpgradeSucceeded || status.VMID != 130 || status.IP != "192.168.1.50" || status.FinishedAt.IsZero() {
		t.Fatalf("done status = %+v", status)
	}
}

func TestAppInstallFailures(t *testing.T) {
	installer, unit := newTestInstaller(t)

	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: validAnswers()}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(installer.exitPath(), []byte("1\n"), 0o600)
	unit.running = false
	if status := installer.Status(); status.State != UpgradeFailed || !strings.Contains(status.Message, "exit code 1") {
		t.Fatalf("status = %+v", status)
	}

	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: validAnswers()}); err != nil {
		t.Fatal(err)
	}
	unit.running = false
	if status := installer.Status(); status.State != UpgradeFailed || status.Message != "the install stopped before it finished" {
		t.Fatalf("status = %+v", status)
	}

	os.RemoveAll(installer.StacksDir)
	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrAppNotAvailable) {
		t.Fatalf("no installer: %v", err)
	}
}

func TestAppRetryUsesTheSavedAnswers(t *testing.T) {
	installer, unit := newTestInstaller(t)
	now := time.Now()
	installer.now = func() time.Time { return now }

	if err := installer.Retry(MediaStackApp); !errors.Is(err, ErrNoSavedAnswers) {
		t.Fatalf("retry without answers: %v", err)
	}
	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: validAnswers()}); err != nil {
		t.Fatal(err)
	}
	fail := func() {
		os.WriteFile(installer.exitPath(), []byte("1\n"), 0o600)
		unit.running = false
		installer.Status()
	}
	fail()

	saved, err := installer.Saved(MediaStackApp)
	if err != nil || saved == nil {
		t.Fatalf("saved = %v, err = %v", saved, err)
	}
	if saved.Answers.WireGuardPrivateKey != "" || saved.Answers.Password != "" || saved.Answers.JellyfinAPIKey != "" {
		t.Fatalf("the view has secrets: %+v", saved.Answers)
	}
	if saved.Answers.NASServer != "192.168.1.5" || !saved.HasJellyfinAPIKey || !saved.Until.Equal(now.Add(SavedAnswersTTL)) {
		t.Fatalf("saved = %+v", saved)
	}
	if info, _ := os.Stat(installer.savedPath(MediaStackApp)); info.Mode().Perm() != 0o600 {
		t.Fatalf("saved answers mode = %v", info.Mode())
	}

	// Try again: the same answers, secrets included.
	if err := installer.Retry(MediaStackApp); err != nil {
		t.Fatal(err)
	}
	answers, _ := os.ReadFile(unit.started[len(unit.started)-1])
	if !strings.Contains(string(answers), "ARR_PASSWORD=correct horse battery\n") {
		t.Fatalf("retry answers:\n%s", answers)
	}
	fail()

	// Change answers: empty secrets keep the saved ones.
	changed := saved.Answers
	changed.NASExport = "/volume1/Media"
	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: changed, KeepSecrets: true}); err != nil {
		t.Fatal(err)
	}
	answers, _ = os.ReadFile(unit.started[len(unit.started)-1])
	for _, line := range []string{"NAS_EXPORT=/volume1/Media\n", "ARR_PASSWORD=correct horse battery\n", "WIREGUARD_PRIVATE_KEY=" + validAnswers().WireGuardPrivateKey + "\n"} {
		if !strings.Contains(string(answers), line) {
			t.Fatalf("answers miss %q:\n%s", line, answers)
		}
	}

	// A working install removes the saved answers.
	os.WriteFile(installer.exitPath(), []byte("0\n"), 0o600)
	unit.running = false
	installer.Status()
	if saved, _ := installer.Saved(MediaStackApp); saved != nil {
		t.Fatalf("saved answers after success: %+v", saved)
	}
}

func TestSavedAnswersExpireAndCanBeForgotten(t *testing.T) {
	installer, unit := newTestInstaller(t)
	now := time.Now()
	installer.now = func() time.Time { return now }

	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: validAnswers()}); err != nil {
		t.Fatal(err)
	}
	unit.running = false

	if err := installer.Forget(MediaStackApp); err != nil {
		t.Fatal(err)
	}
	if saved, _ := installer.Saved(MediaStackApp); saved != nil {
		t.Fatal("forgotten answers are still there")
	}

	if err := installer.Install(MediaStackApp, InstallRequest{MediaStackAnswers: validAnswers()}); err != nil {
		t.Fatal(err)
	}
	unit.running = false
	now = now.Add(SavedAnswersTTL + time.Minute)
	if err := installer.Retry(MediaStackApp); !errors.Is(err, ErrNoSavedAnswers) {
		t.Fatalf("retry after 24 hours: %v", err)
	}
	if _, err := os.Stat(installer.savedPath(MediaStackApp)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old answers were not removed")
	}
	if err := installer.Install(MediaStackApp, InstallRequest{KeepSecrets: true, MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrNoSavedAnswers) {
		t.Fatalf("keep secrets without saved answers: %v", err)
	}
}
