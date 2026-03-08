package arc

import (
	"context"
	"net/http"
	"sort"
	"strings"
)

// RequestContext carries request-scoped values.
type RequestContext struct {
	Writer  http.ResponseWriter
	Request *http.Request
	Params  map[string]string
	Engine  *Engine
	Ctx     context.Context
}

// Route describes runtime route entry.
type Route struct {
	Handler    Handler
	Middleware []Middleware
}

type node struct {
	segment      string
	children     map[string]*node
	paramChild   *node
	paramName    string
	catchAll     *node
	catchAllName string
	route        *Route
}

// Router is radix-like route tree per method.
type Router struct {
	roots map[string]*node
}

// NewRouter creates empty router.
func NewRouter() *Router {
	return &Router{roots: map[string]*node{}}
}

// Add registers method/path.
func (r *Router) Add(method, path string, route Route) {
	method = strings.ToUpper(method)
	if _, ok := r.roots[method]; !ok {
		r.roots[method] = &node{children: map[string]*node{}}
	}
	cur := r.roots[method]
	for _, seg := range splitPath(path) {
		switch {
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "...}"):
			name := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "...}")
			if cur.catchAll == nil {
				cur.catchAll = &node{children: map[string]*node{}, catchAllName: name}
			}
			cur = cur.catchAll
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			name := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
			if cur.paramChild == nil {
				cur.paramChild = &node{children: map[string]*node{}, paramName: name}
			}
			cur = cur.paramChild
		default:
			n, ok := cur.children[seg]
			if !ok {
				n = &node{segment: seg, children: map[string]*node{}}
				cur.children[seg] = n
			}
			cur = n
		}
	}
	cur.route = &route
}

// Match resolves method/path.
func (r *Router) Match(method, path string) (Route, map[string]string, bool) {
	root := r.roots[strings.ToUpper(method)]
	if root == nil {
		return Route{}, nil, false
	}
	segs := splitPath(path)
	params := map[string]string{}
	cur := root
	for i, seg := range segs {
		if n, ok := cur.children[seg]; ok {
			cur = n
			continue
		}
		if cur.paramChild != nil {
			params[cur.paramChild.paramName] = seg
			cur = cur.paramChild
			continue
		}
		if cur.catchAll != nil {
			params[cur.catchAll.catchAllName] = strings.Join(segs[i:], "/")
			cur = cur.catchAll
			break
		}
		return Route{}, nil, false
	}
	if cur.route == nil {
		if cur.catchAll != nil && cur.catchAll.route != nil {
			return *cur.catchAll.route, params, true
		}
		return Route{}, nil, false
	}
	return *cur.route, params, true
}

// AllowedMethods returns methods matching the same path pattern.
func (r *Router) AllowedMethods(path string) []string {
	out := make([]string, 0, len(r.roots))
	for method := range r.roots {
		if _, _, ok := r.Match(method, path); ok {
			out = append(out, method)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
