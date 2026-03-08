package arc

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// OperationParam describes one OpenAPI operation parameter.
type OperationParam struct {
	Name     string
	In       string
	Required bool
	Schema   map[string]any
}

// Operation describes registered endpoint.
type Operation struct {
	Method           string
	Path             string
	OperationID      string
	Handler          Handler
	Middleware       []Middleware
	Metadata         map[string]string
	Version          string
	Callbacks        map[string]any
	InputType        any
	OutputType       any
	Tags             []string
	Security         []string
	MiddlewareN      int
	Params           []OperationParam
	HasRequestBody   bool
	HasFormBody      bool
	InputContentType string
	ResponseKind     responseKind
	IncludeAllowlist []string
	IncludeParamName string
	HasQueryDTO      bool
}

// OperationRegistry stores all operations.
type OperationRegistry struct {
	mu  sync.RWMutex
	ops []Operation
}

func NewOperationRegistry() *OperationRegistry {
	return &OperationRegistry{}
}

func (r *OperationRegistry) Add(op Operation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, op)
}

func (r *OperationRegistry) Update(method, path, operationID string, fn func(*Operation)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.ops {
		op := &r.ops[i]
		if op.Method == method && op.Path == path && op.OperationID == operationID {
			fn(op)
			return
		}
	}
}

func (r *OperationRegistry) List() []Operation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Operation, len(r.ops))
	copy(out, r.ops)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func (r *OperationRegistry) OpenAPISpec() map[string]any {
	paths := map[string]any{}
	components := map[string]any{"schemas": map[string]any{}}
	schemaGen := newSchemaGen(components["schemas"].(map[string]any))

	for _, op := range r.List() {
		path := toOpenAPIPath(op.Path)
		if _, ok := paths[path]; !ok {
			paths[path] = map[string]any{}
		}
		method := strings.ToLower(op.Method)
		entry := map[string]any{
			"operationId": op.OperationID,
			"responses": map[string]any{
				"200": map[string]any{"description": "OK"},
			},
		}
		if len(op.Tags) > 0 {
			entry["tags"] = op.Tags
		}
		if len(op.Security) > 0 {
			items := make([]map[string][]string, 0, len(op.Security))
			for _, s := range op.Security {
				items = append(items, map[string][]string{s: {}})
			}
			entry["security"] = items
		}
		if len(op.Metadata) > 0 {
			meta := map[string]any{}
			keys := make([]string, 0, len(op.Metadata))
			for k := range op.Metadata {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				meta[k] = op.Metadata[k]
			}
			entry["x-arc-metadata"] = meta
		}
		if op.Version != "" {
			entry["x-arc-version"] = op.Version
		}
		if len(op.Callbacks) > 0 {
			entry["callbacks"] = cloneAnyMap(op.Callbacks)
		}
		if len(op.Params) > 0 {
			params := make([]map[string]any, 0, len(op.Params))
			for _, p := range op.Params {
				params = append(params, map[string]any{
					"name":     p.Name,
					"in":       p.In,
					"required": p.Required,
					"schema":   p.Schema,
				})
			}
			entry["parameters"] = params
		}
		if op.HasQueryDTO {
			queryParams := []map[string]any{
				{
					"name":        "limit",
					"in":          "query",
					"required":    false,
					"description": "Pagination limit",
					"schema":      map[string]any{"type": "integer", "minimum": 0},
				},
				{
					"name":        "offset",
					"in":          "query",
					"required":    false,
					"description": "Pagination offset",
					"schema":      map[string]any{"type": "integer", "minimum": 0},
				},
				{
					"name":        "sort",
					"in":          "query",
					"required":    false,
					"description": "Sort fields. Use '-' prefix for DESC.",
					"schema":      map[string]any{"type": "string"},
				},
			}
			if curr, ok := entry["parameters"].([]map[string]any); ok {
				entry["parameters"] = append(curr, queryParams...)
			} else {
				entry["parameters"] = queryParams
			}
		}
		if len(op.IncludeAllowlist) > 0 {
			name := op.IncludeParamName
			if name == "" {
				name = "include"
			}
			allowed := append([]string{}, op.IncludeAllowlist...)
			sort.Strings(allowed)
			includeParam := map[string]any{
				"name":        name,
				"in":          "query",
				"required":    false,
				"style":       "form",
				"explode":     false,
				"description": "Comma-separated relation includes",
				"schema": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
						"enum": allowed,
					},
				},
			}
			if curr, ok := entry["parameters"].([]map[string]any); ok {
				entry["parameters"] = append(curr, includeParam)
			} else {
				entry["parameters"] = []map[string]any{includeParam}
			}
		}

		if op.HasRequestBody {
			if t := reflectType(op.InputType); t != nil && t.Kind() == reflect.Struct {
				content := map[string]any{
					op.InputContentType: map[string]any{
						"schema": schemaGen.refOrInline(t),
					},
				}
				if op.HasFormBody {
					schema := schemaGen.refOrInline(t)
					content["application/x-www-form-urlencoded"] = map[string]any{"schema": schema}
					content["multipart/form-data"] = map[string]any{"schema": schema}
				}
				entry["requestBody"] = map[string]any{
					"required": true,
					"content":  content,
				}
			}
		}

		switch op.ResponseKind {
		case responseKindNone:
			entry["responses"].(map[string]any)["204"] = map[string]any{"description": "No Content"}
			delete(entry["responses"].(map[string]any), "200")
		case responseKindRaw, responseKindStream:
			entry["responses"].(map[string]any)["200"] = map[string]any{"description": "OK"}
		default:
			if t := reflectType(op.OutputType); t != nil {
				entry["responses"].(map[string]any)["200"] = map[string]any{
					"description": "OK",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": schemaGen.refOrInline(t),
						},
					},
				}
			}
		}

		paths[path].(map[string]any)[method] = entry
	}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "arc API",
			"version": "0.1.0",
		},
		"paths":      paths,
		"components": components,
	}
}

