package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// The console protocol, also used between the browser and the manager:
// binary messages are terminal data in both directions, and text messages
// from the client are JSON commands, like {"type":"resize","cols":80,"rows":24}.
type ConsoleCommand struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

const (
	maxConsoles     = 4
	maxConsoleInput = 64 * 1024
)

// Consoles attaches to the tty of a container with `pct console`, the same
// login as the Console tab in Proxmox. It never runs anything else.
type Consoles struct {
	Logger *slog.Logger
	// command builds the process for a container. Tests and local runs replace it.
	command func(vmid int) *exec.Cmd
	// running reports whether the container runs.
	running func(ctx context.Context, vmid int) bool

	mu     sync.Mutex
	active int
}

func NewConsoles(logger *slog.Logger) *Consoles {
	return &Consoles{
		Logger: logger,
		command: func(vmid int) *exec.Cmd {
			return exec.Command("pct", "console", strconv.Itoa(vmid))
		},
		running: func(ctx context.Context, vmid int) bool {
			output, err := exec.CommandContext(ctx, "pct", "status", strconv.Itoa(vmid)).Output()
			return err == nil && strings.TrimSpace(string(output)) == "status: running"
		},
	}
}

func (c *Consoles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil || vmid < 100 {
		http.Error(w, "invalid container", http.StatusBadRequest)
		return
	}
	if !c.running(r.Context(), vmid) {
		http.Error(w, fmt.Sprintf("container %d is not running", vmid), http.StatusConflict)
		return
	}

	c.mu.Lock()
	if c.active >= maxConsoles {
		c.mu.Unlock()
		http.Error(w, "too many consoles are open", http.StatusTooManyRequests)
		return
	}
	c.active++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.active--
		c.mu.Unlock()
	}()

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxConsoleInput)

	cmd := c.command(vmid)
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		conn.Close(websocket.StatusInternalError, "could not start the console")
		return
	}
	defer func() {
		cmd.Process.Kill()
		terminal.Close()
		cmd.Wait()
	}()

	c.Logger.Info("console opened", "vmid", vmid)
	defer c.Logger.Info("console closed", "vmid", vmid)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		copyToSocket(ctx, conn, terminal)
	}()

	copyFromSocket(ctx, conn, terminal)
	conn.Close(websocket.StatusNormalClosure, "")
}

// copyToSocket sends the terminal output until the process ends.
func copyToSocket(ctx context.Context, conn *websocket.Conn, terminal io.Reader) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := terminal.Read(buffer)
		if n > 0 {
			if conn.Write(ctx, websocket.MessageBinary, buffer[:n]) != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// copyFromSocket writes the input to the terminal and handles resizes.
func copyFromSocket(ctx context.Context, conn *websocket.Conn, terminal *os.File) {
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		if kind == websocket.MessageBinary {
			if _, err := terminal.Write(data); err != nil {
				return
			}
			continue
		}

		var command ConsoleCommand
		if json.Unmarshal(data, &command) == nil && command.Type == "resize" &&
			command.Cols > 0 && command.Cols <= 1000 && command.Rows > 0 && command.Rows <= 500 {
			pty.Setsize(terminal, &pty.Winsize{Cols: uint16(command.Cols), Rows: uint16(command.Rows)})
		}
	}
}

// ErrConsoleUnavailable wraps the agent's reason, like "container 101 is not running".
var ErrConsoleUnavailable = errors.New("console unavailable")
