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
	JellyfinApp   = "jellyfin"
	// SavedAnswersTTL is how long the answers of a failed install stay on the
	// host, so you can try again without typing the secrets again.
	SavedAnswersTTL = 24 * time.Hour
)

// AppAction is what the app unit does: install, update or remove an app.
type AppAction string

const (
	ActionInstall AppAction = "install"
	ActionUpdate  AppAction = "update"
	ActionRemove  AppAction = "remove"
	ActionVPN     AppAction = "vpn"
)

// name is the action in a message, like "the removal stopped".
func (a AppAction) name() string {
	switch a {
	case ActionRemove:
		return "removal"
	case ActionVPN:
		return "VPN change"
	}
	return string(a)
}

var (
	ErrInvalidAnswers    = errors.New("invalid answers")
	ErrAppInstallRunning = errors.New("an app is already being installed, updated or removed")
	ErrUnknownApp        = errors.New("unknown app")
	ErrAppNotAvailable   = errors.New("the installer is not on the host yet, update Homelab first")
	ErrNoSavedAnswers    = errors.New("there are no saved answers, fill in the form again")
	ErrInvalidContainer  = errors.New("invalid container")
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
	// The Jellyfin installer prints the API key it made for Homelab. The
	// agent takes it out of the log, so the browser never sees it.
	jellyfinKeyPattern = regexp.MustCompile(`(?m)^HOMELAB jellyfin-key ([A-Za-z0-9]+)\n?`)
)

// MediaStackAnswers are the questions of stacks/arr/install.sh. Every value
// is checked, so no line breaks or shell code reach the answers file.
type MediaStackAnswers struct {
	// The media is on a NAS (NASServer and NASExport), or in a folder on the
	// host (MediaFolder). All three are empty when the media is already mounted.
	NASServer   string `json:"nasServer"`
	NASExport   string `json:"nasExport"`
	MediaFolder string `json:"mediaFolder"`
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
	// OpenSubtitlesUsername and OpenSubtitlesPassword are optional. With them,
	// Bazarr also uses OpenSubtitles.com.
	OpenSubtitlesUsername string `json:"openSubtitlesUsername"`
	OpenSubtitlesPassword string `json:"openSubtitlesPassword"`
	RestartJellyfin       bool   `json:"restartJellyfin"`
	Storage               string `json:"storage"`
	// DownloadsSize is the size of the downloads disk in GB.
	DownloadsSize int `json:"downloadsSize"`
}

func (a MediaStackAnswers) Validate() error {
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswers, message) }

	if message := mediaSourceError(a.NASServer, a.NASExport, a.MediaFolder); message != "" {
		return invalid(message)
	}

	switch {
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
	case a.OpenSubtitlesUsername != "" && !usernamePattern.MatchString(a.OpenSubtitlesUsername):
		return invalid("the OpenSubtitles.com username can have letters, digits, dots, dashes and underscores")
	case (a.OpenSubtitlesUsername == "") != (a.OpenSubtitlesPassword == ""):
		return invalid("give both the OpenSubtitles.com username and password, or neither")
	case len(a.OpenSubtitlesPassword) > 256 || strings.ContainsFunc(a.OpenSubtitlesPassword, unicode.IsControl):
		return invalid("the OpenSubtitles.com password can't have line breaks")
	case !storageID.MatchString(a.Storage):
		return invalid("invalid storage")
	case a.DownloadsSize < 10 || a.DownloadsSize > 10000:
		return invalid("the downloads disk must be 10 to 10000 GB")
	}

	return nil
}

// mediaSourceError checks where the media is: on a NAS, in a folder on the
// host, or neither when the media is already mounted.
func mediaSourceError(server, export, folder string) string {
	switch {
	case folder != "" && (server != "" || export != ""):
		return "give a NAS share or a folder on the host, not both"
	case folder != "" && !ValidMediaFolder(folder):
		return "the media folder must be a full path on the host, like /mnt/pve/media, outside the system folders"
	case (server == "") != (export == ""):
		return "give both the NAS address and the NFS export"
	case server != "" && !hostPattern.MatchString(server):
		return "the NAS address must be an IP address or a hostname"
	case export != "" && !exportPattern.MatchString(export):
		return "the NFS export must be a path, like /volume1/media"
	}

	return ""
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
		"MEDIA_FOLDER=" + a.MediaFolder,
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
		"OPENSUBTITLES_USERNAME=" + a.OpenSubtitlesUsername,
		"OPENSUBTITLES_PASSWORD=" + a.OpenSubtitlesPassword,
		"RESTART_JELLYFIN=" + restart,
		"STORAGE=" + a.Storage,
		"DOWNLOADS_SIZE=" + strconv.Itoa(a.DownloadsSize),
	}

	return strings.Join(lines, "\n") + "\n"
}

