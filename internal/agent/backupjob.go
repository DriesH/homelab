package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// BackupJobID is the Proxmox backup job that Homelab manages.
const BackupJobID = "homelab-backup"

var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var storageID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

var ErrInvalidBackupJob = errors.New("invalid backup job")

// BackupJob holds the few settings Homelab exposes. The agent builds the
// Proxmox job from them, so no free text reaches pvesh.
type BackupJob struct {
	Enabled bool `json:"enabled"`
	// Days are mon to sun. No days means every day.
	Days    []string `json:"days"`
	Hour    int      `json:"hour"`
	Minute  int      `json:"minute"`
	Storage string   `json:"storage"`
	// Exclude lists guests to skip. All other guests, also new ones, are backed up.
	Exclude     []int `json:"exclude"`
	KeepDaily   int   `json:"keepDaily"`
	KeepWeekly  int   `json:"keepWeekly"`
	KeepMonthly int   `json:"keepMonthly"`
}

func (j BackupJob) Validate() error {
	for _, day := range j.Days {
		if !slices.Contains(Weekdays, day) {
			return fmt.Errorf("%w: unknown day %q", ErrInvalidBackupJob, day)
		}
	}
	switch {
	case j.Hour < 0 || j.Hour > 23 || j.Minute < 0 || j.Minute > 59:
		return fmt.Errorf("%w: invalid time", ErrInvalidBackupJob)
	case !storageID.MatchString(j.Storage):
		return fmt.Errorf("%w: invalid storage", ErrInvalidBackupJob)
	case len(j.Exclude) > 200:
		return fmt.Errorf("%w: too many excluded guests", ErrInvalidBackupJob)
	}
	for _, vmid := range j.Exclude {
		if vmid < 100 || vmid > 999999999 {
			return fmt.Errorf("%w: invalid guest %d", ErrInvalidBackupJob, vmid)
		}
	}
	for _, keep := range []int{j.KeepDaily, j.KeepWeekly, j.KeepMonthly} {
		if keep < 0 || keep > 1000 {
			return fmt.Errorf("%w: invalid retention", ErrInvalidBackupJob)
		}
	}

	return nil
}

// Schedule is a Proxmox calendar event, like "mon,thu 02:30" or "02:30".
func (j BackupJob) Schedule() string {
	clock := fmt.Sprintf("%02d:%02d", j.Hour, j.Minute)

	days := []string{}
	for _, day := range Weekdays {
		if slices.Contains(j.Days, day) {
			days = append(days, day)
		}
	}
	if len(days) == 0 || len(days) == len(Weekdays) {
		return clock
	}

	return strings.Join(days, ",") + " " + clock
}

// Retention keeps everything when no keep option is set.
func (j BackupJob) Retention() string {
	parts := []string{}
	for _, keep := range []struct {
		name  string
		value int
	}{{"keep-daily", j.KeepDaily}, {"keep-weekly", j.KeepWeekly}, {"keep-monthly", j.KeepMonthly}} {
		if keep.value > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", keep.name, keep.value))
		}
	}
	if len(parts) == 0 {
		return "keep-all=1"
	}

	return strings.Join(parts, ",")
}

func (j BackupJob) pveshArgs(exists bool) []string {
	enabled := "0"
	if j.Enabled {
		enabled = "1"
	}

	args := []string{"create", "/cluster/backup", "--id", BackupJobID}
	if exists {
		args = []string{"set", "/cluster/backup/" + BackupJobID}
	}
	args = append(args,
		"--schedule", j.Schedule(),
		"--storage", j.Storage,
		"--all", "1",
		"--mode", "snapshot",
		"--compress", "zstd",
		"--prune-backups", j.Retention(),
		"--enabled", enabled,
		"--repeat-missed", "1",
		"--notes-template", "{{guestname}}",
		"--comment", "Managed by Homelab",
	)

	switch {
	case len(j.Exclude) > 0:
		vmids := make([]string, 0, len(j.Exclude))
		for _, vmid := range j.Exclude {
			vmids = append(vmids, strconv.Itoa(vmid))
		}
		args = append(args, "--exclude", strings.Join(vmids, ","))
	case exists:
		args = append(args, "--delete", "exclude")
	}

	return args
}

// SaveBackupJob creates or updates the Proxmox backup job.
func SaveBackupJob(ctx context.Context, job BackupJob) error {
	if err := job.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	exists := exec.CommandContext(ctx, "pvesh", "get", "/cluster/backup/"+BackupJobID).Run() == nil

	output, err := exec.CommandContext(ctx, "pvesh", job.pveshArgs(exists)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pvesh: %v: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}
