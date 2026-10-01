package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirectKeepsOnlyAppNames(t *testing.T) {
	isAppHost := func(host string) bool { return host == "seerr.homelab.local" || host == "seerr.homelab.local:80" }
	handler := redirectHandler("homelab.local", isAppHost, []byte("ca"))

	for host, want := range map[string]string{
		"seerr.homelab.local:80": "https://seerr.homelab.local/requests?x=1",
		"homelab.local":          "https://homelab.local/requests?x=1",
		"evil.example.com":       "https://homelab.local/requests?x=1",
	} {
		r := httptest.NewRequest(http.MethodGet, "/requests?x=1", nil)
		r.Host = host
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if got := w.Header().Get("Location"); got != want {
			t.Errorf("%s: Location = %q, want %q", host, got, want)
		}
	}
}
