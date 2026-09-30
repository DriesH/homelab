// Package tailscale manages the Tailscale node in the manager container with
// the tailscale CLI. The installer makes the manager's user the Tailscale
// operator, so it doesn't need root.
package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// A login waits until you approve it in the browser.
	loginTimeout = 10 * time.Minute
	cliTimeout   = 30 * time.Second
)

var (
	ErrNotInstalled    = errors.New("Tailscale is not installed in the manager container")
	ErrInvalidSettings = errors.New("invalid settings")
	ErrBusy            = errors.New("Tailscale is already connecting")
)

// The admin console link that tailscale serve prints when HTTPS is off for the tailnet.
var enableLink = regexp.MustCompile(`https://login\.tailscale\.com/f/\S+`)

// Runner runs the tailscale CLI and returns its combined output. Tests replace it.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

type Options struct {
	DataDir string
	// Backend is the local address of the manager that Serve forwards to,
	// like https+insecure://127.0.0.1:443.
	Backend string
	Logger  *slog.Logger
	Run     Runner
	// Operator is the Unix user that may run the CLI. It defaults to the current user.
	Operator string
}

type Settings struct {
	// ShareSubnet makes this node a subnet router for Subnet, so other devices
	// on the tailnet can reach the home network.
	ShareSubnet bool   `json:"shareSubnet"`
	Subnet      string `json:"subnet"`
}

type Peer struct {
	Name     string    `json:"name"`
	DNSName  string    `json:"dnsName"`
	OS       string    `json:"os"`
	IPs      []string  `json:"ips"`
	Online   bool      `json:"online"`
	LastSeen time.Time `json:"lastSeen,omitzero"`
}

type View struct {
	Installed bool `json:"installed"`
	// State is Tailscale's backend state, like NeedsLogin, Running or Stopped.
	State      string   `json:"state"`
	AuthURL    string   `json:"authUrl,omitempty"`
	Connecting bool     `json:"connecting"`
	Error      string   `json:"error,omitempty"`
	Health     []string `json:"health"`

	Name    string   `json:"name,omitempty"`
	DNSName string   `json:"dnsName,omitempty"`
	IPs     []string `json:"ips"`
	Tailnet string   `json:"tailnet,omitempty"`

	Serving  bool   `json:"serving"`
	ServeURL string `json:"serveUrl,omitempty"`

	Settings        Settings `json:"settings"`
	SuggestedSubnet string   `json:"suggestedSubnet,omitempty"`
	// SubnetApproved is true when the admin console approved the shared subnet.
	SubnetApproved bool `json:"subnetApproved"`

	Peers []Peer `json:"peers"`
}

type Service struct {
	Options
	path string

	mu         sync.Mutex
	settings   Settings
	connecting bool
	lastError  string
}

func New(options Options) (*Service, error) {
	if options.Run == nil {
		options.Run = runCLI
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.Operator == "" {
		if current, err := user.Current(); err == nil {
			options.Operator = current.Username
		}
	}

	service := &Service{Options: options, path: filepath.Join(options.DataDir, "tailscale.json")}

	data, err := os.ReadFile(service.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &service.settings); err != nil {
			return nil, err
		}
	}

	return service, nil
}

func runCLI(ctx context.Context, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return nil, ErrNotInstalled
	}

	return exec.CommandContext(ctx, "tailscale", args...).CombinedOutput()
}