// JellyfinAnswers are the questions of stacks/jellyfin/install.sh.
type JellyfinAnswers struct {
	// The media is on a NAS (NASServer and NASExport), or in a folder on the
	// host (MediaFolder). All three are empty when the media is already mounted.
	NASServer     string `json:"nasServer"`
	NASExport     string `json:"nasExport"`
	MediaFolder   string `json:"mediaFolder"`
	MoviesFolder  string `json:"moviesFolder"`
	SeriesFolder  string `json:"seriesFolder"`
	AdminUsername string `json:"adminUsername"`
	AdminPassword string `json:"adminPassword"`
	Theme         bool   `json:"theme"`
	Storage       string `json:"storage"`
}

func (a JellyfinAnswers) Validate() error {
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswers, message) }

	if message := mediaSourceError(a.NASServer, a.NASExport, a.MediaFolder); message != "" {
		return invalid(message)
	}

	switch {
	case !arr.ValidFolder(a.MoviesFolder) || !arr.ValidFolder(a.SeriesFolder):
		return invalid("the movies and series folders must be folder names, like movies")
	case strings.EqualFold(a.MoviesFolder, a.SeriesFolder):
		return invalid("movies and series need different folders")
	case !jellyfinUserPattern.MatchString(a.AdminUsername):
		return invalid("the admin username can have letters, digits, spaces, dots, dashes, underscores and @")
	case a.AdminPassword == "" || len(a.AdminPassword) > 256 || strings.ContainsFunc(a.AdminPassword, unicode.IsControl):
		return invalid("the admin password must be 1 to 256 characters, without line breaks")
	case !storageID.MatchString(a.Storage):
		return invalid("invalid storage")
	}

	return nil
}

func (a JellyfinAnswers) file() string {
	theme := "n"
	if a.Theme {
		theme = "y"
	}

	lines := []string{
		"NAS_SERVER=" + a.NASServer,
		"NAS_EXPORT=" + a.NASExport,
		"MEDIA_FOLDER=" + a.MediaFolder,
		"MOVIES_FOLDER=" + a.MoviesFolder,
		"SERIES_FOLDER=" + a.SeriesFolder,
		"JELLYFIN_ADMIN_USERNAME=" + a.AdminUsername,
		"JELLYFIN_ADMIN_PASSWORD=" + a.AdminPassword,
		"THEME=" + theme,
		"STORAGE=" + a.Storage,
	}

	return strings.Join(lines, "\n") + "\n"
}

// VPNSettings are the new VPN settings of the media stack. An empty key keeps the key in the container.
type VPNSettings struct {
	Countries           string `json:"countries"`
	WireGuardPrivateKey string `json:"wireguardPrivateKey"`
}

func (v VPNSettings) Validate() error {
	switch {
	case !countriesPattern.MatchString(v.Countries):
		return fmt.Errorf("%w: the VPN countries must be names, separated by commas", ErrInvalidAnswers)
	case v.WireGuardPrivateKey != "" && !validWireGuardKey(v.WireGuardPrivateKey):
		return fmt.Errorf("%w: the WireGuard private key must be 44 characters of base64", ErrInvalidAnswers)
	}

	return nil
}

func (v VPNSettings) file() string {
	return "VPN_COUNTRIES=" + v.Countries + "\nWIREGUARD_PRIVATE_KEY=" + v.WireGuardPrivateKey + "\n"
}

// appAnswers are the answers of one installer.
type appAnswers interface {
	Validate() error
	file() string
}

// stackDirs are the folders of the installers in the stacks folder.
var stackDirs = map[string]string{MediaStackApp: "arr", JellyfinApp: "jellyfin"}

// InstallRequest is what the manager sends: the answers of the media stack,
// or of Jellyfin. With KeepSecrets, empty secret fields take the values of
// the saved answers of the last failed install.
type InstallRequest struct {
	MediaStackAnswers
	Jellyfin    JellyfinAnswers `json:"jellyfin"`
	KeepSecrets bool            `json:"keepSecrets"`
}

