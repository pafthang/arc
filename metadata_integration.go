package arc

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/pafthang/orm"
	"github.com/pafthang/orm/typemeta"
)

type arcFieldMeta struct {
	GoName         string
	JSONName       string
	Type           reflect.Type
	Index          []int
	IsNullable     bool
	PathName       string
	QueryName      string
	HeaderName     string
	CookieName     string
	FormName       string
	ValidationExpr string
	Validation     *fieldRule
}

type arcTypeMeta struct {
	Type     reflect.Type
	Fields   []arcFieldMeta
	ByGoName map[string]arcFieldMeta
}

var arcTypeMetaCache sync.Map // map[reflect.Type]*arcTypeMeta

const (
	arcAttrPath     = "arc.path"
	arcAttrQuery    = "arc.query"
	arcAttrHeader   = "arc.header"
	arcAttrCookie   = "arc.cookie"
	arcAttrForm     = "arc.form"
	arcAttrValidate = "arc.validate"
)

// EnableLegacyTagSync controls one-time migration of legacy struct tags into typemeta.Attributes.
// Keeping it enabled preserves backward compatibility while bind/validate rely on typemeta only.
var EnableLegacyTagSync = true

func resolveTypeMeta(t reflect.Type) *typemeta.TypeMeta {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	instance := reflect.New(t).Elem().Interface()
	meta, err := orm.SharedTypeRegistry.Resolve(instance)
	if err != nil {
		return nil
	}
	return meta
}

func resolveArcTypeMeta(t reflect.Type) *arcTypeMeta {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	if cached, ok := arcTypeMetaCache.Load(t); ok {
		if meta, ok := cached.(*arcTypeMeta); ok {
			return meta
		}
	}
	typeMeta := resolveTypeMeta(t)
	if typeMeta == nil {
		return nil
	}
	if EnableLegacyTagSync {
		syncLegacyTagsToTypeMeta(t, typeMeta)
	}
	out := &arcTypeMeta{
		Type:     t,
		Fields:   make([]arcFieldMeta, 0, len(typeMeta.Fields)),
		ByGoName: map[string]arcFieldMeta{},
	}
	for _, f := range typeMeta.Fields {
		if f == nil || len(f.Index) == 0 {
			continue
		}
		attrs := f.Attributes
		if attrs == nil {
			attrs = map[string]string{}
		}
		field := arcFieldMeta{
			GoName:         f.GoName,
			JSONName:       f.JSONName,
			Type:           f.Type,
			Index:          append([]int{}, f.Index...),
			IsNullable:     f.IsNullable,
			PathName:       strings.TrimSpace(attrs[arcAttrPath]),
			QueryName:      strings.TrimSpace(attrs[arcAttrQuery]),
			HeaderName:     strings.TrimSpace(attrs[arcAttrHeader]),
			CookieName:     strings.TrimSpace(attrs[arcAttrCookie]),
			FormName:       strings.TrimSpace(attrs[arcAttrForm]),
			ValidationExpr: strings.TrimSpace(attrs[arcAttrValidate]),
		}
		if field.ValidationExpr != "" {
			field.Validation = parseValidationTag(field.ValidationExpr)
		}
		out.Fields = append(out.Fields, field)
		out.ByGoName[field.GoName] = field
		out.ByGoName[field.GoName] = field
	}
	arcTypeMetaCache.Store(t, out)
	return out
}

func syncLegacyTagsToTypeMeta(t reflect.Type, meta *typemeta.TypeMeta) {
	if t == nil || meta == nil {
		return
	}
	for _, f := range meta.Fields {
		if f == nil || len(f.Index) == 0 {
			continue
		}
		sf := t.FieldByIndex(f.Index)
		if f.Attributes == nil {
			f.Attributes = map[string]string{}
		}
		backfillAttr(f.Attributes, arcAttrPath, sf.Tag.Get("path"))
		backfillAttr(f.Attributes, arcAttrQuery, sf.Tag.Get("query"))
		backfillAttr(f.Attributes, arcAttrHeader, sf.Tag.Get("header"))
		backfillAttr(f.Attributes, arcAttrCookie, sf.Tag.Get("cookie"))
		backfillAttr(f.Attributes, arcAttrForm, sf.Tag.Get("form"))
		backfillAttr(f.Attributes, arcAttrValidate, sf.Tag.Get("validate"))
	}
}

func backfillAttr(attrs map[string]string, key, val string) {
	if attrs == nil {
		return
	}
	if strings.TrimSpace(attrs[key]) != "" {
		return
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	attrs[key] = val
}

// FieldMetadataConfig configures arc-specific metadata for one field in shared typemeta.
type FieldMetadataConfig struct {
	Path     string
	Query    string
	Header   string
	Cookie   string
	Form     string
	Validate string
}

// ConfigureFieldMetadata writes arc binding/validation metadata into orm.SharedTypeRegistry attributes.
func ConfigureFieldMetadata(model any, fieldGoName string, cfg FieldMetadataConfig) error {
	t := reflect.TypeOf(model)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return fmt.Errorf("model must be struct or pointer to struct")
	}
	meta := resolveTypeMeta(t)
	if meta == nil {
		return fmt.Errorf("failed to resolve shared typemeta")
	}
	f := meta.FieldsByGo[fieldGoName]
	if f == nil {
		return fmt.Errorf("field %s not found in type %s", fieldGoName, t.Name())
	}
	if f.Attributes == nil {
		f.Attributes = map[string]string{}
	}
	setAttr := func(key, v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			delete(f.Attributes, key)
			return
		}
		f.Attributes[key] = v
	}
	setAttr(arcAttrPath, cfg.Path)
	setAttr(arcAttrQuery, cfg.Query)
	setAttr(arcAttrHeader, cfg.Header)
	setAttr(arcAttrCookie, cfg.Cookie)
	setAttr(arcAttrForm, cfg.Form)
	setAttr(arcAttrValidate, cfg.Validate)
	arcTypeMetaCache.Delete(t)
	return nil
}

func resolveArcFieldByIndex(meta *arcTypeMeta) map[string]arcFieldMeta {
	if meta == nil {
		return nil
	}
	out := make(map[string]arcFieldMeta, len(meta.Fields))
	for _, f := range meta.Fields {
		if len(f.Index) == 0 {
			continue
		}
		out[indexKey(f.Index)] = f
	}
	return out
}
