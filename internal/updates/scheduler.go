package updates

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// RunScheduler starts the weekly run when it is due. It blocks until ctx ends.
func (s *Service) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	s.mu.Lock()
	schedule, lastRun := s.state.Settings.Schedule, s.state.LastScheduled
	s.mu.Unlock()

	slot, due := dueSlot(schedule, s.Now(), lastRun)
	if !due {
		return
	}

	// When something else is running, the next tick tries again.
	if err := s.start(ctx, "Weekly updates", s.scheduledRun); err != nil {
		return
	}

	s.mu.Lock()
	s.state.LastScheduled = slot
	err := s.state.save(s.path)
	s.mu.Unlock()
	if err != nil {
		s.Logger.Error("could not save update state", "error", err)
	}
}

func (s *Service) scheduledRun(ctx context.Context) {
	check := s.checkAll(ctx)
	if check.Status != RunSucceeded {
		s.notify(ctx, "❌ Weekly updates: the check failed. "+check.Message)
		return
	}

	s.mu.Lock()
	host := s.state.Host
	guests := make([]Target, 0, len(s.state.Guests))
	for _, guest := range s.state.Guests {
		guests = append(guests, guest)
	}
	excluded := slices.Clone(s.state.Settings.Excluded)
	s.mu.Unlock()

	slices.SortFunc(guests, func(a, b Target) int { return a.VMID - b.VMID })

	lines := []string{"🗓 Weekly updates"}
	updated := 0
	for _, guest := range guests {
		switch {
		case guest.updateCount() == 0 || guest.Error != "":
			continue
		case guest.VMID == s.SelfVMID:
			lines = append(lines, fmt.Sprintf("ℹ️ %s (this manager): %d updates. Install them from the dashboard.", guest.Name, guest.updateCount()))
			continue
		case slices.Contains(excluded, guest.VMID):
			lines = append(lines, fmt.Sprintf("⏸ %s: %d updates, skipped because auto-update is off.", guest.Name, guest.updateCount()))
			continue
		}

		lines = append(lines, runMessage(s.updateGuest(ctx, guest.VMID, true)))
		updated++
	}
	if updated == 0 {
		lines = append(lines, "Containers: nothing to update.")
	}

	switch {
	case host.Error != "":
		lines = append(lines, "⚠️ Proxmox host: the check failed. "+host.Error)
	case host.updateCount() > 0:
		lines = append(lines, fmt.Sprintf("🖥 Proxmox host: %d updates waiting. Install them from the dashboard.", host.updateCount()))
	}
	if host.RebootRequired {
		lines = append(lines, "🔁 Proxmox host: reboot it to use the new kernel.")
	}

	s.notify(ctx, strings.Join(lines, "\n"))
}
