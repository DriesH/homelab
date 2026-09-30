package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"homelab/internal/config"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New(config.Proxmox{URL: server.URL, TokenID: "homelab@pve!manager", TokenSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func TestResourcesSendsTokenAndDecodesData(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "PVEAPIToken=homelab@pve!manager=secret" {
			t.Errorf("unexpected auth header %q", got)
		}
		w.Write([]byte(`{"data":[{"id":"lxc/101","type":"lxc","node":"pve","vmid":101,"name":"jellyfin","status":"running","cpu":0.25}]}`))
	})

	resources, err := client.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(resources) != 1 || resources[0].Name != "jellyfin" || resources[0].CPU != 0.25 {
		t.Fatalf("unexpected resources: %+v", resources)
	}
}

func TestRunGuestActionPostsToStatusEndpoint(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api2/json/nodes/pve/lxc/101/status/reboot" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"data":"UPID:pve:0001"}`))
	})

	upid, err := client.RunGuestAction(context.Background(), "pve", LXC, 101, Reboot)
	if err != nil {
		t.Fatal(err)
	}
	if upid != "UPID:pve:0001" {
		t.Fatalf("unexpected upid %q", upid)
	}
}

func TestErrorStatusIsReturned(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "permission check failed", http.StatusForbidden)
	})

	if _, err := client.Resources(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
