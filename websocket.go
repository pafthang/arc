package arc

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// WSConfig configures websocket upgrader behavior.
type WSConfig struct {
	CheckOrigin func(*http.Request) bool
}

// WSConn wraps websocket connection.
type WSConn struct {
	Conn *websocket.Conn
}

// ReadText reads text message.
func (c *WSConn) ReadText() (string, error) {
	_, p, err := c.Conn.ReadMessage()
	return string(p), err
}

// WriteText writes text message.
func (c *WSConn) WriteText(msg string) error {
	return c.Conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

// Close closes websocket connection.
func (c *WSConn) Close() error { return c.Conn.Close() }

// HandleWebSocket registers websocket endpoint helper.
func HandleWebSocket[Input any](e *Engine, method, path, operationID string, cfg WSConfig, h func(context.Context, *Input, *WSConn) error, opts ...RouteOption) {
	up := websocket.Upgrader{
		ReadBufferSize:    1024,
		WriteBufferSize:   1024,
		EnableCompression: true,
		CheckOrigin: func(r *http.Request) bool {
			if cfg.CheckOrigin != nil {
				return cfg.CheckOrigin(r)
			}
			return true
		},
		HandshakeTimeout: 10 * time.Second,
	}

	registerTyped(e, method, path, operationID, opts, responseKindRaw, func(rc *RequestContext, in *Input) error {
		conn, err := up.Upgrade(rc.Writer, rc.Request, nil)
		if err != nil {
			return err
		}
		ws := &WSConn{Conn: conn}
		defer ws.Close()
		return h(rc.Ctx, in, ws)
	})
	var inT *Input
	attachInputOutputMeta(e, method, path, operationID, typeOfPtr(inT), nil)
}
