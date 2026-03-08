package arc

import "strings"

// OpenAPIQualityGates configures required quality constraints for generated OpenAPI.
type OpenAPIQualityGates struct {
	RequireRootTags         bool
	RequireServers          bool
	RequireExamples         bool
	RequiredSecuritySchemes []string
}

// ValidateOpenAPIQuality checks generated spec against requested quality gates.
// It returns a list of violations; empty result means the spec passed.
func ValidateOpenAPIQuality(spec map[string]any, gates OpenAPIQualityGates) []string {
	if spec == nil {
		return []string{"spec is nil"}
	}

	violations := make([]string, 0)

	if gates.RequireRootTags {
		if sliceLen(spec["tags"]) == 0 {
			violations = append(violations, "root tags list is required")
		}
	}

	if gates.RequireServers {
		if sliceLen(spec["servers"]) == 0 {
			violations = append(violations, "servers list is required")
		}
	}

	if len(gates.RequiredSecuritySchemes) > 0 {
		components, _ := spec["components"].(map[string]any)
		securitySchemes, _ := components["securitySchemes"].(map[string]any)
		for _, raw := range gates.RequiredSecuritySchemes {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if _, ok := securitySchemes[name]; !ok {
				violations = append(violations, "components.securitySchemes."+name+" is required")
			}
		}
	}

	if gates.RequireExamples && !hasOpenAPIExamples(spec) {
		violations = append(violations, "at least one operation example is required")
	}

	return violations
}

func hasOpenAPIExamples(spec map[string]any) bool {
	paths, _ := spec["paths"].(map[string]any)
	for _, pathNode := range paths {
		pathMap, _ := pathNode.(map[string]any)
		for _, opNode := range pathMap {
			op, _ := opNode.(map[string]any)
			if hasRequestExamples(op) || hasResponseExamples(op) {
				return true
			}
		}
	}
	return false
}

func hasRequestExamples(op map[string]any) bool {
	reqBody, _ := op["requestBody"].(map[string]any)
	content, _ := reqBody["content"].(map[string]any)
	for _, mediaNode := range content {
		media, _ := mediaNode.(map[string]any)
		examples, _ := media["examples"].(map[string]any)
		if len(examples) > 0 {
			return true
		}
	}
	return false
}

func sliceLen(v any) int {
	switch s := v.(type) {
	case []any:
		return len(s)
	case []map[string]any:
		return len(s)
	default:
		return 0
	}
}

func hasResponseExamples(op map[string]any) bool {
	responses, _ := op["responses"].(map[string]any)
	for _, respNode := range responses {
		resp, _ := respNode.(map[string]any)
		content, _ := resp["content"].(map[string]any)
		for _, mediaNode := range content {
			media, _ := mediaNode.(map[string]any)
			examples, _ := media["examples"].(map[string]any)
			if len(examples) > 0 {
				return true
			}
		}
	}
	return false
}
