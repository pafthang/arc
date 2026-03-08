package arc

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
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
	RequestExamples  map[string]any
	ResponseExamples map[string]any
	ProblemStatuses  []int
	ProblemExamples  map[int]map[string]any
}

// OperationRegistry stores all operations.
type OperationRegistry struct {
	mu              sync.RWMutex
	ops             []Operation
	servers         []map[string]any
	securitySchemes map[string]map[string]any
}

func NewOperationRegistry() *OperationRegistry {
	return &OperationRegistry{
		securitySchemes: map[string]map[string]any{},
	}
}

func (r *OperationRegistry) Add(op Operation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, op)
}

// SetServers configures root OpenAPI servers list.
func (r *OperationRegistry) SetServers(servers []map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, 0, len(servers))
	for _, s := range servers {
		if s == nil {
			continue
		}
		out = append(out, cloneAnyMap(s))
	}
	r.servers = out
}

// RegisterSecurityScheme registers OpenAPI security scheme in components.
func (r *OperationRegistry) RegisterSecurityScheme(name string, scheme map[string]any) {
	name = strings.TrimSpace(name)
	if name == "" || len(scheme) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.securitySchemes == nil {
		r.securitySchemes = map[string]map[string]any{}
	}
	r.securitySchemes[name] = cloneAnyMap(scheme)
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
	usedTags := map[string]struct{}{}
	usedSecurity := map[string]struct{}{}
	schemaGen := newSchemaGen(components["schemas"].(map[string]any))
	schemas := components["schemas"].(map[string]any)
	schemas["Problem"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":      map[string]any{"type": "string"},
			"title":     map[string]any{"type": "string"},
			"status":    map[string]any{"type": "integer"},
			"detail":    map[string]any{"type": "string"},
			"code":      map[string]any{"type": "string"},
			"requestId": map[string]any{"type": "string"},
		},
		"required": []string{"type", "title", "status"},
	}

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
				"default": map[string]any{
					"description": "Error",
					"content": map[string]any{
						"application/problem+json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/Problem"},
						},
					},
				},
			},
		}
		if len(op.Tags) > 0 {
			entry["tags"] = op.Tags
			for _, t := range op.Tags {
				if strings.TrimSpace(t) == "" {
					continue
				}
				usedTags[strings.TrimSpace(t)] = struct{}{}
			}
		}
		if len(op.Security) > 0 {
			items := make([]map[string][]string, 0, len(op.Security))
			for _, s := range op.Security {
				if strings.TrimSpace(s) != "" {
					usedSecurity[strings.TrimSpace(s)] = struct{}{}
				}
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
				if len(op.RequestExamples) > 0 {
					content[op.InputContentType].(map[string]any)["examples"] = normalizeExamples(op.RequestExamples)
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
				if len(op.ResponseExamples) > 0 {
					entry["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["examples"] = normalizeExamples(op.ResponseExamples)
				}
			}
		}
		if len(op.ProblemStatuses) > 0 {
			statuses := normalizeProblemStatuses(op.ProblemStatuses)
			for _, code := range statuses {
				key := strconv.Itoa(code)
				resp := map[string]any{
					"description": strings.TrimSpace(httpStatusText(code)),
					"content": map[string]any{
						"application/problem+json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/Problem"},
						},
					},
				}
				if ex := normalizeExamples(op.ProblemExamples[code]); len(ex) > 0 {
					resp["content"].(map[string]any)["application/problem+json"].(map[string]any)["examples"] = ex
				}
				entry["responses"].(map[string]any)[key] = resp
			}
		}

		paths[path].(map[string]any)[method] = entry
	}

	r.mu.RLock()
	servers := make([]map[string]any, 0, len(r.servers))
	for _, s := range r.servers {
		servers = append(servers, cloneAnyMap(s))
	}
	securitySchemes := map[string]any{}
	for name, scheme := range r.securitySchemes {
		securitySchemes[name] = cloneAnyMap(scheme)
	}
	r.mu.RUnlock()

	if len(usedSecurity) > 0 {
		if len(securitySchemes) == 0 {
			securitySchemes = map[string]any{}
		}
		for name := range usedSecurity {
			if _, exists := securitySchemes[name]; exists {
				continue
			}
			// Default scheme for operation security references.
			securitySchemes[name] = map[string]any{
				"type":         "http",
				"scheme":       "bearer",
				"bearerFormat": "JWT",
			}
		}
	}
	if len(securitySchemes) > 0 {
		components["securitySchemes"] = securitySchemes
	}

	tags := make([]map[string]any, 0, len(usedTags))
	if len(usedTags) > 0 {
		names := make([]string, 0, len(usedTags))
		for t := range usedTags {
			names = append(names, t)
		}
		sort.Strings(names)
		for _, t := range names {
			tags = append(tags, map[string]any{"name": t})
		}
	}

	out := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "arc API",
			"version": "0.1.0",
		},
		"paths":      paths,
		"components": components,
	}
	if len(tags) > 0 {
		out["tags"] = tags
	}
	if len(servers) > 0 {
		out["servers"] = servers
	}
	return out
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
	typeNames  map[reflect.Type]string
	nameOwners map[string]reflect.Type
}

