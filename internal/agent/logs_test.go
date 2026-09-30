package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCommands struct {
	outputs map[string]string
	calls   []string
}

func (f *fakeCommands) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	for prefix, output := range f.outputs {
		if strings.HasPrefix(call, prefix) {
			return []byte(output), nil
		}
	}
	return nil, errors.New("exit status 1")
}

func TestParseJournal(t *testing.T) {
	output := `{"MESSAGE":"Started pvedaemon.","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","__REALTIME_TIMESTAMP":"1790690400000000"}
{"MESSAGE":[104,105,255],"PRIORITY":"3","_SYSTEMD_UNIT":"homelab.service","__REALTIME_TIMESTAMP":"1790690401000000"}
{"MESSAGE":null,"PRIORITY":"4","_COMM":"kernel","__REALTIME_TIMESTAMP":"1790690402000000"}
not json
`
	entries := parseJournal([]byte(output))

	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Source != "systemd" || entries[0].Level != 6 || !entries[0].Time.Equal(time.Unix(1790690400, 0)) {
		t.Errorf("first = %+v", entries[0])
	}
	if entries[1].Source != "homelab" || entries[1].Message != "hi�" || entries[1].Level != LevelError {
		t.Errorf("second = %+v", entries[1])
	}
	if entries[2].Source != "kernel" || entries[2].Message != "(message too large to show)" {
		t.Errorf("third = %+v", entries[2])
	}
}

func TestJournalCommands(t *testing.T) {
	commands := &fakeCommands{outputs: map[string]string{
		"pct status 101":   "status: running\n",
		"pct status 102":   "status: stopped\n",
		"journalctl":       "",
		"pct exec 101 -- ": "",
	}}
	logs := &Logs{run: commands.run}
	ctx := context.Background()

	if _, err := logs.Journal(ctx, JournalQuery{Lines: 100, Priority: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := logs.Journal(ctx, JournalQuery{VMID: 101, Lines: 100, Priority: 7}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"journalctl --no-pager --output=json --lines=100 --priority=4",
		"pct status 101",
		"pct exec 101 -- journalctl --no-pager --output=json --lines=100 --priority=7",
	}
	if strings.Join(commands.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q", commands.calls)
	}

	if _, err := logs.Journal(ctx, JournalQuery{VMID: 102, Lines: 100, Priority: 7}); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("stopped container: err = %v", err)
	}
	for _, query := range []JournalQuery{{Lines: 0}, {Lines: 5000}, {Lines: 10, Priority: 8}, {VMID: 5, Lines: 10}} {
		if _, err := logs.Journal(ctx, query); !errors.Is(err, ErrInvalidLogQuery) {
			t.Errorf("%+v: err = %v", query, err)
		}
	}
}

func TestDockerLogs(t *testing.T) {
	commands := &fakeCommands{outputs: map[string]string{
		"pct status 102":            "status: running\n",
		"pct exec 102 -- docker ps": "radarr\trunning\tlscr.io/linuxserver/radarr\nbad name;rm\trunning\tx\nqbittorrent\trunning\tlscr.io/linuxserver/qbittorrent\n",
		"pct exec 102 -- sh -c docker logs --timestamps --tail \"$1\" \"$2\" 2>&1 sh 50 radarr":      "2026-09-30T10:00:01.5Z [Info] RssSyncService: Starting RSS Sync\n2026-09-30T10:00:03Z [Warn] Indexer is slow\n",
		"pct exec 102 -- sh -c docker logs --timestamps --tail \"$1\" \"$2\" 2>&1 sh 50 qbittorrent": "2026-09-30T10:00:02Z (C) Connection refused\n",
	}}
	logs := &Logs{run: commands.run}

	result, err := logs.Docker(context.Background(), 102, 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Containers) != 2 {
		t.Fatalf("containers = %+v", result.Containers)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("entries = %+v", result.Entries)
	}
	levels := []int{result.Entries[0].Level, result.Entries[1].Level, result.Entries[2].Level}
	sources := []string{result.Entries[0].Source, result.Entries[1].Source, result.Entries[2].Source}
	if levels[0] != LevelInfo || levels[1] != LevelError || levels[2] != LevelWarning {
		t.Errorf("levels = %v", levels)
	}
	if sources[0] != "radarr" || sources[1] != "qbittorrent" || sources[2] != "radarr" {
		t.Errorf("sources = %v (should be sorted by time)", sources)
	}
}
