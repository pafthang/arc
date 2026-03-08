package arc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCustomAndCrossFieldValidation(t *testing.T) {
	type in struct {
		Start int `query:"start" validate:"required"`
		End   int `query:"end" validate:"required,gtefield=Start"`
	}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	RegisterValidator(e, func(v *in) error {
		if v.Start%2 != 0 {
			return errors.New("start must be even")
		}
		return nil
	})
	HandleOut(e, http.MethodGet, "/range", "range_get", func(ctx context.Context, in *in) (*out, error) {
		return &out{OK: true}, nil
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/range?start=3&end=2", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "gteField") || !strings.Contains(w.Body.String(), "start must be even") {
		t.Fatalf("validation details not found: %s", w.Body.String())
	}
}

func TestValidationUsesSharedTypeMetaJSONName(t *testing.T) {
	type in struct {
		StartAt int `query:"start" validate:"min=1"`
	}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	HandleOut(e, http.MethodGet, "/meta-name", "meta_name", func(ctx context.Context, in *in) (*out, error) {
		return &out{OK: true}, nil
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/meta-name?start=-1", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"path\":\"start_at\"") {
		t.Fatalf("expected typemeta json field name in validation error: %s", w.Body.String())
	}
}

func TestStrictMetadataBindingValidationFromSharedTypeMeta(t *testing.T) {
	type in struct {
		ID     int64
		Status string
	}
	type out struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	prev := EnableLegacyTagSync
	EnableLegacyTagSync = false
	defer func() { EnableLegacyTagSync = prev }()

	if err := ConfigureFieldMetadata(in{}, "ID", FieldMetadataConfig{Path: "id", Validate: "required,min=1"}); err != nil {
		t.Fatalf("configure ID metadata: %v", err)
	}
	if err := ConfigureFieldMetadata(in{}, "Status", FieldMetadataConfig{Query: "status", Validate: "enum=active|blocked"}); err != nil {
		t.Fatalf("configure Status metadata: %v", err)
	}

	e := New()
	HandleOut(e, http.MethodGet, "/meta/{id}", "meta_bind", func(ctx context.Context, v *in) (*out, error) {
		return &out{ID: v.ID, Status: v.Status}, nil
	})

	wOK := httptest.NewRecorder()
	e.ServeHTTP(wOK, httptest.NewRequest(http.MethodGet, "/meta/42?status=active", nil))
	if wOK.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", wOK.Code, wOK.Body.String())
	}
	if !strings.Contains(wOK.Body.String(), "\"id\":42") || !strings.Contains(wOK.Body.String(), "\"status\":\"active\"") {
		t.Fatalf("unexpected body=%s", wOK.Body.String())
	}

	wBad := httptest.NewRecorder()
	e.ServeHTTP(wBad, httptest.NewRequest(http.MethodGet, "/meta/42?status=wrong", nil))
	if wBad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", wBad.Code, wBad.Body.String())
	}
	if !strings.Contains(wBad.Body.String(), "\"path\":\"status\"") {
		t.Fatalf("validation should use shared typemeta metadata, body=%s", wBad.Body.String())
	}
}

func TestValidationFormatRule(t *testing.T) {
	type in struct {
		Email string `query:"email" validate:"format=email"`
		UUID  string `query:"id" validate:"format=uuid"`
		TS    string `query:"ts" validate:"format=date-time"`
	}
	type out struct {
		OK bool `json:"ok"`
	}
	e := New()
	HandleOut(e, http.MethodGet, "/format", "format", func(ctx context.Context, in *in) (*out, error) {
		return &out{OK: true}, nil
	})

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/format?email=bad&id=oops&ts=not-a-time", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "\"code\":\"format\"") {
		t.Fatalf("format validation detail missing: %s", w.Body.String())
	}

	spec := e.registry.OpenAPISpec()
	paths := spec["paths"].(map[string]any)
	get := paths["/format"].(map[string]any)["get"].(map[string]any)
	if !strings.Contains(mustJSON(get), "\"format\":\"email\"") {
		t.Fatalf("openapi format hint missing: %s", mustJSON(get))
	}
}

