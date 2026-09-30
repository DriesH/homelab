package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// apiClient is a small JSON client for the *arr apps and Jellyfin.
type apiClient struct {
	name    string
	baseURL string
	headers map[string]string
	http    *http.Client
}

type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.status, e.body)
}

func (c *apiClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	for key, value := range c.headers {
		request.Header.Set(key, value)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s: %w", c.name, err)
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("%s %s %s: %w", c.name, method, path, &statusError{response.StatusCode, strings.TrimSpace(string(message))})
	}

	if out == nil {
		return nil
	}

	return json.NewDecoder(response.Body).Decode(out)
}
