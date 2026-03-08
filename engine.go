package arc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pafthang/orm"
)

// Handler executes one route operation.
type Handler func(*RequestContext) error

// Middleware wraps route handler.
type Middleware func(Handler) Handler

// ErrorMapper converts domain/runtime errors into API error payload.
type ErrorMapper interface {
	MapError(err error) *APIError
}

// Encoder writes response payload.
type Encoder interface {
	Encode(w http.ResponseWriter, status int, body any) error
}

// RawResponse allows writing arbitrary response formats.
type RawResponse struct {
	Status  int
	Headers http.Header
	WriteTo func(http.ResponseWriter) error
}

// StreamResponse describes streaming body response.
type StreamResponse struct {
	Status      int
	ContentType string
	Reader      io.Reader
}

// Stream creates stream response helper.
func Stream(status int, contentType string, reader io.Reader) *StreamResponse {
	return &StreamResponse{Status: status, ContentType: contentType, Reader: reader}
}

// Raw creates raw response helper.
func Raw(status int, contentType string, body []byte) *RawResponse {
	return &RawResponse{
		Status:  status,
		Headers: http.Header{"Content-Type": []string{contentType}},
		WriteTo: func(w http.ResponseWriter) error {
			_, err := w.Write(body)
			return err
		},
	}
}

// Engine is HTTP/API runtime core.
type Engine struct {
	router     *Router
	registry   *OperationRegistry
	middleware []Middleware
	errMapper  ErrorMapper
	encoder    Encoder
	encoders   map[string]Encoder
	validators *validatorRegistry
	observers  []Observer
	mu         sync.RWMutex
	reqCounter atomic.Uint64
	ready      atomic.Bool
}

// New creates arc engine with defaults.
func New() *Engine {
	e := &Engine{
		router:    NewRouter(),
		registry:  NewOperationRegistry(),
		errMapper: defaultErrorMapper{},
		encoder:   JSONEncoder{},
		encoders: map[string]Encoder{
			"application/json":         JSONEncoder{},
			"application/problem+json": JSONEncoder{},
		},
		validators: newValidatorRegistry(),
	}
	e.ready.Store(true)
	return e
}

// ServeHTTP implements http.Handler.
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
	route, params, ok := e.router.Match(r.Method, r.URL.Path)
	if !ok && r.Method == http.MethodHead {
		if getRoute, getParams, getOK := e.router.Match(http.MethodGet, r.URL.Path); getOK {
			route = getRoute
			params = getParams
			ok = true
			sw = &statusResponseWriter{ResponseWriter: &headResponseWriter{ResponseWriter: w}, status: http.StatusOK}
		}
	}
	if !ok {
		if r.Method == http.MethodOptions {
			allowed := e.router.AllowedMethods(r.URL.Path)
			if len(allowed) > 0 {
				allowedSet := map[string]struct{}{}
				for _, m := range allowed {
					allowedSet[m] = struct{}{}
				}
				allowedSet[http.MethodOptions] = struct{}{}
				ordered := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions}
				out := make([]string, 0, len(allowedSet))
				for _, m := range ordered {
					if _, exists := allowedSet[m]; exists {
						out = append(out, m)
					}
				}
				w.Header().Set("Allow", strings.Join(out, ", "))
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		_ = e.encodeResponse(sw, r, http.StatusNotFound, &APIError{Code: "not_found", Message: "Route not found"}, true)
		return
	}

	rc := &RequestContext{
		Writer:  sw,
		Request: r,
		Params:  params,
		Engine:  e,
		Ctx:     r.Context(),
	}
	start := time.Now()
	e.notifyRequestStart(rc)

	h := route.Handler
	for i := len(e.middleware) - 1; i >= 0; i-- {
		h = e.middleware[i](h)
	}
	for i := len(route.Middleware) - 1; i >= 0; i-- {
		h = route.Middleware[i](h)
	}

	if err := h(rc); err != nil {
		e.handleError(sw, r, err)
		e.notifyRequestEnd(rc, sw.status, err, time.Since(start))
		return
	}
	e.notifyRequestEnd(rc, sw.status, nil, time.Since(start))
}

