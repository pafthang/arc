package arc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pafthang/orm"
)

type getUserInput struct {
	ID int64 `path:"id" validate:"required,min=1"`
}

type userDTO struct {
	ID int64 `json:"id"`
}

func TestRouterAndTypedHandler(t *testing.T) {
	e := New()
	Handle(e, http.MethodGet, "/users/{id}", "get_user", func(ctx context.Context, in *getUserInput) (*Response[userDTO], error) {
		return OK(userDTO{ID: in.ID}), nil
	})

	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var out userDTO
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ID != 42 {
		t.Fatalf("id=%d", out.ID)
	}
}

func TestValidationError(t *testing.T) {
	e := New()
	Handle(e, http.MethodGet, "/users/{id}", "get_user", func(ctx context.Context, in *getUserInput) (*Response[userDTO], error) {
		return OK(userDTO{ID: in.ID}), nil
	})

	r := httptest.NewRequest(http.MethodGet, "/users/0", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestORMErrorMapping(t *testing.T) {
	type in struct{}
	e := New()
	Handle(e, http.MethodGet, "/missing", "missing", func(ctx context.Context, in *in) (*Response[map[string]string], error) {
		return nil, orm.ErrNotFound
	})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCustomAPIError(t *testing.T) {
	type in struct{}
	e := New()
	Handle(e, http.MethodGet, "/bad", "bad", func(ctx context.Context, in *in) (*Response[map[string]string], error) {
		return nil, &APIError{Status: http.StatusTeapot, Code: "teapot", Message: "short and stout"}
	})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bad", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	type in struct{}
	e := New()
	e.Use(Recovery())
	Handle(e, http.MethodGet, "/panic", "panic", func(ctx context.Context, in *in) (*Response[map[string]string], error) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestDefaultErrorMapperFallback(t *testing.T) {
	type in struct{}
	e := New()
	Handle(e, http.MethodGet, "/err", "err", func(ctx context.Context, in *in) (*Response[map[string]string], error) {
		return nil, errors.New("boom")
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/err", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestErrorRequestIDFromHeader(t *testing.T) {
	type in struct{}
	e := New()
	Handle(e, http.MethodGet, "/err-rid", "err_rid", func(ctx context.Context, in *in) (*Response[map[string]string], error) {
		return nil, errors.New("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/err-rid", nil)
	req.Header.Set("X-Request-ID", "req-123")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", w.Code)
	}
	if got := w.Header().Get("X-Request-ID"); got != "req-123" {
		t.Fatalf("header request id mismatch: %s", got)
	}
	if !strings.Contains(w.Body.String(), "\"requestId\":\"req-123\"") {
		t.Fatalf("body request id missing: %s", w.Body.String())
	}
}

func TestErrorRequestIDGenerated(t *testing.T) {
	type in struct{}
	e := New()
	Handle(e, http.MethodGet, "/err-rid-auto", "err_rid_auto", func(ctx context.Context, in *in) (*Response[map[string]string], error) {
		return nil, errors.New("boom")
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/err-rid-auto", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", w.Code)
	}
	rid := w.Header().Get("X-Request-ID")
	if rid == "" {
		t.Fatalf("generated request id header is empty")
	}
	if !strings.Contains(w.Body.String(), "\"requestId\":\""+rid+"\"") {
		t.Fatalf("generated request id missing in body: header=%s body=%s", rid, w.Body.String())
	}
}

func TestHEADFallsBackToGET(t *testing.T) {
	type in struct{}
	type out struct {
		Msg string `json:"msg"`
	}
	e := New()
	Handle(e, http.MethodGet, "/head-fallback", "head_fallback", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{Msg: "hello"}), nil
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/head-fallback", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("head response should not contain body: %s", w.Body.String())
	}
}

func TestOptionsAutoAllow(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	Handle(e, http.MethodGet, "/opts", "opts_get", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	})
	Handle(e, http.MethodPost, "/opts", "opts_post", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/opts", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	allow := w.Header().Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") || !strings.Contains(allow, "OPTIONS") {
		t.Fatalf("allow header missing expected methods: %s", allow)
	}
}

func TestOptionsAutoAllowRunsGlobalMiddleware(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	e.Use(func(next Handler) Handler {
		return func(rc *RequestContext) error {
			rc.Writer.Header().Set("X-Test-Middleware", "1")
			return next(rc)
		}
	})
	Handle(e, http.MethodGet, "/opts-mw", "opts_mw_get", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/opts-mw", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Test-Middleware") != "1" {
		t.Fatalf("expected middleware header to be set")
	}
	allow := w.Header().Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "OPTIONS") {
		t.Fatalf("allow header missing expected methods: %s", allow)
	}
}

func TestHandleVariants(t *testing.T) {
	type in struct {
		ID int `path:"id"`
	}
	type out struct {
		ID int `json:"id"`
	}
	e := New()
	HandleOut(e, http.MethodGet, "/out/{id}", "out", func(ctx context.Context, in *in) (*out, error) {
		return &out{ID: in.ID}, nil
	})
	HandleErr(e, http.MethodPost, "/err/{id}", "err", func(ctx context.Context, in *in) error {
		return nil
	})
	HandleRawTyped(e, http.MethodGet, "/raw/{id}", "raw", func(ctx context.Context, in *in) (*RawResponse, error) {
		return Raw(http.StatusAccepted, "text/plain", []byte("raw-ok")), nil
	})
	HandleStreamTyped(e, http.MethodGet, "/stream/{id}", "stream", func(ctx context.Context, in *in) (*StreamResponse, error) {
		return Stream(http.StatusOK, "text/plain", bytes.NewBufferString("stream-ok")), nil
	})

	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/out/7", nil))
	if w1.Code != http.StatusOK || !strings.Contains(w1.Body.String(), "\"id\":7") {
		t.Fatalf("out status=%d body=%s", w1.Code, w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/err/7", nil))
	if w2.Code != http.StatusNoContent {
		t.Fatalf("err-only status=%d", w2.Code)
	}

	w3 := httptest.NewRecorder()
	e.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/raw/7", nil))
	if w3.Code != http.StatusAccepted || strings.TrimSpace(w3.Body.String()) != "raw-ok" {
		t.Fatalf("raw status=%d body=%s", w3.Code, w3.Body.String())
	}

	w4 := httptest.NewRecorder()
	e.ServeHTTP(w4, httptest.NewRequest(http.MethodGet, "/stream/7", nil))
	if w4.Code != http.StatusOK || strings.TrimSpace(w4.Body.String()) != "stream-ok" {
		t.Fatalf("stream status=%d body=%s", w4.Code, w4.Body.String())
	}
}

func TestFileHelper(t *testing.T) {
	type in struct{}
	e := New()
	HandleRawTyped(e, http.MethodGet, "/file", "file_download", func(ctx context.Context, in *in) (*RawResponse, error) {
		return File(http.StatusOK, "report.txt", []byte("hello-file")), nil
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/file", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != "hello-file" {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "report.txt") {
		t.Fatalf("content-disposition missing filename: %s", got)
	}
}
