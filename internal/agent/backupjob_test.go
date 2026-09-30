package agent

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func validJob() BackupJob {
	return BackupJob{
		Enabled: true, Days: []string{"thu", "mon"}, Hour: 2, Minute: 5, Storage: "local",
		Exclude: []int{100}, KeepDaily: 7, KeepWeekly: 4,
	}
}

func TestBackupJobSchedule(t *testing.T) {
	job := validJob()
	if got := job.Schedule(); got != "mon,thu 02:05" {
		t.Errorf("schedule = %q", got)
	}

	job.Days = nil
	if got := job.Schedule(); got != "02:05" {
		t.Errorf("daily schedule = %q", got)
	}

	job.Days = Weekdays
	if got := job.Schedule(); got != "02:05" {
		t.Errorf("every day schedule = %q", got)
	}
}

func TestBackupJobRetention(t *testing.T) {
	job := validJob()
	if got := job.Retention(); got != "keep-daily=7,keep-weekly=4" {
		t.Errorf("retention = %q", got)
	}

	job.KeepDaily, job.KeepWeekly = 0, 0
	if got := job.Retention(); got != "keep-all=1" {
		t.Errorf("retention = %q", got)
	}
}

func TestBackupJobValidation(t *testing.T) {
	changes := []func(*BackupJob){
		func(j *BackupJob) { j.Days = []string{"monday"} },
		func(j *BackupJob) { j.Hour = 24 },
		func(j *BackupJob) { j.Minute = -1 },
		func(j *BackupJob) { j.Storage = "local --script /tmp/x" },
		func(j *BackupJob) { j.Storage = "" },
		func(j *BackupJob) { j.Exclude = []int{5} },
		func(j *BackupJob) { j.KeepDaily = -1 },
	}

	for index, change := range changes {
		job := validJob()
		change(&job)
		if err := job.Validate(); !errors.Is(err, ErrInvalidBackupJob) {
			t.Errorf("change %d: err = %v", index, err)
		}
	}
}

func TestBackupJobArgs(t *testing.T) {
	job := validJob()

	create := strings.Join(job.pveshArgs(false), " ")
	for _, want := range []string{"create /cluster/backup --id homelab-backup", "--schedule mon,thu 02:05", "--all 1", "--exclude 100", "--enabled 1"} {
		if !strings.Contains(create, want) {
			t.Errorf("create args %q miss %q", create, want)
		}
	}

	job.Exclude = nil
	update := job.pveshArgs(true)
	if update[0] != "set" || update[1] != "/cluster/backup/homelab-backup" || !slices.Contains(update, "--delete") {
		t.Errorf("update args = %v", update)
	}
}
