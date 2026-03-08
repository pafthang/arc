package arc

import (
	"context"
	"net/http"
	"time"
)

// Server wraps http.Server lifecycle helpers.
type Server struct {
	engine *Engine
	http   *http.Server
}

func NewServer(addr string, engine *Engine) *Server {
	if engine == nil {
		engine = New()
	}
	engine.SetReady(false)
	s := &http.Server{Addr: addr, Handler: engine}
	return &Server{engine: engine, http: s}
}

func (s *Server) Start() error {
	s.engine.SetReady(true)
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.engine.SetReady(false)
	if ctx == nil {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ctx = c
	}
	return s.http.Shutdown(ctx)
}

func (e *Engine) RegisterHealthRoutes() {
	e.RegisterRaw(http.MethodGet, "/health", "health", func(rc *RequestContext) error {
		return e.encoder.Encode(rc.Writer, http.StatusOK, map[string]any{"status": "ok"})
	})
	e.RegisterRaw(http.MethodGet, "/ready", "ready", func(rc *RequestContext) error {
		if e.IsReady() {
			return e.encoder.Encode(rc.Writer, http.StatusOK, map[string]any{"status": "ready"})
		}
		return e.encoder.Encode(rc.Writer, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
	})
}
