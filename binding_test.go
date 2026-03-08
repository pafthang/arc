package arc

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIncludeAllowlistRuntimeAndOpenAPI(t *testing.T) {
	type in struct{}
	type out struct {
		Includes []string `json:"includes"`
	}
	e := New()
	HandleOut(e, http.MethodGet, "/users", "users_list", func(ctx context.Context, in *in) (*out, error) {
		return &out{Includes: IncludesFromContext(ctx)}, nil
	}, WithIncludeAllowlist("profile", "roles", "roles.permissions"))

	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/users?include=profile,roles.permissions", nil))
	if w1.Code != http.StatusOK || !strings.Contains(w1.Body.String(), "roles.permissions") {
		t.Fatalf("include valid failed status=%d body=%s", w1.Code, w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/users?include=secret", nil))
	if w2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("include invalid status=%d body=%s", w2.Code, w2.Body.String())
	}

	spec := e.registry.OpenAPISpec()
	get := spec["paths"].(map[string]any)["/users"].(map[string]any)["get"].(map[string]any)
	if !strings.Contains(mustJSON(get), "\"name\":\"include\"") || !strings.Contains(mustJSON(get), "roles.permissions") {
		t.Fatalf("openapi include parameter missing: %s", mustJSON(get))
	}
}

func TestBindingArraysAndMultipartForm(t *testing.T) {
	type in struct {
		IDs  []int    `query:"id"`
		Tags []string `form:"tag"`
	}
	type out struct {
		IDs  []int    `json:"ids"`
		Tags []string `json:"tags"`
	}
	e := New()
	HandleOut(e, http.MethodPost, "/upload", "upload", func(ctx context.Context, in *in) (*out, error) {
		return &out{IDs: in.IDs, Tags: in.Tags}, nil
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("tag", "a")
	_ = writer.WriteField("tag", "b")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload?id=1&id=2&id=3", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"ids\":[1,2,3]") || !strings.Contains(w.Body.String(), "\"tags\":[\"a\",\"b\"]") {
		t.Fatalf("unexpected body=%s", w.Body.String())
	}
}

func TestBindingMultipartFiles(t *testing.T) {
	type in struct {
		Avatar *multipart.FileHeader   `form:"avatar"`
		Docs   []*multipart.FileHeader `form:"doc"`
	}
	type out struct {
		Avatar string `json:"avatar"`
		Docs   int    `json:"docs"`
	}
	e := New()
	HandleOut(e, http.MethodPost, "/upload-files", "upload_files", func(ctx context.Context, in *in) (*out, error) {
		name := ""
		if in.Avatar != nil {
			name = in.Avatar.Filename
		}
		return &out{Avatar: name, Docs: len(in.Docs)}, nil
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw1, _ := writer.CreateFormFile("avatar", "ava.png")
	_, _ = fw1.Write([]byte("img"))
	fw2, _ := writer.CreateFormFile("doc", "a.txt")
	_, _ = fw2.Write([]byte("a"))
	fw3, _ := writer.CreateFormFile("doc", "b.txt")
	_, _ = fw3.Write([]byte("b"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload-files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"avatar\":\"ava.png\"") || !strings.Contains(w.Body.String(), "\"docs\":2") {
		t.Fatalf("unexpected body=%s", w.Body.String())
	}
}

func TestGroupSharedConfigAndQueryDTO(t *testing.T) {
	type in struct{}
	type out struct {
		TagOK      bool     `json:"tagOk"`
		SecOK      bool     `json:"secOk"`
		Includes   []string `json:"includes,omitempty"`
		FilterVals []string `json:"filterVals,omitempty"`
		SortCount  int      `json:"sortCount"`
	}

	e := New()
	g := e.Group("/v1").
		WithTags("users").
		WithSecurity("bearerAuth").
		WithIncludeAllowlist("profile", "roles").
		WithQueryDTO()

	HandleGroup(g, http.MethodGet, "/users", "users_list_v1", func(ctx context.Context, in *in) (*Response[out], error) {
		dto, ok := QueryDTOFromContext(ctx)
		if !ok {
			return nil, errors.New("query dto missing")
		}
		spec := e.registry.OpenAPISpec()
		get := spec["paths"].(map[string]any)["/v1/users"].(map[string]any)["get"].(map[string]any)
		_, hasTag := get["tags"]
		_, hasSec := get["security"]
		return OK(out{
			TagOK:      hasTag,
			SecOK:      hasSec,
			Includes:   IncludesFromContext(ctx),
			FilterVals: dto.Filters["status"],
			SortCount:  len(dto.Sort),
		}), nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/users?limit=10&offset=20&sort=name,-created_at&status=active&include=profile", nil)
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "\"tagOk\":true") || !strings.Contains(body, "\"secOk\":true") {
		t.Fatalf("group tags/security not inherited: %s", body)
	}
	if !strings.Contains(body, "\"includes\":[\"profile\"]") || !strings.Contains(body, "\"filterVals\":[\"active\"]") {
		t.Fatalf("query dto/include not parsed: %s", body)
	}
	if !strings.Contains(body, "\"sortCount\":2") {
		t.Fatalf("sort parse failed: %s", body)
	}

	spec := e.registry.OpenAPISpec()
	get := spec["paths"].(map[string]any)["/v1/users"].(map[string]any)["get"].(map[string]any)
	js := mustJSON(get)
	if !strings.Contains(js, "\"name\":\"limit\"") || !strings.Contains(js, "\"name\":\"offset\"") || !strings.Contains(js, "\"name\":\"sort\"") {
		t.Fatalf("query dto params missing in openapi: %s", js)
	}
}

func TestIncludeTreeHelpers(t *testing.T) {
	ctx := WithIncludes(context.Background(), []string{"profile", "roles.permissions", "roles"})
	if !HasInclude(ctx, "roles.permissions") {
		t.Fatalf("expected include roles.permissions")
	}
	tree := IncludeTreeFromContext(ctx)
	flat := FlattenIncludeTree(tree)
	raw := strings.Join(flat, ",")
	if !strings.Contains(raw, "profile") || !strings.Contains(raw, "roles.permissions") {
		t.Fatalf("unexpected include tree flatten: %v", flat)
	}
}
