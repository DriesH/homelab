package proxmox

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Snapshot struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// SnapTime is a Unix timestamp. The "current" pseudo snapshot has none.
	SnapTime int64 `json:"snaptime"`
}

func containerPath(node string, vmid int) string {
	return fmt.Sprintf("/nodes/%s/lxc/%d", url.PathEscape(node), vmid)
}

// SnapshotSupported reports whether every volume of the container can be snapshotted.
func (c *Client) SnapshotSupported(ctx context.Context, node string, vmid int) (bool, error) {
	var feature struct {
		HasFeature int `json:"hasFeature"`
	}
	err := c.get(ctx, containerPath(node, vmid)+"/feature?feature=snapshot", &feature)

	return feature.HasFeature == 1, err
}

func (c *Client) Snapshots(ctx context.Context, node string, vmid int) ([]Snapshot, error) {
	var snapshots []Snapshot
	err := c.get(ctx, containerPath(node, vmid)+"/snapshot", &snapshots)

	return snapshots, err
}

func (c *Client) CreateSnapshot(ctx context.Context, node string, vmid int, name, description string) error {
	var upid string
	form := url.Values{"snapname": {name}, "description": {description}}
	if err := c.post(ctx, containerPath(node, vmid)+"/snapshot", form, &upid); err != nil {
		return err
	}

	return c.WaitTask(ctx, node, upid)
}

// RollbackSnapshot stops the container, rolls it back and starts it again.
func (c *Client) RollbackSnapshot(ctx context.Context, node string, vmid int, name string) error {
	var upid string
	path := containerPath(node, vmid) + "/snapshot/" + url.PathEscape(name) + "/rollback"
	if err := c.post(ctx, path, url.Values{"start": {"1"}}, &upid); err != nil {
		return err
	}

	return c.WaitTask(ctx, node, upid)
}

func (c *Client) DeleteSnapshot(ctx context.Context, node string, vmid int, name string) error {
	var upid string
	if err := c.do(ctx, http.MethodDelete, containerPath(node, vmid)+"/snapshot/"+url.PathEscape(name), nil, &upid); err != nil {
		return err
	}

	return c.WaitTask(ctx, node, upid)
}

// WaitTask polls a Proxmox task until it stops, and fails when its exit status isn't OK.
func (c *Client) WaitTask(ctx context.Context, node, upid string) error {
	path := "/nodes/" + url.PathEscape(node) + "/tasks/" + url.PathEscape(upid) + "/status"

	for {
		var task struct {
			Status     string `json:"status"`
			ExitStatus string `json:"exitstatus"`
		}
		if err := c.get(ctx, path, &task); err != nil {
			return err
		}

		if task.Status == "stopped" {
			if task.ExitStatus != "OK" {
				return fmt.Errorf("proxmox task failed: %s", task.ExitStatus)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
