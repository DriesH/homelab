package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"homelab/internal/stacks/arr"
)

const (
	// The install runs in its own unit, so it survives a restart of this agent.
	appUnit       = "homelab-app-install"
	maxAppLog     = 64 * 1024
	MediaStackApp = "media"
	// SavedAnswersTTL is how long the answers of a failed install stay on the
	// host, so you can try again without typing the secrets again.
	SavedAnswersTTL = 24 * time.Hour
)

var (
	ErrInvalidAnswers    = errors.New("invalid answers")
	ErrAppInstallRunning = errors.New("an app is already being installed")
	ErrUnknownApp        = errors.New("unknown app")
	ErrAppNotAvailable   = errors.New("the installer is not on the host yet, update Homelab first")
	ErrNoSavedAnswers    = errors.New("there are no saved answers, fill in the form again")
)

var (
	hostPattern         = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,252})$`)
	exportPattern       = regexp.MustCompile(`^/[A-Za-z0-9._/-]{0,255}$`)
	countriesPattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z ]{1,40}(,[A-Za-z][A-Za-z ]{1,40}){0,9}$`)
	languagesPattern    = regexp.MustCompile(`^[a-z]{2}(,[a-z]{2}){0,9}$`)
	usernamePattern     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	apiKeyPattern       = regexp.MustCompile(`^[a-f0-9]{32}$`)
	jellyfinUserPattern = regexp.MustCompile(`^[A-Za-z0-9._@-]([A-Za-z0-9 ._@-]{0,62}[A-Za-z0-9._@-])?$`)
	ansiPattern         = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	appResultPattern    = regexp.MustCompile(`(?m)^HOMELAB app (\w+) (\d+) (\S+)$`)
)

// MediaStackAnswers are the questions of stacks/arr/install.sh. Every value
// is checked, so no line breaks or shell code reach the answers file.
type MediaStackAnswers struct {
	NASServer string `json:"nasServer"`
	NASExport string `json:"nasExport"`
	// MoviesFolder and SeriesFolder are folders in the share. Each gets a Jellyfin library.
	MoviesFolder        string `json:"moviesFolder"`
	SeriesFolder        string `json:"seriesFolder"`
	WireGuardPrivateKey string `json:"wireguardPrivateKey"`
	VPNCountries        string `json:"vpnCountries"`
	SubtitleLanguages   string `json:"subtitleLanguages"`
	Username            string `json:"username"`
	Password            string `json:"password"`
	// JellyfinAPIKey is optional. With it, the installer adds the libraries to Jellyfin.
	JellyfinAPIKey string `json:"jellyfinApiKey"`
	// JellyfinAdminUsername and JellyfinAdminPassword are optional. With them,
	// the installer also does the setup of Seerr.
	JellyfinAdminUsername string `json:"jellyfinAdminUsername"`
	JellyfinAdminPassword string `json:"jellyfinAdminPassword"`
	RestartJellyfin       bool   `json:"restartJellyfin"`
	Storage               string `json:"storage"`
	// DownloadsSize is the size of the downloads disk in GB.
	DownloadsSize int `json:"downloadsSize"`
}