// SavedAnswers are the answers of the last failed install, without the
// secrets. The secrets stay on the host.
type SavedAnswers struct {
	Answers           MediaStackAnswers `json:"answers"`
	Jellyfin          JellyfinAnswers   `json:"jellyfin"`
	HasJellyfinAPIKey bool              `json:"hasJellyfinApiKey"`
	// HasJellyfinAdminPassword is set when the saved answers have a Jellyfin admin password.
	HasJellyfinAdminPassword bool      `json:"hasJellyfinAdminPassword"`
	HasOpenSubtitlesPassword bool      `json:"hasOpenSubtitlesPassword"`
	Until                    time.Time `json:"until"`
}

type savedFile struct {
	Answers  MediaStackAnswers `json:"answers"`
	Jellyfin JellyfinAnswers   `json:"jellyfin"`
	SavedAt  time.Time         `json:"savedAt"`
}

func (f savedFile) answersFor(app string) appAnswers {
	if app == JellyfinApp {
		return f.Jellyfin
	}
	return f.Answers
}

type AppInstallStatus struct {
	App string `json:"app,omitempty"`
	// Action is empty in the status of an install from before updates and removals.
	Action     AppAction    `json:"action,omitempty"`
	State      UpgradeState `json:"state"`
	Message    string       `json:"message,omitempty"`
	StartedAt  time.Time    `json:"startedAt,omitzero"`
	FinishedAt time.Time    `json:"finishedAt,omitzero"`
	// VMID and IP are set when the install succeeded.
	VMID int    `json:"vmid,omitempty"`
	IP   string `json:"ip,omitempty"`
	Log  string `json:"log,omitempty"`
	// APIKey is the key that the Jellyfin installer made for Homelab. The
	// manager saves it and never passes it on to the browser.
	APIKey string `json:"apiKey,omitempty"`
}

// AppInstaller runs the app installers that the release bundle left on the host.
type AppInstaller struct {
	Dir       string
	StacksDir string
	// start runs the installer with args in the background, and removes cleanup
	// when it stops. Tests replace it.
	start func(script string, args []string, cleanup, logPath, exitPath string) error
	// running reports whether the install unit still runs.
	running func() bool
	// now is the clock for the saved answers. Tests replace it.
	now func() time.Time
	// output runs a command and returns its output. Tests replace it.
	output func(ctx context.Context, name string, args ...string) ([]byte, error)

	mu sync.Mutex
}

func NewAppInstaller() *AppInstaller {
	return &AppInstaller{
		Dir:       "/var/lib/homelab-agent/apps",
		StacksDir: "/usr/local/lib/homelab/stacks",
		start:     startAppUnit,
		running:   func() bool { return exec.Command("systemctl", "is-active", "--quiet", appUnit).Run() == nil },
		now:       time.Now,
		output:    commandOutput,
	}
}

func commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// NewAppInstallerWith runs the installers with start instead of systemd-run,
// and asks running whether one still runs. The dev mock uses it to play installs.
func NewAppInstallerWith(dir, stacksDir string, start func(script string, args []string, cleanup, logPath, exitPath string) error, running func() bool) *AppInstaller {
	return &AppInstaller{Dir: dir, StacksDir: stacksDir, start: start, running: running, now: time.Now, output: commandOutput}
}

func (i *AppInstaller) statusPath() string { return filepath.Join(i.Dir, "status.json") }
func (i *AppInstaller) logPath() string    { return filepath.Join(i.Dir, "install.log") }
func (i *AppInstaller) exitPath() string   { return filepath.Join(i.Dir, "exit-code") }

func (i *AppInstaller) savedPath(app string) string {
	return filepath.Join(i.Dir, app+".saved.json")
}

