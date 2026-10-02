package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homelab/internal/agent"
)

type fakeAgent struct {
	status  agent.CloudStatus
	err     error
	tested  []agent.CloudConfig
	enabled []agent.CloudConfig
}

func (f *fakeAgent) CloudStatus(context.Context) (agent.CloudStatus, error) { return f.status, f.err }

func (f *fakeAgent) TestCloud(_ context.Context, config agent.CloudConfig) error {
	f.tested = append(f.tested, config)
	return nil
}

func (f *fakeAgent) EnableCloud(_ context.Context, config agent.CloudConfig) error {
	f.enabled = append(f.enabled, config)
	return nil
}

func (f *fakeAgent) DisableCloud(context.Context) error { return nil }

func newTestService(t *testing.T) (*Service, *fakeAgent, *[]string) {
	t.Helper()
	fake := &fakeAgent{}
	var messages []string
	service, err := New(Options{
		DataDir: t.TempDir(),
		Agent:   fake,
		Notify:  func(_ context.Context, text string) { messages = append(messages, text) },
	})
	if err != nil {
		t.Fatal(err)
	}

	return service, fake, &messages
}

func r2Settings() Settings {
	return Settings{
		Provider: agent.CloudR2, AccountID: strings.Repeat("ab", 16), Bucket: "media",
		AccessKey: "0123456789abcdef", SecretKey: "secret-0123456789", Path: "homelab",
	}
}

func lastGroups(key string) string {
	groups := strings.Split(key, "-")
	return groups[len(groups)-2] + "-" + groups[len(groups)-1]
}

func TestSetupFlow(t *testing.T) {
	service, fake, _ := newTestService(t)
	ctx := context.Background()

	if err := service.Enable(ctx); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("enable without settings: %v", err)
	}
	if err := service.SaveSettings(ctx, r2Settings()); err != nil {
		t.Fatal(err)
	}
	if err := service.Enable(ctx); !errors.Is(err, ErrNoKey) {
		t.Fatalf("enable without a key: %v", err)
	}

	key, err := service.CreateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Enable(ctx); !errors.Is(err, ErrKeyNotConfirmed) {
		t.Fatalf("enable with an unconfirmed key: %v", err)
	}
	if err := service.ConfirmKey("AAAA-BBB"); !errors.Is(err, ErrWrongKeyEnd) {
		t.Fatalf("confirm with the wrong groups: %v", err)
	}
	if err := service.ConfirmKey(lastGroups(key)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateKey(); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("a second key after confirming: %v", err)
	}

	if err := service.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	config := fake.enabled[0]
	parsed, _ := ParseRecoveryKey(key)
	password, salt := parsed.CryptPasswords()
	if config.Endpoint != "https://"+strings.Repeat("ab", 16)+".r2.cloudflarestorage.com" || config.Region != "auto" ||
		config.CryptPassword != password || config.CryptSalt != salt {
		t.Errorf("config = %+v", config)
	}
}

func TestStatusHasNoSecrets(t *testing.T) {
	service, _, _ := newTestService(t)
	ctx := context.Background()
	service.SaveSettings(ctx, r2Settings())
	key, _ := service.CreateKey()

	view := service.Status(ctx)
	data, _ := json.Marshal(view)
	for _, secret := range []string{"secret-0123456789", key, lastGroups(key)} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the status has %q: %s", secret, data)
		}
	}
	if !view.Settings.HasSecretKey || view.Key != KeyUnconfirmed || view.Label != "Cloudflare R2: media/homelab" {
		t.Errorf("view = %s", data)
	}
}

func TestSaveSettingsKeepsSecrets(t *testing.T) {
	service, fake, _ := newTestService(t)
	ctx := context.Background()
	service.SaveSettings(ctx, r2Settings())

	changed := r2Settings()
	changed.SecretKey, changed.Bucket = "", "other-media"
	if err := service.SaveSettings(ctx, changed); err != nil {
		t.Fatal(err)
	}
	if err := service.Test(ctx, changed); err != nil {
		t.Fatal(err)
	}
	if got := fake.tested[0]; got.SecretKey != "secret-0123456789" || got.Bucket != "other-media" {
		t.Errorf("tested = %+v", got)
	}

	// Another provider never gets the secret of the old one.
	box := Settings{Provider: agent.CloudStorageBox, User: "u123456", Path: "media"}
	if err := service.SaveSettings(ctx, box); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Errorf("storage box without a password: %v", err)
	}
	box.Password = "box password"
	if err := service.SaveSettings(ctx, box); err != nil {
		t.Fatal(err)
	}
	config, _ := service.state.Settings.config()
	if config.Host != "u123456.your-storagebox.de" || config.Port != 23 {
		t.Errorf("storage box config = %+v", config)
	}
}

