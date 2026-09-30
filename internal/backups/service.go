// Package backups shows the vzdump backups of the containers and VMs, and runs
// backups, restores and deletes. The schedule is a normal Proxmox backup job,
// which the host agent creates, so it also runs while Homelab is down.
package backups

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"homelab/internal/agent"
	"homelab/internal/proxmox"
)

const (
	taskTimeout     = 3 * time.Hour
	monitorInterval = 5 * time.Minute
	maxHistory      = 50
)

var (
	ErrBusy     = errors.New("a backup, restore or delete is already running")
	ErrNotFound = errors.New("not found")
	ErrRefused  = errors.New("refused")
)

type Proxmox interface {
	Resources(ctx context.Context) ([]proxmox.Resource, error)
	BackupJobs(ctx context.Context) ([]proxmox.BackupJob, error)
	BackupStorages(ctx context.Context, node string) ([]proxmox.BackupStorage, error)
	Backups(ctx context.Context, node, storage string) ([]proxmox.Backup, error)
	BackupTasks(ctx context.Context, node string, limit int) ([]proxmox.Task, error)
	BackupGuest(ctx context.Context, node string, vmid int, storage string) (string, error)
	GuestStorage(ctx context.Context, node string, guestType proxmox.GuestType, vmid int) (string, error)
	RestoreGuest(ctx context.Context, node string, guestType proxmox.GuestType, vmid int, volid, storage string) (string, error)
	DeleteBackup(ctx context.Context, node, storage, volid string) error
	RunGuestAction(ctx context.Context, node string, guestType proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error)
	WaitTask(ctx context.Context, node, upid string) error
}

type Agent interface {
	SaveBackupJob(ctx context.Context, job agent.BackupJob) error
}

type Options struct {
	DataDir string
	Proxmox Proxmox
	Agent   Agent
	// SelfVMID is the manager's own container, which can't be restored from here.
	SelfVMID int
	Notify   func(ctx context.Context, text string)
	Logger   *slog.Logger
	Now      func() time.Time
}

type RunKind string

const (
	RunBackup  RunKind = "backup"
	RunRestore RunKind = "restore"
	RunDelete  RunKind = "delete"
)

