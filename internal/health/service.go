// Package health watches the disks, storage and services of the homelab and
// sends an alert when something breaks and again when it is fixed.
package health

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"homelab/internal/proxmox"
)

const (
	serviceInterval = time.Minute
	// Disks run SMART on each check, so check them less often.
	systemInterval = 10 * time.Minute
	// A service is down after this many failed checks in a row, so one slow answer doesn't alert.
	downAfter = 2
)

var ErrCheckNotFound = errors.New("service check not found")

type Proxmox interface {
	Resources(ctx context.Context) ([]proxmox.Resource, error)
	Disks(ctx context.Context, node string) ([]proxmox.Disk, error)
	ZFSPools(ctx context.Context, node string) ([]proxmox.ZFSPool, error)
}

type Options struct {
	DataDir string
	Proxmox Proxmox
	// Notify sends an alert, for example to Telegram.
	Notify func(ctx context.Context, text string)
	Logger *slog.Logger
	Now    func() time.Time
	// Probe tries to reach a service. Tests replace it.
	Probe func(ctx context.Context, check Check) error
}

type ServiceStatus string

const (
	StatusPending ServiceStatus = "pending"
	StatusUp      ServiceStatus = "up"
	StatusDown    ServiceStatus = "down"
)

type ServiceView struct {
	Check
	Status    ServiceStatus `json:"status"`
	LatencyMS int64         `json:"latencyMs"`
	Error     string        `json:"error,omitempty"`
	CheckedAt time.Time     `json:"checkedAt,omitzero"`
	// Since is when the status last changed.
	Since    time.Time `json:"since,omitzero"`
	failures int
}

type View struct {
	Services  []ServiceView `json:"services"`
	Disks     []DiskView    `json:"disks"`
	Pools     []PoolView    `json:"pools"`
	Storage   []StorageView `json:"storage"`
	Errors    []string      `json:"errors"`
	CheckedAt time.Time     `json:"checkedAt,omitzero"`
}

type Service struct {
	Options
	path string

	mu       sync.Mutex
	checks   []Check
	services map[string]*ServiceView
	system   system
	systemAt time.Time
	// alerts holds the problems we already sent, by key.
	alerts map[string]string

	// These stop two runs of the same check from overlapping.
	servicesRun sync.Mutex
	systemRun   sync.Mutex
}

func New(options Options) (*Service, error) {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Probe == nil {
		options.Probe = probe
	}
	if options.Notify == nil {
		options.Notify = func(context.Context, string) {}
	}

	service := &Service{
		Options:  options,
		path:     filepath.Join(options.DataDir, "health.json"),
		checks:   []Check{},
		services: map[string]*ServiceView{},
		system:   system{disks: []DiskView{}, pools: []PoolView{}, storage: []StorageView{}, errors: []string{}},
		alerts:   map[string]string{},
	}

	data, err := os.ReadFile(service.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		var saved struct {
			Checks []Check `json:"checks"`
		}
		if err := json.Unmarshal(data, &saved); err != nil {
			return nil, err
		}
		service.checks = saved.Checks
	}

	for _, check := range service.checks {
		service.services[check.ID] = &ServiceView{Check: check, Status: StatusPending}
	}

	return service, nil
}

// Run checks everything right away and then on an interval. It blocks until ctx ends.
func (s *Service) Run(ctx context.Context) {
	go s.CheckSystem(ctx)
	s.CheckServices(ctx)

	services := time.NewTicker(serviceInterval)
	defer services.Stop()
	system := time.NewTicker(systemInterval)
	defer system.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-services.C:
			s.CheckServices(ctx)
		case <-system.C:
			go s.CheckSystem(ctx)
		}
	}
}

func (s *Service) CheckServices(ctx context.Context) {
	s.servicesRun.Lock()
	defer s.servicesRun.Unlock()

	s.mu.Lock()
	checks := slices.Clone(s.checks)
	s.mu.Unlock()

	type result struct {
		check   Check
		err     error
		latency time.Duration
	}
	results := make(chan result, len(checks))
	for _, check := range checks {
		go func() {
			started := s.Now()
			err := s.Probe(ctx, check)
			results <- result{check: check, err: err, latency: s.Now().Sub(started)}
		}()
	}

	messages := []string{}
	for range checks {
		result := <-results
		if message := s.recordService(result.check, result.err, result.latency); message != "" {
			messages = append(messages, message)
		}
	}

	s.send(ctx, messages)
}

func (s *Service) recordService(check Check, err error, latency time.Duration) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	view, ok := s.services[check.ID]
	// The check was deleted while it ran.
	if !ok {
		return ""
	}

	now := s.Now()
	status := StatusUp
	view.Error = ""
	if err == nil {
		view.failures = 0
	} else {
		view.failures++
		view.Error = err.Error()
		status = view.Status
		if view.failures >= downAfter || view.Status == StatusPending {
			status = StatusDown
		}
	}

	if status != view.Status {
		view.Since = now
	}
	view.Status = status
	view.CheckedAt = now
	view.LatencyMS = latency.Milliseconds()

	problem := ""
	if status == StatusDown {
		problem = fmt.Sprintf("Service %s is down: %s", check.Name, view.Error)
	}

	return s.alert("service:"+check.ID, problem, fmt.Sprintf("Service %s is up again", check.Name))
}

