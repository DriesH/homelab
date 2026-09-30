package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab/internal/release"
)

type tarEntry struct {
	name     string
	body     string
	typeflag byte
}

func makeBundle(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(entry.body)), Typeflag: entry.typeflag}
		if entry.typeflag == tar.TypeSymlink {
			header.Linkname, header.Size = "/etc/passwd", 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		writer.Write([]byte(entry.body))
	}
	writer.Close()
	gz.Close()

	return buffer.Bytes()
}

func newTestUpgrader(t *testing.T) (*Upgrader, *[]string) {
	t.Helper()

	started := []string{}
	upgrader := &Upgrader{
		Dir:     t.TempDir(),
		Version: "v1.0.0",
		start: func(installScript, _ string) error {
			started = append(started, installScript)
			return nil
		},
		running: func() bool { return false },
		verify: func(signature release.Signature, sha256Hex string) error {
			if signature.SHA256 != sha256Hex {
				return release.ErrBadSignature
			}
			return nil
		},
	}

	return upgrader, &started
}

func signatureFor(t *testing.T, version string, bundle []byte) release.Signature {
	t.Helper()
	hash, err := release.Hash(bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}

	return release.Signature{Version: version, SHA256: hash}
}

func TestUpgradeStartsInstallScript(t *testing.T) {
	upgrader, started := newTestUpgrader(t)
	bundle := makeBundle(t,
		tarEntry{name: "homelab-v1.1.0-linux-amd64/", typeflag: tar.TypeDir},
		tarEntry{name: "homelab-v1.1.0-linux-amd64/install.sh", body: "#!/bin/bash\n", typeflag: tar.TypeReg},
		tarEntry{name: "homelab-v1.1.0-linux-amd64/homelab", body: "binary", typeflag: tar.TypeReg},
	)

	if err := upgrader.Start(bytes.NewReader(bundle), signatureFor(t, "v1.1.0", bundle)); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(upgrader.Dir, "bundle", "homelab-v1.1.0-linux-amd64", "install.sh")
	if len(*started) != 1 || (*started)[0] != want {
		t.Fatalf("started = %v, want %s", *started, want)
	}

	// The unit is gone but the script never wrote a result, so the upgrade failed.
	status := upgrader.Status()
	if status.State != UpgradeFailed || status.Version != "v1.1.0" {
		t.Fatalf("status = %+v", status)
	}

	os.WriteFile(upgrader.statusPath(), []byte(`{"state":"succeeded","version":"v1.1.0"}`), 0o600)
	if status := upgrader.Status(); status.State != UpgradeSucceeded {
		t.Fatalf("status = %+v", status)
	}
}

func TestUpgradeRejectsBadBundles(t *testing.T) {
	good := makeBundle(t, tarEntry{name: "homelab/install.sh", body: "#!/bin/bash\n", typeflag: tar.TypeReg})

	cases := map[string]struct {
		bundle    []byte
		signature func([]byte) release.Signature
	}{
		"old version": {good, func(b []byte) release.Signature { return signatureFor(t, "v1.0.0", b) }},
		"bad signature": {good, func([]byte) release.Signature {
			return release.Signature{Version: "v1.1.0", SHA256: strings.Repeat("0", 64)}
		}},
		"path outside": {
			makeBundle(t, tarEntry{name: "../evil.sh", body: "x", typeflag: tar.TypeReg}),
			func(b []byte) release.Signature { return signatureFor(t, "v1.1.0", b) },
		},
		"symlink": {
			makeBundle(t,
				tarEntry{name: "homelab/install.sh", body: "#!/bin/bash\n", typeflag: tar.TypeReg},
				tarEntry{name: "homelab/link", typeflag: tar.TypeSymlink}),
			func(b []byte) release.Signature { return signatureFor(t, "v1.1.0", b) },
		},
		"no install.sh": {
			makeBundle(t, tarEntry{name: "homelab/homelab", body: "x", typeflag: tar.TypeReg}),
			func(b []byte) release.Signature { return signatureFor(t, "v1.1.0", b) },
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			upgrader, started := newTestUpgrader(t)
			err := upgrader.Start(bytes.NewReader(c.bundle), c.signature(c.bundle))
			if !errors.Is(err, ErrInvalidBundle) || len(*started) != 0 {
				t.Fatalf("err = %v, started = %v", err, *started)
			}
			if _, statErr := os.Stat(filepath.Join(upgrader.Dir, "evil.sh")); statErr == nil {
				t.Fatal("file written outside the bundle folder")
			}
		})
	}
}

func TestUpgradeRefusesWhileRunning(t *testing.T) {
	upgrader, _ := newTestUpgrader(t)
	upgrader.running = func() bool { return true }

	if err := upgrader.Start(bytes.NewReader(nil), release.Signature{Version: "v2.0.0"}); !errors.Is(err, ErrUpgradeRunning) {
		t.Fatalf("err = %v", err)
	}
}
