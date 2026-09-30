// Package notify sends messages to the user.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Telegram struct {
	BotToken string
	ChatID   string
	// BaseURL is the Bot API address. Tests point it at a fake server.
	BaseURL string
	HTTP    *http.Client
}

func (t Telegram) Configured() bool {
	return t.BotToken != "" && t.ChatID != ""
}

// Send posts a plain-text message, so no Markdown escaping is needed.
func (t Telegram) Send(ctx context.Context, text string) error {
	if !t.Configured() {
		return nil
	}

	baseURL := t.BaseURL
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	body, err := json.Marshal(map[string]string{"chat_id": t.ChatID, "text": text})
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/bot"+t.BotToken+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		// The URL holds the bot token, so don't include it in the error.
		return fmt.Errorf("telegram: could not reach the Bot API")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		var result struct {
			Description string `json:"description"`
		}
		data, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		json.Unmarshal(data, &result)
		return fmt.Errorf("telegram: %s", strings.TrimSpace(result.Description))
	}

	return nil
}