func (e *Engine) handleError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := e.errMapper.MapError(err)
	e.attachRequestID(w, r, apiErr)
	status := apiErr.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	_ = e.encodeResponse(w, r, status, apiErr, true)
}

func (e *Engine) attachRequestID(w http.ResponseWriter, r *http.Request, apiErr *APIError) {
	if apiErr == nil {
		return
	}
	if apiErr.RequestID != "" {
		w.Header().Set("X-Request-ID", apiErr.RequestID)
		return
	}
	rid := ""
	if r != nil {
		rid = r.Header.Get("X-Request-ID")
	}
	if rid == "" {
		n := e.reqCounter.Add(1)
		rid = "req-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(n, 36)
	}
	apiErr.RequestID = rid
	w.Header().Set("X-Request-ID", rid)
}

// Use adds global middleware.
func (e *Engine) Use(mw ...Middleware) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.middleware = append(e.middleware, mw...)
}

// SetErrorMapper overrides default mapper.
func (e *Engine) SetErrorMapper(m ErrorMapper) {
	if m == nil {
		return
	}
	e.errMapper = m
}

// SetEncoder overrides response encoder.
func (e *Engine) SetEncoder(enc Encoder) {
	if enc == nil {
		return
	}
	e.encoder = enc
	e.encoders["application/json"] = enc
	e.encoders["application/problem+json"] = enc
}

// RegisterEncoder registers encoder for specific media type.
func (e *Engine) RegisterEncoder(contentType string, enc Encoder) {
	if contentType == "" || enc == nil {
		return
	}
	e.encoders[contentType] = enc
}

// AddObserver adds request observer hooks.
func (e *Engine) AddObserver(obs Observer) {
	if obs == nil {
		return
	}
	e.observers = append(e.observers, obs)
}

// RegisterValidator registers additional struct validator for input type T.
func RegisterValidator[T any](e *Engine, fn func(*T) error) {
	if e == nil || fn == nil {
		return
	}
	t := inputStructType[T]()
	e.validators.add(t, func(in any) error {
		typed, ok := in.(*T)
		if !ok {
			return nil
		}
		return fn(typed)
	})
}

// Group creates route group.
func (e *Engine) Group(prefix string, middleware ...Middleware) *Group {
	return &Group{engine: e, prefix: prefix, middleware: middleware, includeArg: "include"}
}

// RegisterRaw registers raw route handler.
func (e *Engine) RegisterRaw(method, path, operationID string, h Handler, middleware ...Middleware) {
	e.router.Add(method, path, Route{Handler: h, Middleware: middleware})
	e.registry.Add(Operation{
		Method:      method,
		Path:        path,
		OperationID: operationID,
		Handler:     h,
		Middleware:  append([]Middleware{}, middleware...),
		MiddlewareN: len(middleware),
	})
}

// Handle registers typed handler of shape func(ctx, *Input) (*Response[Output], error).
func Handle[Input any, Output any](e *Engine, method, path, operationID string, h func(context.Context, *Input) (*Response[Output], error), opts ...RouteOption) {
	registerTyped(e, method, path, operationID, opts, responseKindJSON, func(rc *RequestContext, in *Input) error {
		out, err := h(rc.Ctx, in)
		if err != nil {
			return err
		}
		if out == nil {
			rc.Writer.WriteHeader(http.StatusNoContent)
			return nil
		}
		for k, vs := range out.Headers {
			for _, v := range vs {
				rc.Writer.Header().Add(k, v)
			}
		}
		if out.Body == nil {
			rc.Writer.WriteHeader(out.Status)
			return nil
		}
		return rc.Engine.encodeResponse(rc.Writer, rc.Request, out.Status, out.Body, false)
	})

	var inT *Input
	var outT *Output
	attachInputOutputMeta(e, method, path, operationID, typeOfPtr(inT), typeOfPtr(outT))
}

