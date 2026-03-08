package arc

import (
	"encoding/json"
	"fmt"
)

// Optional stores tri-state field state for PATCH DTOs:
// absent, explicit null, explicit value.
type Optional[T any] struct {
	set   bool
	null  bool
	value T
}

// OptionalString is convenience alias for Optional[string].
type OptionalString = Optional[string]

// OptionalInt is convenience alias for Optional[int].
type OptionalInt = Optional[int]

// OptionalBool is convenience alias for Optional[bool].
type OptionalBool = Optional[bool]

// Some creates optional with explicit value.
func Some[T any](v T) Optional[T] {
	return Optional[T]{set: true, value: v}
}

// Null creates optional with explicit null.
func Null[T any]() Optional[T] {
	return Optional[T]{set: true, null: true}
}

// IsSet reports whether field was provided.
func (o Optional[T]) IsSet() bool { return o.set }

// IsNull reports whether field was explicitly set to null.
func (o Optional[T]) IsNull() bool { return o.set && o.null }

// Value returns value when set and non-null.
func (o Optional[T]) Value() (T, bool) {
	if !o.set || o.null {
		var zero T
		return zero, false
	}
	return o.value, true
}

// Set marks value as explicitly set.
func (o *Optional[T]) Set(v T) {
	if o == nil {
		return
	}
	o.set = true
	o.null = false
	o.value = v
}

// SetNull marks value as explicit null.
func (o *Optional[T]) SetNull() {
	if o == nil {
		return
	}
	o.set = true
	o.null = true
	var zero T
	o.value = zero
}

// Unset marks value as absent.
func (o *Optional[T]) Unset() {
	if o == nil {
		return
	}
	o.set = false
	o.null = false
	var zero T
	o.value = zero
}

// IsZero makes omitempty work for absent state.
func (o Optional[T]) IsZero() bool {
	return !o.set
}

// MarshalJSON encodes absent/null as null and explicit value as JSON value.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if !o.set || o.null {
		return []byte("null"), nil
	}
	return json.Marshal(o.value)
}

// UnmarshalJSON tracks absent/null/value state.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	if o == nil {
		return fmt.Errorf("optional is nil")
	}
	o.set = true
	if string(data) == "null" {
		o.null = true
		var zero T
		o.value = zero
		return nil
	}
	o.null = false
	return json.Unmarshal(data, &o.value)
}

// UnmarshalText supports binding from query/header/cookie/form.
func (o *Optional[T]) UnmarshalText(text []byte) error {
	if o == nil {
		return fmt.Errorf("optional is nil")
	}
	o.set = true
	o.null = false
	return assignStringValue(&o.value, string(text))
}

// OptionalValueAny returns underlying value for validation internals.
func (o Optional[T]) OptionalValueAny() any { return o.value }

// OptionalIsSet exposes set state for validation internals.
func (o Optional[T]) OptionalIsSet() bool { return o.set }

// OptionalIsNull exposes null state for validation internals.
func (o Optional[T]) OptionalIsNull() bool { return o.null && o.set }

type optionalState interface {
	OptionalIsSet() bool
	OptionalIsNull() bool
	OptionalValueAny() any
}
