package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"homelab/internal/agent"
)

// console speaks the protocol of the real agent, but opens a shell on this
// computer instead of `pct console`. Only the dev login can reach it.
func console(w http.ResponseWriter, r *http.Request) {
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil || vmid < 100 {
		http.Error(w, "invalid container", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	shell := exec.Command("/bin/sh", "-i")
	shell.Dir = os.TempDir()
	shell.Env = append(os.Environ(), fmt.Sprintf("PS1=root@ct%d (dev shell) # ", vmid))
	terminal, err := pty.StartWithSize(shell, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		conn.Close(websocket.StatusInternalError, "could not start the shell")
		return
	}
	defer func() {
		shell.Process.Kill()
		terminal.Close()
		shell.Wait()
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		buffer := make([]byte, 32*1024)
		for {
			n, err := terminal.Read(buffer)
			if n > 0 && conn.Write(ctx, websocket.MessageBinary, buffer[:n]) != nil {
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind == websocket.MessageBinary {
			terminal.Write(data)
			continue
		}

		var command agent.ConsoleCommand
		if json.Unmarshal(data, &command) == nil && command.Type == "resize" && command.Cols > 0 && command.Rows > 0 {
			pty.Setsize(terminal, &pty.Winsize{Cols: uint16(command.Cols), Rows: uint16(command.Rows)})
		}
	}
}
