package arc

import (
	"context"
	"strings"
)

type apiVersionKey struct{}

// APIVersionSource identifies request source for API version extraction.
type APIVersionSource string

const (
	APIVersionSourceHeader APIVersionSource = "header"
	APIVersionSourceQuery  APIVersionSource = "query"
	APIVersionSourceAccept APIVersionSource = "accept"
)

// WithAPIVersion stores resolved API version in context.
func WithAPIVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, apiVersionKey{}, strings.TrimSpace(version))
}

// APIVersionFromContext returns resolved API version.
func APIVersionFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(apiVersionKey{}).(string)
	return v, ok && v != ""
}

// APIVersioningConfig configures API version extraction middleware.
type APIVersioningConfig struct {
	Header         string
	QueryParam     string
	AcceptParam    string
	Sources        []APIVersionSource
	DefaultVersion string
	Required       bool
	ResponseHeader string
}

// APIVersioning resolves API version from request metadata and injects it into context.
func APIVersioning(cfg APIVersioningConfig) Middleware {
	header := strings.TrimSpace(cfg.Header)
	if header == "" {
		header = "X-API-Version"
	}
	query := strings.TrimSpace(cfg.QueryParam)
	if query == "" {
		query = "version"
	}
	acceptParam := strings.TrimSpace(cfg.AcceptParam)
	if acceptParam == "" {
		acceptParam = "version"
	}
	responseHeader := strings.TrimSpace(cfg.ResponseHeader)
	if responseHeader == "" {
		responseHeader = "X-API-Version"
	}

	return func(next Handler) Handler {
		return func(rc *RequestContext) error {
			version := ""
			sources := cfg.Sources
			if len(sources) == 0 {
				sources = []APIVersionSource{APIVersionSourceHeader, APIVersionSourceQuery, APIVersionSourceAccept}
			}
			for _, src := range sources {
				switch src {
				case APIVersionSourceHeader:
					version = strings.TrimSpace(rc.Request.Header.Get(header))
				case APIVersionSourceQuery:
					version = strings.TrimSpace(rc.Request.URL.Query().Get(query))
				case APIVersionSourceAccept:
					version = parseAcceptParam(rc.Request.Header.Get("Accept"), acceptParam)
				}
				if version != "" {
					break
				}
			}
			if version == "" {
				version = strings.TrimSpace(cfg.DefaultVersion)
			}
			if version == "" && cfg.Required {
				return BadRequest("invalid_version", "API version is required")
			}
			if version != "" {
				rc.Ctx = WithAPIVersion(rc.Ctx, version)
				rc.Request = rc.Request.WithContext(rc.Ctx)
				rc.Writer.Header().Set(responseHeader, version)
			}
			return next(rc)
		}
	}
}

func parseAcceptParam(accept, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || strings.TrimSpace(accept) == "" {
		return ""
	}
	for _, part := range strings.Split(accept, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}
		for _, seg := range segments[1:] {
			param := strings.SplitN(strings.TrimSpace(seg), "=", 2)
			if len(param) != 2 {
				continue
			}
			if strings.ToLower(strings.TrimSpace(param[0])) != key {
				continue
			}
			return strings.Trim(strings.TrimSpace(param[1]), "\"")
		}
	}
	return ""
}
