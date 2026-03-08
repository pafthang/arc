package arc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdvancedNegotiationVendorJSON(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	Handle(e, http.MethodGet, "/neg-vendor", "neg_vendor", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	})
	req := httptest.NewRequest(http.MethodGet, "/neg-vendor", nil)
	req.Header.Set("Accept", "application/vnd.acme+json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("unexpected content-type: %s", ct)
	}
}
