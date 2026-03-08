package arc

import (
	"encoding"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type fieldSource int

const (
	sourcePath fieldSource = iota + 1
	sourceQuery
	sourceHeader
	sourceCookie
	sourceForm
)

type bindField struct {
	index  []int
	source fieldSource
	name   string
}

type bindPlan struct {
	fields []bindField
	body   bool
	form   bool
	query  bool
	cookie bool
}

func compileBindPlan[T any]() bindPlan {
	var zero T
	t := reflect.TypeOf(zero)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	plan := bindPlan{}
	if t == nil || t.Kind() != reflect.Struct {
		return plan
	}
	meta := resolveArcTypeMeta(t)
	if meta == nil {
		return plan
	}
	for _, f := range meta.Fields {
		if name := f.PathName; name != "" {
			plan.fields = append(plan.fields, bindField{index: f.Index, source: sourcePath, name: name})
			continue
		}
		if name := f.QueryName; name != "" {
			plan.fields = append(plan.fields, bindField{index: f.Index, source: sourceQuery, name: name})
			plan.query = true
			continue
		}
		if name := f.HeaderName; name != "" {
			plan.fields = append(plan.fields, bindField{index: f.Index, source: sourceHeader, name: http.CanonicalHeaderKey(name)})
			continue
		}
		if name := f.CookieName; name != "" {
			plan.fields = append(plan.fields, bindField{index: f.Index, source: sourceCookie, name: name})
			plan.cookie = true
			continue
		}
		if name := f.FormName; name != "" {
			plan.fields = append(plan.fields, bindField{index: f.Index, source: sourceForm, name: name})
			plan.body = true
			plan.form = true
			continue
		}
		if f.JSONName != "" {
			plan.body = true
		}
	}
	return plan
}

func bindInto(rc *RequestContext, in any, plan bindPlan) error {
	if plan.body && hasJSONBody(rc.Request) {
		if err := bindBody(rc.Request, in); err != nil {
			return err
		}
	}
	v := reflect.ValueOf(in)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return nil
	}
	sv := v.Elem()
	rt, err := newBindRuntime(rc, plan)
	if err != nil {
		return err
	}
	for _, f := range plan.fields {
		target := sv.FieldByIndex(f.index)
		if !target.CanSet() {
			continue
		}
		if bindMultipartFiles(target, rt.files[f.name]) {
			continue
		}
		vals, ok := sourceValues(rt, f)
		if !ok || len(vals) == 0 {
			continue
		}
		if err := assignStrings(target, vals); err != nil {
			return fmt.Errorf("bind %s: %w", f.name, err)
		}
	}
	return nil
}

type bindRuntime struct {
	rc      *RequestContext
	query   url.Values
	form    url.Values
	files   map[string][]*multipart.FileHeader
	cookies map[string]string
}

func newBindRuntime(rc *RequestContext, plan bindPlan) (*bindRuntime, error) {
	if rc == nil || rc.Request == nil {
		return &bindRuntime{rc: rc}, nil
	}
	rt := &bindRuntime{rc: rc}
	if plan.query {
		rt.query = rc.Request.URL.Query()
	}
	if plan.form {
		if err := parseFormRequest(rc.Request); err != nil {
			return nil, err
		}
		rt.form = rc.Request.Form
		if rc.Request.MultipartForm != nil && len(rc.Request.MultipartForm.File) > 0 {
			rt.files = rc.Request.MultipartForm.File
		}
	}
	if plan.cookie {
		cookies := rc.Request.Cookies()
		if len(cookies) > 0 {
			rt.cookies = make(map[string]string, len(cookies))
		}
		for _, c := range cookies {
			if c != nil && c.Name != "" {
				rt.cookies[c.Name] = c.Value
			}
		}
	}
	return rt, nil
}

func sourceValues(rt *bindRuntime, f bindField) ([]string, bool) {
	if rt == nil || rt.rc == nil || rt.rc.Request == nil {
		return nil, false
	}
	switch f.source {
	case sourcePath:
		v, ok := rt.rc.Params[f.name]
		if !ok || v == "" {
			return nil, false
		}
		return []string{v}, true
	case sourceQuery:
		vals := rt.query[f.name]
		vals = filterNonEmpty(vals)
		return vals, len(vals) > 0
	case sourceHeader:
		vals := rt.rc.Request.Header.Values(f.name)
		vals = filterNonEmpty(vals)
		return vals, len(vals) > 0
	case sourceCookie:
		v := rt.cookies[f.name]
		if v == "" {
			return nil, false
		}
		return []string{v}, true
	case sourceForm:
		vals := rt.form[f.name]
		vals = filterNonEmpty(vals)
		return vals, len(vals) > 0
	default:
		return nil, false
	}
}

func bindMultipartFiles(target reflect.Value, files []*multipart.FileHeader) bool {
	if len(files) == 0 {
		return false
	}
	filePtrType := reflect.TypeOf((*multipart.FileHeader)(nil))
	fileType := reflect.TypeOf(multipart.FileHeader{})
	switch target.Kind() {
	case reflect.Pointer:
		if target.Type() == filePtrType {
			target.Set(reflect.ValueOf(files[0]))
			return true
		}
	case reflect.Struct:
		if target.Type() == fileType && files[0] != nil {
			target.Set(reflect.ValueOf(*files[0]))
			return true
		}
	case reflect.Slice:
		elem := target.Type().Elem()
		if elem == filePtrType {
			out := make([]*multipart.FileHeader, 0, len(files))
			out = append(out, files...)
			target.Set(reflect.ValueOf(out))
			return true
		}
		if elem == fileType {
			out := make([]multipart.FileHeader, 0, len(files))
			for _, f := range files {
				if f != nil {
					out = append(out, *f)
				}
			}
			target.Set(reflect.ValueOf(out))
			return true
		}
	}
	return false
}

func bindBody(r *http.Request, in any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(in); err != nil {
		if err.Error() == "EOF" {
			return nil
		}
		return err
	}
	return nil
}

func hasJSONBody(r *http.Request) bool {
	if r == nil || r.Body == nil {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		return false
	}
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if ct == "" {
		return true
	}
	return strings.HasPrefix(ct, "application/json")
}

func parseFormRequest(r *http.Request) error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		return r.ParseMultipartForm(32 << 20)
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"), ct == "":
		return r.ParseForm()
	default:
		return nil
	}
}

func assignStrings(v reflect.Value, values []string) error {
	if len(values) == 0 {
		return nil
	}
	if v.Kind() == reflect.Slice {
		elemType := v.Type().Elem()
		slice := reflect.MakeSlice(v.Type(), 0, len(values))
		for _, s := range values {
			elem := reflect.New(elemType).Elem()
			if err := assignString(elem, s); err != nil {
				return err
			}
			slice = reflect.Append(slice, elem)
		}
		v.Set(slice)
		return nil
	}
	return assignString(v, values[0])
}

func assignString(v reflect.Value, s string) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return assignStringValue(v.Addr().Interface(), s)
}

func assignStringValue(dst any, s string) error {
	if tu, ok := dst.(encoding.TextUnmarshaler); ok {
		return tu.UnmarshalText([]byte(s))
	}
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("destination must be pointer")
	}
	v = v.Elem()
	if v.Type() == reflect.TypeOf(time.Time{}) {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(t))
		return nil
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("unsupported type %s", v.Type())
	}
	return nil
}

func filterNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
