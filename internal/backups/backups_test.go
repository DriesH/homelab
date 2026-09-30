package backups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"homelab/internal/agent"
	"homelab/internal/proxmox"
)

type fakeProxmox struct {
	mu        sync.Mutex
	resources []proxmox.Resource
	jobs      []proxmox.BackupJob
	backups   map[string][]proxmox.Backup
	tasks     []proxmox.Task
	calls     []string
	failTask  string
}

func (f *fakeProxmox) record(call string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return "UPID:" + call
}

func (f *fakeProxmox) Resources(context.Context) ([]proxmox.Resource, error) { return f.resources, nil }
func (f *fakeProxmox) BackupJobs(context.Context) ([]proxmox.BackupJob, error) {
	return f.jobs, nil
}
func (f *fakeProxmox) BackupStorages(context.Context, string) ([]proxmox.BackupStorage, error) {
	return []proxmox.BackupStorage{{Storage: "nas", Shared: true}, {Storage: "local"}}, nil
}
func (f *fakeProxmox) Backups(_ context.Context, _, storage string) ([]proxmox.Backup, error) {
	return f.backups[storage], nil
}
func (f *fakeProxmox) BackupTasks(context.Context, string, int) ([]proxmox.Task, error) {
	return f.tasks, nil
}
func (f *fakeProxmox) BackupGuest(_ context.Context, _ string, vmid int, storage string) (string, error) {
	return f.record(fmt.Sprintf("backup %d %s", vmid, storage)), nil
}
func (f *fakeProxmox) GuestStorage(context.Context, string, proxmox.GuestType, int) (string, error) {
	return "local-lvm", nil
}
func (f *fakeProxmox) RestoreGuest(_ context.Context, _ string, guestType proxmox.GuestType, vmid int, volid, storage string) (string, error) {
	return f.record(fmt.Sprintf("restore %s %d %s %s", guestType, vmid, volid, storage)), nil
}
func (f *fakeProxmox) DeleteBackup(_ context.Context, _, storage, volid string) error {
	f.record("delete " + volid)
	return nil
}
func (f *fakeProxmox) RunGuestAction(_ context.Context, _ string, _ proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error) {
	return f.record(fmt.Sprintf("%s %d", action, vmid)), nil
}
func (f *fakeProxmox) WaitTask(_ context.Context, _, upid string) error {
	if f.failTask != "" && strings.Contains(upid, f.failTask) {
		return errors.New("proxmox task failed: " + f.failTask)
	}
	return nil
}

type fakeAgent struct{ saved []agent.BackupJob }

func (f *fakeAgent) SaveBackupJob(_ context.Context, job agent.BackupJob) error {
	f.saved = append(f.saved, job)
	return nil
}

