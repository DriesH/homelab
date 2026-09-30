package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"homelab/internal/agent"
	"homelab/internal/auth"
	"homelab/internal/config"
	"homelab/internal/proxmox"
	"homelab/internal/server"
	"homelab/internal/tlsca"
	"homelab/internal/updates"
	"homelab/web"
)

func serve() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	admin, err := auth.LoadAdmin(auth.AdminPath(cfg.DataDir))
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no admin account yet, run `homelab admin` first")
	}
	if err != nil {
		return err
	}

	pve, err := proxmox.New(cfg.Proxmox)
	if err != nil {
		return err
	}

	webFS, err := web.Dist()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	agentClient := agent.NewClient(cfg.AgentSocket)
	updateService, err := updates.New(updates.Options{
		DataDir:  cfg.DataDir,
		Proxmox:  pve,
		Agent:    agentClient,
		SelfVMID: cfg.SelfVMID,
		Logger:   logger,
	})
	if err != nil {
		return err
	}
	go updateService.RunScheduler(ctx)

	handler := server.New(server.Options{
		Auth:          auth.NewService(admin),
		Proxmox:       pve,
		Agent:         agentClient,
		Updates:       updateService,
		Background:    ctx,
		Web:           webFS,
		SecureCookies: !cfg.Dev,
		Logger:        logger,
	})

	if cfg.Dev {
		logger.Warn("dev mode: serving plain HTTP", "addr", cfg.HTTPAddr)
		return run(ctx, newServer(cfg.HTTPAddr, handler))
	}

	authority, err := tlsca.Load(filepath.Join(cfg.DataDir, "tls"), cfg.Hostname)
	if err != nil {
		return err
	}

	httpsServer := newServer(cfg.HTTPSAddr, handler)
	httpsServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: authority.GetCertificate}

	httpServer := newServer(cfg.HTTPAddr, redirectHandler(cfg.Hostname, authority.CertPEM()))

	logger.Info("listening", "https", cfg.HTTPSAddr, "http", cfg.HTTPAddr, "hostname", cfg.Hostname, "version", version)

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return run(ctx, httpsServer) })
	group.Go(func() error { return run(ctx, httpServer) })

	return group.Wait()
}

// redirectHandler sends everything to HTTPS, except the CA certificate that
// devices need to download before they can trust HTTPS.
func redirectHandler(hostname string, caPEM []byte) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ca.crt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="homelab-ca.crt"`)
		w.Write(caPEM)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("https://%s%s", hostname, r.URL.RequestURI()), http.StatusMovedPermanently)
	})

	return mux
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

func run(ctx context.Context, srv *http.Server) error {
	errs := make(chan error, 1)
	go func() {
		if srv.TLSConfig != nil {
			errs <- srv.ListenAndServeTLS("", "")
		} else {
			errs <- srv.ListenAndServe()
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		return srv.Shutdown(shutdownCtx)
	}
}
