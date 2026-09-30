package health

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type CheckKind string

const (
	HTTPCheck CheckKind = "http"
	TCPCheck  CheckKind = "tcp"
)

const (
	checkTimeout = 10 * time.Second
	MaxChecks    = 50
)

var ErrInvalidCheck = errors.New("invalid service check")

// Check is a service the manager tries to reach, like Jellyfin or Radarr.
type Check struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Kind   CheckKind `json:"kind"`
	Target string    `json:"target"`
}

type CheckInput struct {
	Name   string    `json:"name"`
	Kind   CheckKind `json:"kind"`
	Target string    `json:"target"`
}

func (input CheckInput) Validate() error {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return fmt.Errorf("%w: the name must be 1 to 64 characters", ErrInvalidCheck)
	}

	switch input.Kind {
	case HTTPCheck:
		parsed, err := url.Parse(input.Target)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("%w: the URL must look like http://192.168.1.20:8096", ErrInvalidCheck)
		}
	case TCPCheck:
		host, port, err := net.SplitHostPort(input.Target)
		number, portErr := strconv.Atoi(port)
		if err != nil || host == "" || portErr != nil || number < 1 || number > 65535 {
			return fmt.Errorf("%w: the address must look like 192.168.1.20:22", ErrInvalidCheck)
		}
	default:
		return fmt.Errorf("%w: the type must be http or tcp", ErrInvalidCheck)
	}

	return nil
}

// A check sends no credentials and reads no data, so it accepts self-signed
// certificates, which most homelab apps use.
var probeClient = &http.Client{
	Timeout: checkTimeout,
	Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: true,
	},
	// A redirect already shows that the app answers.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// probe returns nil when the service answers. For HTTP, any status below 500
// counts, because a login page (401) still means the app runs.
func probe(ctx context.Context, check Check) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	if check.Kind == TCPCheck {
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "tcp", check.Target)
		if err != nil {
			return simplifyError(err)
		}
		return conn.Close()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, check.Target, nil)
	if err != nil {
		return err
	}

	response, err := probeClient.Do(request)
	if err != nil {
		return simplifyError(err)
	}
	response.Body.Close()

	if response.StatusCode >= 500 {
		return fmt.Errorf("HTTP %s", response.Status)
	}

	return nil
}

// simplifyError turns "dial tcp 10.0.0.5:8096: connect: connection refused" into "connection refused".
func simplifyError(err error) error {
	var netErr net.Error
	var syscallErr *os.SyscallError
	var opErr *net.OpError
	var urlErr *url.Error

	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return errors.New("timed out")
	case errors.As(err, &syscallErr):
		return syscallErr.Err
	case errors.As(err, &opErr) && opErr.Err != nil:
		return opErr.Err
	case errors.As(err, &urlErr):
		return urlErr.Err
	}

	return err
}