func (i *AppInstaller) Install(app string, request InstallRequest) error {
	if _, ok := stackDirs[app]; !ok {
		return ErrUnknownApp
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	var saved savedFile
	if request.KeepSecrets {
		var err error
		if saved, err = i.loadSavedFile(app); err != nil {
			return err
		}
	}

	if app == JellyfinApp {
		answers := request.Jellyfin
		// Only for the same admin: a new username needs its own password.
		if request.KeepSecrets && answers.AdminPassword == "" && answers.AdminUsername == saved.Jellyfin.AdminUsername {
			answers.AdminPassword = saved.Jellyfin.AdminPassword
		}
		return i.installLocked(app, savedFile{Jellyfin: answers})
	}

	answers := request.MediaStackAnswers
	if request.KeepSecrets {
		saved := saved.Answers
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
		if answers.OpenSubtitlesPassword == "" && answers.OpenSubtitlesUsername == saved.OpenSubtitlesUsername {
			answers.OpenSubtitlesPassword = saved.OpenSubtitlesPassword
		}
	}

	return i.installLocked(app, savedFile{Answers: answers})
}

// Retry runs the install again with the saved answers.
func (i *AppInstaller) Retry(app string) error {
	if _, ok := stackDirs[app]; !ok {
		return ErrUnknownApp
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	saved, err := i.loadSavedFile(app)
	if err != nil {
		return err
	}

	return i.installLocked(app, saved)
}

// installLocked needs i.mu.
func (i *AppInstaller) installLocked(app string, saved savedFile) error {
	answers := saved.answersFor(app)
	if err := answers.Validate(); err != nil {
		return err
	}

	script, err := i.prepare(app)
	if err != nil {
		return err
	}

	answersPath := filepath.Join(i.Dir, app+".answers")
	if err := os.WriteFile(answersPath, []byte(answers.file()), 0o600); err != nil {
		return err
	}
	if err := i.save(app, saved); err != nil {
		os.Remove(answersPath)
		return err
	}

	return i.run(app, ActionInstall, script, []string{"--answers", answersPath}, answersPath)
}

// Update updates the packages, the stack and the images of an installed app.
// The installer checks that the container is the app of Homelab.
func (i *AppInstaller) Update(app string, vmid int) error {
	return i.runOnContainer(app, ActionUpdate, vmid)
}

// Remove removes the container of an installed app. Its backups stay.
func (i *AppInstaller) Remove(app string, vmid int) error {
	return i.runOnContainer(app, ActionRemove, vmid)
}

// VPNCountries reads the VPN countries of the media stack in container vmid.
// The WireGuard key stays in the container.
func (i *AppInstaller) VPNCountries(ctx context.Context, vmid int) (string, error) {
	if vmid < 100 || vmid > 999999999 {
		return "", ErrInvalidContainer
	}

	output, err := i.output(ctx, "pct", "exec", strconv.Itoa(vmid), "--", "sed", "-n", "s/^VPN_COUNTRIES=//p", "/opt/arr/.env")
	if err != nil {
		return "", fmt.Errorf("could not read the VPN settings of container %d: %w", vmid, err)
	}
	countries := strings.TrimSpace(string(output))
	if !countriesPattern.MatchString(countries) {
		return "", fmt.Errorf("container %d has no VPN countries in /opt/arr/.env", vmid)
	}

	return countries, nil
}

// ChangeVPN gives the media stack in container vmid new VPN settings. The
// installer goes back to the old settings when the new ones do not connect.
func (i *AppInstaller) ChangeVPN(vmid int, settings VPNSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if vmid < 100 || vmid > 999999999 {
		return ErrInvalidContainer
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	script, err := i.prepare(MediaStackApp)
	if err != nil {
		return err
	}
	// The key goes in a file that only root can read, not on the command line.
	answersPath := filepath.Join(i.Dir, "vpn.answers")
	if err := os.WriteFile(answersPath, []byte(settings.file()), 0o600); err != nil {
		return err
	}

	return i.run(MediaStackApp, ActionVPN, script, []string{"--vpn", "--ctid", strconv.Itoa(vmid), "--answers", answersPath}, answersPath)
}

func (i *AppInstaller) runOnContainer(app string, action AppAction, vmid int) error {
	if _, ok := stackDirs[app]; !ok {
		return ErrUnknownApp
	}
	if vmid < 100 || vmid > 999999999 {
		return ErrInvalidContainer
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	script, err := i.prepare(app)
	if err != nil {
		return err
	}

	return i.run(app, action, script, []string{"--" + string(action), "--ctid", strconv.Itoa(vmid)}, "")
}

// prepare checks that nothing runs and that the installer is on the host, and
// clears the files of the last run. It needs i.mu.
func (i *AppInstaller) prepare(app string) (string, error) {
	if i.running() {
		return "", ErrAppInstallRunning
	}
	script := filepath.Join(i.StacksDir, stackDirs[app], "install.sh")
	if _, err := os.Stat(script); err != nil {
		return "", ErrAppNotAvailable
	}

	if err := os.MkdirAll(i.Dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Remove(i.exitPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.WriteFile(i.logPath(), nil, 0o600); err != nil {
		return "", err
	}

	return script, nil
}

// run starts the installer and saves its status. It needs i.mu.
func (i *AppInstaller) run(app string, action AppAction, script string, args []string, cleanup string) error {
	status := AppInstallStatus{App: app, Action: action, State: UpgradeRunning, StartedAt: time.Now()}
	if err := writeStatus(i.statusPath(), status); err != nil {
		if cleanup != "" {
			os.Remove(cleanup)
		}
		return err
	}

	if err := i.start(script, args, cleanup, i.logPath(), i.exitPath()); err != nil {
		if cleanup != "" {
			os.Remove(cleanup)
		}
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
	if status.Action == "" {
		status.Action = ActionInstall
	}

	log := ""
	if data, err := os.ReadFile(i.logPath()); err == nil {
		log = ansiPattern.ReplaceAllString(string(data), "")
		if match := jellyfinKeyPattern.FindStringSubmatch(log); match != nil {
			status.APIKey = match[1]
			log = jellyfinKeyPattern.ReplaceAllString(log, "")
		}
		status.Log = tail(log, maxAppLog)
	}

	if status.State != UpgradeRunning {
		if status.State != UpgradeSucceeded {
			status.APIKey = ""
		}
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
			status.State, status.Message = UpgradeFailed, fmt.Sprintf("the %s stopped with exit code %s", status.Action.name(), code)
		}
	case !i.running():
		status.State, status.Message = UpgradeFailed, fmt.Sprintf("the %s stopped before it finished", status.Action.name())
	}

	if status.Action != ActionInstall {
		return status
	}

	if match := appResultPattern.FindStringSubmatch(log); match != nil && status.State == UpgradeSucceeded {
		status.VMID, _ = strconv.Atoi(match[2])
		status.IP = match[3]
	}
	// A working install needs no retry, so its secrets can go.
	if status.State == UpgradeSucceeded {
		os.Remove(i.savedPath(status.App))
	} else {
		status.APIKey = ""
	}

	return status
}

// Saved returns the saved answers without the secrets, or nil when there are none.
func (i *AppInstaller) Saved(app string) (*SavedAnswers, error) {
	if _, ok := stackDirs[app]; !ok {
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

	answers, jellyfin := file.Answers, file.Jellyfin
	view := &SavedAnswers{
		HasJellyfinAPIKey:        answers.JellyfinAPIKey != "",
		HasJellyfinAdminPassword: answers.JellyfinAdminPassword != "" || jellyfin.AdminPassword != "",
		HasOpenSubtitlesPassword: answers.OpenSubtitlesPassword != "",
		Until:                    file.SavedAt.Add(SavedAnswersTTL),
	}
	answers.WireGuardPrivateKey, answers.Password, answers.JellyfinAPIKey = "", "", ""
	answers.JellyfinAdminPassword, answers.OpenSubtitlesPassword = "", ""
	jellyfin.AdminPassword = ""
	view.Answers, view.Jellyfin = answers, jellyfin

	return view, nil
}

// Forget removes the saved answers.
func (i *AppInstaller) Forget(app string) error {
	if _, ok := stackDirs[app]; !ok {
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
func (i *AppInstaller) save(app string, saved savedFile) error {
	saved.SavedAt = i.now()
	data, err := json.Marshal(saved)
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

// startAppUnit runs the installer with systemd-run. The shell writes the exit
// code, and removes the answers (cleanup) when the installer did not.
func startAppUnit(script string, args []string, cleanup, logPath, exitPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	command := []string{
		"--unit", appUnit, "--collect", "--quiet",
		"--property", "StandardOutput=append:" + logPath,
		"--property", "StandardError=append:" + logPath,
		"/bin/bash", "-c", `exit_path="$1" cleanup="$2"; shift 2; bash "$@"; code=$?; [[ -z "$cleanup" ]] || rm -f "$cleanup"; echo "$code" >"$exit_path"`,
		"homelab-app", exitPath, cleanup, script,
	}
	output, err := exec.CommandContext(ctx, "systemd-run", append(command, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %v: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}
