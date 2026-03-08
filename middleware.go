package arc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// Logger logs request latency and status.
func Logger(logger *log.Logger) Middleware {
	if logger == nil {
		logger = log.Default()
	}
	return func(next Handler) Handler {
		return func(rc *RequestContext) error {
			start := time.Now()
			err := next(rc)
			logger.Printf("%s %s dur=%s err=%v", rc.Request.Method, rc.Request.URL.Path, time.Since(start), err)
			return err
		}
	}
}

// Recovery converts panic into API error.
func Recovery() Middleware {
	return func(next Handler) Handler {
		return func(rc *RequestContext) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = &APIError{Status: http.StatusInternalServerError, Code: "panic", Message: "panic recovered", Details: []ErrorDetail{{Code: "stack", Message: string(debug.Stack())}}}
				}
			}()
			return next(rc)
		}
	}
}

// TenantKey stores resolved tenant value in context.
type tenantKey struct{}

// TenantFromContext returns tenant value.
func TenantFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(tenantKey{}).(string)
	return v, ok
}

// TenantExtractor reads tenant from request and injects into context.
func TenantExtractor(extractors ...TenantResolveFunc) Middleware {
	if len(extractors) == 0 {
		extractors = []TenantResolveFunc{TenantFromHeader("X-Tenant-ID")}
	}
	return func(next Handler) Handler {
		return func(rc *RequestContext) error {
			for _, ex := range extractors {
				if v, ok := ex(rc); ok {
					rc.Ctx = context.WithValue(rc.Ctx, tenantKey{}, v)
					rc.Request = rc.Request.WithContext(rc.Ctx)
					break
				}
			}
			return next(rc)
		}
	}
}

// TenantResolveFunc extracts tenant id from request context.
type TenantResolveFunc func(*RequestContext) (string, bool)

func TenantFromHeader(name string) TenantResolveFunc {
	return func(rc *RequestContext) (string, bool) {
		v := rc.Request.Header.Get(name)
		return v, v != ""
	}
}

func TenantFromPath(param string) TenantResolveFunc {
	return func(rc *RequestContext) (string, bool) {
		v, ok := rc.Params[param]
		return v, ok && v != ""
	}
}

func TenantFromCookie(name string) TenantResolveFunc {
	return func(rc *RequestContext) (string, bool) {
		c, err := rc.Request.Cookie(name)
		if err != nil || c.Value == "" {
			return "", false
		}
		return c.Value, true
	}
}

// TenantFromJWTClaim extracts tenant from Bearer JWT payload claim.
// It only decodes JWT payload and does not verify signature.
func TenantFromJWTClaim(claim string) TenantResolveFunc {
	if claim == "" {
		claim = "tenant"
	}
	return func(rc *RequestContext) (string, bool) {
		auth := strings.TrimSpace(rc.Request.Header.Get("Authorization"))
		if auth == "" {
			return "", false
		}
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return "", false
		}
		tokenParts := strings.Split(parts[1], ".")
		if len(tokenParts) < 2 {
			return "", false
		}
		payload, err := base64.RawURLEncoding.DecodeString(tokenParts[1])
		if err != nil {
			return "", false
		}
		claims := map[string]any{}
		if err := json.Unmarshal(payload, &claims); err != nil {
			return "", false
		}
		raw, ok := claims[claim]
		if !ok || raw == nil {
			return "", false
		}
		switch v := raw.(type) {
		case string:
			return v, v != ""
		case float64:
			s := strconvTrimFloat(v)
			return s, s != ""
		case bool:
			if v {
				return "true", true
			}
			return "false", true
		default:
			s := fmt.Sprint(v)
			return s, s != ""
		}
	}
}

// JWTVerifier verifies JWT signature and temporal claims.
type JWTVerifier interface {
	VerifyAndParse(token string) (map[string]any, error)
}

// HMACJWTVerifier verifies JWT with HMAC SHA algorithms.
type HMACJWTVerifier struct {
	secret      []byte
	allowedAlgs map[string]func() hash.Hash
	leeway      time.Duration
	now         func() time.Time
}

