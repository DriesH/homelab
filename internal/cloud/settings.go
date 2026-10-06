package cloud

import (
	"fmt"
	"regexp"
	"slices"

	"homelab/internal/agent"
)

var (
	accountIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
	hetznerRegions   = []string{"fsn1", "nbg1", "hel1"}
)

// Settings are the cloud storage as the user fills it in. The service turns
// them into an agent.CloudConfig, with the endpoint of each provider.
type Settings struct {
	Provider string `json:"provider"`
	// AccountID is the account of Cloudflare R2. It is part of the endpoint.
	AccountID string `json:"accountId,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Region    string `json:"region,omitempty"`
	// Endpoint is only for another S3 storage. The others have a known endpoint.
	Endpoint  string `json:"endpoint,omitempty"`
	AccessKey string `json:"accessKey,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	User      string `json:"user,omitempty"`
	Password  string `json:"password,omitempty"`
	Path      string `json:"path,omitempty"`
}

// SettingsView is Settings without the secrets.
type SettingsView struct {
	Settings
	HasSecretKey bool `json:"hasSecretKey"`
	HasPassword  bool `json:"hasPassword"`
}

func (s Settings) view() SettingsView {
	view := SettingsView{Settings: s, HasSecretKey: s.SecretKey != "", HasPassword: s.Password != ""}
	view.SecretKey, view.Password = "", ""

	return view
}

// withSecretsFrom keeps the saved secrets when the form leaves them empty,
// because the browser never gets them back.
func (s Settings) withSecretsFrom(saved *Settings) Settings {
	if saved == nil || saved.Provider != s.Provider {
		return s
	}
	if s.SecretKey == "" {
		s.SecretKey = saved.SecretKey
	}
	if s.Password == "" {
		s.Password = saved.Password
	}

	return s
}

// config is the storage for the agent, with the endpoints filled in. The
// recovery key is not in it yet.
func (s Settings) config() (agent.CloudConfig, error) {
	config := agent.CloudConfig{
		Provider: s.Provider, Bucket: s.Bucket, Region: s.Region,
		AccessKey: s.AccessKey, SecretKey: s.SecretKey,
		Host: s.Host, Port: s.Port, User: s.User, Password: s.Password, Path: s.Path,
	}

	switch s.Provider {
	case agent.CloudR2:
		if !accountIDPattern.MatchString(s.AccountID) {
			return config, invalid("the account ID of Cloudflare is 32 letters and digits, see R2 > Overview")
		}
		config.Endpoint = "https://" + s.AccountID + ".r2.cloudflarestorage.com"
		config.Region = "auto"
	case agent.CloudHetzner:
		if !slices.Contains(hetznerRegions, s.Region) {
			return config, invalid("choose the location of the bucket: fsn1, nbg1 or hel1")
		}
		config.Endpoint = "https://" + s.Region + ".your-objectstorage.com"
	case agent.CloudWasabi:
		if s.Region == "" {
			config.Region = "us-east-1"
		}
		config.Endpoint = "https://s3." + config.Region + ".wasabisys.com"
	case agent.CloudAWS:
		if s.Region == "" {
			return config, invalid("give the region of the bucket, like eu-central-1")
		}
	case agent.CloudS3:
		config.Endpoint = s.Endpoint
	case agent.CloudStorageBox:
		if config.Port == 0 {
			// Port 23 is the SSH of the Storage Box itself, with sha1sum and md5sum for checks.
			config.Port = 23
		}
		if config.Host == "" {
			config.Host = s.User + ".your-storagebox.de"
		}
	case agent.CloudSFTP:
		if config.Port == 0 {
			config.Port = 22
		}
	}

	return config, config.ValidateStorage()
}

func invalid(problem string) error {
	return fmt.Errorf("%w: %s", agent.ErrInvalidAnswers, problem)
}

// Rule decides which media the nightly job moves to the cloud: the oldest
// video files while the local media is more than TargetPercent full, but
// never files younger than MinAgeDays or smaller than MinSizeMB.
type Rule struct {
	MinAgeDays    int `json:"minAgeDays"`
	TargetPercent int `json:"targetPercent"`
	MinSizeMB     int `json:"minSizeMb"`
	// Hour and Minute are when the job runs each night.
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

var DefaultRule = Rule{MinAgeDays: 30, TargetPercent: 80, MinSizeMB: 100, Hour: 5}

func (r Rule) Validate() error {
	switch {
	case r.MinAgeDays < 7 || r.MinAgeDays > 3650:
		return invalid("files must be at least 7 days old")
	case r.TargetPercent < 50 || r.TargetPercent > 95:
		return invalid("the target must be between 50% and 95%")
	case r.MinSizeMB < 10 || r.MinSizeMB > 100_000:
		return invalid("the smallest file must be at least 10 MB")
	case r.Hour < 0 || r.Hour > 23 || r.Minute < 0 || r.Minute > 59:
		return invalid("invalid time")
	}

	return nil
}
