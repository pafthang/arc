package arc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCacheResponsesMiddleware(t *testing.T) {
	type in struct{}
	e := New()
	e.Use(CacheResponses(2 * time.Second))
	counter := 0
	HandleOut(e, http.MethodGet, "/cached", "cached_get", func(ctx context.Context, in *in) (*map[string]any, error) {
		counter++
		out := map[string]any{"n": counter}
		return &out, nil
	})
	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/cached", nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("status1=%d", w1.Code)
	}
	if w1.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("first response should be MISS, got %s", w1.Header().Get("X-Cache"))
	}
	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/cached", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("status2=%d", w2.Code)
	}
	if w2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("second response should be HIT, got %s", w2.Header().Get("X-Cache"))
	}
	if counter != 1 {
		t.Fatalf("handler should be called once, got %d", counter)
	}
}

func TestCacheResponsesBypassForAuthorized(t *testing.T) {
	type in struct{}
	e := New()
	e.Use(CacheResponses(2 * time.Second))
	counter := 0
	HandleOut(e, http.MethodGet, "/secure-cached", "secure_cached_get", func(ctx context.Context, in *in) (*map[string]any, error) {
		counter++
		out := map[string]any{"n": counter}
		return &out, nil
	})

	req1 := httptest.NewRequest(http.MethodGet, "/secure-cached", nil)
	req1.Header.Set("Authorization", "Bearer token")
	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, req1)
	req2 := httptest.NewRequest(http.MethodGet, "/secure-cached", nil)
	req2.Header.Set("Authorization", "Bearer token")
	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, req2)
	if counter != 2 {
		t.Fatalf("authorized requests must bypass cache, counter=%d", counter)
	}
	if got := w2.Header().Get("X-Cache"); got != "BYPASS" {
		t.Fatalf("expected BYPASS, got %s", got)
	}
}

func TestCacheResponsesETag304(t *testing.T) {
	type in struct{}
	store := NewResponseCache()
	e := New()
	e.Use(CacheResponsesWithConfig(CacheConfig{
		TTL:            2 * time.Second,
		Store:          store,
		SkipAuthorized: true,
		SkipCookies:    true,
	}))
	counter := 0
	HandleOut(e, http.MethodGet, "/etagged", "etagged_get", func(ctx context.Context, in *in) (*map[string]any, error) {
		counter++
		out := map[string]any{"n": counter}
		return &out, nil
	})

	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/etagged", nil))
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("etag is required on cached response")
	}
	if counter != 1 {
		t.Fatalf("counter=%d", counter)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/etagged", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", w2.Code)
	}
	if counter != 1 {
		t.Fatalf("handler should not run for 304 cache hit, counter=%d", counter)
	}
}

func TestCacheResponsesSkipsNoStore(t *testing.T) {
	type in struct{}
	e := New()
	e.Use(CacheResponses(2 * time.Second))
	counter := 0
	Handle(e, http.MethodGet, "/nostore", "nostore_get", func(ctx context.Context, in *in) (*Response[map[string]any], error) {
		counter++
		resp := OK(map[string]any{"n": counter})
		resp.Headers = CacheControlHeaders(0)
		resp.Headers.Set("Cache-Control", "no-store")
		return resp, nil
	})

	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/nostore", nil))
	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/nostore", nil))
	if counter != 2 {
		t.Fatalf("no-store responses must not be cached, counter=%d", counter)
	}
	if got := w2.Header().Get("X-Cache"); got != "BYPASS" {
		t.Fatalf("expected BYPASS for no-store response, got %s", got)
	}
}

func TestCacheInvalidationOnWrite(t *testing.T) {
	type in struct{}
	type postIn struct {
		ID int64 `path:"id"`
	}
	store := NewResponseCache()
	e := New()
	e.Use(CacheResponsesWithConfig(CacheConfig{TTL: 5 * time.Second, Store: store}))
	e.Use(InvalidateCacheOnWrite(CacheInvalidationConfig{
		Store:                 store,
		InvalidateRequestPath: true,
	}))
	counter := 0
	HandleOut(e, http.MethodGet, "/products", "products_get", func(ctx context.Context, in *in) (*map[string]any, error) {
		counter++
		out := map[string]any{"n": counter}
		return &out, nil
	})
	HandleErr(e, http.MethodPost, "/products/{id}", "products_update", func(ctx context.Context, in *postIn) error {
		return nil
	})

	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/products", nil))
	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/products", nil))
	if counter != 1 || w2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected warm cache hit, counter=%d x-cache=%s", counter, w2.Header().Get("X-Cache"))
	}

	wp := httptest.NewRecorder()
	e.ServeHTTP(wp, httptest.NewRequest(http.MethodPost, "/products/1", nil))
	if wp.Code != http.StatusNoContent {
		t.Fatalf("post status=%d", wp.Code)
	}

	w3 := httptest.NewRecorder()
	e.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/products", nil))
	if counter != 2 {
		t.Fatalf("cache should be invalidated after write, counter=%d", counter)
	}
	if got := w3.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected MISS after invalidation, got %s", got)
	}
}