func reflectType(v any) reflect.Type {
	if v == nil {
		return nil
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func optionalInnerType(t reflect.Type) (reflect.Type, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t.NumField() != 3 {
		return nil, false
	}
	f0 := t.Field(0)
	f1 := t.Field(1)
	f2 := t.Field(2)
	if f0.Name != "set" || f1.Name != "null" || f2.Name != "value" {
		return nil, false
	}
	if f0.Type.Kind() != reflect.Bool || f1.Type.Kind() != reflect.Bool {
		return nil, false
	}
	return f2.Type, true
}

func toOpenAPIPath(path string) string {
	return strings.ReplaceAll(path, "...}", "}")
}

// MarshalOpenAPIJSON renders OpenAPI spec as JSON bytes.
func (r *OperationRegistry) MarshalOpenAPIJSON() ([]byte, error) {
	return json.MarshalIndent(r.OpenAPISpec(), "", "  ")
}

// MarshalOpenAPIYAML renders OpenAPI spec as YAML bytes.
func (r *OperationRegistry) MarshalOpenAPIYAML() ([]byte, error) {
	return yaml.Marshal(r.OpenAPISpec())
}

// JSONSchemas returns generated component schemas from registered operations.
func (r *OperationRegistry) JSONSchemas() map[string]any {
	spec := r.OpenAPISpec()
	components, _ := spec["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	if schemas == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(schemas))
	for k, v := range schemas {
		out[k] = v
	}
	return out
}

// JSONSchemaForType generates JSON Schema for one Go type.
func (r *OperationRegistry) JSONSchemaForType(model any) map[string]any {
	t := reflectType(model)
	if t == nil {
		return nil
	}
	defs := map[string]any{}
	gen := newSchemaGen(defs)
	schema := gen.refOrInline(t)

	out := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
	}
	for k, v := range schema {
		out[k] = v
	}
	if len(defs) > 0 {
		out["$defs"] = defs
	}
	return out
}

// MarshalJSONSchemasJSON renders all generated schemas as JSON bytes.
func (r *OperationRegistry) MarshalJSONSchemasJSON() ([]byte, error) {
	return json.MarshalIndent(r.JSONSchemas(), "", "  ")
}

func buildOperationParams(inType reflect.Type, plan bindPlan, validator compiledValidator) []OperationParam {
	if inType == nil || inType.Kind() != reflect.Struct {
		return nil
	}
	meta := resolveArcTypeMeta(inType)
	if meta == nil {
		return nil
	}
	byIndex := resolveArcFieldByIndex(meta)
	out := make([]OperationParam, 0, len(plan.fields))
	for _, f := range plan.fields {
		field, ok := byIndex[indexKey(f.index)]
		if !ok {
			continue
		}
		rule, hasRule := validator.ruleForIndex(f.index)
		required := f.source == sourcePath
		if hasRule && rule.required {
			required = true
		}
		schema := schemaFromField(field, hasRule, rule)
		out = append(out, OperationParam{
			Name:     f.name,
			In:       paramLocation(f.source),
			Required: required,
			Schema:   schema,
		})
	}
	return out
}

func paramLocation(s fieldSource) string {
	switch s {
	case sourcePath:
		return "path"
	case sourceHeader:
		return "header"
	case sourceCookie:
		return "cookie"
	default:
		return "query"
	}
}

func schemaFromField(f arcFieldMeta, hasRule bool, rule fieldRule) map[string]any {
	s := primitiveSchema(f.Type)
	if f.Type.Kind() == reflect.Pointer {
		s = nullableSchema(s)
	}
	if hasRule {
		if len(rule.enum) > 0 {
			enumVals := make([]string, 0, len(rule.enum))
			for v := range rule.enum {
				enumVals = append(enumVals, v)
			}
			sort.Strings(enumVals)
			s["enum"] = enumVals
		}
		if rule.min != nil {
			s["minimum"] = *rule.min
		}
		if rule.max != nil {
			s["maximum"] = *rule.max
		}
		if rule.minLength != nil {
			s["minLength"] = *rule.minLength
		}
		if rule.maxLength != nil {
			s["maxLength"] = *rule.maxLength
		}
		if rule.regexpExpr != nil {
			s["pattern"] = rule.regexpExpr.String()
		}
		if rule.format != "" {
			s["format"] = rule.format
		}
	}
	return s
}

func primitiveSchema(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if inner, ok := optionalInnerType(t); ok {
		return nullableSchema(primitiveSchema(inner))
	}
	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": primitiveSchema(t.Elem())}
	default:
		return map[string]any{"type": "string"}
	}
}

func nullableSchema(schema map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}

type schemaGen struct {
	components map[string]any
	seen       map[reflect.Type]bool
}

func newSchemaGen(components map[string]any) *schemaGen {
	return &schemaGen{components: components, seen: map[reflect.Type]bool{}}
}

func (g *schemaGen) refOrInline(t reflect.Type) map[string]any {
	nullable := false
	for t.Kind() == reflect.Pointer {
		nullable = true
		t = t.Elem()
	}
	if inner, ok := optionalInnerType(t); ok {
		s := g.refOrInline(inner)
		return nullableSchema(s)
	}
	var schema map[string]any
	if t.Kind() != reflect.Struct {
		schema = g.inline(t)
	} else {
		name := t.Name()
		if name == "" {
			schema = g.inline(t)
		} else {
			if !g.seen[t] {
				g.seen[t] = true
				g.components[name] = g.objectFromTypeMeta(t)
			}
			schema = map[string]any{"$ref": "#/components/schemas/" + name}
		}
	}
	if nullable {
		return nullableSchema(schema)
	}
	return schema
}

func (g *schemaGen) objectFromTypeMeta(t reflect.Type) map[string]any {
	meta := resolveArcTypeMeta(t)
	if meta == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	props := map[string]any{}
	required := []string{}
	for _, f := range meta.Fields {
		if f.JSONName == "" {
			continue
		}
		s := g.refOrInline(f.Type)
		if f.Validation != nil {
			s = applySchemaRule(s, *f.Validation)
		}
		props[f.JSONName] = s
		if !f.IsNullable {
			if _, isOptional := optionalInnerType(f.Type); isOptional {
				continue
			}
			required = append(required, f.JSONName)
		}
	}
	obj := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		obj["required"] = required
	}
	return obj
}

func (g *schemaGen) inline(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return g.objectFromTypeMeta(t)
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": g.refOrInline(t.Elem())}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.String:
		return map[string]any{"type": "string"}
	default:
		return map[string]any{}
	}
}

func applySchemaRule(schema map[string]any, rule fieldRule) map[string]any {
	if len(rule.enum) > 0 {
		enumVals := make([]string, 0, len(rule.enum))
		for v := range rule.enum {
			enumVals = append(enumVals, v)
		}
		sort.Strings(enumVals)
		schema["enum"] = enumVals
	}
	if rule.min != nil {
		schema["minimum"] = *rule.min
	}
	if rule.max != nil {
		schema["maximum"] = *rule.max
	}
	if rule.minLength != nil {
		schema["minLength"] = *rule.minLength
	}
	if rule.maxLength != nil {
		schema["maxLength"] = *rule.maxLength
	}
	if rule.regexpExpr != nil {
		schema["pattern"] = rule.regexpExpr.String()
	}
	if rule.format != "" {
		schema["format"] = rule.format
	}
	return schema
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
