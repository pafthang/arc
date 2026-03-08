package arc

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	status int
	header http.Header
	body   []byte
	exp    time.Time
}

// ResponseCache stores cached HTTP responses.
type ResponseCache struct {
	mu         sync.RWMutex
	items      map[string]cacheEntry
	pathToKeys map[string]map[string]struct{}
	keyToPath  map[string]string
}

// NewResponseCache creates empty cache store.
func NewResponseCache() *ResponseCache {
	return &ResponseCache{
		items:      map[string]cacheEntry{},
		pathToKeys: map[string]map[string]struct{}{},
		keyToPath:  map[string]string{},
	}
}

func (c *ResponseCache) get(key string, now time.Time) (cacheEntry, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || now.After(e.exp) {
		if ok {
			c.mu.Lock()
			delete(c.items, key)
			if path, exists := c.keyToPath[key]; exists {
				c.removeKeyFromPathLocked(key, path)
			}
			delete(c.keyToPath, key)
			c.mu.Unlock()
		}
		return cacheEntry{}, false
	}
	return e, true
}

func (c *ResponseCache) set(key string, path string, e cacheEntry) {
	c.mu.Lock()
	if prevPath, ok := c.keyToPath[key]; ok && prevPath != "" {
		c.removeKeyFromPathLocked(key, prevPath)
	}
	c.items[key] = e
	c.keyToPath[key] = path
	if path != "" {
		ks := c.pathToKeys[path]
		if ks == nil {
			ks = map[string]struct{}{}
			c.pathToKeys[path] = ks
		}
		ks[key] = struct{}{}
	}
	c.mu.Unlock()
}

// Clear removes all cached entries.
func (c *ResponseCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = map[string]cacheEntry{}
	c.pathToKeys = map[string]map[string]struct{}{}
	c.keyToPath = map[string]string{}
	c.mu.Unlock()
}

// InvalidatePathPrefix removes cached entries where request path starts with prefix.
func (c *ResponseCache) InvalidatePathPrefix(prefix string) {
	if c == nil {
		return
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		c.Clear()
		return
	}
	c.mu.Lock()
	for path := range c.pathToKeys {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		for key := range c.pathToKeys[path] {
			delete(c.items, key)
			delete(c.keyToPath, key)
		}
		delete(c.pathToKeys, path)
	}
	c.mu.Unlock()
}

func (c *ResponseCache) removeKeyFromPathLocked(key, path string) {
	if path == "" {
		return
	}
	ks := c.pathToKeys[path]
	if ks == nil {
		return
	}
	delete(ks, key)
	if len(ks) == 0 {
		delete(c.pathToKeys, path)
	}
}

// CacheConfig configures in-memory response cache middleware.
type CacheConfig struct {
	TTL            time.Duration
	VaryHeaders    []string
	SkipAuthorized bool
	SkipCookies    bool
	Store          *ResponseCache
}

// CacheInvalidationConfig configures cache invalidation middleware.
type CacheInvalidationConfig struct {
	Store                 *ResponseCache
	Methods               []string
	PathPrefixes          []string
	PathPrefixResolver    func(*http.Request) []string
	ClearOnAnyMatch       bool
	InvalidateRequestPath bool
}

// CacheResponses caches GET/HEAD responses in-memory for ttl.
func CacheResponses(ttl time.Duration) Middleware {
	return CacheResponsesWithConfig(CacheConfig{
		TTL:            ttl,
		VaryHeaders:    []string{"Accept", "Accept-Encoding"},
		SkipAuthorized: true,
		SkipCookies:    true,
	})
}

// CacheResponsesWithConfig caches responses using explicit cache configuration.
func CacheResponsesWithConfig(cfg CacheConfig) Middleware {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	varyHeaders := cfg.VaryHeaders
	if len(varyHeaders) == 0 {
		varyHeaders = []string{"Accept", "Accept-Encoding"}
	}
	cache := cfg.Store
	if cache == nil {
		cache = NewResponseCache()
	}
	return func(next Handler) Handler {
		return func(rc *RequestContext) error {
			if rc == nil || rc.Request == nil {
				return next(rc)
			}
			if rc.Request.Method != http.MethodGet && rc.Request.Method != http.MethodHead {
				return next(rc)
			}
			if shouldBypassCache(rc.Request, cfg.SkipAuthorized, cfg.SkipCookies) {
				rc.Writer.Header().Set("X-Cache", "BYPASS")
				return next(rc)
			}
			key := cacheKey(rc.Request, varyHeaders)
			if e, ok := cache.get(key, time.Now()); ok {
				if etag := strings.TrimSpace(e.header.Get("ETag")); etag != "" && ifNoneMatchMatches(rc.Request.Header.Get("If-None-Match"), etag) {
					for k, vs := range e.header {
						for _, v := range vs {
							rc.Writer.Header().Add(k, v)
						}
					}
					rc.Writer.Header().Set("X-Cache", "HIT")
					rc.Writer.WriteHeader(http.StatusNotModified)
					return nil
				}
				for k, vs := range e.header {
					for _, v := range vs {
						rc.Writer.Header().Add(k, v)
					}
				}
				rc.Writer.Header().Set("X-Cache", "HIT")
				rc.Writer.WriteHeader(e.status)
				if rc.Request.Method != http.MethodHead {
					_, _ = rc.Writer.Write(e.body)
				}
				return nil
			}
			rec := newCacheResponseWriter(rc.Writer)
			old := rc.Writer
			rc.Writer = rec
			err := next(rc)
			rc.Writer = old
			if err != nil {
				return err
			}
			if shouldStoreResponse(rec.status, rec.header) {
				if strings.TrimSpace(rec.header.Get("ETag")) == "" && rec.buf.Len() > 0 {
					rec.header.Set("ETag", buildWeakETag(rec.buf.Bytes()))
				}
				cache.set(key, rc.Request.URL.Path, cacheEntry{
					status: rec.status,
					header: cloneHeader(rec.header),
					body:   append([]byte{}, rec.buf.Bytes()...),
					exp:    time.Now().Add(ttl),
				})
				rec.header.Set("X-Cache", "MISS")
			} else {
				rec.header.Set("X-Cache", "BYPASS")
			}
			mergeHeaders(old.Header(), rec.header)
			old.WriteHeader(rec.status)
			if rc.Request.Method != http.MethodHead {
				_, _ = old.Write(rec.buf.Bytes())
			}
			return nil
		}
	}
}

