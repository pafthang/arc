package arc

import (
	"fmt"
	"reflect"
	"regexp"
	"time"
	"unsafe"
)

type fastValueKind uint8

const (
	fastString fastValueKind = iota + 1
	fastInt
	fastInt8
	fastInt16
	fastInt32
	fastInt64
	fastUint
	fastUint8
	fastUint16
	fastUint32
	fastUint64
	fastFloat32
	fastFloat64
	fastBool
	fastTime
)

type fastValidationRule struct {
	name       string
	kind       fastValueKind
	offset     uintptr
	required   bool
	min        *float64
	max        *float64
	minLength  *int
	maxLength  *int
	enum       map[string]struct{}
	regexpExpr *regexp.Regexp
	format     string

	eqOffset  uintptr
	neOffset  uintptr
	gteOffset uintptr
	lteOffset uintptr
	hasEq     bool
	hasNe     bool
	hasGte    bool
	hasLte    bool
}

type fastValidator[T any] struct {
	rules []fastValidationRule
}

func compileFastValidator[T any](compiled compiledValidator) (*fastValidator[T], bool) {
	root := inputStructType[T]()
	if root == nil || root.Kind() != reflect.Struct {
		return nil, false
	}
	fv := &fastValidator[T]{rules: make([]fastValidationRule, 0, len(compiled.rules))}
	for _, r := range compiled.rules {
		offset, typ, ok := compileFieldOffset(root, r.index)
		if !ok {
			return nil, false
		}
		kind, ok := fastKindFromType(typ)
		if !ok {
			return nil, false
		}
		fr := fastValidationRule{
			name:       r.name,
			kind:       kind,
			offset:     offset,
			required:   r.required,
			min:        r.min,
			max:        r.max,
			minLength:  r.minLength,
			maxLength:  r.maxLength,
			enum:       cloneEnum(r.enum),
			regexpExpr: r.regexpExpr,
			format:     r.format,
		}
		if len(r.eqIndex) > 0 {
			off, t2, ok := compileFieldOffset(root, r.eqIndex)
			if !ok || t2 != typ {
				return nil, false
			}
			fr.eqOffset, fr.hasEq = off, true
		}
		if len(r.neIndex) > 0 {
			off, t2, ok := compileFieldOffset(root, r.neIndex)
			if !ok || t2 != typ {
				return nil, false
			}
			fr.neOffset, fr.hasNe = off, true
		}
		if len(r.gteIndex) > 0 {
			off, t2, ok := compileFieldOffset(root, r.gteIndex)
			if !ok || t2 != typ {
				return nil, false
			}
			fr.gteOffset, fr.hasGte = off, true
		}
		if len(r.lteIndex) > 0 {
			off, t2, ok := compileFieldOffset(root, r.lteIndex)
			if !ok || t2 != typ {
				return nil, false
			}
			fr.lteOffset, fr.hasLte = off, true
		}
		fv.rules = append(fv.rules, fr)
	}
	return fv, true
}

func (v *fastValidator[T]) Validate(in *T, extras ...StructValidatorFunc) error {
	if in == nil {
		return fmt.Errorf("input is nil")
	}
	base := unsafe.Pointer(in)
	verr := &ValidationErrors{}
	for _, r := range v.rules {
		v.applyRule(verr, base, r)
	}
	for _, fn := range extras {
		if fn == nil {
			continue
		}
		if err := fn(in); err != nil {
			verr.add("", "custom", err.Error())
		}
	}
	if verr.hasAny() {
		return verr
	}
	return nil
}

