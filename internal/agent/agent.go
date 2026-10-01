// Package agent holds the protocol shared by the host agent and its client.
// They talk over a Unix socket that is bind-mounted into the manager LXC,
// so the agent is never reachable over the network.
package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"homelab/internal/release"
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

	// Reading Docker logs in a container with many apps takes a while.
	return &Client{http: &http.Client{Transport: transport, Timeout: 45 * time.Second}}
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

// MediaFolders returns the Directory storages of Proxmox, as suggestions for the media folder.
func (c *Client) MediaFolders(ctx context.Context) ([]MediaFolder, error) {
	var folders []MediaFolder
	err := c.do(ctx, http.MethodGet, "/v1/media-folders", nil, &folders)

	return folders, err
}

func (c *Client) SaveBackupJob(ctx context.Context, job BackupJob) error {
	return c.do(ctx, http.MethodPut, "/v1/backup-job", job, nil)
}

func (c *Client) Journal(ctx context.Context, query JournalQuery) ([]LogEntry, error) {
	values := url.Values{
		"vmid":     {strconv.Itoa(query.VMID)},
		"lines":    {strconv.Itoa(query.Lines)},
		"priority": {strconv.Itoa(query.Priority)},
	}
	var entries []LogEntry
	err := c.do(ctx, http.MethodGet, "/v1/logs/journal?"+values.Encode(), nil, &entries)

	return entries, err
}

func (c *Client) DockerLogs(ctx context.Context, vmid, lines int) (DockerLogs, error) {
	values := url.Values{"vmid": {strconv.Itoa(vmid)}, "lines": {strconv.Itoa(lines)}}
	var logs DockerLogs
	err := c.do(ctx, http.MethodGet, "/v1/logs/docker?"+values.Encode(), nil, &logs)

	return logs, err
}

// OpenConsole connects to the tty of a container through the agent.
func (c *Client) OpenConsole(ctx context.Context, vmid int) (*websocket.Conn, error) {
	conn, response, err := websocket.Dial(ctx, "ws://agent/v1/console/"+strconv.Itoa(vmid), &websocket.DialOptions{
		// No timeout: a console stays open as long as the user wants.
		HTTPClient: &http.Client{Transport: c.http.Transport},
	})
	if err != nil {
		if response != nil && response.Body != nil {
			message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
			response.Body.Close()
			if text := strings.TrimSpace(string(message)); text != "" {
				return nil, fmt.Errorf("%w: %s", ErrConsoleUnavailable, text)
			}
		}
		return nil, fmt.Errorf("host agent: %w", err)
	}

	return conn, nil
}

// SignatureHeader carries the bundle signature as base64 JSON.
const SignatureHeader = "X-Homelab-Signature"

func DecodeSignatureHeader(value string) (release.Signature, error) {
	var signature release.Signature
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(data, &signature) != nil {
		return release.Signature{}, errors.New("missing or invalid " + SignatureHeader + " header")
	}

	return signature, nil
}

// StartUpgrade streams a release bundle to the agent. It has no fixed timeout,
// because a bundle is tens of megabytes; ctx ends it.
func (c *Client) StartUpgrade(ctx context.Context, bundle io.Reader, signature release.Signature) error {
	data, err := json.Marshal(signature)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent/v1/upgrade", bundle)
	if err != nil {
		return err
	}
	request.Header.Set(SignatureHeader, base64.StdEncoding.EncodeToString(data))
	request.Header.Set("Content-Type", "application/gzip")

	client := &http.Client{Transport: c.http.Transport}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("host agent: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("host agent: %s", strings.TrimSpace(string(message)))
	}

	return nil
}

func (c *Client) InstallApp(ctx context.Context, app string, request InstallRequest) error {
	return c.do(ctx, http.MethodPost, "/v1/apps/"+url.PathEscape(app)+"/install", request, nil)
}

func (c *Client) RetryApp(ctx context.Context, app string) error {
	return c.do(ctx, http.MethodPost, "/v1/apps/"+url.PathEscape(app)+"/retry", nil, nil)
}

// AppContainerRequest names the container of an app to update or remove.
type AppContainerRequest struct {
	VMID int `json:"vmid"`
}

func (c *Client) UpdateApp(ctx context.Context, app string, vmid int) error {
	return c.do(ctx, http.MethodPost, "/v1/apps/"+url.PathEscape(app)+"/update", AppContainerRequest{VMID: vmid}, nil)
}

func (c *Client) RemoveApp(ctx context.Context, app string, vmid int) error {
	return c.do(ctx, http.MethodPost, "/v1/apps/"+url.PathEscape(app)+"/remove", AppContainerRequest{VMID: vmid}, nil)
}

// VPNRequest gives the media stack in container VMID new VPN settings.
type VPNRequest struct {
	VMID int `json:"vmid"`
	VPNSettings
}

// VPNCountries returns the VPN countries of the media stack in container vmid.
func (c *Client) VPNCountries(ctx context.Context, vmid int) (string, error) {
	var settings VPNSettings
	err := c.do(ctx, http.MethodGet, "/v1/apps/media/vpn?vmid="+strconv.Itoa(vmid), nil, &settings)

	return settings.Countries, err
}

func (c *Client) ChangeVPN(ctx context.Context, vmid int, settings VPNSettings) error {
	return c.do(ctx, http.MethodPut, "/v1/apps/media/vpn", VPNRequest{VMID: vmid, VPNSettings: settings}, nil)
}

// SavedAppAnswers returns the answers of the last failed install without secrets, or nil.
func (c *Client) SavedAppAnswers(ctx context.Context, app string) (*SavedAnswers, error) {
	var saved *SavedAnswers
	err := c.do(ctx, http.MethodGet, "/v1/apps/"+url.PathEscape(app)+"/answers", nil, &saved)

	return saved, err
}

func (c *Client) ForgetAppAnswers(ctx context.Context, app string) error {
	return c.do(ctx, http.MethodDelete, "/v1/apps/"+url.PathEscape(app)+"/answers", nil, nil)
}

func (c *Client) AppInstallStatus(ctx context.Context) (AppInstallStatus, error) {
	var status AppInstallStatus
	err := c.do(ctx, http.MethodGet, "/v1/apps/install", nil, &status)

	return status, err
}

func (c *Client) UpgradeStatus(ctx context.Context) (UpgradeStatus, error) {
	var status UpgradeStatus
	err := c.do(ctx, http.MethodGet, "/v1/upgrade", nil, &status)

	return status, err
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

	if out == nil {
		return nil
	}

	return json.NewDecoder(response.Body).Decode(out)
}
