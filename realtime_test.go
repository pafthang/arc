package arc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSSEHelper(t *testing.T) {
	type in struct{}
	e := New()
	HandleSSE(e, http.MethodGet, "/events", "events_sse", func(ctx context.Context, in *in, s *SSEWriter) error {
		return s.WriteEvent(SSEEvent{Event: "ping", Data: "hello"})
	})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/events", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("unexpected content-type: %s", ct)
	}
	if !strings.Contains(w.Body.String(), "event: ping") || !strings.Contains(w.Body.String(), "data: hello") {
		t.Fatalf("invalid sse body: %s", w.Body.String())
	}
}

func TestWebSocketHelper(t *testing.T) {
	type in struct{}
	e := New()
	HandleWebSocket(e, http.MethodGet, "/ws", "ws_echo", WSConfig{}, func(ctx context.Context, in *in, ws *WSConn) error {
		msg, err := ws.ReadText()
		if err != nil {
			return err
		}
		return ws.WriteText("echo:" + msg)
	})
	srv := httptest.NewServer(e)
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("hi")); err != nil {
		t.Fatalf("write ws: %v", err)
	}
	_, p, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ws: %v", err)
	}
	if string(p) != "echo:hi" {
		t.Fatalf("unexpected ws payload: %s", string(p))
	}
}