func (s *Service) run(ctx context.Context, args ...string) ([]byte, error) {
	output, err := s.Run(ctx, args...)
	if err != nil && !errors.Is(err, ErrNotInstalled) {
		if message := strings.TrimSpace(string(output)); message != "" {
			return output, errors.New(firstLine(message))
		}
	}

	return output, err
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// status is the part of `tailscale status --json` that we use.
type status struct {
	BackendState string   `json:"BackendState"`
	AuthURL      string   `json:"AuthURL"`
	Health       []string `json:"Health"`
	Self         *struct {
		HostName      string   `json:"HostName"`
		DNSName       string   `json:"DNSName"`
		TailscaleIPs  []string `json:"TailscaleIPs"`
		PrimaryRoutes []string `json:"PrimaryRoutes"`
	} `json:"Self"`
	CurrentTailnet *struct {
		Name string `json:"Name"`
	} `json:"CurrentTailnet"`
	Peer map[string]struct {
		HostName     string    `json:"HostName"`
		DNSName      string    `json:"DNSName"`
		OS           string    `json:"OS"`
		TailscaleIPs []string  `json:"TailscaleIPs"`
		Online       bool      `json:"Online"`
		LastSeen     time.Time `json:"LastSeen"`
	} `json:"Peer"`
}

func (s *Service) Status(ctx context.Context) View {
	s.mu.Lock()
	view := View{
		Settings:   s.settings,
		Connecting: s.connecting,
		Error:      s.lastError,
		Health:     []string{},
		IPs:        []string{},
		Peers:      []Peer{},
	}
	s.mu.Unlock()
	view.SuggestedSubnet = localSubnet()

	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	output, err := s.run(ctx, "status", "--json")
	if errors.Is(err, ErrNotInstalled) {
		return view
	}
	view.Installed = true

	var parsed status
	// `tailscale status` exits with an error while logged out, but still prints JSON.
	if jsonErr := json.Unmarshal(output, &parsed); jsonErr != nil {
		if err != nil {
			view.Error = err.Error()
		}
		return view
	}

	view.State = parsed.BackendState
	view.AuthURL = parsed.AuthURL
	if parsed.Health != nil {
		view.Health = parsed.Health
	}
	if parsed.CurrentTailnet != nil {
		view.Tailnet = parsed.CurrentTailnet.Name
	}
	if parsed.Self != nil && parsed.BackendState == "Running" {
		view.Name = parsed.Self.HostName
		view.DNSName = strings.TrimSuffix(parsed.Self.DNSName, ".")
		view.IPs = nonNil(parsed.Self.TailscaleIPs)
		view.SubnetApproved = view.Settings.ShareSubnet && slices.Contains(parsed.Self.PrimaryRoutes, view.Settings.Subnet)
	}

	for _, peer := range parsed.Peer {
		view.Peers = append(view.Peers, Peer{
			Name:     peer.HostName,
			DNSName:  strings.TrimSuffix(peer.DNSName, "."),
			OS:       peer.OS,
			IPs:      nonNil(peer.TailscaleIPs),
			Online:   peer.Online,
			LastSeen: peer.LastSeen,
		})
	}
	slices.SortFunc(view.Peers, func(a, b Peer) int { return strings.Compare(a.Name, b.Name) })

	if view.State == "Running" {
		view.Serving = s.serving(ctx)
		if view.Serving && view.DNSName != "" {
			view.ServeURL = "https://" + view.DNSName
		}
	}

	return view
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// serving reports whether Serve forwards HTTPS to the manager.
func (s *Service) serving(ctx context.Context) bool {
	output, err := s.run(ctx, "serve", "status", "--json")
	if err != nil {
		return false
	}

	var config struct {
		TCP map[string]json.RawMessage `json:"TCP"`
		Web map[string]json.RawMessage `json:"Web"`
	}
	if json.Unmarshal(output, &config) != nil {
		return false
	}
	if _, ok := config.TCP["443"]; ok {
		return true
	}
	for host := range config.Web {
		if strings.HasSuffix(host, ":443") {
			return true
		}
	}

	return false
}

// upArgs builds `tailscale up` with every setting we use. --reset makes
// Tailscale forget settings we don't mention, so the saved settings are the only source.
func (s *Service) upArgs(settings Settings, timeout time.Duration) []string {
	args := []string{
		"up", "--reset",
		"--operator=" + s.Operator,
		// Keep the container's own DNS, so homelab.local and the LAN keep working.
		"--accept-dns=false",
		"--timeout=" + timeout.String(),
	}
	if settings.ShareSubnet {
		args = append(args, "--advertise-routes="+settings.Subnet)
	}

	return args
}

// Connect starts a login. Without an auth key, Tailscale shows a login link in
// the status, which the page opens.
func (s *Service) Connect(background context.Context, authKey string) error {
	s.mu.Lock()
	if s.connecting {
		s.mu.Unlock()
		return ErrBusy
	}
	s.connecting = true
	s.lastError = ""
	settings := s.settings
	s.mu.Unlock()

	args := s.upArgs(settings, loginTimeout)
	var keyFile string
	if authKey = strings.TrimSpace(authKey); authKey != "" {
		// Pass the key in a file, so it doesn't show up in the process list.
		file, err := os.CreateTemp(s.DataDir, ".tailscale-key-*")
		if err == nil {
			_, err = file.WriteString(authKey)
			file.Close()
		}
		if err != nil {
			s.finishConnect(err)
			return err
		}
		keyFile = file.Name()
		args = append(args, "--auth-key=file:"+keyFile)
	}

	go func() {
		ctx, cancel := context.WithTimeout(background, loginTimeout+time.Minute)
		defer cancel()

		_, err := s.run(ctx, args...)
		if keyFile != "" {
			os.Remove(keyFile)
		}
		s.finishConnect(err)
	}()

	return nil
}

func (s *Service) finishConnect(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connecting = false
	if err != nil {
		s.lastError = err.Error()
		s.Logger.Warn("tailscale login", "error", err)
	}
}

func (s *Service) Logout(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	_, err := s.run(ctx, "logout")

	return err
}

// SetServe turns on or off HTTPS access to the manager at its tailnet name.
func (s *Service) SetServe(ctx context.Context, enabled bool) error {
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	args := []string{"serve", "--bg", "--yes", s.Backend}
	if !enabled {
		args = []string{"serve", "--https=443", s.Backend, "off"}
	}

	output, err := s.run(ctx, args...)
	// When HTTPS is off for the tailnet, serve prints a link to turn it on and waits.
	if link := enableLink.FindString(string(output)); link != "" {
		return fmt.Errorf("turn on HTTPS certificates for your tailnet first: %s", link)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("tailscale serve did not answer in time")
	}

	return err
}

// SaveSettings saves the settings and applies them when Tailscale runs.
func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	settings.Subnet = strings.TrimSpace(settings.Subnet)
	if settings.ShareSubnet {
		prefix, err := netip.ParsePrefix(settings.Subnet)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return fmt.Errorf("%w: the subnet must look like 192.168.1.0/24", ErrInvalidSettings)
		}
	}

	s.mu.Lock()
	previous := s.settings
	s.settings = settings
	err := s.save()
	if err != nil {
		s.settings = previous
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}

	if s.Status(ctx).State != "Running" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	_, err = s.run(ctx, s.upArgs(settings, 20*time.Second)...)

	return err
}

// save writes the settings. It needs s.mu.
func (s *Service) save() error {
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp")
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temp, s.path)
}

// localSubnet guesses the home network from the container's own address.
func localSubnet() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 ||
			strings.HasPrefix(iface.Name, "tailscale") || strings.HasPrefix(iface.Name, "docker") {
			continue
		}

		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ipNet, ok := address.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil || !ipNet.IP.IsPrivate() {
				continue
			}
			ones, _ := ipNet.Mask.Size()
			prefix, err := netip.ParsePrefix(fmt.Sprintf("%s/%d", ipNet.IP, ones))
			if err == nil {
				return prefix.Masked().String()
			}
		}
	}

	return ""
}