// HandleOut registers handler of shape func(ctx, *Input) (*Output, error).
func HandleOut[Input any, Output any](e *Engine, method, path, operationID string, h func(context.Context, *Input) (*Output, error), opts ...RouteOption) {
	registerTyped(e, method, path, operationID, opts, responseKindJSON, func(rc *RequestContext, in *Input) error {
		out, err := h(rc.Ctx, in)
		if err != nil {
			return err
		}
		if out == nil {
			rc.Writer.WriteHeader(http.StatusNoContent)
			return nil
		}
		return rc.Engine.encodeResponse(rc.Writer, rc.Request, http.StatusOK, out, false)
	})

	var inT *Input
	var outT *Output
	attachInputOutputMeta(e, method, path, operationID, typeOfPtr(inT), typeOfPtr(outT))
}

// HandleErr registers handler of shape func(ctx, *Input) error.
func HandleErr[Input any](e *Engine, method, path, operationID string, h func(context.Context, *Input) error, opts ...RouteOption) {
	registerTyped(e, method, path, operationID, opts, responseKindNone, func(rc *RequestContext, in *Input) error {
		if err := h(rc.Ctx, in); err != nil {
			return err
		}
		rc.Writer.WriteHeader(http.StatusNoContent)
		return nil
	})

	var inT *Input
	attachInputOutputMeta(e, method, path, operationID, typeOfPtr(inT), nil)
}

// HandleRawTyped registers handler of shape func(ctx, *Input) (*RawResponse, error).
func HandleRawTyped[Input any](e *Engine, method, path, operationID string, h func(context.Context, *Input) (*RawResponse, error), opts ...RouteOption) {
	registerTyped(e, method, path, operationID, opts, responseKindRaw, func(rc *RequestContext, in *Input) error {
		out, err := h(rc.Ctx, in)
		if err != nil {
			return err
		}
		if out == nil {
			rc.Writer.WriteHeader(http.StatusNoContent)
			return nil
		}
		if out.Status == 0 {
			out.Status = http.StatusOK
		}
		for k, vs := range out.Headers {
			for _, v := range vs {
				rc.Writer.Header().Add(k, v)
			}
		}
		rc.Writer.WriteHeader(out.Status)
		if out.WriteTo != nil {
			return out.WriteTo(rc.Writer)
		}
		return nil
	})

	var inT *Input
	attachInputOutputMeta(e, method, path, operationID, typeOfPtr(inT), nil)
}

// HandleStreamTyped registers handler of shape func(ctx, *Input) (*StreamResponse, error).
func HandleStreamTyped[Input any](e *Engine, method, path, operationID string, h func(context.Context, *Input) (*StreamResponse, error), opts ...RouteOption) {
	registerTyped(e, method, path, operationID, opts, responseKindStream, func(rc *RequestContext, in *Input) error {
		out, err := h(rc.Ctx, in)
		if err != nil {
			return err
		}
		if out == nil {
			rc.Writer.WriteHeader(http.StatusNoContent)
			return nil
		}
		if out.Status == 0 {
			out.Status = http.StatusOK
		}
		if out.ContentType != "" {
			rc.Writer.Header().Set("Content-Type", out.ContentType)
		}
		rc.Writer.WriteHeader(out.Status)
		if out.Reader != nil {
			_, err = io.Copy(rc.Writer, out.Reader)
			return err
		}
		return nil
	})

	var inT *Input
	attachInputOutputMeta(e, method, path, operationID, typeOfPtr(inT), nil)
}

type responseKind int

const (
	responseKindJSON responseKind = iota + 1
	responseKindNone
	responseKindRaw
	responseKindStream
)

