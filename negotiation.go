package arc

import (
	"sort"
	"strconv"
	"strings"
)

type acceptSpec struct {
	media string
	q     float64
	order int
}

func negotiateContentType(accept string, problem bool, encoders map[string]Encoder) string {
	defaultType := "application/json"
	if problem {
		if _, ok := encoders["application/problem+json"]; ok {
			defaultType = "application/problem+json"
		}
	}
	if strings.TrimSpace(accept) == "" {
		return defaultType
	}
	specs := parseAccept(accept)
	if len(specs) == 0 {
		return defaultType
	}
	candidates := make([]string, 0, len(encoders))
	for ct := range encoders {
		candidates = append(candidates, ct)
	}
	sort.Strings(candidates)
	for _, s := range specs {
		if s.q <= 0 {
			continue
		}
		if acceptMatches(s.media, defaultType) {
			return defaultType
		}
		for _, ct := range candidates {
			if acceptMatches(s.media, ct) {
				return ct
			}
		}
	}
	return defaultType
}

func parseAccept(accept string) []acceptSpec {
	parts := strings.Split(accept, ",")
	out := make([]acceptSpec, 0, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		media := strings.TrimSpace(strings.SplitN(p, ";", 2)[0])
		if media == "" {
			continue
		}
		q := 1.0
		for _, param := range strings.Split(p, ";")[1:] {
			param = strings.TrimSpace(param)
			if !strings.HasPrefix(strings.ToLower(param), "q=") {
				continue
			}
			v := strings.TrimSpace(param[2:])
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				if f < 0 {
					f = 0
				}
				if f > 1 {
					f = 1
				}
				q = f
			}
		}
		out = append(out, acceptSpec{media: strings.ToLower(media), q: q, order: i})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].q == out[j].q {
			return out[i].order < out[j].order
		}
		return out[i].q > out[j].q
	})
	return out
}

func acceptMatches(acceptMedia, contentType string) bool {
	acceptMedia = strings.ToLower(strings.TrimSpace(acceptMedia))
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if acceptMedia == "*/*" {
		return true
	}
	if acceptMedia == contentType {
		return true
	}
	aType, aSub, okA := splitMedia(acceptMedia)
	cType, cSub, okC := splitMedia(contentType)
	if !okA || !okC {
		return false
	}
	if aType == cType && aSub == "*" {
		return true
	}
	// application/*+json matches vendor-specific JSON content types.
	if aType == cType && strings.HasPrefix(aSub, "*+") && strings.HasSuffix(cSub, aSub[2:]) {
		return true
	}
	// application/vnd.x+json should match application/json encoder.
	if cType == "application" && strings.HasSuffix(aSub, "+json") && contentType == "application/json" {
		return true
	}
	// application/json should match vendor-specific +json encoder if present.
	if aType == "application" && aSub == "json" && cType == "application" && strings.HasSuffix(contentType, "+json") {
		return true
	}
	return false
}

func splitMedia(media string) (string, string, bool) {
	parts := strings.SplitN(media, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
