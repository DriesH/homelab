package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

type backup struct {
	VolID     string `json:"volid"`
	VMID      int    `json:"vmid"`
	CTime     int64  `json:"ctime"`
	Size      int64  `json:"size"`
	Notes     string `json:"notes"`
	Protected int    `json:"protected"`
	Format    string `json:"format"`
}

// fakeProxmox answers the Proxmox API calls of the manager with a small,
// fixed homelab: one node, the manager, Jellyfin, a media stack and a VM.
type fakeProxmox struct {
	mu        sync.Mutex
	backups   map[string][]backup
	snapshots map[string][]map[string]any
}

func newProxmox() http.Handler {
	day := time.Now().Add(-24 * time.Hour).Unix()
	proxmox := &fakeProxmox{
		backups: map[string][]backup{
			"local": {
				{"local:backup/vzdump-lxc-101-old.tar.zst", 101, day - 86400, 1_900_000_000, "jellyfin", 0, "tar.zst"},
				{"local:backup/vzdump-lxc-101-new.tar.zst", 101, day, 1_950_000_000, "jellyfin", 1, "tar.zst"},
				{"local:backup/vzdump-lxc-102-new.tar.zst", 102, day + 40, 3_400_000_000, "media", 0, "tar.zst"},
			},
		},
		snapshots: map[string][]map[string]any{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api2/json/cluster/backup", proxmox.backupJobs)
	mux.HandleFunc("/api2/json/cluster/resources", proxmox.resources)
	mux.HandleFunc("/api2/json/nodes/pve/status", func(w http.ResponseWriter, r *http.Request) {
		writeData(w, map[string]any{"pveversion": "pve-manager/9.2.0/dev"})
	})
	mux.HandleFunc("/api2/json/nodes/pve/", proxmox.node)

	return mux
}

func writeData(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": value})
}

func (p *fakeProxmox) resources(w http.ResponseWriter, r *http.Request) {
	resources := []map[string]any{
		{"type": "node", "node": "pve", "status": "online", "cpu": 0.18, "maxcpu": 8, "mem": 9_800_000_000, "maxmem": 34_359_738_368, "disk": 21_000_000_000, "maxdisk": 100_000_000_000, "uptime": 1_234_567},
		{"type": "lxc", "node": "pve", "vmid": 100, "name": "homelab", "status": "running", "tags": "homelab", "cpu": 0.01, "maxcpu": 1, "mem": 90_000_000, "maxmem": 536_870_912, "uptime": 3600},
		{"type": "lxc", "node": "pve", "vmid": 101, "name": "jellyfin", "status": "running", "cpu": 0.07, "maxcpu": 4, "mem": 1_300_000_000, "maxmem": 4_294_967_296, "uptime": 345_600},
		{"type": "lxc", "node": "pve", "vmid": 102, "name": "downloads", "status": "running", "cpu": 0.2, "maxcpu": 2, "mem": 2_300_000_000, "maxmem": 4_294_967_296, "uptime": 345_600},
		{"type": "qemu", "node": "pve", "vmid": 200, "name": "windows-test", "status": "stopped", "maxcpu": 4, "maxmem": 8_589_934_592},
		{"type": "storage", "node": "pve", "storage": "local", "status": "available", "disk": 41_000_000_000, "maxdisk": 98_000_000_000},
		{"type": "storage", "node": "pve", "storage": "local-lvm", "status": "available", "disk": 330_000_000_000, "maxdisk": 350_000_000_000},
		{"type": "storage", "node": "pve", "storage": "backups", "status": "unknown"},
	}
	// Without the Jellyfin of a community script, to try the Jellyfin install.
	if _, err := os.Stat(statePath("no-jellyfin")); err == nil {
		resources = slices.DeleteFunc(resources, func(resource map[string]any) bool { return resource["name"] == "jellyfin" })
	}
	// The fake agent "installs" apps from the Apps page.
	if _, err := os.Stat(statePath("jellyfin-installed")); err == nil {
		resources = append(resources, map[string]any{"type": "lxc", "node": "pve", "vmid": 140, "name": "jellyfin", "status": "running", "tags": "homelab;jellyfin", "maxcpu": 2, "maxmem": 2_147_483_648, "uptime": 60})
	}
	if _, err := os.Stat(statePath("media-installed")); err == nil {
		resources = append(resources, map[string]any{"type": "lxc", "node": "pve", "vmid": 130, "name": "media", "status": "running", "tags": "homelab;media", "maxcpu": 2, "maxmem": 4_294_967_296, "uptime": 60})
	}
	for _, resource := range resources {
		id := resource["type"].(string) + "/" + fmt.Sprint(resource["vmid"])
		switch resource["type"] {
		case "node":
			id = "node/pve"
		case "storage":
			id = "storage/pve/" + resource["storage"].(string)
		}
		resource["id"] = id
	}

	writeData(w, resources)
}

// backupJobs shows the job that the fake agent saved.
func (p *fakeProxmox) backupJobs(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(statePath("backup-job.json"))
	if err != nil {
		writeData(w, []any{})
		return
	}

	var job struct {
		Enabled                            bool
		Days                               []string
		Hour, Minute                       int
		Storage                            string
		Exclude                            []int
		KeepDaily, KeepWeekly, KeepMonthly int
	}
	json.Unmarshal(data, &job)

	schedule := fmt.Sprintf("%02d:%02d", job.Hour, job.Minute)
	if len(job.Days) > 0 && len(job.Days) < 7 {
		schedule = strings.Join(job.Days, ",") + " " + schedule
	}
	exclude := []string{}
	for _, vmid := range job.Exclude {
		exclude = append(exclude, fmt.Sprint(vmid))
	}
	enabled := 0
	if job.Enabled {
		enabled = 1
	}

	writeData(w, []map[string]any{{
		"id": "homelab-backup", "schedule": schedule, "storage": job.Storage, "all": 1, "enabled": enabled,
		"exclude": strings.Join(exclude, ","), "next-run": time.Now().Add(12 * time.Hour).Unix(),
		"prune-backups": map[string]any{
			"keep-daily": fmt.Sprint(job.KeepDaily), "keep-weekly": fmt.Sprint(job.KeepWeekly), "keep-monthly": fmt.Sprint(job.KeepMonthly),
		},
	}})
}

func (p *fakeProxmox) node(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api2/json/nodes/pve/")
	parts := strings.Split(path, "/")
	content := r.URL.Query().Get("content")

	p.mu.Lock()
	defer p.mu.Unlock()

	switch {
	case path == "storage" && content == "rootdir":
		writeData(w, []map[string]any{
			{"storage": "local-lvm", "type": "lvmthin", "active": 1},
			{"storage": "fast-zfs", "type": "zfspool", "active": 1},
		})
	case path == "storage" && content == "backup":
		writeData(w, []map[string]any{
			{"storage": "local", "type": "dir", "active": 1, "shared": 0, "total": 98_000_000_000, "used": 41_000_000_000},
			{"storage": "nas-backups", "type": "nfs", "active": 1, "shared": 1, "total": 7_900_000_000_000, "used": 2_100_000_000_000},
		})
	case len(parts) == 3 && parts[0] == "storage" && parts[2] == "content" && r.Method == http.MethodGet:
		writeData(w, p.backups[parts[1]])
	case len(parts) >= 4 && parts[0] == "storage" && parts[2] == "content" && r.Method == http.MethodDelete:
		volid := strings.Join(parts[3:], "/")
		kept := []backup{}
		for _, item := range p.backups[parts[1]] {
			if item.VolID != volid {
				kept = append(kept, item)
			}
		}
		p.backups[parts[1]] = kept
		writeData(w, "UPID:pve:delete")
	case path == "vzdump" && r.Method == http.MethodPost:
		r.ParseForm()
		var vmid int
		fmt.Sscan(r.Form.Get("vmid"), &vmid)
		storage := r.Form.Get("storage")
		volid := fmt.Sprintf("%s:backup/vzdump-lxc-%d-%d.tar.zst", storage, vmid, time.Now().Unix())
		p.backups[storage] = append(p.backups[storage], backup{volid, vmid, time.Now().Unix(), 1_200_000_000, "", 0, "tar.zst"})
		writeData(w, "UPID:pve:vzdump")
	case path == "lxc" && r.Method == http.MethodPost:
		r.ParseForm()
		log.Println("restore", r.Form.Get("ostemplate"), "to", r.Form.Get("vmid"))
		writeData(w, "UPID:pve:restore")
	case len(parts) == 3 && parts[0] == "lxc" && parts[2] == "config":
		// The installed apps are from an older release, so the Apps page offers an update.
		writeData(w, map[string]any{
			"rootfs":      "local-lvm:vm-" + parts[1] + "-disk-0,size=8G",
			"description": "Installed by Homelab\nhomelab-version: v0.7.1\n",
		})
	case len(parts) == 3 && parts[0] == "lxc" && parts[2] == "interfaces":
		var vmid int
		fmt.Sscan(parts[1], &vmid)
		writeData(w, []map[string]any{{"name": "lo", "inet": "127.0.0.1/8"}, {"name": "eth0", "inet": fmt.Sprintf("192.168.1.%d/24", vmid%200+20)}})
	case len(parts) >= 3 && parts[0] == "lxc" && parts[2] == "feature":
		writeData(w, map[string]any{"hasFeature": 1})
	case len(parts) == 3 && parts[2] == "snapshot" && r.Method == http.MethodGet:
		writeData(w, append(p.snapshots[parts[1]], map[string]any{"name": "current"}))
	case len(parts) == 3 && parts[2] == "snapshot" && r.Method == http.MethodPost:
		r.ParseForm()
		p.snapshots[parts[1]] = append(p.snapshots[parts[1]], map[string]any{"name": r.Form.Get("snapname"), "snaptime": time.Now().Unix()})
		writeData(w, "UPID:pve:snapshot")
	case len(parts) >= 4 && parts[2] == "snapshot":
		writeData(w, "UPID:pve:"+parts[len(parts)-1])
	case r.Method == http.MethodPost && strings.Contains(path, "/status/"):
		writeData(w, "UPID:pve:action")
	case path == "disks/list":
		writeData(w, []map[string]any{
			{"devpath": "/dev/nvme0n1", "model": "Samsung SSD 990 PRO 1TB", "serial": "S6Z1", "size": 1_000_204_886_016, "type": "nvme", "health": "PASSED", "wearout": 4, "used": "LVM"},
			{"devpath": "/dev/sda", "model": "WDC WD40EFRX", "serial": "WD-1", "size": 4_000_787_030_016, "type": "hdd", "health": "PASSED", "wearout": "N/A", "used": "ZFS"},
			{"devpath": "/dev/sdb", "model": "ST4000VN008", "serial": "ZD1", "size": 4_000_787_030_016, "type": "hdd", "health": "FAILED", "wearout": "N/A", "used": "ZFS"},
		})
	case path == "disks/zfs":
		writeData(w, []map[string]any{{"name": "tank", "health": "DEGRADED", "size": 3_985_729_650_688, "alloc": 1_200_000_000_000, "free": 2_785_729_650_688, "frag": 3, "dedup": 1.0}})
	case path == "tasks":
		now := time.Now().Unix()
		tasks := []map[string]any{
			{"upid": "UPID:pve:0001:vzdump::root@pam:", "type": "vzdump", "id": "", "user": "root@pam", "status": "job errors", "starttime": now - 7200, "endtime": now - 6900},
			{"upid": "UPID:pve:0002:vzsnapshot:101:homelab@pve!manager:", "type": "vzsnapshot", "id": "101", "user": "homelab@pve!manager", "status": "OK", "starttime": now - 3600, "endtime": now - 3590},
		}
		if r.URL.Query().Get("typefilter") == "vzdump" {
			tasks = tasks[:1]
		}
		writeData(w, tasks)
	case strings.HasPrefix(path, "tasks/") && strings.HasSuffix(path, "/log"):
		writeData(w, []map[string]any{
			{"n": 1, "t": "INFO: starting new backup job: vzdump --all 1 --storage local --mode snapshot"},
			{"n": 2, "t": "INFO: Starting Backup of VM 101 (lxc)"},
			{"n": 3, "t": "ERROR: Backup of VM 101 failed - no space left on device"},
			{"n": 4, "t": "INFO: Backup job finished with errors"},
		})
	case strings.HasPrefix(path, "tasks/"):
		writeData(w, map[string]any{"status": "stopped", "exitstatus": "OK"})
	default:
		log.Println("fake proxmox: no answer for", r.Method, path)
		http.NotFound(w, r)
	}
}