func TestSettingsAreLockedWhileEnabled(t *testing.T) {
	service, fake, _ := newTestService(t)
	fake.status.Enabled = true

	if err := service.SaveSettings(context.Background(), r2Settings()); !errors.Is(err, ErrEnabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestRevealKeyNotifies(t *testing.T) {
	service, _, messages := newTestService(t)
	ctx := context.Background()

	if _, err := service.RevealKey(ctx); !errors.Is(err, ErrNoKey) {
		t.Fatalf("reveal without a key: %v", err)
	}
	key, _ := service.CreateKey()
	if got, err := service.RevealKey(ctx); err != nil || got != key {
		t.Fatalf("reveal = %q, %v", got, err)
	}
	if len(*messages) != 1 {
		t.Errorf("messages = %v", *messages)
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	service, fake, _ := newTestService(t)
	ctx := context.Background()
	service.SaveSettings(ctx, r2Settings())
	key, _ := service.CreateKey()
	service.ConfirmKey(lastGroups(key))
	rule := Rule{MinAgeDays: 60, TargetPercent: 70, MinSizeMB: 200, Hour: 3, Minute: 30}
	if err := service.SaveRule(rule); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(service.DataDir, "cloud.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cloud.json: %v, %v", info, err)
	}

	again, err := New(Options{DataDir: service.DataDir, Agent: fake})
	if err != nil {
		t.Fatal(err)
	}
	view := again.Status(ctx)
	if view.Key != KeyConfirmed || view.Rule != rule || view.Settings.Bucket != "media" {
		t.Errorf("view after restart = %+v", view)
	}
}

func TestRuleValidate(t *testing.T) {
	if err := DefaultRule.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []Rule{
		{MinAgeDays: 1, TargetPercent: 80, MinSizeMB: 100},
		{MinAgeDays: 30, TargetPercent: 99, MinSizeMB: 100},
		{MinAgeDays: 30, TargetPercent: 80, MinSizeMB: 1},
		{MinAgeDays: 30, TargetPercent: 80, MinSizeMB: 100, Hour: 24},
	} {
		if err := rule.Validate(); !errors.Is(err, agent.ErrInvalidAnswers) {
			t.Errorf("%+v: err = %v", rule, err)
		}
	}
}

func TestProviderSettings(t *testing.T) {
	cases := map[string]struct {
		settings Settings
		endpoint string
		problem  bool
	}{
		"hetzner":           {Settings{Provider: agent.CloudHetzner, Region: "fsn1"}, "https://fsn1.your-objectstorage.com", false},
		"hetzner elsewhere": {Settings{Provider: agent.CloudHetzner, Region: "ams1"}, "", true},
		"wasabi default":    {Settings{Provider: agent.CloudWasabi}, "https://s3.us-east-1.wasabisys.com", false},
		"aws":               {Settings{Provider: agent.CloudAWS, Region: "eu-west-1"}, "", false},
		"aws no region":     {Settings{Provider: agent.CloudAWS}, "", true},
		"r2 bad account":    {Settings{Provider: agent.CloudR2, AccountID: "xyz"}, "", true},
		"other s3":          {Settings{Provider: agent.CloudS3, Endpoint: "https://s3.example.com"}, "https://s3.example.com", false},
	}
	for name, c := range cases {
		c.settings.Bucket, c.settings.AccessKey, c.settings.SecretKey = "media", "0123456789abcdef", "secret-0123456789"
		config, err := c.settings.config()
		if (err != nil) != c.problem || (!c.problem && config.Endpoint != c.endpoint) {
			t.Errorf("%s: endpoint %q, err %v", name, config.Endpoint, err)
		}
	}
}