func TestOptionalPatchSemantics(t *testing.T) {
	type in struct {
		Name OptionalString `json:"name,omitempty"`
		Age  OptionalInt    `json:"age,omitempty"`
	}
	type out struct {
		NameSet  bool   `json:"nameSet"`
		NameNull bool   `json:"nameNull"`
		NameVal  string `json:"nameVal,omitempty"`
		AgeSet   bool   `json:"ageSet"`
		AgeNull  bool   `json:"ageNull"`
		AgeVal   int    `json:"ageVal,omitempty"`
	}
	e := New()
	HandleOut(e, http.MethodPatch, "/users/{id}", "users_patch_optional", func(ctx context.Context, in *in) (*out, error) {
		res := &out{
			NameSet:  in.Name.IsSet(),
			NameNull: in.Name.IsNull(),
			AgeSet:   in.Age.IsSet(),
			AgeNull:  in.Age.IsNull(),
		}
		if v, ok := in.Name.Value(); ok {
			res.NameVal = v
		}
		if v, ok := in.Age.Value(); ok {
			res.AgeVal = v
		}
		return res, nil
	})

	w1 := httptest.NewRecorder()
	e.ServeHTTP(w1, httptest.NewRequest(http.MethodPatch, "/users/1", strings.NewReader(`{}`)))
	if !strings.Contains(w1.Body.String(), "\"nameSet\":false") || !strings.Contains(w1.Body.String(), "\"ageSet\":false") {
		t.Fatalf("absent semantics broken: %s", w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, httptest.NewRequest(http.MethodPatch, "/users/1", strings.NewReader(`{"name":null}`)))
	if !strings.Contains(w2.Body.String(), "\"nameSet\":true") || !strings.Contains(w2.Body.String(), "\"nameNull\":true") {
		t.Fatalf("explicit null semantics broken: %s", w2.Body.String())
	}

	w3 := httptest.NewRecorder()
	e.ServeHTTP(w3, httptest.NewRequest(http.MethodPatch, "/users/1", strings.NewReader(`{"name":"alice","age":30}`)))
	if !strings.Contains(w3.Body.String(), "\"nameVal\":\"alice\"") || !strings.Contains(w3.Body.String(), "\"ageVal\":30") {
		t.Fatalf("explicit value semantics broken: %s", w3.Body.String())
	}
}

func TestOptionalValidationAndSchema(t *testing.T) {
	type in struct {
		Name OptionalString `json:"name,omitempty" validate:"minlength=3"`
	}
	type out struct {
		Name OptionalString `json:"name,omitempty" validate:"minlength=3"`
	}
	e := New()
	Handle(e, http.MethodPatch, "/opt", "optional_validation", func(ctx context.Context, in *in) (*Response[out], error) {
		return OK(out{Name: in.Name}), nil
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPatch, "/opt", strings.NewReader(`{}`))
	req1.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("unset optional should pass, status=%d body=%s", w1.Code, w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPatch, "/opt", strings.NewReader(`{"name":"ab"}`))
	req2.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid optional value must fail, status=%d body=%s", w2.Code, w2.Body.String())
	}

	spec := e.registry.OpenAPISpec()
	components := spec["components"].(map[string]any)["schemas"].(map[string]any)
	inSchema := components[reflect.TypeOf(in{}).Name()].(map[string]any)
	if req, ok := inSchema["required"].([]any); ok {
		for _, item := range req {
			if item == "name" {
				t.Fatalf("optional field must not be required: %s", mustJSON(inSchema))
			}
		}
	}
	nameSchema := inSchema["properties"].(map[string]any)["name"].(map[string]any)
	if _, ok := nameSchema["anyOf"]; !ok {
		t.Fatalf("optional schema should be nullable anyOf: %s", mustJSON(nameSchema))
	}
}
