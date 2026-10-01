// Package tlsca runs a small local certificate authority, because public CAs
// can't issue certificates for .local names.
package tlsca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	caValidity   = 10 * 365 * 24 * time.Hour
	leafValidity = 90 * 24 * time.Hour
	renewBefore  = 30 * 24 * time.Hour
)

type Authority struct {
	hostname string
	// names are the names in the server certificate: the hostname and the app names below it.
	names   []string
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte

	mu   sync.Mutex
	leaf *tls.Certificate
}

// Load reads the CA from dir, or creates one limited to hostname. The server
// certificate also gets appNames, which must be below hostname, like seerr.homelab.local.
func Load(dir, hostname string, appNames ...string) (*Authority, error) {
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca-key.pem")

	certPEM, err := os.ReadFile(certPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := create(certPath, keyPath, hostname); err != nil {
			return nil, err
		}
		certPEM, err = os.ReadFile(certPath)
	}
	if err != nil {
		return nil, err
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certBlock == nil || keyBlock == nil {
		return nil, errors.New("invalid CA files in " + dir)
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}

	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	names := append([]string{hostname}, appNames...)

	return &Authority{hostname: hostname, names: names, cert: cert, key: key, certPEM: certPEM}, nil
}

// CertPEM is the root certificate users install on their devices.
func (a *Authority) CertPEM() []byte {
	return a.certPEM
}

func (a *Authority) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.leaf == nil || time.Until(a.leaf.Leaf.NotAfter) < renewBefore {
		leaf, err := a.issueLeaf()
		if err != nil {
			return nil, err
		}
		a.leaf = leaf
	}

	return a.leaf, nil
}

func (a *Authority) issueLeaf() (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: randomSerial(),
		Subject:      pkix.Name{CommonName: a.hostname},
		DNSNames:     a.names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, err
	}

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, nil
}

func create(certPath, keyPath, hostname string) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "Homelab local CA", Organization: []string{"Homelab"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		// The CA can only sign for our own hostname, so a leaked key can't be used to fake other sites.
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{hostname},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}

	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}

	return os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func randomSerial() *big.Int {
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	return serial
}
