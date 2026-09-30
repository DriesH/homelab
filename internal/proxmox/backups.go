package proxmox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Flag reads the booleans that Proxmox sends as 0/1, "1" or true.
type Flag bool

func (f *Flag) UnmarshalJSON(data []byte) error {
	value := strings.Trim(string(data), `"`)
	*f = Flag(value == "1" || value == "true")
	return nil
}

type BackupJob struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Storage  string `json:"storage"`
	All      Flag   `json:"all"`
	VMID     string `json:"vmid"`
	Exclude  string `json:"exclude"`
	// Enabled is missing when the job is on.
	Enabled      *Flag           `json:"enabled"`
	PruneBackups json.RawMessage `json:"prune-backups"`
	NextRun      int64           `json:"next-run"`
}

func (j BackupJob) IsEnabled() bool {
	return j.Enabled == nil || bool(*j.Enabled)
}

// Retention returns the keep-* options, whether Proxmox sends them as an object or a string.
func (j BackupJob) Retention() map[string]int {
	keep := map[string]int{}

	var object map[string]json.RawMessage
	if json.Unmarshal(j.PruneBackups, &object) == nil {
		for key, raw := range object {
			value, _ := strconv.Atoi(strings.Trim(string(raw), `"`))
			keep[key] = value
		}
		return keep
	}

	var text string
	if json.Unmarshal(j.PruneBackups, &text) == nil {
		for part := range strings.SplitSeq(text, ",") {
			key, value, _ := strings.Cut(part, "=")
			keep[key], _ = strconv.Atoi(value)
		}
	}

	return keep
}

type BackupStorage struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
	Active  Flag   `json:"active"`
	Shared  Flag   `json:"shared"`
	Total   int64  `json:"total"`
	Used    int64  `json:"used"`
	Avail   int64  `json:"avail"`
}

type Backup struct {
	VolID     string `json:"volid"`
	VMID      int    `json:"vmid"`
	CTime     int64  `json:"ctime"`
	Size      int64  `json:"size"`
	Notes     string `json:"notes"`
	Protected Flag   `json:"protected"`
	// Format is like tar.zst for containers and vma.zst for VMs.
	Format string `json:"format"`
}

type Task struct {
	UPID      string `json:"upid"`
	Type      string `json:"type"`
	ID        string `json:"id"`
	User      string `json:"user"`
	Status    string `json:"status"`
	StartTime int64  `json:"starttime"`
	EndTime   int64  `json:"endtime"`
}

func (c *Client) BackupJobs(ctx context.Context) ([]BackupJob, error) {
	var jobs []BackupJob
	err := c.get(ctx, "/cluster/backup", &jobs)

	return jobs, err
}

func (c *Client) BackupStorages(ctx context.Context, node string) ([]BackupStorage, error) {
	var storages []BackupStorage
	err := c.get(ctx, "/nodes/"+url.PathEscape(node)+"/storage?content=backup&enabled=1", &storages)

	return storages, err
}

// ContainerStorages returns the storages that can hold container disks.
func (c *Client) ContainerStorages(ctx context.Context, node string) ([]BackupStorage, error) {
	var storages []BackupStorage
	err := c.get(ctx, "/nodes/"+url.PathEscape(node)+"/storage?content=rootdir&enabled=1", &storages)

	return storages, err
}

type Interface struct {
	Name string `json:"name"`
	// Inet is the IPv4 address with its prefix, like 192.168.1.50/24.
	Inet string `json:"inet"`
}

// ContainerInterfaces returns the network interfaces of a running container.
func (c *Client) ContainerInterfaces(ctx context.Context, node string, vmid int) ([]Interface, error) {
	var interfaces []Interface
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/lxc/%d/interfaces", url.PathEscape(node), vmid), &interfaces)

	return interfaces, err
}

