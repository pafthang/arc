package arc

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type includeCtxKey struct{}

// IncludeTree represents nested include graph: profile, roles.permissions, ...
type IncludeTree map[string]IncludeTree

// WithIncludes stores validated include paths in context.
func WithIncludes(ctx context.Context, includes []string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	uniq := uniqueStrings(includes)
	return context.WithValue(ctx, includeCtxKey{}, uniq)
}

// IncludesFromContext returns validated include paths.
func IncludesFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	items, _ := ctx.Value(includeCtxKey{}).([]string)
	return append([]string{}, items...)
}

// HasInclude reports whether exact include path was requested.
func HasInclude(ctx context.Context, path string) bool {
	target := normalizeIncludePath(path)
	if target == "" {
		return false
	}
	for _, p := range IncludesFromContext(ctx) {
		if normalizeIncludePath(p) == target {
			return true
		}
	}
	return false
}

// IncludeTreeFromContext returns nested relation tree from includes in context.
func IncludeTreeFromContext(ctx context.Context) IncludeTree {
	return BuildIncludeTree(IncludesFromContext(ctx))
}

// ParseIncludes parses comma-separated include and validates against allowlist.
func ParseIncludes(raw string, allowlist []string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	allow := map[string]struct{}{}
	for _, p := range allowlist {
		p = normalizeIncludePath(p)
		if p != "" {
			allow[p] = struct{}{}
		}
	}
	if len(allow) == 0 {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := normalizeIncludePath(part)
		if item == "" {
			continue
		}
		if _, ok := allow[item]; !ok {
			allowed := make([]string, 0, len(allow))
			for p := range allow {
				allowed = append(allowed, p)
			}
			sort.Strings(allowed)
			return nil, fmt.Errorf("include '%s' is not allowed (allowed: %s)", item, strings.Join(allowed, ","))
		}
		out = append(out, item)
	}
	return uniqueStrings(out), nil
}

// BuildIncludeTree builds nested relation tree from flat include paths.
func BuildIncludeTree(includes []string) IncludeTree {
	root := IncludeTree{}
	for _, in := range includes {
		p := normalizeIncludePath(in)
		if p == "" {
			continue
		}
		cur := root
		for _, seg := range strings.Split(p, ".") {
			if seg == "" {
				continue
			}
			next, ok := cur[seg]
			if !ok {
				next = IncludeTree{}
				cur[seg] = next
			}
			cur = next
		}
	}
	return root
}

// FlattenIncludeTree returns sorted flat include list from tree.
func FlattenIncludeTree(tree IncludeTree) []string {
	out := make([]string, 0)
	flattenIncludeTree("", tree, &out)
	sort.Strings(out)
	return out
}

func flattenIncludeTree(prefix string, tree IncludeTree, out *[]string) {
	for seg, child := range tree {
		path := seg
		if prefix != "" {
			path = prefix + "." + seg
		}
		*out = append(*out, path)
		flattenIncludeTree(path, child, out)
	}
}

func normalizeIncludePath(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	parts := strings.Split(in, ".")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		clean = append(clean, p)
	}
	return strings.Join(clean, ".")
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
