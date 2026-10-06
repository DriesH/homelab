package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testCryptPassword = "c2VjcmV0LXBhc3N3b3JkLTE"
	testCryptSalt     = "c2VjcmV0LXNhbHQtdmFsdWU"
)

// The vectors of rclone's own tests (fs/config/obscure/obscure_test.go).
func TestObscureMatchesRclone(t *testing.T) {
	for _, test := range []struct{ in, iv, want string }{
		{"", "aaaaaaaaaaaaaaaa", "YWFhYWFhYWFhYWFhYWFhYQ"},
		{"potato", "aaaaaaaaaaaaaaaa", "YWFhYWFhYWFhYWFhYWFhYXMaGgIlEQ"},
		{"potato", "bbbbbbbbbbbbbbbb", "YmJiYmJiYmJiYmJiYmJiYp3gcEWbAw"},
	} {
		got, err := obscure(test.in, bytes.NewBufferString(test.iv))
		if err != nil || got != test.want {
			t.Errorf("obscure(%q) = %q, %v, want %q", test.in, got, err, test.want)
		}
	}
}

func validR2() CloudConfig {
	return CloudConfig{
		Provider: CloudR2, Bucket: "media", Region: "auto", Endpoint: "https://abc123.r2.cloudflarestorage.com",
		AccessKey: "0123456789abcdef", SecretKey: "abcdef0123456789abcdef", Path: "homelab",
		CryptPassword: testCryptPassword, CryptSalt: testCryptSalt,
	}
}

func validStorageBox() CloudConfig {
	return CloudConfig{
		Provider: CloudStorageBox, Host: "u123456.your-storagebox.de", Port: 23, User: "u123456",
		Password: "box password", Path: "homelab-media",
		CryptPassword: testCryptPassword, CryptSalt: testCryptSalt,
	}
}

func TestRcloneConfig(t *testing.T) {
	random := func() *bytes.Buffer { return bytes.NewBufferString(strings.Repeat("a", 64)) }
	password, _ := obscure(testCryptPassword, random())

	r2, err := validR2().rcloneConfig(random())
	if err != nil {
		t.Fatal(err)
	}
	want := `[homelab-cloud]
type = s3
provider = Cloudflare
access_key_id = 0123456789abcdef
secret_access_key = abcdef0123456789abcdef
region = auto
endpoint = https://abc123.r2.cloudflarestorage.com
no_check_bucket = true

[homelab-media]
type = crypt
remote = homelab-cloud:media/homelab
password = ` + password
	if !strings.HasPrefix(r2, want+"\npassword2 = ") {
		t.Errorf("r2 config:\n%s", r2)
	}

	box, err := validStorageBox().rcloneConfig(random())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"type = sftp", "host = u123456.your-storagebox.de", "port = 23", "user = u123456", "shell_type = unix", "remote = homelab-cloud:homelab-media"} {
		if !strings.Contains(box, "\n"+line+"\n") {
			t.Errorf("storage box config has no %q:\n%s", line, box)
		}
	}
	if strings.Contains(box, "box password") {
		t.Error("the SFTP password is not obscured")
	}

	b2 := CloudConfig{Provider: CloudB2, Bucket: "my-media", AccessKey: "0012345678", SecretKey: "K001abcdefgh", CryptPassword: testCryptPassword, CryptSalt: testCryptSalt}
	config, err := b2.rcloneConfig(random())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"type = b2", "account = 0012345678", "key = K001abcdefgh", "hard_delete = true", "remote = homelab-cloud:my-media"} {
		if !strings.Contains(config, "\n"+line+"\n") {
			t.Errorf("b2 config has no %q:\n%s", line, config)
		}
	}
}

func TestCloudConfigValidate(t *testing.T) {
	if err := validR2().Validate(); err != nil {
		t.Fatalf("r2: %v", err)
	}
	if err := validStorageBox().Validate(); err != nil {
		t.Fatalf("storage box: %v", err)
	}
	aws := validR2()
	aws.Provider, aws.Endpoint, aws.Region = CloudAWS, "", "eu-central-1"
	if err := aws.Validate(); err != nil {
		t.Fatalf("aws without an endpoint: %v", err)
	}

	bad := map[string]func(*CloudConfig){
		"unknown provider":          func(c *CloudConfig) { c.Provider = "dropbox" },
		"bucket with a slash":       func(c *CloudConfig) { c.Bucket = "media/x" },
		"key with a new line":       func(c *CloudConfig) { c.SecretKey = "abcdefgh\nendpoint = x" },
		"key with a comment sign":   func(c *CloudConfig) { c.AccessKey = "abcdefgh;x" },
		"http endpoint":             func(c *CloudConfig) { c.Endpoint = "http://abc.r2.cloudflarestorage.com" },
		"no endpoint for r2":        func(c *CloudConfig) { c.Endpoint = "" },
		"region with a space":       func(c *CloudConfig) { c.Region = "eu central" },
		"path that goes up":         func(c *CloudConfig) { c.Path = "../etc" },
		"path with a leading slash": func(c *CloudConfig) { c.Path = "/media" },
		"short crypt password":      func(c *CloudConfig) { c.CryptPassword = "short" },
		"no crypt salt":             func(c *CloudConfig) { c.CryptSalt = "" },
	}
	for name, change := range bad {
		config := validR2()
		change(&config)
		if err := config.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	badSFTP := map[string]func(*CloudConfig){
		"host with a space":         func(c *CloudConfig) { c.Host = "my host" },
		"port 0":                    func(c *CloudConfig) { c.Port = 0 },
		"user with a colon":         func(c *CloudConfig) { c.User = "u1:x" },
		"no password":               func(c *CloudConfig) { c.Password = "" },
		"password with a new line":  func(c *CloudConfig) { c.Password = "a\nb" },
		"password with a tab":       func(c *CloudConfig) { c.Password = "a\tb" },
		"password with a delete":    func(c *CloudConfig) { c.Password = "a\x7fb" },
		"user with a newline":       func(c *CloudConfig) { c.User = "u1\n" },
		"host that starts with dot": func(c *CloudConfig) { c.Host = ".example.com" },
	}
	for name, change := range badSFTP {
		config := validStorageBox()
		change(&config)
		if err := config.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestCloudConfigLabel(t *testing.T) {
	if label := validR2().Label(); label != "Cloudflare R2: media/homelab" {
		t.Errorf("r2 label = %q", label)
	}
	if label := validStorageBox().Label(); label != "Hetzner Storage Box: u123456@u123456.your-storagebox.de/homelab-media" {
		t.Errorf("storage box label = %q", label)
	}
}

func TestWriteCloudConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "homelab-cloud")

	if err := WriteCloudConfig(dir, validStorageBox()); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{".": 0o700, "rclone.conf": 0o600, "label": 0o644} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != mode {
			t.Errorf("%s: mode %v, err %v, want %v", name, info.Mode().Perm(), err, mode)
		}
	}
	if label, _ := os.ReadFile(filepath.Join(dir, "label")); string(label) != validStorageBox().Label()+"\n" {
		t.Errorf("label = %q", label)
	}

	bad := validStorageBox()
	bad.Port = 0
	if err := WriteCloudConfig(dir, bad); !errors.Is(err, ErrInvalidAnswers) {
		t.Errorf("invalid config: err = %v", err)
	}
}