func registerTyped[Input any](e *Engine, method, path, operationID string, opts []RouteOption, respKind responseKind, run func(*RequestContext, *Input) error) {
	cfg := buildRouteConfig(opts)
	plan := compileBindPlan[Input]()
	fastBinder, hasFastBinder := compileFastBinder[Input](plan)
	validator := compileValidator[Input]()
	fastValidator, hasFastValidator := compileFastValidator[Input](validator)
	inType := inputStructType[Input]()
	extraValidators := e.validators.forType(inType)

	routeHandler := func(rc *RequestContext) error {
		in := new(Input)
		var bindErr error
		if hasFastBinder {
			bindErr = fastBinder.Bind(rc, in)
		} else {
			bindErr = bindInto(rc, in, plan)
		}
		if bindErr != nil {
			return BadRequest("invalid_request", bindErr.Error())
		}
		if cfg.queryDTO {
			reserved := map[string]struct{}{
				"limit":  {},
				"offset": {},
				"sort":   {},
			}
			dto, err := parseListQueryDTO(rc.Request.URL.Query(), cfg.includeParamName, reserved)
			if err != nil {
				return &APIError{
					Status:  http.StatusUnprocessableEntity,
					Code:    "validation_failed",
					Message: "Validation failed",
					Details: []ErrorDetail{{Path: "query", Code: "query_dto", Message: err.Error()}},
				}
			}
			rc.Ctx = WithListQueryDTO(rc.Ctx, dto)
			rc.Request = rc.Request.WithContext(rc.Ctx)
		}
		if len(cfg.includeAllowlist) > 0 {
			raw := rc.Request.URL.Query().Get(cfg.includeParamName)
			items, err := ParseIncludes(raw, cfg.includeAllowlist)
			if err != nil {
				return &APIError{
					Status:  http.StatusUnprocessableEntity,
					Code:    "validation_failed",
					Message: "Validation failed",
					Details: []ErrorDetail{{Path: cfg.includeParamName, Code: "include", Message: err.Error()}},
				}
			}
			rc.Ctx = WithIncludes(rc.Ctx, items)
			if dto, ok := QueryDTOFromContext(rc.Ctx); ok {
				dto.Include = items
				rc.Ctx = WithListQueryDTO(rc.Ctx, dto)
			}
			rc.Request = rc.Request.WithContext(rc.Ctx)
		}
		var validateErr error
		if hasFastValidator {
			validateErr = fastValidator.Validate(in, extraValidators...)
		} else {
			validateErr = validator.Validate(in, extraValidators...)
		}
		if validateErr != nil {
			if verr, ok := validateErr.(*ValidationErrors); ok {
				return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "Validation failed", Details: verr.Details}
			}
			return Validation("validation_failed", validateErr.Error())
		}
		if cfg.version != "" {
			if reqVersion, ok := APIVersionFromContext(rc.Ctx); ok && strings.TrimSpace(reqVersion) != "" && strings.TrimSpace(reqVersion) != cfg.version {
				return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "Route not found for requested API version"}
			}
		}
		return run(rc, in)
	}
	e.router.Add(method, path, Route{Middleware: cfg.middleware, Handler: routeHandler})

	e.registry.Add(Operation{
		Method:           method,
		Path:             path,
		OperationID:      operationID,
		Handler:          routeHandler,
		Middleware:       append([]Middleware{}, cfg.middleware...),
		Metadata:         copyStringMap(cfg.metadata),
		Version:          cfg.version,
		Callbacks:        cloneAnyMap(cfg.callbacks),
		Tags:             cfg.tags,
		Security:         cfg.security,
		MiddlewareN:      len(cfg.middleware),
		Params:           buildOperationParams(inType, plan, validator),
		HasRequestBody:   plan.body,
		HasFormBody:      plan.form,
		ResponseKind:     respKind,
		InputContentType: "application/json",
		IncludeAllowlist: append([]string{}, cfg.includeAllowlist...),
		IncludeParamName: cfg.includeParamName,
		HasQueryDTO:      cfg.queryDTO,
		RequestExamples:  cloneAnyMap(cfg.requestExamples),
		ResponseExamples: cloneAnyMap(cfg.responseExamples),
	})
}

func attachInputOutputMeta(e *Engine, method, path, operationID string, inType any, outType any) {
	e.registry.Update(method, path, operationID, func(op *Operation) {
		op.InputType = inType
		op.OutputType = outType
	})
}

