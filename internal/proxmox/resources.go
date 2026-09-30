package proxmox

import (
	"context"
	"fmt"
	"net/url"
)

// Resource is one row of /cluster/resources. Which fields are set depends on Type.
type Resource struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Node     string `json:"node"`
	VMID     int    `json:"vmid"`
	Name     string `json:"name"`
	Storage  string `json:"storage"`
	Status   string `json:"status"`
	Template int    `json:"template"`
	// Tags are separated by semicolons, like "homelab;media".
	Tags    string  `json:"tags"`
	CPU     float64 `json:"cpu"`
	MaxCPU  float64 `json:"maxcpu"`
	Mem     int64   `json:"mem"`
	MaxMem  int64   `json:"maxmem"`
	Disk    int64   `json:"disk"`
	MaxDisk int64   `json:"maxdisk"`
	Uptime  int64   `json:"uptime"`
}

type NodeStatus struct {
	PVEVersion string `json:"pveversion"`
}

type GuestType string

const (
	LXC  GuestType = "lxc"
	QEMU GuestType = "qemu"
)

type GuestAction string

const (
	Start    GuestAction = "start"
	Shutdown GuestAction = "shutdown"
	Reboot   GuestAction = "reboot"
	Stop     GuestAction = "stop"
)

func (c *Client) Resources(ctx context.Context) ([]Resource, error) {
	var resources []Resource
	err := c.get(ctx, "/cluster/resources", &resources)

	return resources, err
}

func (c *Client) NodeStatus(ctx context.Context, node string) (NodeStatus, error) {
	var status NodeStatus
	err := c.get(ctx, "/nodes/"+url.PathEscape(node)+"/status", &status)

	return status, err
}

// RunGuestAction starts a power action and returns the Proxmox task ID (UPID).
func (c *Client) RunGuestAction(ctx context.Context, node string, guestType GuestType, vmid int, action GuestAction) (string, error) {
	var upid string
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/%s", url.PathEscape(node), guestType, vmid, action)
	err := c.post(ctx, path, url.Values{}, &upid)

	return upid, err
}