func (a MediaStackAnswers) Validate() error {
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswers, message) }

	switch {
	case !hostPattern.MatchString(a.NASServer):
		return invalid("the NAS address must be an IP address or a hostname")
	case !exportPattern.MatchString(a.NASExport):
		return invalid("the NFS export must be a path, like /volume1/media")
	case !arr.ValidFolder(a.MoviesFolder) || !arr.ValidFolder(a.SeriesFolder):
		return invalid("the movies and series folders must be folder names, like movies")
	case strings.EqualFold(a.MoviesFolder, a.SeriesFolder):
		return invalid("movies and series need different folders")
	case !validWireGuardKey(a.WireGuardPrivateKey):
		return invalid("the WireGuard private key must be 44 characters of base64")
	case !countriesPattern.MatchString(a.VPNCountries):
		return invalid("the VPN countries must be names, separated by commas")
	case !languagesPattern.MatchString(a.SubtitleLanguages):
		return invalid("the subtitle languages must be 2-letter codes, like en,nl")
	case !usernamePattern.MatchString(a.Username):
		return invalid("the username can have letters, digits, dots, dashes and underscores")
	case len([]rune(a.Password)) < 12 || len(a.Password) > 128 || strings.ContainsFunc(a.Password, unicode.IsControl):
		return invalid("the password must be 12 to 128 characters")
	case a.JellyfinAPIKey != "" && !apiKeyPattern.MatchString(a.JellyfinAPIKey):
		return invalid("the Jellyfin API key must be 32 characters (0-9 and a-f)")
	case a.JellyfinAdminUsername != "" && !jellyfinUserPattern.MatchString(a.JellyfinAdminUsername):
		return invalid("the Jellyfin admin username can have letters, digits, spaces, dots, dashes, underscores and @")
	case (a.JellyfinAdminUsername == "") != (a.JellyfinAdminPassword == ""):
		return invalid("give both the Jellyfin admin username and password, or neither")
	case len(a.JellyfinAdminPassword) > 256 || strings.ContainsFunc(a.JellyfinAdminPassword, unicode.IsControl):
		return invalid("the Jellyfin admin password can't have line breaks")
	case !storageID.MatchString(a.Storage):
		return invalid("invalid storage")
	case a.DownloadsSize < 10 || a.DownloadsSize > 10000:
		return invalid("the downloads disk must be 10 to 10000 GB")
	}

	return nil
}

func validWireGuardKey(key string) bool {
	decoded, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(decoded) == 32
}

func (a MediaStackAnswers) file() string {
	restart := "n"
	if a.RestartJellyfin {
		restart = "y"
	}

	lines := []string{
		"NAS_SERVER=" + a.NASServer,
		"NAS_EXPORT=" + a.NASExport,
		"MOVIES_FOLDER=" + a.MoviesFolder,
		"SERIES_FOLDER=" + a.SeriesFolder,
		"WIREGUARD_PRIVATE_KEY=" + a.WireGuardPrivateKey,
		"VPN_COUNTRIES=" + a.VPNCountries,
		"SUBTITLE_LANGUAGES=" + a.SubtitleLanguages,
		"ARR_USERNAME=" + a.Username,
		"ARR_PASSWORD=" + a.Password,
		"JELLYFIN_API_KEY=" + a.JellyfinAPIKey,
		"JELLYFIN_ADMIN_USERNAME=" + a.JellyfinAdminUsername,
		"JELLYFIN_ADMIN_PASSWORD=" + a.JellyfinAdminPassword,
		"RESTART_JELLYFIN=" + restart,
		"STORAGE=" + a.Storage,
		"DOWNLOADS_SIZE=" + strconv.Itoa(a.DownloadsSize),
	}

	return strings.Join(lines, "\n") + "\n"
}

// InstallRequest is what the manager sends. With KeepSecrets, empty secret
// fields take the values of the saved answers of the last failed install.
type InstallRequest struct {
	MediaStackAnswers
	KeepSecrets bool `json:"keepSecrets"`
}

// SavedAnswers are the answers of the last failed install, without the
// secrets. The secrets stay on the host.
type SavedAnswers struct {
	Answers           MediaStackAnswers `json:"answers"`
	HasJellyfinAPIKey bool              `json:"hasJellyfinApiKey"`
	// HasJellyfinAdminPassword is set when the saved answers have a Jellyfin admin password.
	HasJellyfinAdminPassword bool      `json:"hasJellyfinAdminPassword"`
	Until                    time.Time `json:"until"`
}

type savedFile struct {
	Answers MediaStackAnswers `json:"answers"`
	SavedAt time.Time         `json:"savedAt"`
}

type AppInstallStatus struct {
	App        string       `json:"app,omitempty"`
	State      UpgradeState `json:"state"`
	Message    string       `json:"message,omitempty"`
	StartedAt  time.Time    `json:"startedAt,omitzero"`
	FinishedAt time.Time    `json:"finishedAt,omitzero"`
	// VMID and IP are set when the install succeeded.
	VMID int    `json:"vmid,omitempty"`
	IP   string `json:"ip,omitempty"`
	Log  string `json:"log,omitempty"`
}