// InvalidateCacheOnWrite clears/invalidate cache entries after mutating requests.
func InvalidateCacheOnWrite(cfg CacheInvalidationConfig) Middleware {
	store := cfg.Store
	if store == nil {
		store = NewResponseCache()
	}
	methods := normalizeMethods(cfg.Methods)
	if len(methods) == 0 {
		methods = map[string]struct{}{
			http.MethodPost:   {},
			http.MethodPut:    {},
			http.MethodPatch:  {},
			http.MethodDelete: {},
		}
	}
	return func(next Handler) Handler {
		return func(rc *RequestContext) error {
			err := next(rc)
			if err != nil || rc == nil || rc.Request == nil {
				return err
			}
			if _, ok := methods[rc.Request.Method]; !ok {
				return nil
			}
			if cfg.ClearOnAnyMatch {
				store.Clear()
				return nil
			}
			prefixes := make([]string, 0, 4)
			prefixes = append(prefixes, cfg.PathPrefixes...)
			if cfg.PathPrefixResolver != nil {
				prefixes = append(prefixes, cfg.PathPrefixResolver(rc.Request)...)
			}
			if cfg.InvalidateRequestPath || len(prefixes) == 0 {
				prefixes = append(prefixes, rc.Request.URL.Path)
				if parent := parentCollectionPath(rc.Request.URL.Path); parent != "" {
					prefixes = append(prefixes, parent)
				}
			}
			for _, p := range prefixes {
				if strings.TrimSpace(p) == "" {
					continue
				}
				store.InvalidatePathPrefix(p)
			}
			return nil
		}
	}
}

type cacheResponseWriter struct {
	http.ResponseWriter
	header http.Header
	status int
	buf    *bytes.Buffer
}

func newCacheResponseWriter(w http.ResponseWriter) *cacheResponseWriter {
	return &cacheResponseWriter{
		ResponseWriter: w,
		header:         make(http.Header),
		status:         http.StatusOK,
		buf:            &bytes.Buffer{},
	}
}

func (w *cacheResponseWriter) Header() http.Header {
	return w.header
}

func (w *cacheResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *cacheResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.buf.Write(p)
}

func cacheKey(r *http.Request, varyHeaders []string) string {
	var b strings.Builder
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	b.WriteString(method)
	b.WriteString(" ")
	b.WriteString(r.URL.RequestURI())
	for _, h := range varyHeaders {
		name := http.CanonicalHeaderKey(strings.TrimSpace(h))
		if name == "" {
			continue
		}
		b.WriteString(" ")
		b.WriteString(strings.ToLower(name))
		b.WriteString("=")
		b.WriteString(strings.TrimSpace(r.Header.Get(name)))
	}
	return b.String()
}

func normalizeMethods(items []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range items {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		out[m] = struct{}{}
	}
	return out
}

func parentCollectionPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "/"
	}
	return path[:idx]
}

func buildWeakETag(body []byte) string {
	sum := sha1.Sum(body)
	return `W/"` + hex.EncodeToString(sum[:]) + `"`
}

func ifNoneMatchMatches(ifNoneMatch, etag string) bool {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	for _, part := range strings.Split(ifNoneMatch, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if p == etag {
			return true
		}
		// Weak comparison: normalize W/ prefix in both directions.
		if strings.TrimPrefix(p, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

func shouldStoreResponse(status int, h http.Header) bool {
	if status < 200 || status >= 300 {
		return false
	}
	cc := strings.ToLower(h.Get("Cache-Control"))
	if strings.Contains(cc, "no-store") {
		return false
	}
	if h.Get("Set-Cookie") != "" {
		return false
	}
	return true
}

func shouldBypassCache(r *http.Request, skipAuthorized bool, skipCookies bool) bool {
	cc := strings.ToLower(r.Header.Get("Cache-Control"))
	if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		return true
	}
	if skipAuthorized && strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return true
	}
	if skipCookies && strings.TrimSpace(r.Header.Get("Cookie")) != "" {
		return true
	}
	return false
}

func cloneHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vs := range h {
		out[k] = append([]string{}, vs...)
	}
	return out
}

func mergeHeaders(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// CacheControlHeaders helper for response cache directives.
func CacheControlHeaders(maxAge time.Duration) http.Header {
	h := make(http.Header)
	h.Set("Cache-Control", "public, max-age="+strconv.FormatInt(int64(maxAge.Seconds()), 10))
	return h
}
