package arc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTenantExtractorFromHeader(t *testing.T) {
	type in struct{}
	type out struct {
		Tenant string `json:"tenant"`
	}
	e := New()
	e.Use(TenantExtractor(TenantFromHeader("X-Tenant-ID")))
	Handle(e, http.MethodGet, "/tenant", "tenant", func(ctx context.Context, in *in) (*Response[out], error) {
		tid, _ := TenantFromContext(ctx)
		return OK(out{Tenant: tid}), nil
	})

	r := httptest.NewRequest(http.MethodGet, "/tenant", nil)
	r.Header.Set("X-Tenant-ID", "acme")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if got := w.Body.String(); !strings.Contains(got, "acme") {
		t.Fatalf("body=%s", got)
	}
}

func TestTenantExtractorFromJWTClaim(t *testing.T) {
	type in struct{}
	type out struct {
		Tenant string `json:"tenant"`
	}
	e := New()
	e.Use(TenantExtractor(TenantFromJWTClaim("tenant_id")))
	Handle(e, http.MethodGet, "/jwt-tenant", "jwt_tenant", func(ctx context.Context, in *in) (*Response[out], error) {
		tid, _ := TenantFromContext(ctx)
		return OK(out{Tenant: tid}), nil
	})

	token := testJWT(map[string]any{"tenant_id": "acme-jwt"})
	req := httptest.NewRequest(http.MethodGet, "/jwt-tenant", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"tenant\":\"acme-jwt\"") {
		t.Fatalf("tenant not extracted: %s", w.Body.String())
	}
}

func TestTenantExtractorFromVerifiedJWTClaim(t *testing.T) {
	type in struct{}
	type out struct {
		Tenant string `json:"tenant"`
	}
	secret := []byte("super-secret")
	verifier := NewHMACJWTVerifier(secret).WithLeeway(0)

	e := New()
	e.Use(TenantExtractor(TenantFromVerifiedJWTClaim("tenant_id", verifier)))
	Handle(e, http.MethodGet, "/jwt-tenant-verified", "jwt_tenant_verified", func(ctx context.Context, in *in) (*Response[out], error) {
		tid, _ := TenantFromContext(ctx)
		return OK(out{Tenant: tid}), nil
	})

	token := testHMACJWT(secret, map[string]any{"tenant_id": "acme-secure"})
	req := httptest.NewRequest(http.MethodGet, "/jwt-tenant-verified", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"tenant\":\"acme-secure\"") {
		t.Fatalf("tenant not extracted: %s", w.Body.String())
	}
}

func TestTenantExtractorFromVerifiedJWTClaimRejectsInvalid(t *testing.T) {
	type in struct{}
	type out struct {
		Tenant string `json:"tenant"`
	}
	secret := []byte("super-secret")
	verifier := NewHMACJWTVerifier(secret)

	e := New()
	e.Use(TenantExtractor(TenantFromVerifiedJWTClaim("tenant_id", verifier)))
	Handle(e, http.MethodGet, "/jwt-tenant-verified", "jwt_tenant_verified_invalid", func(ctx context.Context, in *in) (*Response[out], error) {
		tid, _ := TenantFromContext(ctx)
		return OK(out{Tenant: tid}), nil
	})

	badToken := testJWT(map[string]any{"tenant_id": "evil"})
	req := httptest.NewRequest(http.MethodGet, "/jwt-tenant-verified", nil)
	req.Header.Set("Authorization", "Bearer "+badToken)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "\"tenant\":\"evil\"") {
		t.Fatalf("invalid token must not be accepted: %s", w.Body.String())
	}
}

func TestReadinessEndpointState(t *testing.T) {
	e := New()
	e.RegisterHealthRoutes()
	e.SetReady(false)
	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w1.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready status=%d body=%s", w1.Code, w1.Body.String())
	}
	e.SetReady(true)
	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("ready status=%d body=%s", w2.Code, w2.Body.String())
	}
}

func TestNewServerSetsEngineNotReady(t *testing.T) {
	e := New()
	if !e.IsReady() {
		t.Fatalf("engine should start ready")
	}
	_ = NewServer(":0", e)
	if e.IsReady() {
		t.Fatalf("engine should be marked not ready after NewServer")
	}
}
