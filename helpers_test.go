package arc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
)

func testJWT(claims map[string]any) string {
	header := mustJSON(map[string]any{"alg": "none", "typ": "JWT"})
	payload := mustJSON(claims)
	h := base64.RawURLEncoding.EncodeToString([]byte(header))
	p := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return h + "." + p + "."
}

func testHMACJWT(secret []byte, claims map[string]any) string {
	header := mustJSON(map[string]any{"alg": "HS256", "typ": "JWT"})
	payload := mustJSON(claims)
	h := base64.RawURLEncoding.EncodeToString([]byte(header))
	p := base64.RawURLEncoding.EncodeToString([]byte(payload))
	unsigned := h + "." + p
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write([]byte(unsigned))
	sig := base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	return unsigned + "." + sig
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
