package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

var errQBittorrentLogin = errors.New("qbittorrent: login failed")

type qbittorrent struct {
	baseURL string
	http    *http.Client
}

func newQBittorrent(baseURL string, client *http.Client) *qbittorrent {
	jar, _ := cookiejar.New(nil)

	return &qbittorrent{
		baseURL: strings.TrimRight(baseURL, "/") + "/api/v2",
		http:    &http.Client{Transport: client.Transport, Timeout: client.Timeout, Jar: jar},
	}
}

// ready succeeds once the Web UI answers. 403 is fine: we aren't logged in yet.
func (q *qbittorrent) ready(ctx context.Context) error {
	err := q.post(ctx, "/app/version", nil)

	var status *statusError
	if errors.As(err, &status) && status.status == http.StatusForbidden {
		return nil
	}

	return err
}

func (q *qbittorrent) login(ctx context.Context, username, password string) error {
	if err := q.post(ctx, "/auth/login", url.Values{"username": {username}, "password": {password}}); err != nil {
		return err
	}

	target, _ := url.Parse(q.baseURL)
	// qBittorrent 5.2 names the cookie QBT_SID_<port>, older versions SID.
	for _, cookie := range q.http.Jar.Cookies(target) {
		if cookie.Name == "SID" || strings.HasPrefix(cookie.Name, "QBT_SID") {
			return nil
		}
	}

	return errQBittorrentLogin
}

func (q *qbittorrent) setPreferences(ctx context.Context, preferences map[string]any) error {
	data, err := json.Marshal(preferences)
	if err != nil {
		return err
	}

	return q.post(ctx, "/app/setPreferences", url.Values{"json": {string(data)}})
}

func (q *qbittorrent) ensureCategory(ctx context.Context, name string) error {
	err := q.post(ctx, "/torrents/createCategory", url.Values{"category": {name}, "savePath": {""}})

	// 409 means the category already exists.
	var status *statusError
	if errors.As(err, &status) && status.status == http.StatusConflict {
		return nil
	}

	return err
}

func (q *qbittorrent) post(ctx context.Context, path string, form url.Values) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, q.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := q.http.Do(request)
	if err != nil {
		return fmt.Errorf("qbittorrent: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("qbittorrent %s: %w", path, &statusError{response.StatusCode, strings.TrimSpace(string(message))})
	}

	return nil
}
