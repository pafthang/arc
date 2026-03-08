package arc

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// SSEEvent describes one server-sent event.
type SSEEvent struct {
	ID    string
	Event string
	Data  string
	Retry time.Duration
}

// SSEWriter writes SSE events to response stream.
type SSEWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

// NewSSEWriter prepares SSE response headers.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	return &SSEWriter{w: w, f: f}, nil
}

// WriteEvent sends one SSE frame.
func (s *SSEWriter) WriteEvent(ev SSEEvent) error {
	if ev.ID != "" {
		if _, err := fmt.Fprintf(s.w, "id: %s\n", ev.ID); err != nil {
			return err
		}
	}
	if ev.Event != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", ev.Event); err != nil {
			return err
		}
	}
	if ev.Retry > 0 {
		if _, err := fmt.Fprintf(s.w, "retry: %d\n", ev.Retry.Milliseconds()); err != nil {
			return err
		}
	}
	if ev.Data != "" {
		if _, err := fmt.Fprintf(s.w, "data: %s\n", ev.Data); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprint(s.w, "data:\n"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(s.w, "\n"); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// HandleSSE registers route that streams server-sent events.
func HandleSSE[Input any](e *Engine, method, path, operationID string, h func(context.Context, *Input, *SSEWriter) error, opts ...RouteOption) {
	registerTyped(e, method, path, operationID, opts, responseKindStream, func(rc *RequestContext, in *Input) error {
		s, err := NewSSEWriter(rc.Writer)
		if err != nil {
			return err
		}
		rc.Writer.WriteHeader(http.StatusOK)
		return h(rc.Ctx, in, s)
	})

	var inT *Input
	attachInputOutputMeta(e, method, path, operationID, typeOfPtr(inT), nil)
}
