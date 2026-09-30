// Package agent holds the protocol shared by the host agent and its client.
// They talk over a Unix socket that is bind-mounted into the manager LXC,
// so the agent is never reachable over the network.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

type Health struct {
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
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

	return &Client{http: &http.Client{Transport: transport, Timeout: 5 * time.Second}}
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/v1/health", nil)
	if err != nil {
		return Health{}, err
	}

	response, err := c.http.Do(request)
	if err != nil {
		return Health{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("agent health: %s", response.Status)
	}

	var health Health
	err = json.NewDecoder(response.Body).Decode(&health)

	return health, err
}