func newSchemaGen(components map[string]any) *schemaGen {
	return &schemaGen{
		components: components,
		seen:       map[reflect.Type]bool{},
		typeNames:  map[reflect.Type]string{},
		nameOwners: map[string]reflect.Type{},
	}
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
		name := g.componentNameFor(t)
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

func (g *schemaGen) componentNameFor(t reflect.Type) string {
	if t == nil {
		return ""
	}
	if name, ok := g.typeNames[t]; ok {
		return name
	}
	base := sanitizeSchemaName(t.Name())
	if base == "" {
		return ""
	}
	name := base
	if owner, exists := g.nameOwners[name]; exists && owner != t {
		for i := 2; ; i++ {
			candidate := base + "_" + strconv.Itoa(i)
			if owner, taken := g.nameOwners[candidate]; !taken || owner == t {
				name = candidate
				break
			}
		}
	}
	g.typeNames[t] = name
	g.nameOwners[name] = t
	return name
}

func sanitizeSchemaName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, "[") && strings.Contains(name, "]") {
		if generic := sanitizeGenericSchemaName(name); generic != "" {
			return generic
		}
	}
	return sanitizeSchemaToken(name)
}

func sanitizeGenericSchemaName(name string) string {
	base, args, ok := splitGenericName(name)
	if !ok {
		return ""
	}
	base = sanitizeSchemaToken(typeShortName(base))
	if base == "" {
		return ""
	}
	argItems := splitGenericArgs(args)
	parts := []string{base}
	for _, item := range argItems {
		token := sanitizeSchemaToken(typeShortName(item))
		if token != "" {
			parts = append(parts, token)
		}
	}
	return strings.Join(parts, "_")
}

func splitGenericName(name string) (base, args string, ok bool) {
	start := strings.IndexByte(name, '[')
	if start <= 0 || !strings.HasSuffix(name, "]") {
		return "", "", false
	}
	return strings.TrimSpace(name[:start]), strings.TrimSpace(name[start+1 : len(name)-1]), true
}

func splitGenericArgs(args string) []string {
	if strings.TrimSpace(args) == "" {
		return nil
	}
	out := make([]string, 0, 2)
	depth := 0
	start := 0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(args[start:i])
				if part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	last := strings.TrimSpace(args[start:])
	if last != "" {
		out = append(out, last)
	}
	return out
}

func typeShortName(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimLeft(v, "*[]")
	for strings.HasPrefix(v, "[]") {
		v = strings.TrimPrefix(v, "[]")
	}
	if idx := strings.LastIndexByte(v, '/'); idx >= 0 && idx+1 < len(v) {
		v = v[idx+1:]
	}
	if idx := strings.LastIndexByte(v, '.'); idx >= 0 && idx+1 < len(v) {
		v = v[idx+1:]
	}
	return strings.TrimSpace(v)
}

func sanitizeSchemaToken(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c)
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c)
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '.', c == '-', c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return "Model"
	}
	return out
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

func normalizeExamples(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for name, v := range in {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if vm, ok := v.(map[string]any); ok {
			if _, hasValue := vm["value"]; hasValue {
				out[name] = cloneAnyMap(vm)
				continue
			}
			if _, hasRef := vm["$ref"]; hasRef {
				out[name] = cloneAnyMap(vm)
				continue
			}
		}
		out[name] = map[string]any{"value": v}
	}
	return out
}

func normalizeProblemStatuses(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, code := range in {
		if code < 100 || code > 599 {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	sort.Ints(out)
	return out
}

func httpStatusText(code int) string {
	switch code {
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 409:
		return "Conflict"
	case 422:
		return "Unprocessable Entity"
	case 500:
		return "Internal Server Error"
	default:
		return "Error"
	}
}