func (v *fastValidator[T]) applyRule(verr *ValidationErrors, base unsafe.Pointer, r fastValidationRule) {
	switch r.kind {
	case fastString:
		cur := *(*string)(unsafe.Add(base, r.offset))
		if r.required && cur == "" {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == "" {
			return
		}
		if r.minLength != nil && len(cur) < *r.minLength {
			verr.add(r.name, "minLength", fmt.Sprintf("%s length must be >= %d", r.name, *r.minLength))
		}
		if r.maxLength != nil && len(cur) > *r.maxLength {
			verr.add(r.name, "maxLength", fmt.Sprintf("%s length must be <= %d", r.name, *r.maxLength))
		}
		if r.regexpExpr != nil && !r.regexpExpr.MatchString(cur) {
			verr.add(r.name, "regex", fmt.Sprintf("%s format is invalid", r.name))
		}
		if r.format != "" && !matchFormat(cur, r.format) {
			verr.add(r.name, "format", fmt.Sprintf("%s must match format %s", r.name, r.format))
		}
		if len(r.enum) > 0 {
			if _, ok := r.enum[cur]; !ok {
				verr.add(r.name, "enum", fmt.Sprintf("%s must be one of enum values", r.name))
			}
		}
		v.applyCrossString(verr, base, cur, r)
	case fastInt:
		cur := *(*int)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastInt8:
		cur := *(*int8)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastInt16:
		cur := *(*int16)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastInt32:
		cur := *(*int32)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastInt64:
		cur := *(*int64)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastUint:
		cur := *(*uint)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastUint8:
		cur := *(*uint8)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastUint16:
		cur := *(*uint16)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastUint32:
		cur := *(*uint32)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastUint64:
		cur := *(*uint64)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastFloat32:
		cur := *(*float32)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, float64(cur))
		v.applyCrossNum(verr, r, float64(cur), base)
	case fastFloat64:
		cur := *(*float64)(unsafe.Add(base, r.offset))
		if r.required && cur == 0 {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur == 0 {
			return
		}
		v.applyMinMax(verr, r, cur)
		v.applyCrossNum(verr, r, cur, base)
	case fastBool:
		cur := *(*bool)(unsafe.Add(base, r.offset))
		if r.required && !cur {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
		}
	case fastTime:
		cur := *(*time.Time)(unsafe.Add(base, r.offset))
		if r.required && cur.IsZero() {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if cur.IsZero() {
			return
		}
		v.applyCrossTime(verr, r, cur, base)
	}
}

func (v *fastValidator[T]) applyMinMax(verr *ValidationErrors, r fastValidationRule, n float64) {
	if r.min != nil && n < *r.min {
		verr.add(r.name, "min", fmt.Sprintf("%s must be >= %v", r.name, *r.min))
	}
	if r.max != nil && n > *r.max {
		verr.add(r.name, "max", fmt.Sprintf("%s must be <= %v", r.name, *r.max))
	}
}

func (v *fastValidator[T]) applyCrossString(verr *ValidationErrors, base unsafe.Pointer, cur string, r fastValidationRule) {
	if r.hasEq {
		other := *(*string)(unsafe.Add(base, r.eqOffset))
		if other != "" && cur != other {
			verr.add(r.name, "eqField", fmt.Sprintf("%s comparison with %s failed", r.name, "eqField"))
		}
	}
	if r.hasNe {
		other := *(*string)(unsafe.Add(base, r.neOffset))
		if other != "" && cur == other {
			verr.add(r.name, "neField", fmt.Sprintf("%s comparison with %s failed", r.name, "neField"))
		}
	}
	if r.hasGte {
		other := *(*string)(unsafe.Add(base, r.gteOffset))
		if other != "" && cur < other {
			verr.add(r.name, "gteField", fmt.Sprintf("%s comparison with %s failed", r.name, "gteField"))
		}
	}
	if r.hasLte {
		other := *(*string)(unsafe.Add(base, r.lteOffset))
		if other != "" && cur > other {
			verr.add(r.name, "lteField", fmt.Sprintf("%s comparison with %s failed", r.name, "lteField"))
		}
	}
}

func (v *fastValidator[T]) applyCrossNum(verr *ValidationErrors, r fastValidationRule, cur float64, base unsafe.Pointer) {
	if r.hasEq && cur != numericAt(base, r.eqOffset, r.kind) {
		verr.add(r.name, "eqField", fmt.Sprintf("%s comparison with %s failed", r.name, "eqField"))
	}
	if r.hasNe && cur == numericAt(base, r.neOffset, r.kind) {
		verr.add(r.name, "neField", fmt.Sprintf("%s comparison with %s failed", r.name, "neField"))
	}
	if r.hasGte && cur < numericAt(base, r.gteOffset, r.kind) {
		verr.add(r.name, "gteField", fmt.Sprintf("%s comparison with %s failed", r.name, "gteField"))
	}
	if r.hasLte && cur > numericAt(base, r.lteOffset, r.kind) {
		verr.add(r.name, "lteField", fmt.Sprintf("%s comparison with %s failed", r.name, "lteField"))
	}
}

func (v *fastValidator[T]) applyCrossTime(verr *ValidationErrors, r fastValidationRule, cur time.Time, base unsafe.Pointer) {
	if r.hasEq {
		other := *(*time.Time)(unsafe.Add(base, r.eqOffset))
		if !other.IsZero() && !cur.Equal(other) {
			verr.add(r.name, "eqField", fmt.Sprintf("%s comparison with %s failed", r.name, "eqField"))
		}
	}
	if r.hasNe {
		other := *(*time.Time)(unsafe.Add(base, r.neOffset))
		if !other.IsZero() && cur.Equal(other) {
			verr.add(r.name, "neField", fmt.Sprintf("%s comparison with %s failed", r.name, "neField"))
		}
	}
	if r.hasGte {
		other := *(*time.Time)(unsafe.Add(base, r.gteOffset))
		if !other.IsZero() && cur.Before(other) {
			verr.add(r.name, "gteField", fmt.Sprintf("%s comparison with %s failed", r.name, "gteField"))
		}
	}
	if r.hasLte {
		other := *(*time.Time)(unsafe.Add(base, r.lteOffset))
		if !other.IsZero() && cur.After(other) {
			verr.add(r.name, "lteField", fmt.Sprintf("%s comparison with %s failed", r.name, "lteField"))
		}
	}
}

func numericAt(base unsafe.Pointer, offset uintptr, kind fastValueKind) float64 {
	switch kind {
	case fastInt:
		return float64(*(*int)(unsafe.Add(base, offset)))
	case fastInt8:
		return float64(*(*int8)(unsafe.Add(base, offset)))
	case fastInt16:
		return float64(*(*int16)(unsafe.Add(base, offset)))
	case fastInt32:
		return float64(*(*int32)(unsafe.Add(base, offset)))
	case fastInt64:
		return float64(*(*int64)(unsafe.Add(base, offset)))
	case fastUint:
		return float64(*(*uint)(unsafe.Add(base, offset)))
	case fastUint8:
		return float64(*(*uint8)(unsafe.Add(base, offset)))
	case fastUint16:
		return float64(*(*uint16)(unsafe.Add(base, offset)))
	case fastUint32:
		return float64(*(*uint32)(unsafe.Add(base, offset)))
	case fastUint64:
		return float64(*(*uint64)(unsafe.Add(base, offset)))
	case fastFloat32:
		return float64(*(*float32)(unsafe.Add(base, offset)))
	case fastFloat64:
		return *(*float64)(unsafe.Add(base, offset))
	default:
		return 0
	}
}

func fastKindFromType(t reflect.Type) (fastValueKind, bool) {
	if t == nil {
		return 0, false
	}
	for t.Kind() == reflect.Pointer {
		return 0, false
	}
	switch t.Kind() {
	case reflect.String:
		return fastString, true
	case reflect.Int:
		return fastInt, true
	case reflect.Int8:
		return fastInt8, true
	case reflect.Int16:
		return fastInt16, true
	case reflect.Int32:
		return fastInt32, true
	case reflect.Int64:
		return fastInt64, true
	case reflect.Uint:
		return fastUint, true
	case reflect.Uint8:
		return fastUint8, true
	case reflect.Uint16:
		return fastUint16, true
	case reflect.Uint32:
		return fastUint32, true
	case reflect.Uint64:
		return fastUint64, true
	case reflect.Float32:
		return fastFloat32, true
	case reflect.Float64:
		return fastFloat64, true
	case reflect.Bool:
		return fastBool, true
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return fastTime, true
		}
	}
	return 0, false
}

func cloneEnum(in map[string]struct{}) map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
