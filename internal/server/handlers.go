package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"time"

	"homelab/internal/auth"
	"homelab/internal/proxmox"
)

var nodeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,62}$`)

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token, err := s.Auth.Login(body.Username, body.Password, body.Code, clientIP(r))
	switch {
	case errors.Is(err, auth.ErrLockedOut):
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.Logger.Warn("failed login", "ip", clientIP(r))
		writeError(w, http.StatusUnauthorized, "wrong username, password or code")
		return
	case err != nil:
		s.Logger.Error("login", "error", err)
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   s.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"username": s.Auth.Username()})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.Auth.Logout(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.SecureCookies,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"username": s.Auth.Username()})
}

type usage struct {
	CPU     float64 `json:"cpu"`
	MaxCPU  float64 `json:"maxCpu"`
	Mem     int64   `json:"mem"`
	MaxMem  int64   `json:"maxMem"`
	Disk    int64   `json:"disk"`
	MaxDisk int64   `json:"maxDisk"`
	Uptime  int64   `json:"uptime"`
}

type nodeView struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	PVEVersion string `json:"pveVersion"`
	usage
}

type guestView struct {
	ID     string `json:"id"`
	VMID   int    `json:"vmid"`
	Type   string `json:"type"`
	Node   string `json:"node"`
	Name   string `json:"name"`
	Status string `json:"status"`
	usage
}

type agentView struct {
	Connected bool   `json:"connected"`
	Hostname  string `json:"hostname,omitempty"`
	Version   string `json:"version,omitempty"`
}

func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	resources, err := s.Proxmox.Resources(r.Context())
	if err != nil {
		s.Logger.Error("proxmox resources", "error", err)
		writeError(w, http.StatusBadGateway, "could not reach Proxmox")
		return
	}

	nodes := []nodeView{}
	guests := []guestView{}

	for _, resource := range resources {
		usage := usage{
			CPU: resource.CPU, MaxCPU: resource.MaxCPU,
			Mem: resource.Mem, MaxMem: resource.MaxMem,
			Disk: resource.Disk, MaxDisk: resource.MaxDisk,
			Uptime: resource.Uptime,
		}

		switch {
		case resource.Type == "node":
			node := nodeView{Name: resource.Node, Status: resource.Status, usage: usage}
			if status, err := s.Proxmox.NodeStatus(r.Context(), resource.Node); err == nil {
				node.PVEVersion = status.PVEVersion
			}
			nodes = append(nodes, node)
		case (resource.Type == "lxc" || resource.Type == "qemu") && resource.Template == 0:
			guests = append(guests, guestView{
				ID: resource.ID, VMID: resource.VMID, Type: resource.Type, Node: resource.Node,
				Name: resource.Name, Status: resource.Status, usage: usage,
			})
		}
	}

	slices.SortFunc(guests, func(a, b guestView) int { return a.VMID - b.VMID })

	agent := agentView{}
	if health, err := s.Agent.Health(r.Context()); err == nil {
		agent = agentView{Connected: true, Hostname: health.Hostname, Version: health.Version}
	}

	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "guests": guests, "agent": agent})
}

func (s *server) guestAction(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("node")
	guestType := proxmox.GuestType(r.PathValue("type"))
	action := proxmox.GuestAction(r.PathValue("action"))
	vmid, err := strconv.Atoi(r.PathValue("vmid"))

	valid := err == nil && vmid > 0 &&
		nodeName.MatchString(node) &&
		slices.Contains([]proxmox.GuestType{proxmox.LXC, proxmox.QEMU}, guestType) &&
		slices.Contains([]proxmox.GuestAction{proxmox.Start, proxmox.Shutdown, proxmox.Reboot, proxmox.Stop}, action)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid guest or action")
		return
	}

	upid, err := s.Proxmox.RunGuestAction(r.Context(), node, guestType, vmid, action)
	if err != nil {
		s.Logger.Error("guest action", "vmid", vmid, "action", action, "error", err)
		writeError(w, http.StatusBadGateway, "Proxmox refused the action")
		return
	}

	s.Logger.Info("guest action", "vmid", vmid, "action", action)
	writeJSON(w, http.StatusAccepted, map[string]string{"task": upid})
}
