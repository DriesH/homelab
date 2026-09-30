package agent

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func newTestRunner(script string) *Runner {
	runner := NewRunner()
	runner.command = func(ctx context.Context, _ JobRequest) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "sh", "-c", script), nil
	}
	runner.rebootRequired = func() bool { return true }

	return runner
}

func waitForJob(t *testing.T, runner *Runner, id string) Job {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := runner.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != JobRunning {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("job did not finish")
	return Job{}
}

func TestJobCollectsResultsFromScriptOutput(t *testing.T) {
	runner := newTestRunner(`echo "Reading package lists..."
echo "HOMELAB package libssl3t64 3.5.7-1 3.5.7-2"
echo "HOMELAB image radarr lscr.io/linuxserver/radarr:latest"
echo "HOMELAB done"`)

	started, err := runner.Start(JobRequest{Kind: HostCheck})
	if err != nil {
		t.Fatal(err)
	}
	job := waitForJob(t, runner, started.ID)

	if job.Status != JobSucceeded {
		t.Fatalf("expected success, got %s: %s", job.Status, job.Error)
	}
	if len(job.Packages) != 1 || job.Packages[0] != (Package{Name: "libssl3t64", From: "3.5.7-1", To: "3.5.7-2"}) {
		t.Errorf("unexpected packages: %+v", job.Packages)
	}
	if len(job.Images) != 1 || job.Images[0].Service != "radarr" {
		t.Errorf("unexpected images: %+v", job.Images)
	}
	if !job.RebootRequired {
		t.Error("host job should report the reboot check")
	}
}

func TestFailingCommandFailsJob(t *testing.T) {
	runner := newTestRunner("echo boom; exit 113")

	started, _ := runner.Start(JobRequest{Kind: GuestUpdate, VMID: 101})
	job := waitForJob(t, runner, started.ID)

	if job.Status != JobFailed || job.Error == "" {
		t.Fatalf("expected failure with error, got %+v", job)
	}
	if job.RebootRequired {
		t.Error("guest job must not report host reboot")
	}
}

func TestOneJobPerTarget(t *testing.T) {
	runner := newTestRunner("sleep 0.3")

	first, err := runner.Start(JobRequest{Kind: GuestCheck, VMID: 101})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(JobRequest{Kind: GuestUpdate, VMID: 101}); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
	if _, err := runner.Start(JobRequest{Kind: GuestCheck, VMID: 102}); err != nil {
		t.Fatalf("other guest should not be blocked: %v", err)
	}

	waitForJob(t, runner, first.ID)
	if _, err := runner.Start(JobRequest{Kind: GuestUpdate, VMID: 101}); err != nil {
		t.Fatalf("target should be free after the job: %v", err)
	}
}

func TestValidateRejectsBadRequests(t *testing.T) {
	for _, request := range []JobRequest{
		{Kind: "rm -rf"},
		{Kind: GuestUpdate, VMID: 5},
		{Kind: HostUpgrade, VMID: 101},
	} {
		if err := validate(request); !errors.Is(err, ErrInvalidJob) {
			t.Errorf("%+v: expected ErrInvalidJob, got %v", request, err)
		}
	}
}

func TestNewestKernelVersion(t *testing.T) {
	versions := []string{"6.14.8-2-pve", "6.14.10-1-pve", "6.14.9-3-pve", "6.8.12-4-pve"}

	if newest := newestVersion(versions); newest != "6.14.10-1-pve" {
		t.Fatalf("expected 6.14.10-1-pve, got %s", newest)
	}
}
