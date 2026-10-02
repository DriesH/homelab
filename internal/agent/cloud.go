package agent

import (
	"context"
	"net/http"
	"time"
)

type CloudAction string

const (
	CloudEnable  CloudAction = "enable"
	CloudDisable CloudAction = "disable"
)

// CloudStatus is the cloud storage on the host.
type CloudStatus struct {
	// Enabled is true when the media folder joins the local media with the cloud.
	Enabled bool   `json:"enabled"`
	Label   string `json:"label,omitempty"`
	// Job is the last time the agent turned the cloud storage on or off.
	Job *CloudJob `json:"job,omitempty"`
}

type CloudJob struct {
	Action     CloudAction  `json:"action"`
	State      UpgradeState `json:"state"`
	Message    string       `json:"message,omitempty"`
	Log        string       `json:"log,omitempty"`
	StartedAt  time.Time    `json:"startedAt,omitzero"`
	FinishedAt time.Time    `json:"finishedAt,omitzero"`
}

func (c *Client) CloudStatus(ctx context.Context) (CloudStatus, error) {
	var status CloudStatus
	err := c.do(ctx, http.MethodGet, "/v1/cloud", nil, &status)

	return status, err
}

// TestCloud connects to the storage. A refusal, like wrong keys, comes back
// as a RefusedError with the reason.
func (c *Client) TestCloud(ctx context.Context, config CloudConfig) error {
	return c.do(ctx, http.MethodPost, "/v1/cloud/test", config, nil)
}

// EnableCloud starts to move the media folder onto the cloud storage, in the background.
func (c *Client) EnableCloud(ctx context.Context, config CloudConfig) error {
	return c.do(ctx, http.MethodPost, "/v1/cloud/enable", config, nil)
}

// DisableCloud starts to bring all media back from the cloud, in the background.
func (c *Client) DisableCloud(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/cloud/disable", nil, nil)
}
