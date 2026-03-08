package arc

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	emailFormatRE = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	uuidFormatRE  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
)

type compiledValidator struct {
	rules   []fieldRule
	byIndex map[string]fieldRule
}

type fieldRule struct {
	index      []int
	name       string
	required   bool
	min        *float64
	max        *float64
	minLength  *int
	maxLength  *int
	enum       map[string]struct{}
	regexpExpr *regexp.Regexp
	format     string
	eqField    string
	neField    string
	gteField   string
	lteField   string
	eqIndex    []int
	neIndex    []int
	gteIndex   []int
	lteIndex   []int
}

type customValidator interface {
	Validate() error
}

// ValidationErrors collects multiple input violations.
type ValidationErrors struct {
	Details []ErrorDetail
}

func (e *ValidationErrors) Error() string {
	if e == nil || len(e.Details) == 0 {
		return "validation failed"
	}
	return e.Details[0].Message
}

func (e *ValidationErrors) add(path, code, msg string) {
	e.Details = append(e.Details, ErrorDetail{Path: path, Code: code, Message: msg})
}

func (e *ValidationErrors) hasAny() bool { return e != nil && len(e.Details) > 0 }

func compileValidator[T any]() compiledValidator {
	var zero T
	t := reflect.TypeOf(zero)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	cv := compiledValidator{byIndex: map[string]fieldRule{}}
	if t == nil || t.Kind() != reflect.Struct {
		return cv
	}
	meta := resolveArcTypeMeta(t)
	if meta == nil {
		return cv
	}
	for _, f := range meta.Fields {
		rule := cloneFieldRule(f.Validation)
		if rule == nil {
			continue
		}
		rule.index = append([]int{}, f.Index...)
		rule.name = f.JSONName
		if rule.name == "" {
			rule.name = f.GoName
		}
		if other, ok := meta.ByGoName[rule.eqField]; ok {
			rule.eqIndex = append([]int{}, other.Index...)
		}
		if other, ok := meta.ByGoName[rule.neField]; ok {
			rule.neIndex = append([]int{}, other.Index...)
		}
		if other, ok := meta.ByGoName[rule.gteField]; ok {
			rule.gteIndex = append([]int{}, other.Index...)
		}
		if other, ok := meta.ByGoName[rule.lteField]; ok {
			rule.lteIndex = append([]int{}, other.Index...)
		}
		cv.rules = append(cv.rules, *rule)
		cv.byIndex[indexKey(f.Index)] = *rule
	}
	return cv
}

func cloneFieldRule(in *fieldRule) *fieldRule {
	if in == nil {
		return nil
	}
	r := *in
	if len(in.enum) > 0 {
		r.enum = make(map[string]struct{}, len(in.enum))
		for k, v := range in.enum {
			r.enum[k] = v
		}
	}
	if len(in.index) > 0 {
		r.index = append([]int{}, in.index...)
	}
	if len(in.eqIndex) > 0 {
		r.eqIndex = append([]int{}, in.eqIndex...)
	}
	if len(in.neIndex) > 0 {
		r.neIndex = append([]int{}, in.neIndex...)
	}
	if len(in.gteIndex) > 0 {
		r.gteIndex = append([]int{}, in.gteIndex...)
	}
	if len(in.lteIndex) > 0 {
		r.lteIndex = append([]int{}, in.lteIndex...)
	}
	return &r
}

func parseValidationTag(tag string) *fieldRule {
	if strings.TrimSpace(tag) == "" {
		return nil
	}
	r := &fieldRule{}
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == "required" {
			r.required = true
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, val := strings.ToLower(kv[0]), kv[1]
		switch key {
		case "min":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				r.min = &f
			}
		case "max":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				r.max = &f
			}
		case "minlength":
			if n, err := strconv.Atoi(val); err == nil {
				r.minLength = &n
			}
		case "maxlength":
			if n, err := strconv.Atoi(val); err == nil {
				r.maxLength = &n
			}
		case "enum":
			r.enum = map[string]struct{}{}
			for _, item := range strings.Split(val, "|") {
				item = strings.TrimSpace(item)
				if item != "" {
					r.enum[item] = struct{}{}
				}
			}
		case "regex":
			if re, err := regexp.Compile(val); err == nil {
				r.regexpExpr = re
			}
		case "format":
			r.format = strings.ToLower(strings.TrimSpace(val))
		case "eqfield":
			r.eqField = val
		case "nefield":
			r.neField = val
		case "gtefield":
			r.gteField = val
		case "ltefield":
			r.lteField = val
		}
	}
	return r
}

