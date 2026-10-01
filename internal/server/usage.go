package server

import (
	"net/http"
	"slices"
	"strconv"

	"homelab/internal/proxmox"
)

func (s *server) nodeUsage(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("node")
	timeframe := proxmox.Timeframe(r.URL.Query().Get("timeframe"))
	if !nodeName.MatchString(node) || !slices.Contains(proxmox.Timeframes, timeframe) {
		writeError(w, http.StatusBadRequest, "invalid node or timeframe")
		return
	}

	points, err := s.Proxmox.NodeUsage(r.Context(), node, timeframe)
	s.writeUsage(w, points, err)
}

func (s *server) guestUsage(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("node")
	guestType := proxmox.GuestType(r.PathValue("type"))
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	timeframe := proxmox.Timeframe(r.URL.Query().Get("timeframe"))

	valid := err == nil && vmid > 0 &&
		nodeName.MatchString(node) &&
		slices.Contains([]proxmox.GuestType{proxmox.LXC, proxmox.QEMU}, guestType) &&
		slices.Contains(proxmox.Timeframes, timeframe)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid guest or timeframe")
		return
	}

	points, err := s.Proxmox.GuestUsage(r.Context(), node, guestType, vmid, timeframe)
	s.writeUsage(w, points, err)
}

func (s *server) writeUsage(w http.ResponseWriter, points []proxmox.UsagePoint, err error) {
	if err != nil {
		s.Logger.Error("proxmox usage", "error", err)
		writeError(w, http.StatusBadGateway, "could not read the usage history from Proxmox")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"points": downsample(points, maxUsagePoints)})
}

// maxUsagePoints keeps the charts readable: Proxmox returns 1440 points for a month or a year.
const maxUsagePoints = 240

// downsample averages neighbouring points until there are at most limit points.
func downsample(points []proxmox.UsagePoint, limit int) []proxmox.UsagePoint {
	size := (len(points) + limit - 1) / limit
	if size <= 1 {
		return points
	}

	result := make([]proxmox.UsagePoint, 0, limit)
	for start := 0; start < len(points); start += size {
		bucket := points[start:min(start+size, len(points))]
		result = append(result, proxmox.UsagePoint{
			Time:   bucket[0].Time,
			CPU:    average(bucket, func(point proxmox.UsagePoint) *float64 { return point.CPU }),
			Mem:    average(bucket, func(point proxmox.UsagePoint) *float64 { return point.Mem }),
			MaxMem: average(bucket, func(point proxmox.UsagePoint) *float64 { return point.MaxMem }),
			NetIn:  average(bucket, func(point proxmox.UsagePoint) *float64 { return point.NetIn }),
			NetOut: average(bucket, func(point proxmox.UsagePoint) *float64 { return point.NetOut }),
		})
	}

	return result
}

// average skips missing values, and is nil when the bucket has none.
func average(bucket []proxmox.UsagePoint, value func(proxmox.UsagePoint) *float64) *float64 {
	sum, count := 0.0, 0
	for _, point := range bucket {
		if v := value(point); v != nil {
			sum += *v
			count++
		}
	}
	if count == 0 {
		return nil
	}

	result := sum / float64(count)
	return &result
}
