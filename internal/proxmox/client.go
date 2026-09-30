package proxmox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"homelab/internal/config"
)

type Client struct {
	baseURL string
	auth    string
	http    *http.Client
}

func New(cfg config.Proxmox) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if cfg.CAFile != "" {
		tlsConfig, err := clusterTLSConfig(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig = tlsConfig
	}

	return &Client{
		baseURL: strings.TrimRight(cfg.URL, "/") + "/api2/json",
		auth:    fmt.Sprintf("PVEAPIToken=%s=%s", cfg.TokenID, cfg.TokenSecret),
		http:    &http.Client{Transport: transport, Timeout: 15 * time.Second},
	}, nil
}

// clusterTLSConfig trusts only the Proxmox cluster CA. The node certificate
// often doesn't list the IP we connect to, so we check the chain but not the name.
// This is safe because the cluster CA only signs this cluster's nodes.
func clusterTLSConfig(caFile string) (*tls.Config, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("no certificates found in " + caFile)
	}

	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("proxmox sent no certificate")
			}

			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}

			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates})
			return err
		},
	}, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, form url.Values, out any) error {
	return c.do(ctx, http.MethodPost, path, form, out)
}

func (c *Client) do(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", c.auth)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("proxmox %s %s: %s %s", method, path, response.Status, strings.TrimSpace(string(message)))
	}

	envelope := struct {
		Data any `json:"data"`
	}{Data: out}

	return json.NewDecoder(response.Body).Decode(&envelope)
}
