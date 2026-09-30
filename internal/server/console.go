package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/coder/websocket"

	"homelab/internal/agent"
)

const maxConsoleMessage = 64 * 1024

// console relays a container console between the browser and the host agent.
// websocket.Accept only allows the page's own origin, so another site can't
// open a console with the user's session.
func (s *server) console(w http.ResponseWriter, r *http.Request) {
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	if err != nil || vmid < 100 {
		writeError(w, http.StatusBadRequest, "invalid container")
		return
	}

	name, err := s.containerName(r.Context(), vmid)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	upstream, err := s.Console.OpenConsole(r.Context(), vmid)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, agent.ErrConsoleUnavailable) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	defer upstream.CloseNow()
	upstream.SetReadLimit(maxConsoleMessage)

	browser, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer browser.CloseNow()
	browser.SetReadLimit(maxConsoleMessage)

	s.Logger.Info("console opened", "vmid", vmid, "name", name, "client", clientIP(r))
	if s.Notify != nil {
		s.Notify(s.Background, fmt.Sprintf("🖥️ Console opened for %s (%d) from %s", name, vmid, clientIP(r)))
	}
	defer s.Logger.Info("console closed", "vmid", vmid)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		relay(ctx, upstream, browser)
	}()
	relay(ctx, browser, upstream)

	browser.Close(websocket.StatusNormalClosure, "")
	upstream.Close(websocket.StatusNormalClosure, "")
}

func relay(ctx context.Context, from, to *websocket.Conn) {
	for {
		kind, data, err := from.Read(ctx)
		if err != nil {
			return
		}
		if err := to.Write(ctx, kind, data); err != nil {
			return
		}
	}
}

func (s *server) containerName(ctx context.Context, vmid int) (string, error) {
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return "", errors.New("could not reach Proxmox")
	}

	for _, resource := range resources {
		if resource.VMID == vmid && resource.Type == "lxc" && resource.Template == 0 {
			return resource.Name, nil
		}
	}

	return "", fmt.Errorf("container %d not found", vmid)
}
