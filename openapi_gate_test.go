package arc

import (
	"context"
	"net/http"
	"testing"
)

func TestValidateOpenAPIQualityPasses(t *testing.T) {
	type in struct {
		Name string `json:"name"`
	}
	type out struct {
		ID int64 `json:"id"`
	}
	e := New()
	e.SetOpenAPIServers([]map[string]any{{"url": "http://localhost:8080/api/v1", "description": "dev"}})
	e.RegisterOpenAPISecurityScheme("BearerAuth", map[string]any{
		"type":         "http",
		"scheme":       "bearer",
		"bearerFormat": "JWT",
	})
	Handle(e, http.MethodPost, "/users", "users_create_qg", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{ID: 1}), nil
	}, WithTags("Users"), WithSecurity("BearerAuth"), WithRequestExamples(map[string]any{
		"ok": map[string]any{"name": "alice"},
	}))

	issues := ValidateOpenAPIQuality(e.OpenAPISpec(), OpenAPIQualityGates{
		RequireRootTags:         true,
		RequireServers:          true,
		RequireExamples:         true,
		RequiredSecuritySchemes: []string{"BearerAuth"},
	})
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
}

func TestValidateOpenAPIQualityDetectsMissingFields(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	Handle(e, http.MethodGet, "/x", "x_get_qg", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	})

	issues := ValidateOpenAPIQuality(e.OpenAPISpec(), OpenAPIQualityGates{
		RequireRootTags:         true,
		RequireServers:          true,
		RequireExamples:         true,
		RequiredSecuritySchemes: []string{"BearerAuth"},
	})
	if len(issues) != 4 {
		t.Fatalf("expected 4 issues, got %d: %v", len(issues), issues)
	}
}
