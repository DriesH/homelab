package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"homelab/internal/agent"
	"homelab/internal/auth"
	"homelab/internal/health"
	"homelab/internal/jellyfin"
	"homelab/internal/proxmox"
	"homelab/internal/selfupdate"
	"homelab/internal/updates"
)

const sessionCookie = "homelab_session"

// csrfHeader must be sent on every state-changing request. Browsers can't add
// custom headers to cross-site requests without CORS, which we don't allow.
const csrfHeader = "X-Homelab-Request"

type Proxmox interface {
	Resources(ctx context.Context) ([]proxmox.Resource, error)
	NodeStatus(ctx context.Context, node string) (proxmox.NodeStatus, error)
	RunGuestAction(ctx context.Context, node string, guestType proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error)
}

type Agent interface {
	Health(ctx context.Context) (agent.Health, error)
}

type Updates interface {
	Status(ctx context.Context) (updates.View, error)
	Run(id string) (updates.Run, error)
	StartCheck(ctx context.Context) error
	StartHostUpdate(ctx context.Context) error
	StartGuestUpdate(ctx context.Context, vmid int) error
	SaveSettings(input updates.SettingsInput) error
	SendTestNotification(ctx context.Context) error
}

type Jellyfin interface {
	Status(ctx context.Context) jellyfin.View
	SaveSettings(ctx context.Context, input jellyfin.Settings) error
	SetTheme(ctx context.Context, enabled bool) error
	Image(ctx context.Context, itemID, imageType string, maxWidth int) (*http.Response, error)
}

type Health interface {
	Status() health.View
	CheckSystem(ctx context.Context)
	CheckServices(ctx context.Context)
	AddCheck(input health.CheckInput) (health.Check, error)
	UpdateCheck(id string, input health.CheckInput) error
	DeleteCheck(id string) error
}

type SelfUpdate interface {
	Status(ctx context.Context) selfupdate.View
	Check(ctx context.Context) error
	Install(ctx context.Context) error
	SaveSettings(input selfupdate.SettingsInput) error
}

type Options struct {
	Auth       *auth.Service
	Proxmox    Proxmox
	Agent      Agent
	Updates    Updates
	Jellyfin   Jellyfin
	Health     Health
	SelfUpdate SelfUpdate
	// Background is the context for work that outlives a request, like updates.
	Background context.Context
	Web        fs.FS
	// SecureCookies is false only in dev mode, where we serve plain HTTP.
	SecureCookies bool
	Logger        *slog.Logger
}

type server struct {
	Options
}

func New(options Options) http.Handler {
	s := &server{Options: options}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.Handle("GET /api/auth/me", s.requireSession(http.HandlerFunc(s.me)))
	mux.Handle("GET /api/overview", s.requireSession(http.HandlerFunc(s.overview)))
	mux.Handle("POST /api/guests/{node}/{type}/{vmid}/{action}", s.requireSession(http.HandlerFunc(s.guestAction)))
	mux.Handle("GET /api/updates", s.requireSession(http.HandlerFunc(s.updatesStatus)))
	mux.Handle("GET /api/updates/runs/{id}", s.requireSession(http.HandlerFunc(s.updatesRun)))
	mux.Handle("POST /api/updates/check", s.requireSession(http.HandlerFunc(s.updatesCheck)))
	mux.Handle("POST /api/updates/host", s.requireSession(http.HandlerFunc(s.updatesHost)))
	mux.Handle("POST /api/updates/guests/{vmid}", s.requireSession(http.HandlerFunc(s.updatesGuest)))
	mux.Handle("PUT /api/updates/settings", s.requireSession(http.HandlerFunc(s.updatesSettings)))
	mux.Handle("POST /api/updates/test-notification", s.requireSession(http.HandlerFunc(s.updatesTestNotification)))
	mux.Handle("GET /api/jellyfin", s.requireSession(http.HandlerFunc(s.jellyfinStatus)))
	mux.Handle("PUT /api/jellyfin/settings", s.requireSession(http.HandlerFunc(s.jellyfinSettings)))
	mux.Handle("PUT /api/jellyfin/theme", s.requireSession(http.HandlerFunc(s.jellyfinTheme)))
	mux.Handle("GET /api/jellyfin/items/{id}/image", s.requireSession(http.HandlerFunc(s.jellyfinImage)))
	mux.Handle("GET /api/health", s.requireSession(http.HandlerFunc(s.healthStatus)))
	mux.Handle("POST /api/health/refresh", s.requireSession(http.HandlerFunc(s.healthRefresh)))
	mux.Handle("POST /api/health/checks", s.requireSession(http.HandlerFunc(s.healthAddCheck)))
	mux.Handle("PUT /api/health/checks/{id}", s.requireSession(http.HandlerFunc(s.healthUpdateCheck)))
	mux.Handle("DELETE /api/health/checks/{id}", s.requireSession(http.HandlerFunc(s.healthDeleteCheck)))
	mux.Handle("GET /api/self-update", s.requireSession(http.HandlerFunc(s.selfUpdateStatus)))
	mux.Handle("POST /api/self-update/check", s.requireSession(http.HandlerFunc(s.selfUpdateCheck)))
	mux.Handle("POST /api/self-update/install", s.requireSession(http.HandlerFunc(s.selfUpdateInstall)))
	mux.Handle("PUT /api/self-update/settings", s.requireSession(http.HandlerFunc(s.selfUpdateSettings)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", spa(options.Web))

	return securityHeaders(csrf(mux))
}

func (s *server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || !s.Auth.Validate(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead
		if !safe && strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get(csrfHeader) != "1" {
			writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		// style-src needs 'unsafe-inline' for Radix positioning and the theme switcher.
		header.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Frame-Options", "DENY")
		if r.TLS != nil {
			header.Set("Strict-Transport-Security", "max-age=31536000")
		}

		next.ServeHTTP(w, r)
	})
}

func spa(files fs.FS) http.Handler {
	fileServer := http.FileServerFS(files)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		if path != "" {
			if info, err := fs.Stat(files, path); err == nil && !info.IsDir() {
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, files, "index.html")
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
