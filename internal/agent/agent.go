// Package agent holds the protocol shared by the host agent and its client.
// They talk over a Unix socket that is bind-mounted into the manager LXC,
// so the agent is never reachable over the network.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Health struct {
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
}

type JobKind string

const (
	HostCheck   JobKind = "host-check"
	HostUpgrade JobKind = "host-upgrade"
	GuestCheck  JobKind = "guest-check"
	GuestUpdate JobKind = "guest-update"
)

type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

type JobRequest struct {
	Kind JobKind `json:"kind"`
	// VMID is the container for guest jobs.
	VMID int `json:"vmid,omitempty"`
}

type Package struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

type Image struct {
	Service string `json:"service"`
	Image   string `json:"image"`
}

type Job struct {
	ID         string    `json:"id"`
	Kind       JobKind   `json:"kind"`
	VMID       int       `json:"vmid,omitempty"`
	Status     JobStatus `json:"status"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	Log        string    `json:"log"`

	Packages []Package `json:"packages"`
	Images   []Image   `json:"images"`
	// Unsupported is set when the guest has no package manager we know.
	Unsupported bool `json:"unsupported,omitempty"`
	// RebootRequired is set for host jobs when a newer kernel is installed.
	RebootRequired bool `json:"rebootRequired,omitempty"`
}

type Client struct {
	http *http.Client
}

func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}

	return &Client{http: &http.Client{Transport: transport, Timeout: 10 * time.Second}}
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	err := c.do(ctx, http.MethodGet, "/v1/health", nil, &health)

	return health, err
}

func (c *Client) StartJob(ctx context.Context, request JobRequest) (Job, error) {
	var job Job
	err := c.do(ctx, http.MethodPost, "/v1/jobs", request, &job)

	return job, err
}

func (c *Client) Job(ctx context.Context, id string) (Job, error) {
	var job Job
	err := c.do(ctx, http.MethodGet, "/v1/jobs/"+id, nil, &job)

	return job, err
}

func (c *Client) Mounts(ctx context.Context) ([]Mount, error) {
	var mounts []Mount
	err := c.do(ctx, http.MethodGet, "/v1/mounts", nil, &mounts)

	return mounts, err
}

// RunJob starts a job and waits until it finishes.
func (c *Client) RunJob(ctx context.Context, request JobRequest) (Job, error) {
	job, err := c.StartJob(ctx, request)
	if err != nil {
		return Job{}, err
	}

	for job.Status == JobRunning {
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-time.After(2 * time.Second):
		}

		if job, err = c.Job(ctx, job.ID); err != nil {
			return Job{}, err
		}
	}

	return job, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, "http://agent"+path, reader)
	if err != nil {
		return err
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("host agent: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("host agent %s: %s %s", path, response.Status, strings.TrimSpace(string(message)))
	}

	return json.NewDecoder(response.Body).Decode(out)
}
