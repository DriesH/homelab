package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"homelab/internal/agent"
	"homelab/internal/cloud"
)

type fakeCloud struct {
	revealed int
	testErr  error
}

func (f *fakeCloud) Status(context.Context) cloud.View                  { return cloud.View{Key: cloud.KeyNone} }
func (f *fakeCloud) SaveSettings(context.Context, cloud.Settings) error { return nil }
func (f *fakeCloud) Test(context.Context, cloud.Settings) error         { return f.testErr }
func (f *fakeCloud) CreateKey() (string, error)                         { return "HLC1-AAAA", nil }
func (f *fakeCloud) ConfirmKey(string) error                            { return cloud.ErrWrongKeyEnd }
func (f *fakeCloud) SaveRule(cloud.Rule) error                          { return nil }
func (f *fakeCloud) Enable(context.Context) error                       { return cloud.ErrKeyNotConfirmed }
func (f *fakeCloud) Disable(context.Context) error                      { return nil }
func (f *fakeCloud) RevealKey(context.Context) (string, error) {
	f.revealed++
	return "HLC1-SECRET", nil
}

func TestCloudRevealKeyNeedsPasswordAndCode(t *testing.T) {
	fake := &fakeCloud{}
	server, _ := newTestServerWithOptions(t, func(options *Options) { options.Cloud = fake })
	cookie := login(t, server)
	url := server.URL + "/api/cloud/key/reveal"
	// The login used the code of now, and a code works only once.
	next := codeAt(t, time.Now().Add(30*time.Second))

	for name, body := range map[string]string{
		"no code":        `{"password":"secret"}`,
		"wrong password": fmt.Sprintf(`{"password":"wrong","code":%q}`, next),
	} {
		if response := request(t, http.MethodPost, url, body, cookie); response.StatusCode != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %s", name, response.Status)
		}
	}
	if fake.revealed != 0 {
		t.Fatal("the key was revealed without the password and code")
	}

	response := request(t, http.MethodPost, url, fmt.Sprintf(`{"password":"secret","code":%q}`, next), cookie)
	var body struct{ Key string }
	json.NewDecoder(response.Body).Decode(&body)
	if response.StatusCode != http.StatusOK || body.Key != "HLC1-SECRET" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("reveal: %s %+v %v", response.Status, body, response.Header)
	}

	if response := request(t, http.MethodPost, url, fmt.Sprintf(`{"password":"secret","code":%q}`, next), cookie); response.StatusCode != http.StatusForbidden {
		t.Errorf("the same code twice: expected 403, got %s", response.Status)
	}
}

func TestCloudRoutes(t *testing.T) {
	fake := &fakeCloud{testErr: &agent.RefusedError{Message: "AccessDenied: wrong keys"}}
	server, _ := newTestServerWithOptions(t, func(options *Options) { options.Cloud = fake })

	if response := request(t, http.MethodGet, server.URL+"/api/cloud", "", nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a session: expected 401, got %s", response.Status)
	}

	cookie := login(t, server)
	cases := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/cloud", "", http.StatusOK},
		{http.MethodPost, "/api/cloud/test", `{"provider":"r2"}`, http.StatusUnprocessableEntity},
		{http.MethodPost, "/api/cloud/key/confirm", `{"groups":"AAAA-BBBB"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/cloud/enable", "", http.StatusConflict},
		{http.MethodPut, "/api/cloud/rule", `{"minAgeDays":30}`, http.StatusNoContent},
	}
	for _, c := range cases {
		response := request(t, c.method, server.URL+c.path, c.body, cookie)
		if response.StatusCode != c.status {
			t.Errorf("%s %s: expected %d, got %s", c.method, c.path, c.status, response.Status)
		}
	}
}
