package arc

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"time"
	"unsafe"
)

type fastBindField struct {
	source fieldSource
	name   string
	assign func(base unsafe.Pointer, values []string) error
}

type fastBinder[T any] struct {
	fields []fastBindField
	plan   bindPlan
}

func compileFastBinder[T any](plan bindPlan) (*fastBinder[T], bool) {
	root := inputStructType[T]()
	if root == nil || root.Kind() != reflect.Struct {
		return nil, false
	}
	fb := &fastBinder[T]{plan: plan, fields: make([]fastBindField, 0, len(plan.fields))}
	for _, f := range plan.fields {
		offset, typ, ok := compileFieldOffset(root, f.index)
		if !ok {
			return nil, false
		}
		if isMultipartFileType(typ) {
			return nil, false
		}
		assign, ok := makeFastAssign(typ, offset)
		if !ok {
			return nil, false
		}
		name := f.name
		if f.source == sourceHeader {
			name = http.CanonicalHeaderKey(name)
		}
		fb.fields = append(fb.fields, fastBindField{
			source: f.source,
			name:   name,
			assign: assign,
		})
	}
	return fb, true
}

func isMultipartFileType(t reflect.Type) bool {
	fileType := reflect.TypeOf(multipart.FileHeader{})
	filePtrType := reflect.TypeOf((*multipart.FileHeader)(nil))
	switch {
	case t == fileType, t == filePtrType:
		return true
	case t.Kind() == reflect.Slice:
		elem := t.Elem()
		return elem == fileType || elem == filePtrType
	default:
		return false
	}
}

func (b *fastBinder[T]) Bind(rc *RequestContext, in *T) error {
	if b == nil || in == nil {
		return nil
	}
	if b.plan.body && hasJSONBody(rc.Request) {
		if err := bindBody(rc.Request, in); err != nil {
			return err
		}
	}
	rt, err := newBindRuntime(rc, b.plan)
	if err != nil {
		return err
	}
	base := unsafe.Pointer(in)
	for _, f := range b.fields {
		vals, ok := sourceValues(rt, bindField{source: f.source, name: f.name})
		if !ok || len(vals) == 0 {
			continue
		}
		if err := f.assign(base, vals); err != nil {
			return fmt.Errorf("bind %s: %w", f.name, err)
		}
	}
	return nil
}

func compileFieldOffset(root reflect.Type, index []int) (uintptr, reflect.Type, bool) {
	cur := root
	var off uintptr
	for i, idx := range index {
		if cur.Kind() != reflect.Struct || idx < 0 || idx >= cur.NumField() {
			return 0, nil, false
		}
		sf := cur.Field(idx)
		off += sf.Offset
		if i == len(index)-1 {
			return off, sf.Type, true
		}
		if sf.Type.Kind() != reflect.Struct {
			return 0, nil, false
		}
		cur = sf.Type
	}
	return 0, nil, false
}

