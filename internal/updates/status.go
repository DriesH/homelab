package updates

import (
	"context"
	"errors"
	"slices"
	"time"
)

var ErrRunNotFound = errors.New("run not found")

type GuestView struct {
	Target
	Status     string `json:"status"`
	AutoUpdate bool   `json:"autoUpdate"`
	Self       bool   `json:"self"`
}

type TelegramView struct {
	Configured bool   `json:"configured"`
	ChatID     string `json:"chatId"`
}

type View struct {
	// Busy describes the running operation. Empty means idle.
	Busy     string       `json:"busy"`
	Schedule Schedule     `json:"schedule"`
	NextRun  *time.Time   `json:"nextRun"`
	Telegram TelegramView `json:"telegram"`
	Host     Target       `json:"host"`
	Guests   []GuestView  `json:"guests"`
	// History has no logs, to keep polling light. Use Run for the log.
	History []Run `json:"history"`
}

func (s *Service) Status(ctx context.Context) (View, error) {
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return View{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	settings := s.state.Settings
	view := View{
		Busy:     s.busy,
		Schedule: settings.Schedule,
		Telegram: TelegramView{Configured: settings.Telegram.ChatID != "", ChatID: settings.Telegram.ChatID},
		Host:     s.state.Host.withLists(),
		Guests:   []GuestView{},
		History:  make([]Run, 0, len(s.state.History)),
	}
	if settings.Schedule.Enabled {
		next := nextSlot(settings.Schedule, s.Now())
		view.NextRun = &next
	}

	for _, resource := range containers(resources) {
		target := s.state.Guests[resource.VMID].withLists()
		target.VMID, target.Name = resource.VMID, resource.Name

		view.Guests = append(view.Guests, GuestView{
			Target:     target,
			Status:     resource.Status,
			AutoUpdate: resource.VMID != s.SelfVMID && !slices.Contains(settings.Excluded, resource.VMID),
			Self:       resource.VMID == s.SelfVMID,
		})
	}

	for _, run := range s.state.History {
		run.Log = ""
		view.History = append(view.History, run)
	}

	return view, nil
}

// Run returns one history entry with its log.
func (s *Service) Run(id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, run := range s.state.History {
		if run.ID == id {
			return run, nil
		}
	}

	return Run{}, ErrRunNotFound
}
