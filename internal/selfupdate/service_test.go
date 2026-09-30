package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"homelab/internal/agent"
	"homelab/internal/release"
)

type fakeAgent struct {
	bundle    string
	signature release.Signature
}

func (f *fakeAgent) StartUpgrade(_ context.Context, bundle io.Reader, signature release.Signature) error {
	data, err := io.ReadAll(bundle)
	f.bundle, f.signature = string(data), signature
	return err
}

func (f *fakeAgent) UpgradeStatus(context.Context) (agent.UpgradeStatus, error) {
	return agent.UpgradeStatus{State: agent.UpgradeIdle}, nil
}

type fakeGitHub struct {
	version string
	token   string
	seen    []string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.seen = append(f.seen, r.URL.Path+" "+r.Header.Get("Authorization"))
	// Like GitHub, the signed download URL needs no token.
	if f.token != "" && !strings.HasPrefix(r.URL.Path, "/download/") && r.Header.Get("Authorization") != "Bearer "+f.token {
		http.NotFound(w, r)
		return
	}

	base := "http://" + r.Host
	switch r.URL.Path {
	case "/repos/owner/homelab/releases/latest":
		name := fmt.Sprintf("homelab-%s-linux-amd64.tar.gz", f.version)
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": f.version, "body": "What changed", "html_url": base + "/release",
			"assets": []map[string]any{
				{"name": name, "url": base + "/assets/1"},
				{"name": name + ".sig", "url": base + "/assets/2"},
			},
		})
	case "/assets/1":
		// GitHub redirects downloads to another host.
		http.Redirect(w, r, base+"/download/bundle", http.StatusFound)
	case "/download/bundle":
		w.Write([]byte("bundle bytes"))
	case "/assets/2":
		json.NewEncoder(w).Encode(release.Signature{Version: f.version, SHA256: "abc", Signature: "c2ln"})
	default:
		http.NotFound(w, r)
	}
}

func newTestService(t *testing.T, github *fakeGitHub) (*Service, *fakeAgent, *[]string) {
	t.Helper()

	server := httptest.NewServer(github)
	t.Cleanup(server.Close)

	fake := &fakeAgent{}
	messages := []string{}
	service, err := New(Options{
		DataDir: t.TempDir(),
		Version: "v1.0.0",
		Agent:   fake,
		Notify:  func(_ context.Context, text string) { messages = append(messages, text) },
		Logger:  slog.New(slog.DiscardHandler),
		APIURL:  server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveSettings(SettingsInput{Repo: "owner/homelab", Token: github.token}); err != nil {
		t.Fatal(err)
	}

	return service, fake, &messages
}

func TestCheckAndInstall(t *testing.T) {
	github := &fakeGitHub{version: "v1.1.0", token: "github_pat_test"}
	service, fake, _ := newTestService(t, github)
	ctx := context.Background()

	if err := service.Check(ctx); err != nil {
		t.Fatal(err)
	}
	view := service.Status(ctx)
	if !view.UpdateAvailable || view.Latest.Version != "v1.1.0" || view.Latest.Notes != "What changed" || !view.TokenSet {
		t.Fatalf("view = %+v", view)
	}

	if err := service.Install(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.bundle != "bundle bytes" || fake.signature.Version != "v1.1.0" {
		t.Fatalf("agent got bundle %q, signature %+v", fake.bundle, fake.signature)
	}

	for _, request := range github.seen {
		if strings.HasPrefix(request, "/download/") && strings.Contains(request, "Bearer") {
			t.Errorf("token sent to the download host: %s", request)
		}
	}
}

func TestNoUpdateForSameVersion(t *testing.T) {
	service, _, _ := newTestService(t, &fakeGitHub{version: "v1.0.0"})
	ctx := context.Background()

	if err := service.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if service.Status(ctx).UpdateAvailable {
		t.Fatal("same version should not be an update")
	}
	if err := service.Install(ctx); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("err = %v, want ErrNoUpdate", err)
	}
}

func TestPrivateRepoWithoutToken(t *testing.T) {
	github := &fakeGitHub{version: "v1.1.0", token: "secret"}
	service, _, _ := newTestService(t, github)
	service.SaveSettings(SettingsInput{Repo: "owner/homelab", ClearToken: true})

	err := service.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "add a token") {
		t.Fatalf("err = %v", err)
	}
}

func TestScheduledCheckNotifiesOnce(t *testing.T) {
	service, fake, messages := newTestService(t, &fakeGitHub{version: "v1.1.0"})
	ctx := context.Background()

	service.scheduledCheck(ctx)
	service.scheduledCheck(ctx)
	if len(*messages) != 1 || !strings.Contains((*messages)[0], "v1.1.0 is available") || fake.bundle != "" {
		t.Fatalf("messages = %v, bundle = %q", *messages, fake.bundle)
	}

	service.SaveSettings(SettingsInput{Repo: "owner/homelab", AutoInstall: true})
	service.scheduledCheck(ctx)
	if fake.bundle != "bundle bytes" {
		t.Fatalf("auto install did not run, messages = %v", *messages)
	}
}

func TestSettingsValidation(t *testing.T) {
	service, _, _ := newTestService(t, &fakeGitHub{version: "v1.1.0"})

	for _, repo := range []string{"", "homelab", "https://github.com/owner/homelab", "owner/home lab"} {
		if err := service.SaveSettings(SettingsInput{Repo: repo}); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("repo %q: err = %v", repo, err)
		}
	}
}