func inputStructType[T any]() reflect.Type {
	var zero T
	t := reflect.TypeOf(zero)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func typeOfPtr[T any](zero *T) any {
	_ = zero
	var x T
	return x
}

// RouteOption configures operation metadata.
type RouteOption func(*routeConfig)

type routeConfig struct {
	middleware       []Middleware
	tags             []string
	security         []string
	metadata         map[string]string
	version          string
	callbacks        map[string]any
	requestExamples  map[string]any
	responseExamples map[string]any
	includeAllowlist []string
	includeParamName string
	queryDTO         bool
}

func buildRouteConfig(opts []RouteOption) routeConfig {
	cfg := routeConfig{includeParamName: "include"}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithMiddleware attaches route middleware.
func WithMiddleware(mw ...Middleware) RouteOption {
	return func(c *routeConfig) { c.middleware = append(c.middleware, mw...) }
}

// WithTags attaches OpenAPI tags.
func WithTags(tags ...string) RouteOption {
	return func(c *routeConfig) { c.tags = append(c.tags, tags...) }
}

// WithSecurity marks security schemes.
func WithSecurity(items ...string) RouteOption {
	return func(c *routeConfig) { c.security = append(c.security, items...) }
}

// WithMetadata attaches operation metadata.
func WithMetadata(items map[string]string) RouteOption {
	return func(c *routeConfig) {
		if len(items) == 0 {
			return
		}
		if c.metadata == nil {
			c.metadata = map[string]string{}
		}
		for k, v := range items {
			if k == "" {
				continue
			}
			c.metadata[k] = v
		}
	}
}

// WithVersion marks API version for route metadata/OpenAPI extension.
func WithVersion(version string) RouteOption {
	return func(c *routeConfig) {
		c.version = strings.TrimSpace(version)
	}
}

// WithCallbacks attaches OpenAPI callbacks object for the route.
func WithCallbacks(callbacks map[string]any) RouteOption {
	return func(c *routeConfig) {
		if len(callbacks) == 0 {
			return
		}
		c.callbacks = cloneAnyMap(callbacks)
	}
}

// WithRequestExamples attaches OpenAPI request body examples for operation input content type.
func WithRequestExamples(examples map[string]any) RouteOption {
	return func(c *routeConfig) {
		if len(examples) == 0 {
			return
		}
		if c.requestExamples == nil {
			c.requestExamples = map[string]any{}
		}
		for k, v := range examples {
			if strings.TrimSpace(k) == "" {
				continue
			}
			c.requestExamples[k] = v
		}
	}
}

// WithResponseExamples attaches OpenAPI 200 application/json examples.
func WithResponseExamples(examples map[string]any) RouteOption {
	return func(c *routeConfig) {
		if len(examples) == 0 {
			return
		}
		if c.responseExamples == nil {
			c.responseExamples = map[string]any{}
		}
		for k, v := range examples {
			if strings.TrimSpace(k) == "" {
				continue
			}
			c.responseExamples[k] = v
		}
	}
}

// WithIncludeAllowlist enables relation include parsing and validation.
func WithIncludeAllowlist(paths ...string) RouteOption {
	return func(c *routeConfig) { c.includeAllowlist = append(c.includeAllowlist, paths...) }
}

// WithIncludeParamName overrides include query parameter name (default: include).
func WithIncludeParamName(name string) RouteOption {
	return func(c *routeConfig) {
		if name != "" {
			c.includeParamName = name
		}
	}
}

// WithQueryDTO enables parsing list query DTO from URL params.
func WithQueryDTO() RouteOption {
	return func(c *routeConfig) { c.queryDTO = true }
}

// Group holds shared route config.
type Group struct {
	engine     *Engine
	prefix     string
	middleware []Middleware
	tags       []string
	security   []string
	metadata   map[string]string
	version    string
	callbacks  map[string]any
	includes   []string
	includeArg string
	queryDTO   bool
}

// Group creates nested group.
func (g *Group) Group(prefix string, middleware ...Middleware) *Group {
	all := append([]Middleware{}, g.middleware...)
	all = append(all, middleware...)
	return &Group{
		engine:     g.engine,
		prefix:     g.prefix + prefix,
		middleware: all,
		tags:       append([]string{}, g.tags...),
		security:   append([]string{}, g.security...),
		metadata:   copyStringMap(g.metadata),
		version:    g.version,
		callbacks:  cloneAnyMap(g.callbacks),
		includes:   append([]string{}, g.includes...),
		includeArg: g.includeArg,
		queryDTO:   g.queryDTO,
	}
}

// WithTags returns group copy with inherited tags.
func (g *Group) WithTags(tags ...string) *Group {
	next := g.Group("")
	next.tags = append(next.tags, tags...)
	return next
}

// WithSecurity returns group copy with inherited security schemes.
func (g *Group) WithSecurity(items ...string) *Group {
	next := g.Group("")
	next.security = append(next.security, items...)
	return next
}

// WithMetadata returns group copy with inherited operation metadata.
func (g *Group) WithMetadata(items map[string]string) *Group {
	next := g.Group("")
	if next.metadata == nil {
		next.metadata = map[string]string{}
	}
	for k, v := range items {
		if k == "" {
			continue
		}
		next.metadata[k] = v
	}
	return next
}

// WithVersion returns group copy with inherited API version marker.
func (g *Group) WithVersion(version string) *Group {
	next := g.Group("")
	next.version = strings.TrimSpace(version)
	return next
}

// WithVersioning returns group copy with API version extraction middleware.
func (g *Group) WithVersioning(cfg APIVersioningConfig) *Group {
	next := g.Group("")
	next.middleware = append(next.middleware, APIVersioning(cfg))
	return next
}

// WithHeaderVersioning resolves API version only from header within this group.
func (g *Group) WithHeaderVersioning(header string, required bool, defaultVersion string) *Group {
	return g.WithVersioning(APIVersioningConfig{
		Header:         header,
		Sources:        []APIVersionSource{APIVersionSourceHeader},
		Required:       required,
		DefaultVersion: defaultVersion,
	})
}

// WithQueryVersioning resolves API version only from query param within this group.
func (g *Group) WithQueryVersioning(param string, required bool, defaultVersion string) *Group {
	return g.WithVersioning(APIVersioningConfig{
		QueryParam:     param,
		Sources:        []APIVersionSource{APIVersionSourceQuery},
		Required:       required,
		DefaultVersion: defaultVersion,
	})
}

// WithAcceptVersioning resolves API version only from Accept parameter within this group.
func (g *Group) WithAcceptVersioning(param string, required bool, defaultVersion string) *Group {
	return g.WithVersioning(APIVersioningConfig{
		AcceptParam:    param,
		Sources:        []APIVersionSource{APIVersionSourceAccept},
		Required:       required,
		DefaultVersion: defaultVersion,
	})
}

// WithCallbacks returns group copy with inherited OpenAPI callbacks.
func (g *Group) WithCallbacks(callbacks map[string]any) *Group {
	next := g.Group("")
	if len(callbacks) > 0 {
		next.callbacks = cloneAnyMap(callbacks)
	}
	return next
}

// Version creates versioned group with /v<version> prefix and version marker.
func (e *Engine) Version(version string, middleware ...Middleware) *Group {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	g := e.Group("/v"+v, middleware...)
	g.version = v
	return g
}

// WithIncludeAllowlist returns group copy with inherited include allowlist.
func (g *Group) WithIncludeAllowlist(paths ...string) *Group {
	next := g.Group("")
	next.includes = append(next.includes, paths...)
	return next
}

// WithIncludeParamName returns group copy with inherited include query name.
func (g *Group) WithIncludeParamName(name string) *Group {
	next := g.Group("")
	if name != "" {
		next.includeArg = name
	}
	return next
}

// WithQueryDTO returns group copy that enables query DTO parsing.
func (g *Group) WithQueryDTO() *Group {
	next := g.Group("")
	next.queryDTO = true
	return next
}

// Handle registers typed handler in group.
func HandleGroup[Input any, Output any](g *Group, method, path, operationID string, h func(context.Context, *Input) (*Response[Output], error), opts ...RouteOption) {
	groupOpts := []RouteOption{WithMiddleware(g.middleware...)}
	if len(g.tags) > 0 {
		groupOpts = append(groupOpts, WithTags(g.tags...))
	}
	if len(g.security) > 0 {
		groupOpts = append(groupOpts, WithSecurity(g.security...))
	}
	if len(g.metadata) > 0 {
		groupOpts = append(groupOpts, WithMetadata(g.metadata))
	}
	if g.version != "" {
		groupOpts = append(groupOpts, WithVersion(g.version))
	}
	if len(g.callbacks) > 0 {
		groupOpts = append(groupOpts, WithCallbacks(g.callbacks))
	}
	if len(g.includes) > 0 {
		groupOpts = append(groupOpts, WithIncludeAllowlist(g.includes...))
	}
	if g.includeArg != "" {
		groupOpts = append(groupOpts, WithIncludeParamName(g.includeArg))
	}
	if g.queryDTO {
		groupOpts = append(groupOpts, WithQueryDTO())
	}
	opts = append(groupOpts, opts...)
	Handle(g.engine, method, g.prefix+path, operationID, h, opts...)
}

// RegisterSystemRoutes mounts openapi and docs endpoints.
func (e *Engine) RegisterSystemRoutes(openapiPath, docsPath string) {
	e.RegisterRaw(http.MethodGet, openapiPath, "openapi_json", func(rc *RequestContext) error {
		spec := e.registry.OpenAPISpec()
		rc.Writer.Header().Set("Content-Type", "application/json")
		return e.encoder.Encode(rc.Writer, http.StatusOK, spec)
	})
	e.RegisterRaw(http.MethodGet, "/openapi.yaml", "openapi_yaml", func(rc *RequestContext) error {
		data, err := e.registry.MarshalOpenAPIYAML()
		if err != nil {
			return err
		}
		rc.Writer.Header().Set("Content-Type", "application/yaml")
		rc.Writer.WriteHeader(http.StatusOK)
		_, _ = rc.Writer.Write(data)
		return nil
	})
	e.RegisterRaw(http.MethodGet, docsPath, "docs_ui", func(rc *RequestContext) error {
		rc.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		rc.Writer.WriteHeader(http.StatusOK)
		_, _ = rc.Writer.Write([]byte(DefaultDocsHTML(openapiPath)))
		return nil
	})
	e.RegisterSchemaRoutes("/schemas", "/schemas/{name}")
}

// RegisterSchemaRoutes mounts JSON Schema endpoints.
func (e *Engine) RegisterSchemaRoutes(listPath, itemPath string) {
	e.RegisterRaw(http.MethodGet, listPath, "json_schemas", func(rc *RequestContext) error {
		rc.Writer.Header().Set("Content-Type", "application/json")
		return e.encoder.Encode(rc.Writer, http.StatusOK, e.registry.JSONSchemas())
	})
	e.RegisterRaw(http.MethodGet, itemPath, "json_schema_by_name", func(rc *RequestContext) error {
		name := rc.Params["name"]
		schemas := e.registry.JSONSchemas()
		s, ok := schemas[name]
		if !ok {
			return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "Schema not found"}
		}
		rc.Writer.Header().Set("Content-Type", "application/json")
		return e.encoder.Encode(rc.Writer, http.StatusOK, s)
	})
}

type defaultErrorMapper struct{}

func (defaultErrorMapper) MapError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	switch {
	case errors.Is(err, orm.ErrNotFound):
		return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "Resource not found"}
	case errors.Is(err, orm.ErrConflict):
		return &APIError{Status: http.StatusConflict, Code: "conflict", Message: "Conflict"}
	case errors.Is(err, orm.ErrInvalidModel), errors.Is(err, orm.ErrInvalidField), errors.Is(err, orm.ErrInvalidQuery), errors.Is(err, orm.ErrNoRowsAffected):
		return &APIError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	default:
		return &APIError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "Internal server error"}
	}
}

