package appproxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRoutesByHost(t *testing.T) {
	seerr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "seerr "+r.URL.Path+" "+r.Header.Get("X-Forwarded-Host"))
	}))
	defer seerr.Close()

	resolved := 0
	proxy := New("homelab.local", []App{
		{Name: "seerr", Title: "Seerr", Resolve: func(context.Context) (string, error) { resolved++; return seerr.URL, nil }},
		{Name: "jellyfin", Title: "Jellyfin", Resolve: func(context.Context) (string, error) { return "", errors.New("connect Jellyfin first") }},
	}, slog.New(slog.DiscardHandler))
	homelab := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "homelab") })
	handler := proxy.Handler(homelab)

	get := func(host, path string) (int, string) {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Host = host
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	if code, body := get("Seerr.HomeLab.local:443", "/api/v1/status"); code != 200 || body != "seerr /api/v1/status Seerr.HomeLab.local:443" {
		t.Fatalf("seerr: %d %q", code, body)
	}
	if _, body := get("homelab.local", "/"); body != "homelab" {
		t.Fatalf("homelab: %q", body)
	}
	for _, host := range []string{"other.local", "x.seerr.homelab.local", "seerr.homelab.local.evil.com", "192.168.0.10"} {
		if _, body := get(host, "/"); body != "homelab" {
			t.Errorf("%s went to an app: %q", host, body)
		}
	}
	if code, body := get("jellyfin.homelab.local", "/"); code != http.StatusBadGateway || !strings.Contains(body, "connect Jellyfin first") {
		t.Fatalf("jellyfin: %d %q", code, body)
	}

	get("seerr.homelab.local", "/again")
	if resolved != 1 {
		t.Fatalf("resolved %d times, want 1 within the cache time", resolved)
	}
}

func TestDownAppIsLookedUpAgain(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	address := backend.URL
	backend.Close()

	resolved := 0
	proxy := New("homelab.local", []App{{Name: "seerr", Title: "Seerr", Resolve: func(context.Context) (string, error) {
		resolved++
		return address, nil
	}}}, slog.New(slog.DiscardHandler))

	w := httptest.NewRecorder()
	proxy.App("seerr").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "did not answer") {
		t.Fatalf("down app: %d %q", w.Code, w.Body.String())
	}

	// After an error, the next request looks the address up again, like after a new IP.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer up.Close()
	address = up.URL
	w = httptest.NewRecorder()
	proxy.App("seerr").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Body.String() != "ok" || resolved != 2 {
		t.Fatalf("after a new address: %q, resolved %d", w.Body.String(), resolved)
	}
}

func TestCacheExpires(t *testing.T) {
	now := time.Now()
	resolved := 0
	proxy := New("homelab.local", []App{{Name: "seerr", Resolve: func(context.Context) (string, error) {
		resolved++
		return "", errors.New("not installed")
	}}}, slog.New(slog.DiscardHandler))
	proxy.now = func() time.Time { return now }

	for range 3 {
		proxy.App("seerr").ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	now = now.Add(cacheFor + time.Second)
	proxy.App("seerr").ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if resolved != 2 {
		t.Fatalf("resolved %d times, want 2", resolved)
	}
	if names := proxy.Hostnames(); len(names) != 1 || names[0] != "seerr.homelab.local" {
		t.Fatalf("hostnames = %v", names)
	}
}