type Run struct {
	ID         string    `json:"id"`
	Kind       RunKind   `json:"kind"`
	VMID       int       `json:"vmid"`
	Target     string    `json:"target"`
	Succeeded  bool      `json:"succeeded"`
	Message    string    `json:"message"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

type state struct {
	History []Run `json:"history"`
	// LastTask is the end time of the newest Proxmox backup task we checked for failures.
	LastTask int64 `json:"lastTask"`
}

type Service struct {
	Options
	path string

	mu    sync.Mutex
	state state
	busy  string
	// ours holds the tasks Homelab started, which report their own result.
	ours map[string]bool
}

func New(options Options) (*Service, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Notify == nil {
		options.Notify = func(context.Context, string) {}
	}

	service := &Service{
		Options: options,
		path:    filepath.Join(options.DataDir, "backups.json"),
		state:   state{History: []Run{}},
		ours:    map[string]bool{},
	}

	data, err := os.ReadFile(service.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &service.state); err != nil {
			return nil, err
		}
	}
	if service.state.History == nil {
		service.state.History = []Run{}
	}

	return service, nil
}

// SaveJob creates or updates the Proxmox backup job through the host agent.
func (s *Service) SaveJob(ctx context.Context, job agent.BackupJob) error {
	if err := job.Validate(); err != nil {
		return err
	}

	return s.Agent.SaveBackupJob(ctx, job)
}

// start runs an operation in the background, one at a time.
func (s *Service) start(ctx context.Context, name string, operation func(context.Context) Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.busy != "" {
		return ErrBusy
	}
	s.busy = name

	go func() {
		run := operation(ctx)

		icon := "✅"
		if !run.Succeeded {
			icon = "❌"
		}
		s.Notify(ctx, fmt.Sprintf("%s %s: %s", icon, run.Target, run.Message))

		s.mu.Lock()
		s.busy = ""
		s.state.History = append([]Run{run}, s.state.History...)
		if len(s.state.History) > maxHistory {
			s.state.History = s.state.History[:maxHistory]
		}
		err := s.save()
		s.mu.Unlock()
		if err != nil {
			s.Logger.Error("could not save backup history", "error", err)
		}
	}()

	return nil
}

func (s *Service) newRun(kind RunKind, guest proxmox.Resource) Run {
	return Run{
		ID:        newID(),
		Kind:      kind,
		VMID:      guest.VMID,
		Target:    fmt.Sprintf("%s (%d)", guest.Name, guest.VMID),
		StartedAt: s.Now(),
	}
}

func (s *Service) finish(run Run, err error, message string) Run {
	run.FinishedAt = s.Now()
	run.Succeeded = err == nil
	run.Message = message
	if err != nil {
		run.Message = err.Error()
	}

	return run
}

// BackUp makes a backup of one guest on the storage of the backup job.
func (s *Service) BackUp(ctx context.Context, vmid int) error {
	guest, err := s.findGuest(ctx, vmid)
	if err != nil {
		return err
	}
	storage, err := s.jobStorage(ctx, guest.Node)
	if err != nil {
		return err
	}

	return s.start(ctx, fmt.Sprintf("Backing up %s", guest.Name), func(ctx context.Context) Run {
		run := s.newRun(RunBackup, guest)
		err := s.runTask(ctx, guest.Node, func() (string, error) {
			return s.Proxmox.BackupGuest(ctx, guest.Node, vmid, storage)
		})

		return s.finish(run, err, "backup made on "+storage)
	})
}

// Restore overwrites a guest with one of its backups. A running guest is
// shut down first and started again afterwards.
func (s *Service) Restore(ctx context.Context, vmid int, volid string) error {
	if vmid == s.SelfVMID && s.SelfVMID != 0 {
		return fmt.Errorf("%w: Homelab can't restore its own container while it runs in it", ErrRefused)
	}

	guest, err := s.findGuest(ctx, vmid)
	if err != nil {
		return err
	}
	backup, _, err := s.findBackup(ctx, volid)
	if err != nil {
		return err
	}
	if backup.VMID != vmid {
		return fmt.Errorf("%w: this backup belongs to guest %d", ErrRefused, backup.VMID)
	}
	isContainerBackup := strings.HasPrefix(backup.Format, "tar")
	if isContainerBackup != (guest.Type == string(proxmox.LXC)) {
		return fmt.Errorf("%w: this backup is not for a %s", ErrRefused, guest.Type)
	}

	return s.start(ctx, fmt.Sprintf("Restoring %s", guest.Name), func(ctx context.Context) Run {
		run := s.newRun(RunRestore, guest)
		err := s.restore(ctx, guest, volid)

		return s.finish(run, err, "restored from the backup of "+formatBackupTime(backup.CTime))
	})
}

func (s *Service) restore(ctx context.Context, guest proxmox.Resource, volid string) error {
	guestType := proxmox.GuestType(guest.Type)

	storage, err := s.Proxmox.GuestStorage(ctx, guest.Node, guestType, guest.VMID)
	if err != nil {
		return err
	}

	wasRunning := guest.Status == "running"
	if wasRunning {
		if err := s.stopGuest(ctx, guest); err != nil {
			return fmt.Errorf("could not stop the guest: %w", err)
		}
	}

	err = s.runTask(ctx, guest.Node, func() (string, error) {
		return s.Proxmox.RestoreGuest(ctx, guest.Node, guestType, guest.VMID, volid, storage)
	})
	if err != nil {
		return err
	}

	if wasRunning {
		return s.runTask(ctx, guest.Node, func() (string, error) {
			return s.Proxmox.RunGuestAction(ctx, guest.Node, guestType, guest.VMID, proxmox.Start)
		})
	}

	return nil
}

// stopGuest shuts the guest down cleanly, and stops it when that doesn't work.
func (s *Service) stopGuest(ctx context.Context, guest proxmox.Resource) error {
	guestType := proxmox.GuestType(guest.Type)

	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	err := s.runTask(shutdownCtx, guest.Node, func() (string, error) {
		return s.Proxmox.RunGuestAction(shutdownCtx, guest.Node, guestType, guest.VMID, proxmox.Shutdown)
	})
	cancel()
	if err == nil {
		return nil
	}

	s.Logger.Warn("shutdown failed, stopping the guest", "vmid", guest.VMID, "error", err)

	return s.runTask(ctx, guest.Node, func() (string, error) {
		return s.Proxmox.RunGuestAction(ctx, guest.Node, guestType, guest.VMID, proxmox.Stop)
	})
}

// Delete removes a backup that is not protected.
func (s *Service) Delete(ctx context.Context, volid string) error {
	backup, location, err := s.findBackup(ctx, volid)
	if err != nil {
		return err
	}
	if backup.Protected {
		return fmt.Errorf("%w: this backup is protected in Proxmox", ErrRefused)
	}

	guest := proxmox.Resource{VMID: backup.VMID, Name: "Backup of " + formatBackupTime(backup.CTime)}
	if found, err := s.findGuest(ctx, backup.VMID); err == nil {
		guest = found
	}

	return s.start(ctx, "Deleting a backup", func(ctx context.Context) Run {
		run := s.newRun(RunDelete, guest)
		err := s.Proxmox.DeleteBackup(ctx, location.node, location.storage, volid)

		return s.finish(run, err, "deleted the backup of "+formatBackupTime(backup.CTime))
	})
}

// runTask starts a Proxmox task and waits for it. vzdump ends with WARNINGS
// when a guest skipped something, which still gives a usable backup.
func (s *Service) runTask(ctx context.Context, node string, startTask func() (string, error)) error {
	ctx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	upid, err := startTask()
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.ours[upid] = true
	s.mu.Unlock()

	err = s.Proxmox.WaitTask(ctx, node, upid)
	if err != nil && strings.Contains(err.Error(), "WARNINGS") {
		return nil
	}

	return err
}

func (s *Service) findGuest(ctx context.Context, vmid int) (proxmox.Resource, error) {
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return proxmox.Resource{}, err
	}

	for _, resource := range resources {
		if resource.VMID == vmid && (resource.Type == "lxc" || resource.Type == "qemu") && resource.Template == 0 {
			return resource, nil
		}
	}

	return proxmox.Resource{}, fmt.Errorf("%w: guest %d", ErrNotFound, vmid)
}

type backupLocation struct {
	node    string
	storage string
}

// findBackup looks the volume up, so only real backups can be restored or deleted.
func (s *Service) findBackup(ctx context.Context, volid string) (proxmox.Backup, backupLocation, error) {
	storageName, _, found := strings.Cut(volid, ":")
	if !found {
		return proxmox.Backup{}, backupLocation{}, fmt.Errorf("%w: backup %s", ErrNotFound, volid)
	}

	nodes, err := s.onlineNodes(ctx)
	if err != nil {
		return proxmox.Backup{}, backupLocation{}, err
	}

	for _, node := range nodes {
		backups, err := s.Proxmox.Backups(ctx, node, storageName)
		if err != nil {
			continue
		}
		for _, backup := range backups {
			if backup.VolID == volid {
				return backup, backupLocation{node: node, storage: storageName}, nil
			}
		}
	}

	return proxmox.Backup{}, backupLocation{}, fmt.Errorf("%w: backup %s", ErrNotFound, volid)
}

func (s *Service) onlineNodes(ctx context.Context) ([]string, error) {
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return nil, err
	}

	nodes := []string{}
	for _, resource := range resources {
		if resource.Type == "node" && resource.Status == "online" {
			nodes = append(nodes, resource.Node)
		}
	}
	slices.Sort(nodes)

	return nodes, nil
}

// jobStorage is the storage of the backup job, or the default when there is no job yet.
func (s *Service) jobStorage(ctx context.Context, node string) (string, error) {
	if job, err := s.homelabJob(ctx); err == nil && job != nil && job.Storage != "" {
		return job.Storage, nil
	}

	storages, err := s.Proxmox.BackupStorages(ctx, node)
	if err != nil {
		return "", err
	}

	return defaultStorage(storages), nil
}

func defaultStorage(storages []proxmox.BackupStorage) string {
	for _, storage := range storages {
		if storage.Storage == "local" {
			return storage.Storage
		}
	}
	if len(storages) > 0 {
		return storages[0].Storage
	}

	return ""
}

func (s *Service) homelabJob(ctx context.Context) (*proxmox.BackupJob, error) {
	jobs, err := s.Proxmox.BackupJobs(ctx)
	if err != nil {
		return nil, err
	}

	for _, job := range jobs {
		if job.ID == agent.BackupJobID {
			return &job, nil
		}
	}

	return nil, nil
}

// RunMonitor sends an alert for each failed backup task that Homelab didn't
// start itself, like the scheduled job. It blocks until ctx ends.
func (s *Service) RunMonitor(ctx context.Context) {
	s.mu.Lock()
	if s.state.LastTask == 0 {
		// Don't alert about failures from before Homelab watched them.
		s.state.LastTask = s.Now().Unix()
		s.save()
	}
	s.mu.Unlock()

	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkTasks(ctx)
		}
	}
}

func (s *Service) checkTasks(ctx context.Context) {
	nodes, err := s.onlineNodes(ctx)
	if err != nil {
		s.Logger.Warn("could not check backup tasks", "error", err)
		return
	}

	s.mu.Lock()
	since := s.state.LastTask
	s.mu.Unlock()

	newest := since
	messages := []string{}
	for _, node := range nodes {
		tasks, err := s.Proxmox.BackupTasks(ctx, node, 50)
		if err != nil {
			s.Logger.Warn("could not check backup tasks", "node", node, "error", err)
			continue
		}

		for _, task := range tasks {
			if task.EndTime <= since {
				continue
			}
			newest = max(newest, task.EndTime)

			s.mu.Lock()
			ours := s.ours[task.UPID]
			delete(s.ours, task.UPID)
			s.mu.Unlock()

			if !ours && task.Status != "OK" && !strings.HasPrefix(task.Status, "WARNINGS") {
				target := "all guests"
				if task.ID != "" {
					target = "guest " + task.ID
				}
				messages = append(messages, fmt.Sprintf("❌ Backup of %s on %s failed: %s", target, node, task.Status))
			}
		}
	}

	s.mu.Lock()
	s.state.LastTask = newest
	err = s.save()
	s.mu.Unlock()
	if err != nil {
		s.Logger.Error("could not save backup state", "error", err)
	}

	if len(messages) > 0 {
		s.Notify(ctx, strings.Join(messages, "\n"))
	}
}

// save writes the state. It needs s.mu.
func (s *Service) save() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp")
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temp, s.path)
}

func formatBackupTime(unix int64) string {
	return time.Unix(unix, 0).Format("2 Jan 2006 15:04")
}

func parseVMIDs(list string) []int {
	vmids := []int{}
	for part := range strings.SplitSeq(list, ",") {
		if vmid, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			vmids = append(vmids, vmid)
		}
	}

	return vmids
}

func newID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)

	return hex.EncodeToString(bytes)
}