type harness struct {
	service  *Service
	pve      *fakeProxmox
	agent    *fakeAgent
	mu       sync.Mutex
	messages []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		pve: &fakeProxmox{
			resources: []proxmox.Resource{
				{Type: "node", Node: "pve", Status: "online"},
				{Type: "lxc", Node: "pve", VMID: 100, Name: "homelab", Status: "running"},
				{Type: "lxc", Node: "pve", VMID: 101, Name: "jellyfin", Status: "running"},
				{Type: "qemu", Node: "pve", VMID: 200, Name: "windows", Status: "stopped"},
			},
			backups: map[string][]proxmox.Backup{
				"local": {
					{VolID: "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst", VMID: 101, CTime: 1790000000, Format: "tar.zst"},
					{VolID: "local:backup/vzdump-lxc-101-2026_09_30-03_00_01.tar.zst", VMID: 101, CTime: 1790086400, Format: "tar.zst", Protected: true},
					{VolID: "local:backup/vzdump-qemu-200-2026_09_30-03_00_01.vma.zst", VMID: 200, CTime: 1790086400, Format: "vma.zst"},
				},
			},
		},
		agent: &fakeAgent{},
	}

	service, err := New(Options{
		DataDir:  t.TempDir(),
		Proxmox:  h.pve,
		Agent:    h.agent,
		SelfVMID: 100,
		Logger:   slog.New(slog.DiscardHandler),
		Notify: func(_ context.Context, text string) {
			h.mu.Lock()
			h.messages = append(h.messages, text)
			h.mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.service = service

	return h
}

// wait blocks until the background operation is done.
func (h *harness) wait(t *testing.T) Run {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.service.mu.Lock()
		busy, history := h.service.busy, h.service.state.History
		h.service.mu.Unlock()
		if busy == "" && len(history) > 0 {
			return history[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("operation did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStatusGroupsBackups(t *testing.T) {
	h := newHarness(t)
	h.pve.jobs = []proxmox.BackupJob{{
		ID: agent.BackupJobID, Schedule: "mon,thu 02:30", Storage: "local", Exclude: "200",
		PruneBackups: json.RawMessage(`{"keep-daily":"7"}`),
	}}

	view, err := h.service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	job := view.Job
	if !job.Exists || !job.Enabled || !slices.Equal(job.Days, []string{"mon", "thu"}) || job.Hour != 2 || job.Minute != 30 || job.KeepDaily != 7 {
		t.Fatalf("job = %+v", job)
	}

	jellyfin, windows := view.Guests[1], view.Guests[2]
	if !jellyfin.Included || windows.Included || !view.Guests[0].Self {
		t.Errorf("included: jellyfin %v, windows %v", jellyfin.Included, windows.Included)
	}
	if len(jellyfin.Backups) != 2 || !jellyfin.Backups[0].Protected {
		t.Errorf("jellyfin backups should be newest first: %+v", jellyfin.Backups)
	}
	if len(view.Storages) != 2 {
		t.Errorf("storages = %+v", view.Storages)
	}
}

func TestDefaultJobUsesLocalStorage(t *testing.T) {
	view, err := newHarness(t).service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if view.Job.Exists || view.Job.Storage != "local" || view.Job.Hour != 3 || view.Guests[1].Included {
		t.Fatalf("job = %+v", view.Job)
	}
}

func TestCustomScheduleIsKept(t *testing.T) {
	h := newHarness(t)
	h.pve.jobs = []proxmox.BackupJob{{ID: agent.BackupJobID, Schedule: "*-*-1 04:00", Storage: "local"}}

	view, _ := h.service.Status(context.Background())
	if view.Job.Custom != "*-*-1 04:00" {
		t.Fatalf("job = %+v", view.Job)
	}
}

func TestRestoreRunningContainer(t *testing.T) {
	h := newHarness(t)
	volid := "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"

	if err := h.service.Restore(context.Background(), 101, volid); err != nil {
		t.Fatal(err)
	}
	run := h.wait(t)

	want := []string{"shutdown 101", "restore lxc 101 " + volid + " local-lvm", "start 101"}
	if !run.Succeeded || !slices.Equal(h.pve.calls, want) {
		t.Fatalf("run = %+v, calls = %v", run, h.pve.calls)
	}
	if len(h.messages) != 1 || !strings.HasPrefix(h.messages[0], "✅ jellyfin (101): restored") {
		t.Fatalf("messages = %v", h.messages)
	}
}

func TestRestoreStopsWhenShutdownFails(t *testing.T) {
	h := newHarness(t)
	h.pve.failTask = "shutdown"

	if err := h.service.Restore(context.Background(), 101, "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"); err != nil {
		t.Fatal(err)
	}
	h.wait(t)

	if len(h.pve.calls) < 2 || h.pve.calls[1] != "stop 101" {
		t.Fatalf("calls = %v", h.pve.calls)
	}
}

func TestRestoreRefusals(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	cases := map[string]struct {
		vmid  int
		volid string
	}{
		"own container":   {100, "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"},
		"other guest":     {200, "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"},
		"unknown backup":  {101, "local:backup/nope.tar.zst"},
		"unknown guest":   {999, "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"},
		"no storage part": {101, "nope"},
	}
	for name, c := range cases {
		if err := h.service.Restore(ctx, c.vmid, c.volid); err == nil {
			t.Errorf("%s: restore was allowed", name)
		}
	}
	if len(h.pve.calls) != 0 {
		t.Fatalf("calls = %v", h.pve.calls)
	}
}

func TestBackUpUsesJobStorage(t *testing.T) {
	h := newHarness(t)
	h.pve.jobs = []proxmox.BackupJob{{ID: agent.BackupJobID, Schedule: "03:00", Storage: "nas"}}

	if err := h.service.BackUp(context.Background(), 101); err != nil {
		t.Fatal(err)
	}
	h.wait(t)

	if !slices.Equal(h.pve.calls, []string{"backup 101 nas"}) {
		t.Fatalf("calls = %v", h.pve.calls)
	}
}

func TestDeleteRefusesProtectedBackup(t *testing.T) {
	h := newHarness(t)

	err := h.service.Delete(context.Background(), "local:backup/vzdump-lxc-101-2026_09_30-03_00_01.tar.zst")
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v", err)
	}

	if err := h.service.Delete(context.Background(), "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"); err != nil {
		t.Fatal(err)
	}
	h.wait(t)
	if !slices.Equal(h.pve.calls, []string{"delete local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"}) {
		t.Fatalf("calls = %v", h.pve.calls)
	}
}

func TestMonitorAlertsForeignFailures(t *testing.T) {
	h := newHarness(t)
	h.service.state.LastTask = 1000
	h.service.ours["UPID:ours"] = true
	h.pve.tasks = []proxmox.Task{
		{UPID: "UPID:old", Status: "job errors", EndTime: 900},
		{UPID: "UPID:ours", Status: "job errors", EndTime: 1100},
		{UPID: "UPID:ok", Status: "OK", EndTime: 1200},
		{UPID: "UPID:warn", Status: "WARNINGS: 1", EndTime: 1250},
		{UPID: "UPID:failed", Status: "job errors", EndTime: 1300},
	}

	h.service.checkTasks(context.Background())
	if len(h.messages) != 1 || h.messages[0] != "❌ Backup of all guests on pve failed: job errors" {
		t.Fatalf("messages = %v", h.messages)
	}
	if h.service.state.LastTask != 1300 {
		t.Fatalf("last task = %d", h.service.state.LastTask)
	}

	h.service.checkTasks(context.Background())
	if len(h.messages) != 1 {
		t.Fatalf("alerted twice: %v", h.messages)
	}
}

func TestSaveJobValidates(t *testing.T) {
	h := newHarness(t)

	if err := h.service.SaveJob(context.Background(), agent.BackupJob{Storage: "bad storage!"}); !errors.Is(err, agent.ErrInvalidBackupJob) {
		t.Fatalf("err = %v", err)
	}
	if len(h.agent.saved) != 0 {
		t.Fatal("invalid job reached the agent")
	}
}
