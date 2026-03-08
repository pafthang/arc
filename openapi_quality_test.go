package arc

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestOpenAPIRootTagsServersAndSecuritySchemes(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	e.SetOpenAPIServers([]map[string]any{{"url": "http://localhost:8080/api/v1", "description": "dev"}})
	e.RegisterOpenAPISecurityScheme("BearerAuth", map[string]any{
		"type":         "http",
		"scheme":       "bearer",
		"bearerFormat": "JWT",
	})
	Handle(e, http.MethodGet, "/x", "x_get", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	}, WithTags("Auth"), WithSecurity("BearerAuth"))

	spec := e.registry.OpenAPISpec()
	raw := mustJSON(spec)
	if !strings.Contains(raw, `"tags":[{"name":"Auth"}]`) {
		t.Fatalf("root tags missing: %s", raw)
	}
	if !strings.Contains(raw, `"servers":[{"description":"dev","url":"http://localhost:8080/api/v1"}]`) {
		t.Fatalf("servers missing: %s", raw)
	}
	if !strings.Contains(raw, `"securitySchemes":{"BearerAuth"`) {
		t.Fatalf("security schemes missing: %s", raw)
	}
	if !strings.Contains(raw, `"application/problem+json"`) {
		t.Fatalf("problem+json response missing: %s", raw)
	}
}

func TestOpenAPIExamples(t *testing.T) {
	type in struct {
		Name string `json:"name"`
	}
	type out struct {
		ID int64 `json:"id"`
	}
	e := New()
	Handle(e, http.MethodPost, "/users", "users_create", func(ctx context.Context, in *in) (*Response[out], error) {
		return Created(out{ID: 1}), nil
	}, WithRequestExamples(map[string]any{
		"valid": map[string]any{"name": "alice"},
	}), WithResponseExamples(map[string]any{
		"ok": map[string]any{"id": 1},
	}))

	spec := e.registry.OpenAPISpec()
	raw := mustJSON(spec)
	if !strings.Contains(raw, `"requestBody"`) || !strings.Contains(raw, `"examples":{"valid":{"value":{"name":"alice"}}}`) {
		t.Fatalf("request examples missing: %s", raw)
	}
	if !strings.Contains(raw, `"responses"`) || !strings.Contains(raw, `"examples":{"ok":{"value":{"id":1}}}`) {
		t.Fatalf("response examples missing: %s", raw)
	}
}

func TestOpenAPIComponentNamesAreURIFragmentSafe(t *testing.T) {
	type wrapped struct {
		Data []map[string]any `json:"data"`
	}
	e := New()
	Handle(e, http.MethodGet, "/safe", "safe_get", func(ctx context.Context, in *struct{}) (*Response[wrapped], error) {
		return OK(wrapped{Data: []map[string]any{{"ok": true}}}), nil
	})

	raw := mustJSON(e.registry.OpenAPISpec())
	if strings.Contains(raw, "#/components/schemas/") && strings.Contains(raw, "/internal/") {
		t.Fatalf("unsafe component ref detected: %s", raw)
	}
	re := regexp.MustCompile(`"#/components/schemas/[^"]*[/\[\]]`)
	if re.MatchString(raw) {
		t.Fatalf("component reference contains unsafe chars: %s", raw)
	}
}
