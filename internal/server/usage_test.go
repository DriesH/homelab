package server

import (
	"math"
	"testing"

	"homelab/internal/proxmox"
)

func TestDownsampleAveragesAndSkipsMissingValues(t *testing.T) {
	value := func(v float64) *float64 { return &v }
	points := []proxmox.UsagePoint{
		{Time: 0, CPU: value(0.2)},
		{Time: 60, CPU: value(0.4)},
		{Time: 120, CPU: value(0.6)},
		{Time: 180},
		{Time: 240},
	}

	result := downsample(points, 3)

	if len(result) != 3 {
		t.Fatalf("expected 3 points, got %d", len(result))
	}
	if result[0].Time != 0 || math.Abs(*result[0].CPU-0.3) > 1e-9 {
		t.Errorf("unexpected first point: time %d cpu %v", result[0].Time, *result[0].CPU)
	}
	if result[1].Time != 120 || *result[1].CPU != 0.6 {
		t.Errorf("a missing value should not count in the average: %+v", result[1])
	}
	if result[2].CPU != nil {
		t.Errorf("a bucket without values should stay empty: %+v", result[2])
	}
	if got := downsample(points, 10); len(got) != len(points) {
		t.Errorf("short series should not change, got %d points", len(got))
	}
}