// NewHMACJWTVerifier creates HMAC JWT verifier for HS256/HS384/HS512.
func NewHMACJWTVerifier(secret []byte, allowedAlgs ...string) *HMACJWTVerifier {
	algMap := map[string]func() hash.Hash{
		"HS256": sha256.New,
		"HS384": sha512.New384,
		"HS512": sha512.New,
	}
	if len(allowedAlgs) > 0 {
		filtered := map[string]func() hash.Hash{}
		for _, alg := range allowedAlgs {
			if h, ok := algMap[strings.ToUpper(strings.TrimSpace(alg))]; ok {
				filtered[strings.ToUpper(strings.TrimSpace(alg))] = h
			}
		}
		if len(filtered) > 0 {
			algMap = filtered
		}
	}
	return &HMACJWTVerifier{
		secret:      append([]byte{}, secret...),
		allowedAlgs: algMap,
		leeway:      0,
		now:         time.Now,
	}
}

// WithLeeway configures allowed clock skew for exp/nbf/iat checks.
func (v *HMACJWTVerifier) WithLeeway(d time.Duration) *HMACJWTVerifier {
	if v == nil {
		return v
	}
	v.leeway = d
	return v
}

// VerifyAndParse verifies JWT signature and temporal claims.
func (v *HMACJWTVerifier) VerifyAndParse(token string) (map[string]any, error) {
	if v == nil {
		return nil, errors.New("jwt verifier is nil")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid jwt format")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid jwt header encoding")
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid jwt payload encoding")
	}
	sigRaw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("invalid jwt signature encoding")
	}

	header := map[string]any{}
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return nil, errors.New("invalid jwt header")
	}
	alg, _ := header["alg"].(string)
	alg = strings.ToUpper(strings.TrimSpace(alg))
	if alg == "" || alg == "NONE" {
		return nil, errors.New("jwt alg is not allowed")
	}
	hf, ok := v.allowedAlgs[alg]
	if !ok {
		return nil, errors.New("jwt alg is not allowed")
	}
	mac := hmac.New(hf, v.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)
	if !hmac.Equal(expected, sigRaw) {
		return nil, errors.New("invalid jwt signature")
	}

	claims := map[string]any{}
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return nil, errors.New("invalid jwt claims")
	}
	if err := validateTemporalClaims(claims, v.now(), v.leeway); err != nil {
		return nil, err
	}
	return claims, nil
}

// TenantFromVerifiedJWTClaim extracts tenant from verified JWT claim.
func TenantFromVerifiedJWTClaim(claim string, verifier JWTVerifier) TenantResolveFunc {
	if claim == "" {
		claim = "tenant"
	}
	return func(rc *RequestContext) (string, bool) {
		if verifier == nil || rc == nil || rc.Request == nil {
			return "", false
		}
		auth := strings.TrimSpace(rc.Request.Header.Get("Authorization"))
		if auth == "" {
			return "", false
		}
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return "", false
		}
		claims, err := verifier.VerifyAndParse(parts[1])
		if err != nil {
			return "", false
		}
		raw, ok := claims[claim]
		if !ok || raw == nil {
			return "", false
		}
		switch v := raw.(type) {
		case string:
			return v, v != ""
		case float64:
			s := strconvTrimFloat(v)
			return s, s != ""
		case bool:
			if v {
				return "true", true
			}
			return "false", true
		default:
			s := fmt.Sprint(v)
			return s, s != ""
		}
	}
}

func validateTemporalClaims(claims map[string]any, now time.Time, leeway time.Duration) error {
	epoch := float64(now.Unix())
	if exp, ok := numericClaim(claims["exp"]); ok {
		if epoch > exp+leeway.Seconds() {
			return errors.New("jwt expired")
		}
	}
	if nbf, ok := numericClaim(claims["nbf"]); ok {
		if epoch+leeway.Seconds() < nbf {
			return errors.New("jwt not active yet")
		}
	}
	if iat, ok := numericClaim(claims["iat"]); ok {
		if epoch+leeway.Seconds() < iat {
			return errors.New("jwt issued in the future")
		}
	}
	return nil
}

func numericClaim(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func strconvTrimFloat(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%v", v)
}
