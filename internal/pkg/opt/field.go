// Package opt represents the fields of partial updates, where a JSON body has to tell three
// cases apart that a pointer cannot: a field left out (keep the value), a field set to null
// (clear the value) and a field set to a value.
package opt

import "encoding/json"

// Field is one field of a partial update. The zero value is an absent field.
type Field[T any] struct {
	// Set reports whether the field was given, as null or as a value.
	Set bool
	// Null reports whether the field was given as null.
	Null bool
	// Value is the value of a field that is set and not null.
	Value T
}

// Of returns a field set to v.
func Of[T any](v T) Field[T] {
	return Field[T]{Set: true, Value: v}
}

// Null returns a field set to null.
func Null[T any]() Field[T] {
	return Field[T]{Set: true, Null: true}
}

// Ptr returns a pointer to a copy of the value, or nil when the field is absent or null.
func (f Field[T]) Ptr() *T {
	if !f.Set || f.Null {
		return nil
	}
	v := f.Value
	return &v
}

// UnmarshalJSON implements json.Unmarshaler. encoding/json calls it only for keys present in
// the input, null included, which is what tells a present field from an absent one.
func (f *Field[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = Null[T]()
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = Of(v)
	return nil
}
