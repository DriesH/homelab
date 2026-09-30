package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed guest.sh
var guestScript string

const (
	maxLogBytes  = 256 * 1024
	maxJobs      = 50
	checkTimeout = 15 * time.Minute
	// Community-script updates can rebuild an app, which takes a while.
	updateTimeout = 90 * time.Minute
)

var (
	ErrBusy        = errors.New("another job is already running for this target")
	ErrInvalidJob  = errors.New("invalid job")
	ErrJobNotFound = errors.New("job not found")
)

// Runner runs jobs on the Proxmox host. Only one job runs per target at a time,
// because apt holds a lock.
type Runner struct {
	// command builds the process for a job. Tests replace it.
	command func(ctx context.Context, request JobRequest) (*exec.Cmd, error)
	// rebootRequired reports whether the host booted an older kernel than the newest one installed.
	rebootRequired func() bool

	mu    sync.Mutex
	jobs  []*Job
	busy  map[string]bool
	clock func() time.Time
}

func NewRunner() *Runner {
	return &Runner{
		command:        jobCommand,
		rebootRequired: kernelRebootRequired,
		busy:           map[string]bool{},
		clock:          time.Now,
	}
}

func (r *Runner) Start(request JobRequest) (Job, error) {
	if err := validate(request); err != nil {
		return Job{}, err
	}

	target := targetKey(request)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.busy[target] {
		return Job{}, ErrBusy
	}

	job := &Job{
		ID:        newJobID(),
		Kind:      request.Kind,
		VMID:      request.VMID,
		Status:    JobRunning,
		StartedAt: r.clock(),
	}
	r.busy[target] = true
	r.jobs = append(r.jobs, job)
	if len(r.jobs) > maxJobs {
		r.jobs = r.jobs[len(r.jobs)-maxJobs:]
	}

	go r.run(job, request, target)

	return *job, nil
}

func (r *Runner) Get(id string) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, job := range r.jobs {
		if job.ID == id {
			return *job, nil
		}
	}

	return Job{}, ErrJobNotFound
}

func (r *Runner) run(job *Job, request JobRequest, target string) {
	timeout := checkTimeout
	if request.Kind == HostUpgrade || request.Kind == GuestUpdate {
		timeout = updateTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	output, err := r.execute(ctx, request)
	result := parseOutput(output)
	rebootRequired := isHostJob(request.Kind) && err == nil && r.rebootRequired()

	r.mu.Lock()
	defer r.mu.Unlock()

	job.Log = tail(output, maxLogBytes)
	job.Packages = result.packages
	job.Images = result.images
	job.Unsupported = result.unsupported
	job.RebootRequired = rebootRequired
	job.FinishedAt = r.clock()
	job.Status = JobSucceeded
	if err != nil {
		job.Status = JobFailed
		job.Error = err.Error()
	}

	delete(r.busy, target)
}

func (r *Runner) execute(ctx context.Context, request JobRequest) (string, error) {
	cmd, err := r.command(ctx, request)
	if err != nil {
		return "", err
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()

	return output.String(), err
}

// jobCommand builds the real command. The script is a constant, and the VMID
// is a validated number, so no user input reaches a shell.
func jobCommand(ctx context.Context, request JobRequest) (*exec.Cmd, error) {
	mode := "check"
	if request.Kind == HostUpgrade || request.Kind == GuestUpdate {
		mode = "update"
	}

	if isHostJob(request.Kind) {
		cmd := exec.CommandContext(ctx, "sh", "-c", guestScript, "guest.sh", mode)
		// On the host we only want apt, not Docker or community-script updates.
		cmd.Env = append(os.Environ(), "HOMELAB_APT_ONLY=1")
		return cmd, nil
	}

	vmid := strconv.Itoa(request.VMID)
	status, err := exec.CommandContext(ctx, "pct", "status", vmid).Output()
	if err != nil {
		return nil, fmt.Errorf("container %s not found", vmid)
	}
	if strings.TrimSpace(string(status)) != "status: running" {
		return nil, fmt.Errorf("container %s is not running", vmid)
	}

	return exec.CommandContext(ctx, "pct", "exec", vmid, "--", "sh", "-c", guestScript, "guest.sh", mode), nil
}

func validate(request JobRequest) error {
	switch request.Kind {
	case HostCheck, HostUpgrade:
		if request.VMID != 0 {
			return fmt.Errorf("%w: host jobs take no vmid", ErrInvalidJob)
		}
	case GuestCheck, GuestUpdate:
		// Proxmox VMIDs start at 100.
		if request.VMID < 100 {
			return fmt.Errorf("%w: vmid must be 100 or higher", ErrInvalidJob)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidJob, request.Kind)
	}

	return nil
}

func isHostJob(kind JobKind) bool {
	return kind == HostCheck || kind == HostUpgrade
}

func targetKey(request JobRequest) string {
	if isHostJob(request.Kind) {
		return "host"
	}

	return "guest:" + strconv.Itoa(request.VMID)
}

type scriptResult struct {
	packages    []Package
	images      []Image
	unsupported bool
}

func parseOutput(output string) scriptResult {
	result := scriptResult{packages: []Package{}, images: []Image{}}

	for line := range strings.Lines(output) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "HOMELAB" {
			continue
		}

		switch {
		case fields[1] == "package" && len(fields) == 5:
			result.packages = append(result.packages, Package{Name: fields[2], From: fields[3], To: fields[4]})
		case fields[1] == "image" && len(fields) == 4:
			result.images = append(result.images, Image{Service: fields[2], Image: fields[3]})
		case fields[1] == "unsupported":
			result.unsupported = true
		}
	}

	return result
}

// kernelRebootRequired compares the running kernel with the newest one in /boot.
func kernelRebootRequired() bool {
	running, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}

	kernels, err := filepath.Glob("/boot/vmlinuz-*")
	if err != nil || len(kernels) == 0 {
		return false
	}

	versions := make([]string, 0, len(kernels))
	for _, kernel := range kernels {
		versions = append(versions, strings.TrimPrefix(filepath.Base(kernel), "vmlinuz-"))
	}

	return newestVersion(versions) != strings.TrimSpace(string(running))
}

func newestVersion(versions []string) string {
	return slices.MaxFunc(versions, compareVersions)
}

// compareVersions compares dotted and dashed versions number by number,
// so 6.14.10 is newer than 6.14.9.
func compareVersions(a, b string) int {
	split := func(version string) []string {
		return strings.FieldsFunc(version, func(r rune) bool { return r == '.' || r == '-' })
	}
	partsA, partsB := split(a), split(b)

	for index := range min(len(partsA), len(partsB)) {
		numberA, errA := strconv.Atoi(partsA[index])
		numberB, errB := strconv.Atoi(partsB[index])

		switch {
		case errA == nil && errB == nil && numberA != numberB:
			return numberA - numberB
		case (errA != nil || errB != nil) && partsA[index] != partsB[index]:
			return strings.Compare(partsA[index], partsB[index])
		}
	}

	return len(partsA) - len(partsB)
}

func tail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}

	return "…" + text[len(text)-limit:]
}

func newJobID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)

	return hex.EncodeToString(bytes)
}