func (c *Client) Backups(ctx context.Context, node, storage string) ([]Backup, error) {
	var backups []Backup
	path := fmt.Sprintf("/nodes/%s/storage/%s/content?content=backup", url.PathEscape(node), url.PathEscape(storage))
	err := c.get(ctx, path, &backups)

	return backups, err
}

// BackupTasks returns the last vzdump tasks on a node, newest first.
func (c *Client) BackupTasks(ctx context.Context, node string, limit int) ([]Task, error) {
	var tasks []Task
	path := fmt.Sprintf("/nodes/%s/tasks?typefilter=vzdump&source=archive&limit=%d", url.PathEscape(node), limit)
	err := c.get(ctx, path, &tasks)

	return tasks, err
}

// Tasks returns the last finished tasks of all types on a node, newest first.
func (c *Client) Tasks(ctx context.Context, node string, limit int) ([]Task, error) {
	var tasks []Task
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/tasks?source=archive&limit=%d", url.PathEscape(node), limit), &tasks)

	return tasks, err
}

// TaskLog returns the log lines of a task.
func (c *Client) TaskLog(ctx context.Context, node, upid string, limit int) ([]string, error) {
	var lines []struct {
		N int    `json:"n"`
		T string `json:"t"`
	}
	path := fmt.Sprintf("/nodes/%s/tasks/%s/log?limit=%d", url.PathEscape(node), url.PathEscape(upid), limit)
	if err := c.get(ctx, path, &lines); err != nil {
		return nil, err
	}

	text := make([]string, 0, len(lines))
	for _, line := range lines {
		text = append(text, line.T)
	}

	return text, nil
}

// BackupGuest starts a snapshot backup of one guest and returns the task ID.
func (c *Client) BackupGuest(ctx context.Context, node string, vmid int, storage string) (string, error) {
	var upid string
	err := c.post(ctx, "/nodes/"+url.PathEscape(node)+"/vzdump", url.Values{
		"vmid":           {strconv.Itoa(vmid)},
		"storage":        {storage},
		"mode":           {"snapshot"},
		"compress":       {"zstd"},
		"notes-template": {"{{guestname}}"},
	}, &upid)

	return upid, err
}

// GuestStorage returns the storage of a container's root disk, or "" for a VM.
func (c *Client) GuestStorage(ctx context.Context, node string, guestType GuestType, vmid int) (string, error) {
	if guestType != LXC {
		return "", nil
	}

	var config struct {
		RootFS string `json:"rootfs"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/lxc/%d/config", url.PathEscape(node), vmid), &config); err != nil {
		return "", err
	}

	storage, _, found := strings.Cut(config.RootFS, ":")
	if !found {
		return "", fmt.Errorf("container %d has no root disk on a storage", vmid)
	}

	return storage, nil
}

// RestoreGuest overwrites a stopped guest with a backup and returns the task ID.
// A container is restored to the storage of its current root disk; a VM to its
// original storages.
func (c *Client) RestoreGuest(ctx context.Context, node string, guestType GuestType, vmid int, volid, storage string) (string, error) {
	form := url.Values{"vmid": {strconv.Itoa(vmid)}, "force": {"1"}}
	switch guestType {
	case LXC:
		form.Set("ostemplate", volid)
		form.Set("restore", "1")
		form.Set("storage", storage)
	case QEMU:
		form.Set("archive", volid)
	default:
		return "", fmt.Errorf("unknown guest type %q", guestType)
	}

	var upid string
	err := c.post(ctx, fmt.Sprintf("/nodes/%s/%s", url.PathEscape(node), guestType), form, &upid)

	return upid, err
}

func (c *Client) DeleteBackup(ctx context.Context, node, storage, volid string) error {
	var upid string
	path := fmt.Sprintf("/nodes/%s/storage/%s/content/%s", url.PathEscape(node), url.PathEscape(storage), url.PathEscape(volid))

	return c.do(ctx, http.MethodDelete, path, nil, &upid)
}
