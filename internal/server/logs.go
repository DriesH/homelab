package server

import (
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"homelab/internal/agent"
)

var upidPattern = regexp.MustCompile(`^UPID:[A-Za-z0-9:._@!+-]+$`)

type taskView struct {
	agent.LogEntry
	Node string `json:"node"`
	UPID string `json:"upid"`
}

func (s *server) logsJournal(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	vmid, _ := strconv.Atoi(query.Get("vmid"))
	lines, _ := strconv.Atoi(query.Get("lines"))
	priority, _ := strconv.Atoi(query.Get("priority"))

	entries, err := s.Logs.Journal(r.Context(), agent.JournalQuery{VMID: vmid, Lines: lines, Priority: priority})
	s.writeLogs(w, entries, err)
}

func (s *server) logsDocker(w http.ResponseWriter, r *http.Request) {
	vmid, _ := strconv.Atoi(r.URL.Query().Get("vmid"))
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))

	result, err := s.Logs.DockerLogs(r.Context(), vmid, lines)
	s.writeLogs(w, result, err)
}

func (s *server) writeLogs(w http.ResponseWriter, result any, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, agent.ErrInvalidLogQuery) || strings.Contains(err.Error(), "400 Bad Request"):
		writeError(w, http.StatusBadRequest, "invalid log query")
	default:
		// The agent's message says why, like "container 101 is not running".
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// logsTasks lists the recent Proxmox tasks of all nodes as log entries.
func (s *server) logsTasks(w http.ResponseWriter, r *http.Request) {
	resources, err := s.Proxmox.Resources(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not reach Proxmox")
		return
	}

	tasks := []taskView{}
	for _, resource := range resources {
		if resource.Type != "node" || resource.Status != "online" {
			continue
		}

		nodeTasks, err := s.Proxmox.Tasks(r.Context(), resource.Node, 200)
		if err != nil {
			s.Logger.Warn("tasks", "node", resource.Node, "error", err)
			continue
		}

		for _, task := range nodeTasks {
			level := agent.LevelInfo
			switch {
			case strings.HasPrefix(task.Status, "WARNINGS"):
				level = agent.LevelWarning
			case task.Status != "" && task.Status != "OK":
				level = agent.LevelError
			}

			message := task.Type
			if task.ID != "" {
				message += " " + task.ID
			}
			message += " by " + task.User
			if task.Status != "" {
				message += ": " + task.Status
			}

			tasks = append(tasks, taskView{
				LogEntry: agent.LogEntry{
					Time:    time.Unix(max(task.EndTime, task.StartTime), 0).UTC(),
					Level:   level,
					Source:  task.Type,
					Message: message,
				},
				Node: resource.Node,
				UPID: task.UPID,
			})
		}
	}
	slices.SortFunc(tasks, func(a, b taskView) int { return a.Time.Compare(b.Time) })

	writeJSON(w, http.StatusOK, tasks)
}

func (s *server) logsTaskLog(w http.ResponseWriter, r *http.Request) {
	node, upid := r.PathValue("node"), r.URL.Query().Get("upid")
	if !nodeName.MatchString(node) || !upidPattern.MatchString(upid) {
		writeError(w, http.StatusBadRequest, "invalid task")
		return
	}

	lines, err := s.Proxmox.TaskLog(r.Context(), node, upid, 5000)
	if err != nil {
		s.Logger.Warn("task log", "upid", upid, "error", err)
		writeError(w, http.StatusBadGateway, "could not read the task log")
		return
	}

	writeJSON(w, http.StatusOK, map[string][]string{"lines": lines})
}
