package backups

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"homelab/internal/agent"
	"homelab/internal/proxmox"
)

// The schedules Homelab writes: "02:30" or "mon,thu 02:30".
var schedulePattern = regexp.MustCompile(`^(?:((?:mon|tue|wed|thu|fri|sat|sun)(?:,(?:mon|tue|wed|thu|fri|sat|sun))*) )?(\d{1,2}):(\d{2})$`)

// DefaultJob is shown until the job exists: every night at 03:00, one hour
// before the weekly updates.
var DefaultJob = agent.BackupJob{
	Enabled: true, Days: []string{}, Hour: 3, Minute: 0, Exclude: []int{},
	KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 3,
}

type JobView struct {
	agent.BackupJob
	// Exists is false until the job is saved once.
	Exists  bool      `json:"exists"`
	NextRun time.Time `json:"nextRun,omitzero"`
	// Custom is set when the schedule was changed in Proxmox to something Homelab can't show.
	Custom string `json:"custom,omitempty"`
}

type StorageView struct {
	Node  string `json:"node"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Total int64  `json:"total"`
	Used  int64  `json:"used"`
}

type BackupView struct {
	VolID     string    `json:"volid"`
	Storage   string    `json:"storage"`
	CreatedAt time.Time `json:"createdAt"`
	Size      int64     `json:"size"`
	Notes     string    `json:"notes"`
	Protected bool      `json:"protected"`
}

type GuestView struct {
	VMID     int          `json:"vmid"`
	Name     string       `json:"name"`
	Type     string       `json:"type"`
	Status   string       `json:"status"`
	Included bool         `json:"included"`
	Self     bool         `json:"self"`
	Backups  []BackupView `json:"backups"`
}

type View struct {
	Busy     string        `json:"busy"`
	Job      JobView       `json:"job"`
	Storages []StorageView `json:"storages"`
	Guests   []GuestView   `json:"guests"`
	History  []Run         `json:"history"`
}

func (s *Service) Status(ctx context.Context) (View, error) {
	s.mu.Lock()
	view := View{Busy: s.busy, History: slices.Clone(s.state.History), Storages: []StorageView{}, Guests: []GuestView{}}
	s.mu.Unlock()

	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return View{}, err
	}

	backupsByGuest := map[int][]BackupView{}
	seenShared := map[string]bool{}
	for _, resource := range resources {
		if resource.Type != "node" || resource.Status != "online" {
			continue
		}

		storages, err := s.Proxmox.BackupStorages(ctx, resource.Node)
		if err != nil {
			return View{}, err
		}

		for _, storage := range storages {
			// Shared storage shows up on every node, but holds the same backups.
			if storage.Shared {
				if seenShared[storage.Storage] {
					continue
				}
				seenShared[storage.Storage] = true
			}

			view.Storages = append(view.Storages, StorageView{
				Node: resource.Node, Name: storage.Storage, Type: storage.Type,
				Total: storage.Total, Used: storage.Used,
			})

			backups, err := s.Proxmox.Backups(ctx, resource.Node, storage.Storage)
			if err != nil {
				s.Logger.Warn("could not list backups", "storage", storage.Storage, "error", err)
				continue
			}
			for _, backup := range backups {
				backupsByGuest[backup.VMID] = append(backupsByGuest[backup.VMID], BackupView{
					VolID:     backup.VolID,
					Storage:   storage.Storage,
					CreatedAt: time.Unix(backup.CTime, 0),
					Size:      backup.Size,
					Notes:     backup.Notes,
					Protected: bool(backup.Protected),
				})
			}
		}
	}

	job, err := s.homelabJob(ctx)
	if err != nil {
		return View{}, err
	}
	view.Job = jobView(job, view.Storages)

	for _, resource := range resources {
		if (resource.Type != "lxc" && resource.Type != "qemu") || resource.Template != 0 {
			continue
		}

		backups := backupsByGuest[resource.VMID]
		if backups == nil {
			backups = []BackupView{}
		}
		slices.SortFunc(backups, func(a, b BackupView) int { return b.CreatedAt.Compare(a.CreatedAt) })

		view.Guests = append(view.Guests, GuestView{
			VMID:     resource.VMID,
			Name:     resource.Name,
			Type:     resource.Type,
			Status:   resource.Status,
			Included: view.Job.Exists && view.Job.Enabled && !slices.Contains(view.Job.Exclude, resource.VMID),
			Self:     resource.VMID == s.SelfVMID && s.SelfVMID != 0,
			Backups:  backups,
		})
	}
	slices.SortFunc(view.Guests, func(a, b GuestView) int { return a.VMID - b.VMID })

	return view, nil
}

func jobView(job *proxmox.BackupJob, storages []StorageView) JobView {
	if job == nil {
		view := JobView{BackupJob: DefaultJob}
		view.Days = []string{}
		view.Exclude = []int{}
		for _, storage := range storages {
			if storage.Name == "local" || view.Storage == "" {
				view.Storage = storage.Name
			}
		}
		return view
	}

	keep := job.Retention()
	view := JobView{
		BackupJob: agent.BackupJob{
			Enabled:     job.IsEnabled(),
			Days:        []string{},
			Storage:     job.Storage,
			Exclude:     parseVMIDs(job.Exclude),
			KeepDaily:   keep["keep-daily"],
			KeepWeekly:  keep["keep-weekly"],
			KeepMonthly: keep["keep-monthly"],
		},
		Exists: true,
	}
	if job.NextRun > 0 {
		view.NextRun = time.Unix(job.NextRun, 0)
	}

	match := schedulePattern.FindStringSubmatch(strings.TrimSpace(job.Schedule))
	if match == nil {
		view.Custom = job.Schedule
		view.Hour = DefaultJob.Hour
		return view
	}
	if match[1] != "" {
		view.Days = strings.Split(match[1], ",")
	}
	view.Hour, _ = strconv.Atoi(match[2])
	view.Minute, _ = strconv.Atoi(match[3])

	return view
}
