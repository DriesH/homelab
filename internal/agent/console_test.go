package agent

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func newConsoleServer(t *testing.T, running bool) *httptest.Server {
	t.Helper()

	consoles := &Consoles{
		Logger:  slog.New(slog.DiscardHandler),
		command: func(int) *exec.Cmd { return exec.Command("sh", "-c", "echo ready; cat") },
		running: func(context.Context, int) bool { return running },
	}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/console/{vmid}", consoles)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

func readUntil(t *testing.T, ctx context.Context, conn *websocket.Conn, want string) {
	t.Helper()

	var output strings.Builder
	for !strings.Contains(output.String(), want) {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q, got %q: %v", want, output.String(), err)
		}
		output.Write(data)
	}
}

func TestConsoleRelaysTerminal(t *testing.T) {
	server := newConsoleServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/console/101", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	readUntil(t, ctx, conn, "ready")
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`)); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("hello console\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ctx, conn, "hello console")
}

func TestConsoleRefusesStoppedContainer(t *testing.T) {
	server := newConsoleServer(t, false)

	for path, want := range map[string]int{"/v1/console/101": http.StatusConflict, "/v1/console/5": http.StatusBadRequest} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != want {
			t.Errorf("%s: status %d (%s), want %d", path, response.StatusCode, body, want)
		}
	}
}
