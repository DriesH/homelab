package tlsca

import (
	"crypto/x509"
	"testing"
)

func TestLeafVerifiesAgainstCAAndOnlyForHostname(t *testing.T) {
	authority, err := Load(t.TempDir(), "homelab.local")
	if err != nil {
		t.Fatal(err)
	}

	leaf, err := authority.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(authority.CertPEM())

	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "homelab.local"}); err != nil {
		t.Fatalf("leaf does not verify: %v", err)
	}
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "example.com"}); err == nil {
		t.Fatal("leaf verified for another hostname")
	}
}

func TestLoadReusesExistingCA(t *testing.T) {
	dir := t.TempDir()

	first, err := Load(dir, "homelab.local")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(dir, "homelab.local")
	if err != nil {
		t.Fatal(err)
	}

	if string(first.CertPEM()) != string(second.CertPEM()) {
		t.Fatal("CA was regenerated")
	}
}