// JSONEncoder writes JSON payloads.
type JSONEncoder struct{}

func (JSONEncoder) Encode(w http.ResponseWriter, status int, body any) error {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(body)
}

// Operations returns copy of registered operations.
func (e *Engine) Operations() []Operation {
	return e.registry.List()
}

// SetOpenAPIServers configures root OpenAPI servers list.
func (e *Engine) SetOpenAPIServers(servers []map[string]any) {
	if e == nil || e.registry == nil {
		return
	}
	e.registry.SetServers(servers)
}

// AddOpenAPIServer appends one server entry with URL and optional description.
func (e *Engine) AddOpenAPIServer(url, description string) {
	if e == nil || e.registry == nil {
		return
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return
	}
	server := map[string]any{"url": url}
	if strings.TrimSpace(description) != "" {
		server["description"] = strings.TrimSpace(description)
	}
	e.registry.mu.RLock()
	curr := make([]map[string]any, 0, len(e.registry.servers)+1)
	for _, s := range e.registry.servers {
		curr = append(curr, cloneAnyMap(s))
	}
	e.registry.mu.RUnlock()
	curr = append(curr, server)
	e.registry.SetServers(curr)
}

// RegisterOpenAPISecurityScheme registers components.securitySchemes entry.
func (e *Engine) RegisterOpenAPISecurityScheme(name string, scheme map[string]any) {
	if e == nil || e.registry == nil {
		return
	}
	e.registry.RegisterSecurityScheme(name, scheme)
}

