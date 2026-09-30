package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Syslog levels, as journald uses them.
const (
	LevelError   = 3
	LevelWarning = 4
	LevelInfo    = 6
	LevelDebug   = 7
)

const (
	maxJournalLines = 2000
	maxDockerLines  = 1000
	maxMessage      = 4096
	logsTimeout     = 30 * time.Second
)

var ErrInvalidLogQuery = errors.New("invalid log query")

var dockerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   int       `json:"level"`
	Source  string    `json:"source"`
	Message string    `json:"message"`
}

type JournalQuery struct {
	// VMID 0 reads the host journal.
	VMID  int
	Lines int
	// Priority shows this level and more important ones, like journalctl -p.
	Priority int
}

func (q JournalQuery) validate() error {
	if q.VMID != 0 && q.VMID < 100 {
		return fmt.Errorf("%w: invalid guest", ErrInvalidLogQuery)
	}
	if q.Lines < 1 || q.Lines > maxJournalLines {
		return fmt.Errorf("%w: lines must be 1 to %d", ErrInvalidLogQuery, maxJournalLines)
	}
	if q.Priority < 0 || q.Priority > LevelDebug {
		return fmt.Errorf("%w: invalid level", ErrInvalidLogQuery)
	}

	return nil
}

type DockerContainer struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Image string `json:"image"`
}

type DockerLogs struct {
	Containers []DockerContainer `json:"containers"`
	Entries    []LogEntry        `json:"entries"`
}

// Logs reads journals and Docker logs with fixed, read-only commands.
type Logs struct {
	// run executes a command and returns its output. Tests replace it.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func NewLogs() *Logs {
	return &Logs{run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}}
}

// inGuest runs the command on the host, or in the container when vmid is set.
func (l *Logs) inGuest(ctx context.Context, vmid int, args ...string) ([]byte, error) {
	if vmid == 0 {
		return l.run(ctx, args[0], args[1:]...)
	}

	status, err := l.run(ctx, "pct", "status", strconv.Itoa(vmid))
	if err != nil {
		return nil, fmt.Errorf("container %d not found", vmid)
	}
	if strings.TrimSpace(string(status)) != "status: running" {
		return nil, fmt.Errorf("container %d is not running", vmid)
	}

	return l.run(ctx, "pct", append([]string{"exec", strconv.Itoa(vmid), "--"}, args...)...)
}

func (l *Logs) Journal(ctx context.Context, query JournalQuery) ([]LogEntry, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, logsTimeout)
	defer cancel()

	output, err := l.inGuest(ctx, query.VMID, "journalctl", "--no-pager", "--output=json",
		"--lines="+strconv.Itoa(query.Lines), "--priority="+strconv.Itoa(query.Priority))
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("could not read the journal: %w", err)
	}

	return parseJournal(output), nil
}

type journalLine struct {
	Message    json.RawMessage `json:"MESSAGE"`
	Priority   string          `json:"PRIORITY"`
	Identifier string          `json:"SYSLOG_IDENTIFIER"`
	Unit       string          `json:"_SYSTEMD_UNIT"`
	Command    string          `json:"_COMM"`
	Realtime   string          `json:"__REALTIME_TIMESTAMP"`
}

func parseJournal(output []byte) []LogEntry {
	entries := []LogEntry{}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var line journalLine
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}

		level, err := strconv.Atoi(line.Priority)
		if err != nil {
			level = LevelInfo
		}
		micros, _ := strconv.ParseInt(line.Realtime, 10, 64)

		entries = append(entries, LogEntry{
			Time:    time.UnixMicro(micros).UTC(),
			Level:   level,
			Source:  firstNonEmpty(line.Identifier, strings.TrimSuffix(line.Unit, ".service"), line.Command, "journal"),
			Message: truncate(journalMessage(line.Message)),
		})
	}

	return entries
}

// journalMessage decodes MESSAGE, which journald writes as a string, as a
// list of bytes when it isn't UTF-8, or as null when it is very large.
func journalMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if string(raw) == "null" {
		return "(message too large to show)"
	}

	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}

	var data []byte
	var numbers []int
	if json.Unmarshal(raw, &numbers) == nil {
		for _, number := range numbers {
			data = append(data, byte(number))
		}
		return strings.ToValidUTF8(string(data), "�")
	}

	return "(message too large to show)"
}

// Docker reads the recent logs of every Docker container in a container.
func (l *Logs) Docker(ctx context.Context, vmid, lines int) (DockerLogs, error) {
	if vmid < 100 || lines < 1 || lines > maxDockerLines {
		return DockerLogs{}, fmt.Errorf("%w: invalid guest or lines", ErrInvalidLogQuery)
	}

	ctx, cancel := context.WithTimeout(ctx, logsTimeout)
	defer cancel()

	output, err := l.inGuest(ctx, vmid, "docker", "ps", "--all", "--format", "{{.Names}}\t{{.State}}\t{{.Image}}")
	if err != nil {
		return DockerLogs{}, fmt.Errorf("could not list Docker containers, is Docker installed? %w", err)
	}

	result := DockerLogs{Containers: []DockerContainer{}, Entries: []LogEntry{}}
	for line := range strings.Lines(string(output)) {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) != 3 || !dockerName.MatchString(fields[0]) {
			continue
		}
		result.Containers = append(result.Containers, DockerContainer{Name: fields[0], State: fields[1], Image: fields[2]})
	}

	for _, container := range result.Containers {
		// Docker writes the app's stderr to our stderr, so combine both.
		logs, err := l.inGuest(ctx, vmid, "sh", "-c", `docker logs --timestamps --tail "$1" "$2" 2>&1`, "sh", strconv.Itoa(lines), container.Name)
		if err != nil && len(logs) == 0 {
			continue
		}
		result.Entries = append(result.Entries, parseDockerLogs(container.Name, logs)...)
	}
	slices.SortStableFunc(result.Entries, func(a, b LogEntry) int { return a.Time.Compare(b.Time) })

	return result, nil
}

var (
	errorWords   = regexp.MustCompile(`(?i)\b(fatal|panic|crit|critical|err|error|exception)\b|\(C\)`)
	warningWords = regexp.MustCompile(`(?i)\b(warn|warning)\b|\(W\)`)
	debugWords   = regexp.MustCompile(`(?i)\b(debug|trace|dbg|verbose)\b`)
)

// parseDockerLogs reads "<RFC 3339 time> <message>" lines. Docker has no log
// levels, so the level is guessed from words like ERROR or [Warn].
func parseDockerLogs(source string, output []byte) []LogEntry {
	entries := []LogEntry{}

	for line := range strings.Lines(string(output)) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		stamp, message, _ := strings.Cut(line, " ")
		timestamp, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			message = line
		}

		entries = append(entries, LogEntry{
			Time:    timestamp.UTC(),
			Level:   guessLevel(message),
			Source:  source,
			Message: truncate(message),
		})
	}

	return entries
}

func guessLevel(message string) int {
	switch {
	case errorWords.MatchString(message):
		return LevelError
	case warningWords.MatchString(message):
		return LevelWarning
	case debugWords.MatchString(message):
		return LevelDebug
	default:
		return LevelInfo
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncate(message string) string {
	if len(message) <= maxMessage {
		return message
	}
	return strings.ToValidUTF8(message[:maxMessage], "") + "…"
}