// AppInstaller runs the app installers that the release bundle left on the host.
type AppInstaller struct {
	Dir       string
	StacksDir string
	// start runs the installer in the background. Tests replace it.
	start func(script, answersPath, logPath, exitPath string) error
	// running reports whether the install unit still runs.
	running func() bool
	// now is the clock for the saved answers. Tests replace it.
	now func() time.Time

	mu sync.Mutex
}

func NewAppInstaller() *AppInstaller {
	return &AppInstaller{
		Dir:       "/var/lib/homelab-agent/apps",
		StacksDir: "/usr/local/lib/homelab/stacks",
		start:     startAppUnit,
		running:   func() bool { return exec.Command("systemctl", "is-active", "--quiet", appUnit).Run() == nil },
		now:       time.Now,
	}
}

func (i *AppInstaller) statusPath() string { return filepath.Join(i.Dir, "status.json") }
func (i *AppInstaller) logPath() string    { return filepath.Join(i.Dir, "install.log") }
func (i *AppInstaller) exitPath() string   { return filepath.Join(i.Dir, "exit-code") }

func (i *AppInstaller) savedPath(app string) string {
	return filepath.Join(i.Dir, app+".saved.json")
}

func (i *AppInstaller) Install(app string, request InstallRequest) error {
	if app != MediaStackApp {
		return ErrUnknownApp
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	answers := request.MediaStackAnswers
	if request.KeepSecrets {
		saved, err := i.loadSaved(app)
		if err != nil {
			return err
		}
		if answers.WireGuardPrivateKey == "" {
			answers.WireGuardPrivateKey = saved.WireGuardPrivateKey
		}
		if answers.Password == "" {
			answers.Password = saved.Password
		}
		if answers.JellyfinAPIKey == "" {
			answers.JellyfinAPIKey = saved.JellyfinAPIKey
		}
		// Only for the same admin: a new username needs its own password.
		if answers.JellyfinAdminPassword == "" && answers.JellyfinAdminUsername == saved.JellyfinAdminUsername {
			answers.JellyfinAdminPassword = saved.JellyfinAdminPassword
		}
	}

	return i.installLocked(app, answers)
}

// Retry runs the install again with the saved answers.
func (i *AppInstaller) Retry(app string) error {
	if app != MediaStackApp {
		return ErrUnknownApp
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	answers, err := i.loadSaved(app)
	if err != nil {
		return err
	}

	return i.installLocked(app, answers)
}

// installLocked needs i.mu.
func (i *AppInstaller) installLocked(app string, answers MediaStackAnswers) error {
	if err := answers.Validate(); err != nil {
		return err
	}

	if i.running() {
		return ErrAppInstallRunning
	}
	script := filepath.Join(i.StacksDir, "arr", "install.sh")
	if _, err := os.Stat(script); err != nil {
		return ErrAppNotAvailable
	}

	if err := os.MkdirAll(i.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Remove(i.exitPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(i.logPath(), nil, 0o600); err != nil {
		return err
	}
	answersPath := filepath.Join(i.Dir, app+".answers")
	if err := os.WriteFile(answersPath, []byte(answers.file()), 0o600); err != nil {
		return err
	}
	if err := i.save(app, answers); err != nil {
		os.Remove(answersPath)
		return err
	}

	status := AppInstallStatus{App: app, State: UpgradeRunning, StartedAt: time.Now()}
	if err := writeStatus(i.statusPath(), status); err != nil {
		return err
	}

	if err := i.start(script, answersPath, i.logPath(), i.exitPath()); err != nil {
		os.Remove(answersPath)
		status.State, status.Message, status.FinishedAt = UpgradeFailed, err.Error(), time.Now()
		writeStatus(i.statusPath(), status)
		return err
	}

	return nil
}

func (i *AppInstaller) Status() AppInstallStatus {
	i.mu.Lock()
	defer i.mu.Unlock()

	var status AppInstallStatus
	data, err := os.ReadFile(i.statusPath())
	if err != nil || json.Unmarshal(data, &status) != nil {
		return AppInstallStatus{State: UpgradeIdle}
	}

	log := ""
	if data, err := os.ReadFile(i.logPath()); err == nil {
		log = ansiPattern.ReplaceAllString(string(data), "")
		status.Log = tail(log, maxAppLog)
	}

	if status.State != UpgradeRunning {
		return status
	}

	exit, err := os.ReadFile(i.exitPath())
	switch {
	case err == nil:
		if info, statErr := os.Stat(i.exitPath()); statErr == nil {
			status.FinishedAt = info.ModTime()
		}
		status.State = UpgradeSucceeded
		if code := strings.TrimSpace(string(exit)); code != "0" {
			status.State, status.Message = UpgradeFailed, "the installer stopped with exit code "+code
		}
	case !i.running():
		status.State, status.Message = UpgradeFailed, "the install stopped before it finished"
	}

	if match := appResultPattern.FindStringSubmatch(log); match != nil && status.State == UpgradeSucceeded {
		status.VMID, _ = strconv.Atoi(match[2])
		status.IP = match[3]
	}
	// A working install needs no retry, so its secrets can go.
	if status.State == UpgradeSucceeded {
		os.Remove(i.savedPath(status.App))
	}

	return status
}

// Saved returns the saved answers without the secrets, or nil when there are none.
func (i *AppInstaller) Saved(app string) (*SavedAnswers, error) {
	if app != MediaStackApp {
		return nil, ErrUnknownApp
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	file, err := i.loadSavedFile(app)
	if errors.Is(err, ErrNoSavedAnswers) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	answers := file.Answers
	view := &SavedAnswers{
		HasJellyfinAPIKey:        answers.JellyfinAPIKey != "",
		HasJellyfinAdminPassword: answers.JellyfinAdminPassword != "",
		Until:                    file.SavedAt.Add(SavedAnswersTTL),
	}
	answers.WireGuardPrivateKey, answers.Password, answers.JellyfinAPIKey, answers.JellyfinAdminPassword = "", "", "", ""
	view.Answers = answers

	return view, nil
}

// Forget removes the saved answers.
func (i *AppInstaller) Forget(app string) error {
	if app != MediaStackApp {
		return ErrUnknownApp
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if err := os.Remove(i.savedPath(app)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

// save keeps the answers for a retry, readable by root only. It needs i.mu.
func (i *AppInstaller) save(app string, answers MediaStackAnswers) error {
	data, err := json.Marshal(savedFile{Answers: answers, SavedAt: i.now()})
	if err != nil {
		return err
	}

	temp := i.savedPath(app) + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temp, i.savedPath(app))
}

// loadSavedFile reads the saved answers and removes them when they are too old. It needs i.mu.
func (i *AppInstaller) loadSavedFile(app string) (savedFile, error) {
	data, err := os.ReadFile(i.savedPath(app))
	if errors.Is(err, os.ErrNotExist) {
		return savedFile{}, ErrNoSavedAnswers
	}
	if err != nil {
		return savedFile{}, err
	}

	var file savedFile
	if err := json.Unmarshal(data, &file); err != nil || i.now().Sub(file.SavedAt) > SavedAnswersTTL {
		os.Remove(i.savedPath(app))
		return savedFile{}, ErrNoSavedAnswers
	}

	return file, nil
}

func (i *AppInstaller) loadSaved(app string) (MediaStackAnswers, error) {
	file, err := i.loadSavedFile(app)
	return file.Answers, err
}

// startAppUnit runs the installer with systemd-run. The shell writes the exit
// code, and removes the answers when the installer did not.
func startAppUnit(script, answersPath, logPath, exitPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, "systemd-run",
		"--unit", appUnit, "--collect", "--quiet",
		"--property", "StandardOutput=append:"+logPath,
		"--property", "StandardError=append:"+logPath,
		"/bin/bash", "-c", `bash "$0" --answers "$1"; code=$?; rm -f "$1"; echo "$code" >"$2"`,
		script, answersPath, exitPath,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %v: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}
