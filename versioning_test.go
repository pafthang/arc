package arc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIVersioningAndCallbacksInOpenAPI(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	callbacks := map[string]any{
		"onDone": map[string]any{
			"{$request.body#/callbackUrl}": map[string]any{
				"post": map[string]any{
					"responses": map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	g := e.Version("2").WithCallbacks(callbacks)
	HandleGroup(g, http.MethodGet, "/items", "v2_items", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	})

	spec := e.registry.OpenAPISpec()
	get := spec["paths"].(map[string]any)["/v2/items"].(map[string]any)["get"].(map[string]any)
	raw := mustJSON(get)
	if !strings.Contains(raw, "\"x-arc-version\":\"2\"") {
		t.Fatalf("version extension missing: %s", raw)
	}
	if !strings.Contains(raw, "\"callbacks\"") || !strings.Contains(raw, "onDone") {
		t.Fatalf("callbacks missing: %s", raw)
	}
}

func TestAPIVersioningMiddleware(t *testing.T) {
	type in struct{}
	type out struct {
		Version string `json:"version"`
	}
	e := New()
	e.Use(APIVersioning(APIVersioningConfig{Required: true}))
	Handle(e, http.MethodGet, "/versioned", "versioned_get", func(ctx context.Context, in *in) (*Response[out], error) {
		v, _ := APIVersionFromContext(ctx)
		return OK(out{Version: v}), nil
	})

	r := httptest.NewRequest(http.MethodGet, "/versioned?version=2", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"version\":\"2\"") {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
	if got := w.Header().Get("X-API-Version"); got != "2" {
		t.Fatalf("unexpected version header: %s", got)
	}
}

func TestAPIVersioningRouteMismatch(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	e.Use(APIVersioning(APIVersioningConfig{Header: "X-API-Version"}))
	Handle(e, http.MethodGet, "/vroute", "vroute_get", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	}, WithVersion("2"))

	r := httptest.NewRequest(http.MethodGet, "/vroute", nil)
	r.Header.Set("X-API-Version", "1")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on version mismatch, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGroupQueryVersioningStrategy(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	g := e.Group("/api").WithQueryVersioning("v", true, "")
	HandleGroup(g, http.MethodGet, "/items", "group_versioned_get", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	}, WithVersion("2"))

	req1 := httptest.NewRequest(http.MethodGet, "/api/items?v=2", nil)
	req1.Header.Set("X-API-Version", "1")
	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w1.Code, w1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req2.Header.Set("X-API-Version", "2")
	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("query strategy must ignore header and require query version, got %d", w2.Code)
	}
}
