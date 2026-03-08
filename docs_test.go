package arc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAPIEndpoint(t *testing.T) {
	e := New()
	Handle(e, http.MethodGet, "/users/{id}", "get_user", func(ctx context.Context, in *getUserInput) (*Response[userDTO], error) {
		return OK(userDTO{ID: in.ID}), nil
	}, WithTags("users"))
	e.RegisterSystemRoutes("/openapi.json", "/docs")

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "openapi") || !strings.Contains(w.Body.String(), "get_user") {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestOpenAPIParametersAndSchemaHints(t *testing.T) {
	type in struct {
		ID      int64   `path:"id" validate:"required,min=1"`
		Status  string  `query:"status" validate:"enum=active|blocked"`
		XTrace  *string `header:"X-Trace-ID"`
		Include string  `query:"include" validate:"enum=profile|roles"`
	}
	type out struct {
		ID int64 `json:"id"`
	}
	e := New()
	HandleOut(e, http.MethodGet, "/users/{id}", "users_get", func(ctx context.Context, in *in) (*out, error) {
		return &out{ID: in.ID}, nil
	})
	spec := e.registry.OpenAPISpec()
	paths := spec["paths"].(map[string]any)
	get := paths["/users/{id}"].(map[string]any)["get"].(map[string]any)
	params := get["parameters"].([]map[string]any)

	if len(params) != 4 {
		t.Fatalf("parameters=%d", len(params))
	}
	joined, _ := json.Marshal(get)
	body := string(joined)
	if !strings.Contains(body, "\"in\":\"path\"") || !strings.Contains(body, "\"enum\":[\"active\",\"blocked\"]") {
		t.Fatalf("openapi missing expected params: %s", body)
	}
}

func TestNullableSchema(t *testing.T) {
	type in struct {
		Name *string `json:"name,omitempty"`
	}
	type out struct {
		Name *string `json:"name,omitempty" validate:"enum=alice|bob"`
	}
	e := New()
	Handle(e, http.MethodPost, "/nullable", "nullable", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{Name: in.Name}), nil
	})
	spec := e.registry.OpenAPISpec()
	components := spec["components"].(map[string]any)["schemas"].(map[string]any)
	outSchema := components[reflect.TypeOf(out{}).Name()].(map[string]any)
	props := outSchema["properties"].(map[string]any)
	nameSchema := props["name"].(map[string]any)
	if _, ok := nameSchema["anyOf"]; !ok {
		t.Fatalf("nullable anyOf missing: %+v", nameSchema)
	}
}

func TestOpenAPIYAMLEndpoint(t *testing.T) {
	e := New()
	HandleOut(e, http.MethodGet, "/ping", "ping", func(ctx context.Context, in *struct{}) (*map[string]string, error) {
		out := map[string]string{"ok": "true"}
		return &out, nil
	})
	e.RegisterSystemRoutes("/openapi.json", "/docs")

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "openapi: 3.1.0") {
		t.Fatalf("not yaml content: %s", w.Body.String())
	}
}

func TestDocsEndpointSwaggerUI(t *testing.T) {
	e := New()
	HandleOut(e, http.MethodGet, "/ping", "ping", func(ctx context.Context, in *struct{}) (*map[string]string, error) {
		out := map[string]string{"ok": "true"}
		return &out, nil
	})
	e.RegisterSystemRoutes("/openapi.json", "/docs")

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "SwaggerUIBundle") || !strings.Contains(body, "/openapi.json") {
		t.Fatalf("docs page is not swagger ui: %s", body)
	}
}

func TestJSONSchemaEndpoints(t *testing.T) {
	type out struct {
		ID   int64  `json:"id"`
		Name string `json:"name" validate:"minlength=2"`
	}
	e := New()
	HandleOut(e, http.MethodGet, "/users/{id}", "users_get_schema", func(ctx context.Context, in *getUserInput) (*out, error) {
		return &out{ID: in.ID, Name: "ab"}, nil
	})
	e.RegisterSystemRoutes("/openapi.json", "/docs")

	wList := httptest.NewRecorder()
	e.ServeHTTP(wList, httptest.NewRequest(http.MethodGet, "/schemas", nil))
	if wList.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", wList.Code, wList.Body.String())
	}
	if !strings.Contains(wList.Body.String(), reflect.TypeOf(out{}).Name()) {
		t.Fatalf("schemas list missing out schema: %s", wList.Body.String())
	}

	wOne := httptest.NewRecorder()
	e.ServeHTTP(wOne, httptest.NewRequest(http.MethodGet, "/schemas/"+reflect.TypeOf(out{}).Name(), nil))
	if wOne.Code != http.StatusOK {
		t.Fatalf("item status=%d body=%s", wOne.Code, wOne.Body.String())
	}
	if !strings.Contains(wOne.Body.String(), "\"minLength\":2") {
		t.Fatalf("schema details missing validation hints: %s", wOne.Body.String())
	}

	wMissing := httptest.NewRecorder()
	e.ServeHTTP(wMissing, httptest.NewRequest(http.MethodGet, "/schemas/UnknownType", nil))
	if wMissing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", wMissing.Code, wMissing.Body.String())
	}
}

func TestOperationMetadataInheritanceAndOpenAPI(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	g := e.Group("/meta").
		WithMetadata(map[string]string{
			"domain": "users",
			"owner":  "team-a",
		})
	HandleGroup(g, http.MethodGet, "/items", "meta_items", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	}, WithMetadata(map[string]string{
		"owner": "team-b",
		"tier":  "public",
	}))

	ops := e.Operations()
	var op *Operation
	for i := range ops {
		if ops[i].OperationID == "meta_items" {
			op = &ops[i]
			break
		}
	}
	if op == nil {
		t.Fatalf("operation not found")
	}
	if op.Metadata["domain"] != "users" || op.Metadata["owner"] != "team-b" || op.Metadata["tier"] != "public" {
		t.Fatalf("metadata inheritance/override broken: %+v", op.Metadata)
	}

	spec := e.registry.OpenAPISpec()
	get := spec["paths"].(map[string]any)["/meta/items"].(map[string]any)["get"].(map[string]any)
	raw := mustJSON(get)
	if !strings.Contains(raw, "\"x-arc-metadata\"") || !strings.Contains(raw, "\"domain\":\"users\"") || !strings.Contains(raw, "\"owner\":\"team-b\"") {
		t.Fatalf("openapi metadata extension missing: %s", raw)
	}
}

func TestOperationRegistryStoresHandlerAndMiddleware(t *testing.T) {
	type in struct{}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	mw := func(next Handler) Handler {
		return func(rc *RequestContext) error { return next(rc) }
	}
	Handle(e, http.MethodGet, "/ops-introspect", "ops_introspect", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{OK: true}), nil
	}, WithMiddleware(mw))

	ops := e.Operations()
	var op *Operation
	for i := range ops {
		if ops[i].OperationID == "ops_introspect" {
			op = &ops[i]
			break
		}
	}
	if op == nil {
		t.Fatalf("operation not found")
	}
	if op.Handler == nil {
		t.Fatalf("operation handler must be stored")
	}
	if len(op.Middleware) != 1 || op.MiddlewareN != 1 {
		t.Fatalf("operation middleware not stored correctly: len=%d n=%d", len(op.Middleware), op.MiddlewareN)
	}
}
