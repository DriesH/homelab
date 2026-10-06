package agent

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The providers of cloud storage. All of them hold the media through rclone
// crypt, so the provider only sees encrypted data.
const (
	CloudAWS        = "aws"
	CloudR2         = "r2"
	CloudB2         = "b2"
	CloudWasabi     = "wasabi"
	CloudHetzner    = "hetzner"
	CloudS3         = "s3"
	CloudStorageBox = "storage-box"
	CloudSFTP       = "sftp"
)

var cloudProviders = map[string]struct {
	name string
	// s3Provider is the provider of the rclone s3 backend. Empty means
	// another backend.
	s3Provider string
}{
	CloudAWS:        {"Amazon S3", "AWS"},
	CloudR2:         {"Cloudflare R2", "Cloudflare"},
	CloudB2:         {"Backblaze B2", ""},
	CloudWasabi:     {"Wasabi", "Wasabi"},
	CloudHetzner:    {"Hetzner Object Storage", "Hetzner"},
	CloudS3:         {"S3", "Other"},
	CloudStorageBox: {"Hetzner Storage Box", ""},
	CloudSFTP:       {"SFTP", ""},
}

// The names of the remotes in the rclone config. CloudRemote is the storage
// itself, CloudCryptRemote the encrypted media in it.
const (
	CloudRemote      = "homelab-cloud"
	CloudCryptRemote = "homelab-media"
)

var (
	bucketPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{1,61}[A-Za-z0-9]$`)
	regionPattern   = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	endpointPattern = regexp.MustCompile(`^https://[A-Za-z0-9]([A-Za-z0-9.-]{0,252})(:[0-9]{1,5})?$`)
	// Keys of S3 and B2 are letters, digits and a few signs. Nothing that the
	// rclone config could read as a comment or a new line.
	cloudKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9/+=._-]{8,256}$`)
	cloudPathPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	cloudUserPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	// The crypt passwords come from the recovery key that the manager makes.
	cryptPasswordPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
)

// CloudConfig is the cloud storage for the media. The manager keeps it with
// its secrets, and the agent writes it to the rclone config on the host.
type CloudConfig struct {
	Provider string `json:"provider"`
	// Bucket, Region and Endpoint are for S3 and B2. AccessKey is the key ID
	// for B2.
	Bucket    string `json:"bucket,omitempty"`
	Region    string `json:"region,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	AccessKey string `json:"accessKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
	// Host, Port, User and Password are for SFTP.
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	// Path is the folder for the media, in the bucket or on the server.
	Path string `json:"path,omitempty"`
	// CryptPassword and CryptSalt are the two passwords of rclone crypt.
	// Without them, nobody can read the media in the cloud.
	CryptPassword string `json:"cryptPassword"`
	CryptSalt     string `json:"cryptSalt"`
}

func (c CloudConfig) sftp() bool {
	return c.Provider == CloudStorageBox || c.Provider == CloudSFTP
}

// Validate checks the storage and the recovery key.
func (c CloudConfig) Validate() error {
	if !cryptPasswordPattern.MatchString(c.CryptPassword) || !cryptPasswordPattern.MatchString(c.CryptSalt) {
		return fmt.Errorf("%w: invalid recovery key", ErrInvalidAnswers)
	}

	return c.ValidateStorage()
}

// ValidateStorage checks the storage only. A connection test needs no recovery key yet.
func (c CloudConfig) ValidateStorage() error {
	provider, ok := cloudProviders[c.Provider]
	if !ok {
		return fmt.Errorf("%w: unknown provider %q", ErrInvalidAnswers, c.Provider)
	}

	var problem string
	switch {
	case c.Path != "" && !validCloudPath(c.Path):
		problem = "the folder can only have letters, digits, dots, dashes and slashes"
	case c.sftp():
		problem = c.sftpProblem()
	default:
		problem = c.bucketProblem(provider.s3Provider)
	}
	if problem != "" {
		return fmt.Errorf("%w: %s", ErrInvalidAnswers, problem)
	}

	return nil
}

func validCloudPath(path string) bool {
	if !cloudPathPattern.MatchString(path) || len(path) > 255 {
		return false
	}

	for part := range strings.SplitSeq(path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}

	return true
}

func (c CloudConfig) sftpProblem() string {
	switch {
	case !hostPattern.MatchString(c.Host):
		return "invalid server address"
	case c.Port < 1 || c.Port > 65535:
		return "invalid port"
	case !cloudUserPattern.MatchString(c.User):
		return "invalid username"
	case c.Password == "" || len(c.Password) > 256 || strings.ContainsFunc(c.Password, isControl):
		return "invalid password"
	}

	return ""
}

