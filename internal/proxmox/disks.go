package proxmox

import (
	"context"
	"encoding/json"
	"net/url"
)

type Disk struct {
	DevPath string `json:"devpath"`
	Model   string `json:"model"`
	Serial  string `json:"serial"`
	Size    int64  `json:"size"`
	// Type is hdd, ssd, nvme or usb.
	Type string `json:"type"`
	// Health is the SMART result, like PASSED, OK or UNKNOWN.
	Health string `json:"health"`
	// Wearout is the percent of SSD life that is used. Proxmox sends "N/A" when it doesn't know.
	Wearout json.RawMessage `json:"wearout"`
	Used    string          `json:"used"`
}

type ZFSPool struct {
	Name   string `json:"name"`
	Health string `json:"health"`
	Size   int64  `json:"size"`
	Alloc  int64  `json:"alloc"`
	Free   int64  `json:"free"`
	Frag   int    `json:"frag"`
}

// Disks runs SMART on each disk, so it can take a few seconds.
func (c *Client) Disks(ctx context.Context, node string) ([]Disk, error) {
	var disks []Disk
	err := c.get(ctx, "/nodes/"+url.PathEscape(node)+"/disks/list", &disks)

	return disks, err
}

func (c *Client) ZFSPools(ctx context.Context, node string) ([]ZFSPool, error) {
	var pools []ZFSPool
	err := c.get(ctx, "/nodes/"+url.PathEscape(node)+"/disks/zfs", &pools)

	return pools, err
}
