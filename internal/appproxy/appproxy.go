// Package appproxy forwards app names like seerr.homelab.local to the
// containers of those apps, so they get a name and HTTPS on the LAN.
package appproxy

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

const cacheFor = 30 * time.Second

// App is one app with a name of its own.
type App struct {
	// Name is the first label, like "seerr" for seerr.homelab.local.
	Name  string
	Title string
	// Resolve returns the address of the app, like http://192.168.0.50:5055.
	Resolve func(ctx context.Context) (string, error)
}

type route struct {
	App

	mu        sync.Mutex
	target    *url.URL
	err       error
	checkedAt time.Time
}

type Proxy struct {
	hostname string
	routes   map[string]*route
	logger   *slog.Logger
	now      func() time.Time
}

func New(hostname string, apps []App, logger *slog.Logger) *Proxy {
	proxy := &Proxy{hostname: strings.ToLower(hostname), routes: map[string]*route{}, logger: logger, now: time.Now}
	for _, app := range apps {
		proxy.routes[app.Name] = &route{App: app}
	}

	return proxy
}

// Hostnames are the full names of the apps, for the certificate and mDNS.
func (p *Proxy) Hostnames() []string {
	names := make([]string, 0, len(p.routes))
	for name := range p.routes {
		names = append(names, name+"."+p.hostname)
	}

	return names
}

// Handler sends requests for an app name to that app, and all others to next.
func (p *Proxy) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if route := p.routeFor(r.Host); route != nil {
			p.serve(route, w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// App serves one app on any host, for listeners that only carry that app.
func (p *Proxy) App(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := p.routes[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		p.serve(route, w, r)
	})
}

// IsAppHost reports whether host is the name of an app, for redirects.
func (p *Proxy) IsAppHost(host string) bool {
	return p.routeFor(host) != nil
}

func (p *Proxy) routeFor(host string) *route {
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	name, found := strings.CutSuffix(strings.ToLower(host), "."+p.hostname)
	if !found {
		return nil
	}

	return p.routes[name]
}

func (p *Proxy) serve(route *route, w http.ResponseWriter, r *http.Request) {
	target, err := p.resolve(r.Context(), route)
	if err != nil {
		writeUnavailable(w, route.Title, err)
		return
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(out *httputil.ProxyRequest) {
			out.SetURL(target)
			out.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			p.logger.Warn("app proxy", "app", route.Name, "error", err)
			route.forget()
			writeUnavailable(w, route.Title, fmt.Errorf("%s did not answer", target.Host))
		},
	}
	proxy.ServeHTTP(w, r)
}

// resolve returns the cached address, or looks it up again after a while.
func (p *Proxy) resolve(ctx context.Context, route *route) (*url.URL, error) {
	route.mu.Lock()
	defer route.mu.Unlock()

	if !route.checkedAt.IsZero() && p.now().Sub(route.checkedAt) < cacheFor {
		return route.target, route.err
	}

	address, err := route.Resolve(ctx)
	var target *url.URL
	if err == nil {
		target, err = url.Parse(address)
		if err == nil && ((target.Scheme != "http" && target.Scheme != "https") || target.Host == "") {
			err = fmt.Errorf("invalid address %q", address)
		}
	}
	route.target, route.err, route.checkedAt = target, err, p.now()

	return target, err
}

// forget drops the cached address, so the next request looks it up again.
func (r *route) forget() {
	r.mu.Lock()
	r.checkedAt = time.Time{}
	r.mu.Unlock()
}

func writeUnavailable(w http.ResponseWriter, title string, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%[1]s is not available</title>
<body style="font-family:system-ui;max-width:32rem;margin:4rem auto;padding:0 1rem">
<h1>%[1]s is not available</h1><p>%[2]s</p><p>Look on the Homelab page for more.</p>`,
		html.EscapeString(title), html.EscapeString(err.Error()))
}