func (c CloudConfig) bucketProblem(s3Provider string) string {
	switch {
	case !bucketPattern.MatchString(c.Bucket):
		return "invalid bucket name"
	case !cloudKeyPattern.MatchString(c.AccessKey):
		return "invalid access key"
	case !cloudKeyPattern.MatchString(c.SecretKey):
		return "invalid secret key"
	case c.Region != "" && !regionPattern.MatchString(c.Region):
		return "invalid region"
	case c.Endpoint != "" && !endpointPattern.MatchString(c.Endpoint):
		return "the endpoint must be an https:// address"
	case c.Endpoint == "" && s3Provider != "" && s3Provider != "AWS":
		return "give the endpoint of the storage"
	}

	return ""
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// Label names the storage, like "Cloudflare R2: media/homelab". The Health
// page shows it.
func (c CloudConfig) Label() string {
	where := c.Bucket
	if c.sftp() {
		where = c.User + "@" + c.Host
	}
	if c.Path != "" {
		where += "/" + c.Path
	}

	return cloudProviders[c.Provider].name + ": " + where
}

// rcloneConfig is the rclone config with both remotes. Passwords are obscured
// like rclone does it.
func (c CloudConfig) rcloneConfig(random io.Reader) (string, error) {
	var lines []string
	add := func(key, value string) {
		if value != "" {
			lines = append(lines, key+" = "+value)
		}
	}
	obscured := func(value string) (string, error) { return obscure(value, random) }

	lines = append(lines, "["+CloudRemote+"]")
	switch {
	case c.sftp():
		password, err := obscured(c.Password)
		if err != nil {
			return "", err
		}
		add("type", "sftp")
		add("host", c.Host)
		add("port", strconv.Itoa(c.Port))
		add("user", c.User)
		add("pass", password)
		add("shell_type", "unix")
	case c.Provider == CloudB2:
		add("type", "b2")
		add("account", c.AccessKey)
		add("key", c.SecretKey)
		// Delete files for real. Otherwise B2 keeps old versions, and you pay for them.
		add("hard_delete", "true")
	default:
		add("type", "s3")
		add("provider", cloudProviders[c.Provider].s3Provider)
		add("access_key_id", c.AccessKey)
		add("secret_access_key", c.SecretKey)
		add("region", c.Region)
		add("endpoint", c.Endpoint)
		if c.Provider == CloudR2 {
			// Tokens for one bucket can't check that the bucket exists.
			add("no_check_bucket", "true")
		}
	}

	password, err := obscured(c.CryptPassword)
	if err != nil {
		return "", err
	}
	salt, err := obscured(c.CryptSalt)
	if err != nil {
		return "", err
	}
	lines = append(lines, "", "["+CloudCryptRemote+"]")
	add("type", "crypt")
	add("remote", c.target())
	add("password", password)
	add("password2", salt)

	return strings.Join(lines, "\n") + "\n", nil
}

// target is where the media goes: the bucket and folder, or the folder on the server.
func (c CloudConfig) target() string {
	var parts []string
	if !c.sftp() {
		parts = append(parts, c.Bucket)
	}
	if c.Path != "" {
		parts = append(parts, c.Path)
	}

	return CloudRemote + ":" + strings.Join(parts, "/")
}

// WriteCloudConfig writes the rclone config and the label of the storage in
// dir. Only root can read the config.
func WriteCloudConfig(dir string, config CloudConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	content, err := config.rcloneConfig(rand.Reader)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "rclone.conf"), []byte(content), 0o600); err != nil {
		return err
	}

	return writeFileAtomic(filepath.Join(dir, "label"), []byte(config.Label()+"\n"), 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(temp, data, mode); err != nil {
		return err
	}

	return os.Rename(temp, path)
}

// rcloneObscureKey is the fixed key that rclone uses to obscure passwords in
// its config. It only stops someone from reading a password over your
// shoulder. The file mode is what keeps the config secret.
var rcloneObscureKey = []byte{
	0x9c, 0x93, 0x5b, 0x48, 0x73, 0x0a, 0x55, 0x4d,
	0x6b, 0xfd, 0x7c, 0x63, 0xc8, 0x86, 0xa9, 0x2b,
	0xd3, 0x90, 0x19, 0x8e, 0xb8, 0x12, 0x8a, 0xfb,
	0xf4, 0xde, 0x16, 0x2b, 0x8b, 0x95, 0xf6, 0x38,
}

// obscure does what `rclone obscure` does: AES-CTR with a random IV in front.
func obscure(value string, random io.Reader) (string, error) {
	block, err := aes.NewCipher(rcloneObscureKey)
	if err != nil {
		return "", err
	}

	out := make([]byte, aes.BlockSize+len(value))
	iv := out[:aes.BlockSize]
	if _, err := io.ReadFull(random, iv); err != nil {
		return "", err
	}
	cipher.NewCTR(block, iv).XORKeyStream(out[aes.BlockSize:], []byte(value))

	return base64.RawURLEncoding.EncodeToString(out), nil
}
