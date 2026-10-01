package proxmox

import (
	"context"
	"net/http"
	"testing"
)

func TestNodeUsageReadsMemoryOfTheNode(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve/rrddata" || r.URL.Query().Get("timeframe") != "day" || r.URL.Query().Get("cf") != "AVERAGE" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Write([]byte(`{"data":[{"time":60,"cpu":0.5,"memused":2048,"memtotal":4096,"netin":10,"netout":20},{"time":120}]}`))
	})

	points, err := client.NodeUsage(context.Background(), "pve", Day)
	if err != nil {
		t.Fatal(err)
	}

	if len(points) != 2 || *points[0].CPU != 0.5 || *points[0].Mem != 2048 || *points[0].MaxMem != 4096 || *points[0].NetOut != 20 {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].CPU != nil || points[1].Mem != nil {
		t.Fatalf("missing values should stay nil: %+v", points[1])
	}
}

func TestGuestUsageReadsMemoryOfTheGuest(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve/qemu/200/rrddata" || r.URL.Query().Get("timeframe") != "hour" {
			t.Errorf("unexpected request %s", r.URL)
		}
		w.Write([]byte(`{"data":[{"time":60,"cpu":0.1,"mem":512,"maxmem":1024}]}`))
	})

	points, err := client.GuestUsage(context.Background(), "pve", QEMU, 200, Hour)
	if err != nil {
		t.Fatal(err)
	}

	if len(points) != 1 || *points[0].Mem != 512 || *points[0].MaxMem != 1024 {
		t.Fatalf("unexpected points: %+v", points)
	}
}
