package config

import (
	"errors"
	"os"
)

type Config struct {
	Hostname    string
	HTTPSAddr   string
	HTTPAddr    string
	DataDir     string
	AgentSocket string
	// Dev serves plain HTTP on HTTPAddr so the Vite dev server can proxy to it.
	Dev     bool
	Proxmox Proxmox
}

type Proxmox struct {
	URL         string
	TokenID     string
	TokenSecret string
	// CAFile is the cluster CA (/etc/pve/pve-root-ca.pem). Empty means system roots.
	CAFile string
}

func Load() (Config, error) {
	cfg := Config{
		Hostname:    env("HOMELAB_HOSTNAME", "homelab.local"),
		HTTPSAddr:   env("HOMELAB_HTTPS_ADDR", ":443"),
		HTTPAddr:    env("HOMELAB_HTTP_ADDR", ":80"),
		DataDir:     DataDir(),
		AgentSocket: env("HOMELAB_AGENT_SOCKET", "/mnt/homelab-agent/agent.sock"),
		Dev:         os.Getenv("HOMELAB_DEV") == "1",
		Proxmox: Proxmox{
			URL:         os.Getenv("PROXMOX_URL"),
			TokenID:     os.Getenv("PROXMOX_TOKEN_ID"),
			TokenSecret: os.Getenv("PROXMOX_TOKEN_SECRET"),
			CAFile:      os.Getenv("PROXMOX_CA_FILE"),
		},
	}

	if cfg.Proxmox.URL == "" || cfg.Proxmox.TokenID == "" || cfg.Proxmox.TokenSecret == "" {
		return Config{}, errors.New("PROXMOX_URL, PROXMOX_TOKEN_ID and PROXMOX_TOKEN_SECRET are required")
	}

	return cfg, nil
}

func DataDir() string {
	return env("HOMELAB_DATA_DIR", "/var/lib/homelab")
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
