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

func TestLeafCoversAppNamesWithTheSameCA(t *testing.T) {
	dir := t.TempDir()
	// A CA from an older install, made without app names.
	if _, err := Load(dir, "homelab.local"); err != nil {
		t.Fatal(err)
	}

	authority, err := Load(dir, "homelab.local", "seerr.homelab.local", "jellyfin.homelab.local")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := authority.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(authority.CertPEM())
	for _, name := range []string{"homelab.local", "seerr.homelab.local", "jellyfin.homelab.local"} {
		if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name}); err != nil {
			t.Errorf("%s does not verify: %v", name, err)
		}
	}

	// The name limit of the CA still holds for names outside homelab.local.
	outside, err := Load(dir, "homelab.local", "seerr.example.com")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := outside.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "seerr.example.com"}); err == nil {
		t.Fatal("the CA signed a name outside homelab.local")
	}
}
