package arc

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type queryDTOKey struct{}

// SortField describes one sorting clause.
type SortField struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc"`
}

// ListQueryDTO contains normalized list-query primitives.
type ListQueryDTO struct {
	Limit   *int                `json:"limit,omitempty"`
	Offset  *int                `json:"offset,omitempty"`
	Sort    []SortField         `json:"sort,omitempty"`
	Include []string            `json:"include,omitempty"`
	Filters map[string][]string `json:"filters,omitempty"`
}

// WithListQueryDTO stores parsed list query DTO in context.
func WithListQueryDTO(ctx context.Context, dto ListQueryDTO) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, queryDTOKey{}, dto)
}

// QueryDTOFromContext returns parsed list query DTO.
func QueryDTOFromContext(ctx context.Context) (ListQueryDTO, bool) {
	if ctx == nil {
		return ListQueryDTO{}, false
	}
	v, ok := ctx.Value(queryDTOKey{}).(ListQueryDTO)
	return v, ok
}

func parseListQueryDTO(values url.Values, includeParam string, reserved map[string]struct{}) (ListQueryDTO, error) {
	if includeParam == "" {
		includeParam = "include"
	}
	dto := ListQueryDTO{Filters: map[string][]string{}}

	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return ListQueryDTO{}, fmt.Errorf("limit must be a non-negative integer")
		}
		dto.Limit = &n
	}
	if raw := strings.TrimSpace(values.Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return ListQueryDTO{}, fmt.Errorf("offset must be a non-negative integer")
		}
		dto.Offset = &n
	}

	sortVals := append([]string{}, values["sort"]...)
	for _, raw := range sortVals {
		for _, token := range strings.Split(raw, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			sf := SortField{}
			if strings.HasPrefix(token, "-") {
				sf.Desc = true
				sf.Field = strings.TrimSpace(strings.TrimPrefix(token, "-"))
			} else {
				sf.Field = token
			}
			if sf.Field == "" {
				return ListQueryDTO{}, fmt.Errorf("sort contains empty field")
			}
			dto.Sort = append(dto.Sort, sf)
		}
	}

	includeVals := append([]string{}, values[includeParam]...)
	for _, raw := range includeVals {
		for _, token := range strings.Split(raw, ",") {
			token = normalizeIncludePath(token)
			if token == "" {
				continue
			}
			dto.Include = append(dto.Include, token)
		}
	}
	dto.Include = uniqueStrings(dto.Include)

	for k, vals := range values {
		if _, ok := reserved[k]; ok || k == includeParam {
			continue
		}
		clean := filterNonEmpty(vals)
		if len(clean) == 0 {
			continue
		}
		dto.Filters[k] = append([]string{}, clean...)
	}
	if len(dto.Filters) == 0 {
		dto.Filters = nil
	}

	return dto, nil
}
