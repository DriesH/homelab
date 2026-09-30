package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPostsMessageToChat(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	telegram := Telegram{BotToken: "TOKEN", ChatID: "42", BaseURL: server.URL}
	if err := telegram.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	if got["chat_id"] != "42" || got["text"] != "hello" {
		t.Fatalf("unexpected body: %v", got)
	}
}

func TestSendReturnsTelegramErrorWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer server.Close()

	err := Telegram{BotToken: "SECRET", ChatID: "1", BaseURL: server.URL}.Send(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("expected Telegram error, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatal("error leaks the bot token")
	}
}

func TestSendSkipsWhenNotConfigured(t *testing.T) {
	if err := (Telegram{}).Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
}