// OpenAPISpec returns current OpenAPI spec as map.
func (e *Engine) OpenAPISpec() map[string]any {
	if e == nil || e.registry == nil {
		return nil
	}
	return e.registry.OpenAPISpec()
}

// MarshalOpenAPIJSON renders current OpenAPI spec as JSON bytes.
func (e *Engine) MarshalOpenAPIJSON() ([]byte, error) {
	if e == nil || e.registry == nil {
		return nil, errors.New("engine is nil")
	}
	return e.registry.MarshalOpenAPIJSON()
}

// MarshalOpenAPIYAML renders current OpenAPI spec as YAML bytes.
func (e *Engine) MarshalOpenAPIYAML() ([]byte, error) {
	if e == nil || e.registry == nil {
		return nil, errors.New("engine is nil")
	}
	return e.registry.MarshalOpenAPIYAML()
}

// SetReady updates engine readiness state.
func (e *Engine) SetReady(v bool) {
	if e == nil {
		return
	}
	e.ready.Store(v)
}

// IsReady reports current readiness state.
func (e *Engine) IsReady() bool {
	if e == nil {
		return false
	}
	return e.ready.Load()
}

func (e *Engine) encodeResponse(w http.ResponseWriter, r *http.Request, status int, body any, problem bool) error {
	contentType := "application/json"
	if problem {
		contentType = "application/problem+json"
	}
	if r != nil {
		if negotiated := negotiateContentType(r.Header.Get("Accept"), problem, e.encoders); negotiated != "" {
			contentType = negotiated
		}
	}
	enc := e.encoders[contentType]
	if enc == nil {
		enc = e.encoder
	}
	w.Header().Set("Content-Type", contentType)
	return enc.Encode(w, status, body)
}

func (e *Engine) notifyRequestStart(rc *RequestContext) {
	for _, obs := range e.observers {
		obs.OnRequestStart(rc)
	}
}

func (e *Engine) notifyRequestEnd(rc *RequestContext, status int, err error, dur time.Duration) {
	for _, obs := range e.observers {
		obs.OnRequestEnd(rc, status, err, dur)
	}
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return h.Hijack()
}

func (w *statusResponseWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

type headResponseWriter struct {
	http.ResponseWriter
}

func (w *headResponseWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
