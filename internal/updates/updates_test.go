package updates

import (
	"context"
	"errors"
	"io"
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
	mu                sync.Mutex
	resources         []proxmox.Resource
	snapshotsDisabled bool
	snapshots         map[int][]proxmox.Snapshot
	calls             []string
	// stopAfterUpdate marks containers that are stopped once the agent updated them.
	stopAfterUpdate map[int]bool
	updated         map[int]bool
}

func newFakeProxmox() *fakeProxmox {
	return &fakeProxmox{
		resources: []proxmox.Resource{
			{Type: "node", Node: "pve", Status: "online"},
			{Type: "lxc", Node: "pve", VMID: 100, Name: "homelab", Status: "running"},
			{Type: "lxc", Node: "pve", VMID: 101, Name: "jellyfin", Status: "running"},
			{Type: "lxc", Node: "pve", VMID: 102, Name: "media", Status: "running"},
			{Type: "lxc", Node: "pve", VMID: 103, Name: "old", Status: "stopped"},
		},
		snapshots:       map[int][]proxmox.Snapshot{},
		stopAfterUpdate: map[int]bool{},
		updated:         map[int]bool{},
	}
}

func (f *fakeProxmox) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeProxmox) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var matches []string
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			matches = append(matches, call)
		}
	}
	return matches
}

func (f *fakeProxmox) Resources(context.Context) ([]proxmox.Resource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	resources := slices.Clone(f.resources)
	for index, resource := range resources {
		if f.updated[resource.VMID] && f.stopAfterUpdate[resource.VMID] {
			resources[index].Status = "stopped"
		}
	}
	return resources, nil
}

func (f *fakeProxmox) SnapshotSupported(context.Context, string, int) (bool, error) {
	return !f.snapshotsDisabled, nil
}

func (f *fakeProxmox) Snapshots(_ context.Context, _ string, vmid int) ([]proxmox.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.snapshots[vmid]), nil
}

func (f *fakeProxmox) CreateSnapshot(_ context.Context, _ string, vmid int, name, _ string) error {
	f.record("snapshot " + name)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots[vmid] = append(f.snapshots[vmid], proxmox.Snapshot{Name: name, SnapTime: 9999})
	return nil
}

func (f *fakeProxmox) RollbackSnapshot(_ context.Context, _ string, vmid int, name string) error {
	f.record("rollback " + name)
	return nil
}

func (f *fakeProxmox) DeleteSnapshot(_ context.Context, _ string, _ int, name string) error {
	f.record("delete " + name)
	return nil
}

type fakeAgent struct {
	mu       sync.Mutex
	proxmox  *fakeProxmox
	failVMID map[int]bool
	checks   map[int]agent.Job
	host     agent.Job
	jobs     []agent.JobRequest
}

func (f *fakeAgent) RunJob(_ context.Context, request agent.JobRequest) (agent.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, request)

	switch request.Kind {
	case agent.HostCheck:
		return f.host, nil
	case agent.GuestCheck:
		return f.checks[request.VMID], nil
	case agent.GuestUpdate:
		f.proxmox.mu.Lock()
		f.proxmox.updated[request.VMID] = true
		f.proxmox.mu.Unlock()
		if f.failVMID[request.VMID] {
			return agent.Job{Status: agent.JobFailed, Error: "exit status 100", Log: "E: broken packages"}, nil
		}
	}

	return agent.Job{Status: agent.JobSucceeded, Log: "done"}, nil
}

func (f *fakeAgent) updatedVMIDs() []int {
	f.mu.Lock()
	defer f.mu.Unlock()

	var vmids []int
	for _, job := range f.jobs {
		if job.Kind == agent.GuestUpdate {
			vmids = append(vmids, job.VMID)
		}
	}
	return vmids
}

type recordingNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (n *recordingNotifier) Send(_ context.Context, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, text)
	return nil
}

func withUpdates(names ...string) agent.Job {
	job := agent.Job{Status: agent.JobSucceeded, Images: []agent.Image{}}
	for _, name := range names {
		job.Packages = append(job.Packages, agent.Package{Name: name, From: "1", To: "2"})
	}
	return job
}

