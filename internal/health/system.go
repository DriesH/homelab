package health

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"homelab/internal/agent"
	"homelab/internal/proxmox"
)

const (
	wearoutLimit      = 90
	storageLimit      = 0.9
	healthySMART      = "PASSED"
	healthySCSI       = "OK"
	healthyPool       = "ONLINE"
	unknownDiskHealth = "UNKNOWN"
)

type DiskView struct {
	Node    string `json:"node"`
	DevPath string `json:"devPath"`
	Model   string `json:"model"`
	Serial  string `json:"serial"`
	Size    int64  `json:"size"`
	Type    string `json:"type"`
	Used    string `json:"used"`
	Health  string `json:"health"`
	// Wearout is the percent of SSD life that is used, nil when unknown.
	Wearout *int   `json:"wearout"`
	Problem string `json:"problem,omitempty"`
}

type PoolView struct {
	Node    string `json:"node"`
	Name    string `json:"name"`
	Health  string `json:"health"`
	Size    int64  `json:"size"`
	Alloc   int64  `json:"alloc"`
	Frag    int    `json:"frag"`
	Problem string `json:"problem,omitempty"`
}

type StorageView struct {
	Node    string `json:"node"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Used    int64  `json:"used"`
	Total   int64  `json:"total"`
	Problem string `json:"problem,omitempty"`
}

type ShareView struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	FSType  string `json:"fsType"`
	Mounted bool   `json:"mounted"`
	Size    int64  `json:"size"`
	Used    int64  `json:"used"`
	Error   string `json:"error,omitempty"`
	Problem string `json:"problem,omitempty"`
	// Role is set for the mounts behind the media folder, see agent.Mount.
	Role string `json:"role,omitempty"`
}

type system struct {
	disks   []DiskView
	pools   []PoolView
	storage []StorageView
	shares  []ShareView
	// errors are per node, so one broken node doesn't hide the others.
	errors []string
}

func newSystem() system {
	return system{disks: []DiskView{}, pools: []PoolView{}, storage: []StorageView{}, shares: []ShareView{}, errors: []string{}}
}

func (s *Service) checkSystem(ctx context.Context) system {
	result := newSystem()

	if s.Agent != nil {
		mounts, err := s.Agent.Mounts(ctx)
		if err != nil {
			s.Logger.Warn("could not list network shares", "error", err)
			result.errors = append(result.errors, "The host agent is not reachable, so network shares were not checked")
		}
		for _, mount := range mounts {
			result.shares = append(result.shares, shareView(mount))
		}
	}

	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		result.errors = append(result.errors, "could not reach Proxmox: "+err.Error())
		return result
	}

	for _, resource := range resources {
		switch {
		case resource.Type == "node" && resource.Status == "online":
			s.checkNode(ctx, resource.Node, &result)
		case resource.Type == "storage":
			result.storage = append(result.storage, storageView(resource))
		}
	}

	slices.SortFunc(result.disks, func(a, b DiskView) int {
		return strings.Compare(a.Node+a.DevPath, b.Node+b.DevPath)
	})
	slices.SortFunc(result.storage, func(a, b StorageView) int {
		return strings.Compare(a.Node+a.Name, b.Node+b.Name)
	})

	return result
}

func (s *Service) checkNode(ctx context.Context, node string, result *system) {
	disks, err := s.Proxmox.Disks(ctx, node)
	if err != nil {
		result.errors = append(result.errors, fmt.Sprintf("%s: could not read disks: %v", node, err))
	}
	for _, disk := range disks {
		result.disks = append(result.disks, diskView(node, disk))
	}

	// Hosts without ZFS return an error here, which is not a problem.
	pools, _ := s.Proxmox.ZFSPools(ctx, node)
	for _, pool := range pools {
		view := PoolView{
			Node: node, Name: pool.Name, Health: pool.Health,
			Size: pool.Size, Alloc: pool.Alloc, Frag: pool.Frag,
		}
		if pool.Health != healthyPool {
			view.Problem = fmt.Sprintf("ZFS pool %s on %s is %s", pool.Name, node, pool.Health)
		}
		result.pools = append(result.pools, view)
	}
}

func diskView(node string, disk proxmox.Disk) DiskView {
	view := DiskView{
		Node: node, DevPath: disk.DevPath, Model: disk.Model, Serial: disk.Serial,
		Size: disk.Size, Type: disk.Type, Used: disk.Used, Health: disk.Health,
		Wearout: parseWearout(disk.Wearout),
	}

	label := strings.TrimSpace(disk.DevPath + " " + disk.Model)
	switch {
	case disk.Health != "" && disk.Health != healthySMART && disk.Health != healthySCSI && disk.Health != unknownDiskHealth:
		view.Problem = fmt.Sprintf("Disk %s on %s: SMART says %s", label, node, disk.Health)
	case view.Wearout != nil && *view.Wearout >= wearoutLimit:
		view.Problem = fmt.Sprintf("Disk %s on %s is %d%% worn out", label, node, *view.Wearout)
	}

	return view
}

// parseWearout turns the life that is left, as Proxmox sends it, into the
// percent that is worn out. It returns nil for "N/A" and other text.
func parseWearout(raw json.RawMessage) *int {
	var left float64
	if json.Unmarshal(raw, &left) != nil {
		return nil
	}

	worn := min(max(100-int(left), 0), 100)
	return &worn
}

func storageView(resource proxmox.Resource) StorageView {
	view := StorageView{
		Node: resource.Node, Name: resource.Storage, Status: resource.Status,
		Used: resource.Disk, Total: resource.MaxDisk,
	}

	// Proxmox does not document the storage status values, so only alert on space.
	if resource.MaxDisk > 0 && float64(resource.Disk)/float64(resource.MaxDisk) >= storageLimit {
		percent := resource.Disk * 100 / resource.MaxDisk
		view.Problem = fmt.Sprintf("Storage %s on %s is %d%% full", resource.Storage, resource.Node, percent)
	}

	return view
}

func shareView(mount agent.Mount) ShareView {
	view := ShareView{
		Path: mount.Path, Source: mount.Source, FSType: mount.FSType,
		Mounted: mount.Mounted, Size: mount.Size, Used: mount.Used, Error: mount.Error, Role: mount.Role,
	}

	access, space := shareProblems(view)
	view.Problem = cmp.Or(access, space)

	return view
}

// shareProblems returns two problems, so that a share that comes back full
// still sends a new alert.
func shareProblems(share ShareView) (access, space string) {
	name := "Share"
	if share.Role == agent.RoleMediaCloud {
		name = "Cloud storage"
	}
	switch {
	case !share.Mounted:
		access = fmt.Sprintf("%s %s is not mounted at %s", name, share.Source, share.Path)
	case share.Error != "":
		access = fmt.Sprintf("%s %s at %s: %s", name, share.Source, share.Path, share.Error)
	}

	// With cloud storage, the media folder shows the space of its local part,
	// so only the media folder warns about it.
	if share.Role == agent.RoleMediaLocal {
		return access, ""
	}
	if share.Size > 0 && float64(share.Used)/float64(share.Size) >= storageLimit {
		space = fmt.Sprintf("Share %s is %d%% full", share.Source, share.Used*100/share.Size)
	}

	return access, space
}