func (s *Service) CheckSystem(ctx context.Context) {
	s.systemRun.Lock()
	defer s.systemRun.Unlock()

	result := s.checkSystem(ctx)

	s.mu.Lock()
	s.system = result
	s.systemAt = s.Now()

	messages := []string{}
	// When Proxmox is unreachable we know nothing, so keep the open alerts as they are.
	if len(result.disks)+len(result.pools)+len(result.storage) > 0 {
		problems := map[string]string{}
		for _, disk := range result.disks {
			problems["disk:"+disk.Node+":"+disk.DevPath] = disk.Problem
		}
		for _, pool := range result.pools {
			problems["pool:"+pool.Node+":"+pool.Name] = pool.Problem
		}
		for _, storage := range result.storage {
			problems["storage:"+storage.Node+":"+storage.Name] = storage.Problem
		}

		for key, problem := range problems {
			if message := s.alert(key, problem, ""); message != "" {
				messages = append(messages, message)
			}
		}

		// Forget alerts for disks or storage that are gone, unless a node could not be read.
		if len(result.errors) == 0 {
			for key := range s.alerts {
				if _, found := problems[key]; !found && !strings.HasPrefix(key, "service:") {
					delete(s.alerts, key)
				}
			}
		}
	}
	s.mu.Unlock()

	for _, message := range result.errors {
		s.Logger.Warn("health check", "error", message)
	}
	s.send(ctx, messages)
}

// alert returns the message to send when a problem starts or ends. An empty
// resolved message repeats the problem. Call it with s.mu held.
func (s *Service) alert(key, problem, resolved string) string {
	open, isOpen := s.alerts[key]

	switch {
	case problem != "" && !isOpen:
		s.alerts[key] = problem
		return "⚠️ " + problem
	case problem == "" && isOpen:
		delete(s.alerts, key)
		if resolved == "" {
			resolved = "Fixed: " + open
		}
		return "✅ " + resolved
	}

	return ""
}

func (s *Service) send(ctx context.Context, messages []string) {
	if len(messages) == 0 {
		return
	}

	slices.Sort(messages)
	s.Notify(ctx, strings.Join(messages, "\n"))
}

func (s *Service) Status() View {
	s.mu.Lock()
	defer s.mu.Unlock()

	services := make([]ServiceView, 0, len(s.checks))
	for _, check := range s.checks {
		services = append(services, *s.services[check.ID])
	}

	return View{
		Services:  services,
		Disks:     s.system.disks,
		Pools:     s.system.pools,
		Storage:   s.system.storage,
		Errors:    s.system.errors,
		CheckedAt: s.systemAt,
	}
}

func (s *Service) AddCheck(input CheckInput) (Check, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Target = strings.TrimSpace(input.Target)
	if err := input.validate(); err != nil {
		return Check{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.checks) >= maxChecks {
		return Check{}, fmt.Errorf("%w: you can add up to %d checks", ErrInvalidCheck, maxChecks)
	}

	check := Check{ID: newID(), Name: input.Name, Kind: input.Kind, Target: input.Target}
	checks := append(slices.Clone(s.checks), check)
	if err := s.save(checks); err != nil {
		return Check{}, err
	}

	s.checks = checks
	s.services[check.ID] = &ServiceView{Check: check, Status: StatusPending}

	return check, nil
}

func (s *Service) UpdateCheck(id string, input CheckInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.Target = strings.TrimSpace(input.Target)
	if err := input.validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index := slices.IndexFunc(s.checks, func(check Check) bool { return check.ID == id })
	if index < 0 {
		return ErrCheckNotFound
	}

	check := Check{ID: id, Name: input.Name, Kind: input.Kind, Target: input.Target}
	checks := slices.Clone(s.checks)
	checks[index] = check
	if err := s.save(checks); err != nil {
		return err
	}

	s.checks = checks
	// A new target starts over, and its old alert no longer applies.
	s.services[id] = &ServiceView{Check: check, Status: StatusPending}
	delete(s.alerts, "service:"+id)

	return nil
}

func (s *Service) DeleteCheck(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	checks := slices.DeleteFunc(slices.Clone(s.checks), func(check Check) bool { return check.ID == id })
	if len(checks) == len(s.checks) {
		return ErrCheckNotFound
	}
	if err := s.save(checks); err != nil {
		return err
	}

	s.checks = checks
	delete(s.services, id)
	delete(s.alerts, "service:"+id)

	return nil
}

// save writes the checks. Call it with s.mu held.
func (s *Service) save(checks []Check) error {
	data, err := json.MarshalIndent(map[string]any{"checks": checks}, "", "  ")
	if err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp")
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temp, s.path)
}

func newID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)

	return hex.EncodeToString(bytes)
}
