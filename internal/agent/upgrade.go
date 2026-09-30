package agent

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"homelab/internal/release"
)

type UpgradeState string

const (
	UpgradeIdle      UpgradeState = "idle"
	UpgradeRunning   UpgradeState = "running"
	UpgradeSucceeded UpgradeState = "succeeded"
	UpgradeFailed    UpgradeState = "failed"
)

type UpgradeStatus struct {
	State      UpgradeState `json:"state"`
	Version    string       `json:"version,omitempty"`
	Message    string       `json:"message,omitempty"`
	StartedAt  time.Time    `json:"startedAt,omitzero"`
	FinishedAt time.Time    `json:"finishedAt,omitzero"`
	Log        string       `json:"log,omitempty"`
}

const (
	maxBundleBytes = 256 << 20
	maxUpgradeLog  = 32 * 1024
	// The upgrade runs in its own unit, so it survives the restart of this agent.
	upgradeUnit = "homelab-upgrade"
)

var (
	ErrUpgradeRunning = errors.New("an upgrade is already running")
	ErrInvalidBundle  = errors.New("invalid bundle")
)

// Upgrader installs a signed release bundle with the install.sh inside it.
type Upgrader struct {
	Dir     string
	Version string
	// start runs install.sh --upgrade in the background. Tests replace it.
	start func(installScript, logPath string) error
	// running reports whether the upgrade unit still runs.
	running func() bool
	// verify checks the signature. Tests replace it.
	verify func(signature release.Signature, sha256Hex string) error

	mu sync.Mutex
}

func NewUpgrader(version string) *Upgrader {
	return &Upgrader{
		Dir:     "/var/lib/homelab-agent/upgrade",
		Version: version,
		start:   startUpgradeUnit,
		running: upgradeUnitRunning,
		verify:  release.Verify,
	}
}

func (u *Upgrader) statusPath() string { return filepath.Join(u.Dir, "status.json") }
func (u *Upgrader) logPath() string    { return filepath.Join(u.Dir, "upgrade.log") }

// Start saves and checks the bundle, then starts the upgrade.
func (u *Upgrader) Start(bundle io.Reader, signature release.Signature) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.running() {
		return ErrUpgradeRunning
	}
	if !release.Newer(signature.Version, u.Version) {
		return fmt.Errorf("%w: %s is not newer than %s", ErrInvalidBundle, signature.Version, u.Version)
	}

	if err := os.MkdirAll(u.Dir, 0o700); err != nil {
		return err
	}

	bundlePath := filepath.Join(u.Dir, "bundle.tar.gz")
	hash, err := saveBundle(bundle, bundlePath)
	if err != nil {
		return err
	}
	if err := u.verify(signature, hash); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}

	extractDir := filepath.Join(u.Dir, "bundle")
	if err := os.RemoveAll(extractDir); err != nil {
		return err
	}
	installScript, err := extractBundle(bundlePath, extractDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}

	status := UpgradeStatus{State: UpgradeRunning, Version: signature.Version, StartedAt: time.Now()}
	if err := writeStatus(u.statusPath(), status); err != nil {
		return err
	}
	if err := os.WriteFile(u.logPath(), nil, 0o600); err != nil {
		return err
	}

	if err := u.start(installScript, u.logPath()); err != nil {
		status.State, status.Message, status.FinishedAt = UpgradeFailed, err.Error(), time.Now()
		writeStatus(u.statusPath(), status)
		return err
	}

	return nil
}

func (u *Upgrader) Status() UpgradeStatus {
	u.mu.Lock()
	defer u.mu.Unlock()

	status := UpgradeStatus{State: UpgradeIdle}
	data, err := os.ReadFile(u.statusPath())
	if err != nil || json.Unmarshal(data, &status) != nil {
		return UpgradeStatus{State: UpgradeIdle}
	}

	if status.State == UpgradeRunning && !u.running() {
		status.State = UpgradeFailed
		status.Message = "the upgrade stopped before it finished"
	}

	if log, err := os.ReadFile(u.logPath()); err == nil {
		status.Log = tail(string(log), maxUpgradeLog)
	}

	return status
}

func saveBundle(bundle io.Reader, path string) (string, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(bundle, maxBundleBytes+1))
	if err != nil {
		return "", err
	}
	if written > maxBundleBytes {
		return "", fmt.Errorf("%w: larger than %d MB", ErrInvalidBundle, maxBundleBytes>>20)
	}

	return hex.EncodeToString(hash.Sum(nil)), file.Close()
}

// extractBundle unpacks only plain files and folders inside dir, and returns
// the path of install.sh in the bundle's top folder.
func extractBundle(bundlePath, dir string) (string, error) {
	file, err := os.Open(bundlePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	topDirs := map[string]bool{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}

		name := filepath.Clean(header.Name)
		if !filepath.IsLocal(name) {
			return "", fmt.Errorf("unsafe path %q", header.Name)
		}
		topDirs[strings.Split(name, string(filepath.Separator))[0]] = true
		target := filepath.Join(dir, name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := writeBundleFile(target, reader, os.FileMode(header.Mode)&0o755); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("unexpected entry %q", header.Name)
		}
	}

	if len(topDirs) != 1 {
		return "", errors.New("the bundle must have one top folder")
	}
	for top := range topDirs {
		installScript := filepath.Join(dir, top, "install.sh")
		if _, err := os.Stat(installScript); err != nil {
			return "", errors.New("install.sh is missing")
		}
		return installScript, nil
	}

	return "", errors.New("empty bundle")
}

func writeBundleFile(path string, content io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, content); err != nil {
		file.Close()
		return err
	}

	return file.Close()
}

func writeStatus(path string, status UpgradeStatus) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func startUpgradeUnit(installScript, logPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, "systemd-run",
		"--unit", upgradeUnit, "--collect", "--quiet",
		"--property", "StandardOutput=append:"+logPath,
		"--property", "StandardError=append:"+logPath,
		"/bin/bash", installScript, "--upgrade",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %v: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

func upgradeUnitRunning() bool {
	return exec.Command("systemctl", "is-active", "--quiet", upgradeUnit).Run() == nil
}