func makeFastAssign(t reflect.Type, offset uintptr) (func(base unsafe.Pointer, values []string) error, bool) {
	for t.Kind() == reflect.Pointer {
		return nil, false
	}
	switch t.Kind() {
	case reflect.String:
		return func(base unsafe.Pointer, values []string) error {
			*(*string)(unsafe.Add(base, offset)) = values[0]
			return nil
		}, true
	case reflect.Bool:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseBool(values[0])
			if err != nil {
				return err
			}
			*(*bool)(unsafe.Add(base, offset)) = v
			return nil
		}, true
	case reflect.Int:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil {
				return err
			}
			*(*int)(unsafe.Add(base, offset)) = int(v)
			return nil
		}, true
	case reflect.Int8:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseInt(values[0], 10, 8)
			if err != nil {
				return err
			}
			*(*int8)(unsafe.Add(base, offset)) = int8(v)
			return nil
		}, true
	case reflect.Int16:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseInt(values[0], 10, 16)
			if err != nil {
				return err
			}
			*(*int16)(unsafe.Add(base, offset)) = int16(v)
			return nil
		}, true
	case reflect.Int32:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseInt(values[0], 10, 32)
			if err != nil {
				return err
			}
			*(*int32)(unsafe.Add(base, offset)) = int32(v)
			return nil
		}, true
	case reflect.Int64:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil {
				return err
			}
			*(*int64)(unsafe.Add(base, offset)) = v
			return nil
		}, true
	case reflect.Uint:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseUint(values[0], 10, 64)
			if err != nil {
				return err
			}
			*(*uint)(unsafe.Add(base, offset)) = uint(v)
			return nil
		}, true
	case reflect.Uint8:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseUint(values[0], 10, 8)
			if err != nil {
				return err
			}
			*(*uint8)(unsafe.Add(base, offset)) = uint8(v)
			return nil
		}, true
	case reflect.Uint16:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseUint(values[0], 10, 16)
			if err != nil {
				return err
			}
			*(*uint16)(unsafe.Add(base, offset)) = uint16(v)
			return nil
		}, true
	case reflect.Uint32:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseUint(values[0], 10, 32)
			if err != nil {
				return err
			}
			*(*uint32)(unsafe.Add(base, offset)) = uint32(v)
			return nil
		}, true
	case reflect.Uint64:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseUint(values[0], 10, 64)
			if err != nil {
				return err
			}
			*(*uint64)(unsafe.Add(base, offset)) = v
			return nil
		}, true
	case reflect.Float32:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseFloat(values[0], 32)
			if err != nil {
				return err
			}
			*(*float32)(unsafe.Add(base, offset)) = float32(v)
			return nil
		}, true
	case reflect.Float64:
		return func(base unsafe.Pointer, values []string) error {
			v, err := strconv.ParseFloat(values[0], 64)
			if err != nil {
				return err
			}
			*(*float64)(unsafe.Add(base, offset)) = v
			return nil
		}, true
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return func(base unsafe.Pointer, values []string) error {
				v, err := time.Parse(time.RFC3339, values[0])
				if err != nil {
					return err
				}
				*(*time.Time)(unsafe.Add(base, offset)) = v
				return nil
			}, true
		}
	case reflect.Slice:
		elem := t.Elem()
		switch elem.Kind() {
		case reflect.String:
			return func(base unsafe.Pointer, values []string) error {
				out := make([]string, len(values))
				copy(out, values)
				*(*[]string)(unsafe.Add(base, offset)) = out
				return nil
			}, true
		case reflect.Int:
			return func(base unsafe.Pointer, values []string) error {
				out := make([]int, 0, len(values))
				for _, s := range values {
					v, err := strconv.ParseInt(s, 10, 64)
					if err != nil {
						return err
					}
					out = append(out, int(v))
				}
				*(*[]int)(unsafe.Add(base, offset)) = out
				return nil
			}, true
		case reflect.Int32:
			return func(base unsafe.Pointer, values []string) error {
				out := make([]int32, 0, len(values))
				for _, s := range values {
					v, err := strconv.ParseInt(s, 10, 32)
					if err != nil {
						return err
					}
					out = append(out, int32(v))
				}
				*(*[]int32)(unsafe.Add(base, offset)) = out
				return nil
			}, true
		case reflect.Int64:
			return func(base unsafe.Pointer, values []string) error {
				out := make([]int64, 0, len(values))
				for _, s := range values {
					v, err := strconv.ParseInt(s, 10, 64)
					if err != nil {
						return err
					}
					out = append(out, v)
				}
				*(*[]int64)(unsafe.Add(base, offset)) = out
				return nil
			}, true
		case reflect.Uint:
			return func(base unsafe.Pointer, values []string) error {
				out := make([]uint, 0, len(values))
				for _, s := range values {
					v, err := strconv.ParseUint(s, 10, 64)
					if err != nil {
						return err
					}
					out = append(out, uint(v))
				}
				*(*[]uint)(unsafe.Add(base, offset)) = out
				return nil
			}, true
		case reflect.Float64:
			return func(base unsafe.Pointer, values []string) error {
				out := make([]float64, 0, len(values))
				for _, s := range values {
					v, err := strconv.ParseFloat(s, 64)
					if err != nil {
						return err
					}
					out = append(out, v)
				}
				*(*[]float64)(unsafe.Add(base, offset)) = out
				return nil
			}, true
		}
	}
	return nil, false
}
