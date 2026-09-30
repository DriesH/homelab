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
)

var (
	ErrInvalidAnswers    = errors.New("invalid answers")
	ErrAppInstallRunning = errors.New("an app is already being installed")
	ErrUnknownApp        = errors.New("unknown app")
	ErrAppNotAvailable   = errors.New("the installer is not on the host yet, update Homelab first")
)

var (
	hostPattern      = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,252})$`)
	exportPattern    = regexp.MustCompile(`^/[A-Za-z0-9._/-]{0,255}$`)
	countriesPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z ]{1,40}(,[A-Za-z][A-Za-z ]{1,40}){0,9}$`)
	languagesPattern = regexp.MustCompile(`^[a-z]{2}(,[a-z]{2}){0,9}$`)
	usernamePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	apiKeyPattern    = regexp.MustCompile(`^[a-f0-9]{32}$`)
	ansiPattern      = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	appResultPattern = regexp.MustCompile(`(?m)^HOMELAB app (\w+) (\d+) (\S+)$`)
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
	JellyfinAPIKey  string `json:"jellyfinApiKey"`
	RestartJellyfin bool   `json:"restartJellyfin"`
	Storage         string `json:"storage"`
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
		"RESTART_JELLYFIN=" + restart,
		"STORAGE=" + a.Storage,
		"DOWNLOADS_SIZE=" + strconv.Itoa(a.DownloadsSize),
	}

	return strings.Join(lines, "\n") + "\n"
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

	mu sync.Mutex
}

func NewAppInstaller() *AppInstaller {
	return &AppInstaller{
		Dir:       "/var/lib/homelab-agent/apps",
		StacksDir: "/usr/local/lib/homelab/stacks",
		start:     startAppUnit,
		running:   func() bool { return exec.Command("systemctl", "is-active", "--quiet", appUnit).Run() == nil },
	}
}

func (i *AppInstaller) statusPath() string { return filepath.Join(i.Dir, "status.json") }
func (i *AppInstaller) logPath() string    { return filepath.Join(i.Dir, "install.log") }
func (i *AppInstaller) exitPath() string   { return filepath.Join(i.Dir, "exit-code") }

func (i *AppInstaller) Install(app string, answers MediaStackAnswers) error {
	if app != MediaStackApp {
		return ErrUnknownApp
	}
	if err := answers.Validate(); err != nil {
		return err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

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

	return status
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