func newTestService(t *testing.T) (*Service, *fakeProxmox, *fakeAgent, *recordingNotifier) {
	t.Helper()

	pve := newFakeProxmox()
	fakeAgent := &fakeAgent{proxmox: pve, failVMID: map[int]bool{}, checks: map[int]agent.Job{}, host: withUpdates()}
	notifier := &recordingNotifier{}

	service, err := New(Options{
		DataDir:     t.TempDir(),
		Proxmox:     pve,
		Agent:       fakeAgent,
		SelfVMID:    100,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		NewNotifier: func(TelegramSettings) Notifier { return notifier },
		Now:         func() time.Time { return time.Date(2026, 10, 4, 4, 0, 30, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}

	return service, pve, fakeAgent, notifier
}

func TestGuestUpdateSnapshotsFirstAndKeepsThreeSnapshots(t *testing.T) {
	service, pve, _, _ := newTestService(t)
	pve.snapshots[101] = []proxmox.Snapshot{
		{Name: "homelab_1", SnapTime: 1}, {Name: "homelab_2", SnapTime: 2},
		{Name: "homelab_3", SnapTime: 3}, {Name: "before-upgrade", SnapTime: 0},
	}

	run := service.updateGuest(context.Background(), 101, false)

	if run.Status != RunSucceeded || run.Snapshot == "" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if got := pve.called("delete"); !slices.Equal(got, []string{"delete homelab_1"}) {
		t.Fatalf("expected only the oldest homelab snapshot deleted, got %v", got)
	}
	if len(pve.called("rollback")) != 0 {
		t.Fatal("rolled back after a successful update")
	}
}

func TestFailedGuestUpdateRollsBack(t *testing.T) {
	service, pve, fakeAgent, _ := newTestService(t)
	fakeAgent.failVMID[101] = true

	run := service.updateGuest(context.Background(), 101, false)

	if run.Status != RunRolledBack || run.Log != "E: broken packages" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if got := pve.called("rollback"); len(got) != 1 || got[0] != "rollback "+run.Snapshot {
		t.Fatalf("expected rollback to %s, got %v", run.Snapshot, got)
	}
}

func TestContainerStoppedAfterUpdateRollsBack(t *testing.T) {
	service, pve, _, _ := newTestService(t)
	pve.stopAfterUpdate[101] = true

	if run := service.updateGuest(context.Background(), 101, false); run.Status != RunRolledBack {
		t.Fatalf("expected rollback, got %+v", run)
	}
}

func TestNoRollbackWithoutSnapshotOrForManagerItself(t *testing.T) {
	service, pve, fakeAgent, _ := newTestService(t)
	fakeAgent.failVMID[100] = true
	fakeAgent.failVMID[101] = true

	if run := service.updateGuest(context.Background(), 100, false); run.Status != RunFailed {
		t.Fatalf("manager itself: expected failed, got %+v", run)
	}

	pve.snapshotsDisabled = true
	if run := service.updateGuest(context.Background(), 101, false); run.Status != RunFailed || run.Snapshot != "" {
		t.Fatalf("no snapshot support: expected failed without snapshot, got %+v", run)
	}

	if len(pve.called("rollback")) != 0 {
		t.Fatal("rolled back when it must not")
	}
}

func TestScheduledRunUpdatesContainersButOnlyChecksHost(t *testing.T) {
	service, _, fakeAgent, notifier := newTestService(t)
	fakeAgent.host = withUpdates("pve-manager", "proxmox-kernel")
	fakeAgent.checks[100] = withUpdates("openssl")
	fakeAgent.checks[101] = withUpdates("jellyfin")
	fakeAgent.checks[102] = withUpdates("curl")
	if err := service.SaveSettings(SettingsInput{Schedule: defaultSettings.Schedule, Excluded: []int{102}}); err != nil {
		t.Fatal(err)
	}

	service.scheduledRun(context.Background())

	if got := fakeAgent.updatedVMIDs(); !slices.Equal(got, []int{101}) {
		t.Fatalf("expected only jellyfin updated, got %v", got)
	}
	for _, job := range fakeAgent.jobs {
		if job.Kind == agent.HostUpgrade {
			t.Fatal("the schedule must not upgrade the host")
		}
	}

	message := notifier.messages[len(notifier.messages)-1]
	for _, want := range []string{"jellyfin", "auto-update is off", "this manager", "Proxmox host: 2 updates waiting"} {
		if !strings.Contains(message, want) {
			t.Errorf("summary misses %q:\n%s", want, message)
		}
	}
}

func TestSettingsKeepBotTokenAndRejectBadValues(t *testing.T) {
	service, _, _, _ := newTestService(t)
	token := "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	if err := service.SaveSettings(SettingsInput{Schedule: defaultSettings.Schedule, Telegram: TelegramSettings{BotToken: token, ChatID: "42"}}); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveSettings(SettingsInput{Schedule: defaultSettings.Schedule, Telegram: TelegramSettings{ChatID: "42"}}); err != nil {
		t.Fatal(err)
	}
	if service.state.Settings.Telegram.BotToken != token {
		t.Fatal("an empty token should keep the saved one")
	}

	for _, input := range []SettingsInput{
		{Schedule: Schedule{Hour: 24}},
		{Schedule: defaultSettings.Schedule, Telegram: TelegramSettings{BotToken: "nope", ChatID: "42"}},
	} {
		if err := service.SaveSettings(input); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("%+v: expected ErrInvalidSettings, got %v", input, err)
		}
	}
}

func TestOnlyOneOperationAtATime(t *testing.T) {
	service, _, _, _ := newTestService(t)
	release := make(chan struct{})

	if err := service.start(context.Background(), "slow", func(context.Context) { <-release }); err != nil {
		t.Fatal(err)
	}
	if err := service.StartCheck(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
	close(release)
}

func TestDueSlot(t *testing.T) {
	schedule := Schedule{Enabled: true, Weekday: int(time.Sunday), Hour: 4}
	sunday := time.Date(2026, 10, 4, 4, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		now     time.Time
		lastRun time.Time
		due     bool
	}{
		{"at the slot", sunday, time.Time{}, true},
		{"half an hour late", sunday.Add(30 * time.Minute), time.Time{}, true},
		{"already ran", sunday.Add(time.Minute), sunday, false},
		{"too late", sunday.Add(2 * time.Hour), time.Time{}, false},
		{"before the slot", sunday.Add(-time.Minute), sunday.AddDate(0, 0, -7), false},
	}
	for _, c := range cases {
		if _, due := dueSlot(schedule, c.now, c.lastRun); due != c.due {
			t.Errorf("%s: expected due=%v", c.name, c.due)
		}
	}

	if next := nextSlot(schedule, sunday.Add(-time.Hour)); !next.Equal(sunday) {
		t.Errorf("expected next run %v, got %v", sunday, next)
	}
}

func TestStatusNeverReturnsNullLists(t *testing.T) {
	service, _, _, _ := newTestService(t)

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if view.Host.Packages == nil || view.Host.Images == nil {
		t.Fatal("host lists are nil before the first check")
	}
	for _, guest := range view.Guests {
		if guest.Packages == nil || guest.Images == nil {
			t.Fatalf("guest %d lists are nil", guest.VMID)
		}
	}
}
