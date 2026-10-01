package proxmox

import (
	"context"
	"fmt"
	"net/url"
)

type Timeframe string

const (
	Hour  Timeframe = "hour"
	Day   Timeframe = "day"
	Week  Timeframe = "week"
	Month Timeframe = "month"
	Year  Timeframe = "year"
)

var Timeframes = []Timeframe{Hour, Day, Week, Month, Year}

// UsagePoint is one averaged sample of the Proxmox RRD statistics.
// A value is nil when Proxmox has no data for it, like when a guest was stopped.
type UsagePoint struct {
	Time int64 `json:"time"`
	// CPU is the share of all cores, from 0 to 1.
	CPU    *float64 `json:"cpu"`
	Mem    *float64 `json:"mem"`
	MaxMem *float64 `json:"maxMem"`
	// NetIn and NetOut are in bytes per second.
	NetIn  *float64 `json:"netIn"`
	NetOut *float64 `json:"netOut"`
}

// rrdRow has the fields of both node and guest rows: nodes use memused and memtotal, guests mem and maxmem.
type rrdRow struct {
	Time     int64    `json:"time"`
	CPU      *float64 `json:"cpu"`
	Mem      *float64 `json:"mem"`
	MaxMem   *float64 `json:"maxmem"`
	MemUsed  *float64 `json:"memused"`
	MemTotal *float64 `json:"memtotal"`
	NetIn    *float64 `json:"netin"`
	NetOut   *float64 `json:"netout"`
}

func (c *Client) NodeUsage(ctx context.Context, node string, timeframe Timeframe) ([]UsagePoint, error) {
	return c.usage(ctx, fmt.Sprintf("/nodes/%s/rrddata", url.PathEscape(node)), timeframe)
}

func (c *Client) GuestUsage(ctx context.Context, node string, guestType GuestType, vmid int, timeframe Timeframe) ([]UsagePoint, error) {
	return c.usage(ctx, fmt.Sprintf("/nodes/%s/%s/%d/rrddata", url.PathEscape(node), guestType, vmid), timeframe)
}

func (c *Client) usage(ctx context.Context, path string, timeframe Timeframe) ([]UsagePoint, error) {
	var rows []rrdRow
	if err := c.get(ctx, path+"?"+url.Values{"timeframe": {string(timeframe)}, "cf": {"AVERAGE"}}.Encode(), &rows); err != nil {
		return nil, err
	}

	points := make([]UsagePoint, 0, len(rows))
	for _, row := range rows {
		point := UsagePoint{Time: row.Time, CPU: row.CPU, Mem: row.Mem, MaxMem: row.MaxMem, NetIn: row.NetIn, NetOut: row.NetOut}
		if row.MemUsed != nil || row.MemTotal != nil {
			point.Mem, point.MaxMem = row.MemUsed, row.MemTotal
		}
		points = append(points, point)
	}

	return points, nil
}