func (v compiledValidator) ruleForIndex(index []int) (fieldRule, bool) {
	r, ok := v.byIndex[indexKey(index)]
	return r, ok
}

func (v compiledValidator) Validate(in any, extras ...StructValidatorFunc) error {
	if cv, ok := in.(customValidator); ok {
		if err := cv.Validate(); err != nil {
			return err
		}
	}
	rv := reflect.ValueOf(in)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return fmt.Errorf("input is nil")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	verr := &ValidationErrors{}
	for _, rule := range v.rules {
		fv := rv.FieldByIndex(rule.index)
		applyRule(verr, rv, fv, rule)
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

func applyRule(verr *ValidationErrors, root reflect.Value, v reflect.Value, r fieldRule) {
	if o, ok := optionalStateFromValue(v); ok {
		if r.required && (!o.OptionalIsSet() || o.OptionalIsNull()) {
			verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
			return
		}
		if !o.OptionalIsSet() || o.OptionalIsNull() {
			return
		}
		val := reflect.ValueOf(o.OptionalValueAny())
		if !val.IsValid() {
			return
		}
		applyValueRule(verr, root, val, r)
		return
	}
	if r.required && isZero(v) {
		verr.add(r.name, "required", fmt.Sprintf("%s is required", r.name))
		return
	}
	if isZero(v) {
		return
	}
	applyValueRule(verr, root, deref(v), r)
}

func applyValueRule(verr *ValidationErrors, root reflect.Value, val reflect.Value, r fieldRule) {
	switch val.Kind() {
	case reflect.String:
		s := val.String()
		if r.minLength != nil && len(s) < *r.minLength {
			verr.add(r.name, "minLength", fmt.Sprintf("%s length must be >= %d", r.name, *r.minLength))
		}
		if r.maxLength != nil && len(s) > *r.maxLength {
			verr.add(r.name, "maxLength", fmt.Sprintf("%s length must be <= %d", r.name, *r.maxLength))
		}
		if r.regexpExpr != nil && !r.regexpExpr.MatchString(s) {
			verr.add(r.name, "regex", fmt.Sprintf("%s format is invalid", r.name))
		}
		if r.format != "" && !matchFormat(s, r.format) {
			verr.add(r.name, "format", fmt.Sprintf("%s must match format %s", r.name, r.format))
		}
		if len(r.enum) > 0 {
			if _, ok := r.enum[s]; !ok {
				verr.add(r.name, "enum", fmt.Sprintf("%s must be one of enum values", r.name))
			}
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := float64(val.Int())
		if r.min != nil && n < *r.min {
			verr.add(r.name, "min", fmt.Sprintf("%s must be >= %v", r.name, *r.min))
		}
		if r.max != nil && n > *r.max {
			verr.add(r.name, "max", fmt.Sprintf("%s must be <= %v", r.name, *r.max))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := float64(val.Uint())
		if r.min != nil && n < *r.min {
			verr.add(r.name, "min", fmt.Sprintf("%s must be >= %v", r.name, *r.min))
		}
		if r.max != nil && n > *r.max {
			verr.add(r.name, "max", fmt.Sprintf("%s must be <= %v", r.name, *r.max))
		}
	case reflect.Float32, reflect.Float64:
		n := val.Float()
		if r.min != nil && n < *r.min {
			verr.add(r.name, "min", fmt.Sprintf("%s must be >= %v", r.name, *r.min))
		}
		if r.max != nil && n > *r.max {
			verr.add(r.name, "max", fmt.Sprintf("%s must be <= %v", r.name, *r.max))
		}
	}
	if len(r.eqIndex) > 0 {
		checkFieldCmp(verr, root, val, r, r.eqIndex, r.eqField, func(c int) bool { return c == 0 }, "eqField")
	}
	if len(r.neIndex) > 0 {
		checkFieldCmp(verr, root, val, r, r.neIndex, r.neField, func(c int) bool { return c != 0 }, "neField")
	}
	if len(r.gteIndex) > 0 {
		checkFieldCmp(verr, root, val, r, r.gteIndex, r.gteField, func(c int) bool { return c >= 0 }, "gteField")
	}
	if len(r.lteIndex) > 0 {
		checkFieldCmp(verr, root, val, r, r.lteIndex, r.lteField, func(c int) bool { return c <= 0 }, "lteField")
	}
}

func optionalStateFromValue(v reflect.Value) (optionalState, bool) {
	for v.IsValid() && v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil, false
	}
	if st, ok := v.Interface().(optionalState); ok {
		return st, true
	}
	if v.CanAddr() {
		if st, ok := v.Addr().Interface().(optionalState); ok {
			return st, true
		}
	}
	return nil, false
}

func checkFieldCmp(verr *ValidationErrors, root reflect.Value, val reflect.Value, rule fieldRule, otherIndex []int, otherName string, okFn func(int) bool, code string) {
	other := root.FieldByIndex(otherIndex)
	if !other.IsValid() || isZero(other) {
		return
	}
	cmp, ok := compareValues(val, deref(other))
	if !ok {
		return
	}
	if !okFn(cmp) {
		verr.add(rule.name, code, fmt.Sprintf("%s comparison with %s failed", rule.name, otherName))
	}
}

func compareValues(a, b reflect.Value) (int, bool) {
	a = deref(a)
	b = deref(b)
	if !a.IsValid() || !b.IsValid() {
		return 0, false
	}
	if a.Type() == reflect.TypeOf(time.Time{}) && b.Type() == reflect.TypeOf(time.Time{}) {
		at := a.Interface().(time.Time)
		bt := b.Interface().(time.Time)
		switch {
		case at.Before(bt):
			return -1, true
		case at.After(bt):
			return 1, true
		default:
			return 0, true
		}
	}
	switch a.Kind() {
	case reflect.String:
		bs, ok := anyAsString(b)
		if !ok {
			return 0, false
		}
		as := a.String()
		switch {
		case as < bs:
			return -1, true
		case as > bs:
			return 1, true
		default:
			return 0, true
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bn, ok := anyAsFloat(b)
		if !ok {
			return 0, false
		}
		an := float64(a.Int())
		return numCmp(an, bn), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		bn, ok := anyAsFloat(b)
		if !ok {
			return 0, false
		}
		an := float64(a.Uint())
		return numCmp(an, bn), true
	case reflect.Float32, reflect.Float64:
		bn, ok := anyAsFloat(b)
		if !ok {
			return 0, false
		}
		return numCmp(a.Float(), bn), true
	default:
		if reflect.DeepEqual(a.Interface(), b.Interface()) {
			return 0, true
		}
		return 0, false
	}
}

func anyAsString(v reflect.Value) (string, bool) {
	v = deref(v)
	if !v.IsValid() || v.Kind() != reflect.String {
		return "", false
	}
	return v.String(), true
}

func anyAsFloat(v reflect.Value) (float64, bool) {
	v = deref(v)
	if !v.IsValid() {
		return 0, false
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	default:
		return 0, false
	}
}

func numCmp(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func matchFormat(v, format string) bool {
	switch format {
	case "email":
		return emailFormatRE.MatchString(v)
	case "uuid":
		return uuidFormatRE.MatchString(v)
	case "date-time":
		_, err := time.Parse(time.RFC3339, v)
		return err == nil
	default:
		return true
	}
}

func isZero(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}
	if v.Kind() == reflect.Pointer {
		return v.IsNil()
	}
	return v.IsZero()
}

func deref(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return v
		}
		v = v.Elem()
	}
	return v
}

func indexKey(index []int) string {
	if len(index) == 0 {
		return ""
	}
	parts := make([]string, len(index))
	for i, n := range index {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// StructValidatorFunc validates one input struct instance.
type StructValidatorFunc func(any) error

type validatorRegistry struct {
	mu    sync.RWMutex
	items map[reflect.Type][]StructValidatorFunc
}

func newValidatorRegistry() *validatorRegistry {
	return &validatorRegistry{items: map[reflect.Type][]StructValidatorFunc{}}
}

func (r *validatorRegistry) add(t reflect.Type, fn StructValidatorFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[t] = append(r.items[t], fn)
}

func (r *validatorRegistry) forType(t reflect.Type) []StructValidatorFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]StructValidatorFunc{}, r.items[t]...)
	return out
}
