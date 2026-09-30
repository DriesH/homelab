package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	Hostname    string
	HTTPSAddr   string
	HTTPAddr    string
	DataDir     string
	AgentSocket string
	// SelfVMID is the manager's own container, 0 when unknown.
	SelfVMID int
	// GitHubAPIURL is where the manager looks for new releases. Tests change it.
	GitHubAPIURL string
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
		Hostname:     env("HOMELAB_HOSTNAME", "homelab.local"),
		HTTPSAddr:    env("HOMELAB_HTTPS_ADDR", ":443"),
		HTTPAddr:     env("HOMELAB_HTTP_ADDR", ":80"),
		DataDir:      DataDir(),
		AgentSocket:  env("HOMELAB_AGENT_SOCKET", "/mnt/homelab-agent/agent.sock"),
		Dev:          os.Getenv("HOMELAB_DEV") == "1",
		GitHubAPIURL: env("HOMELAB_GITHUB_API_URL", "https://api.github.com"),
		SelfVMID:     envInt("HOMELAB_SELF_VMID"),
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

func envInt(key string) int {
	value, _ := strconv.Atoi(os.Getenv(key))

	return value
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
