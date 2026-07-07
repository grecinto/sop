// Package encoding provides pluggable marshal/unmarshal helpers used by SOP.
// JSON is the default, and helpers can pass through []byte values without copying.
package encoding

import (
	goccyjson "github.com/goccy/go-json"
)

// Marshaler defines methods to marshal/unmarshal values to/from byte slices.
type Marshaler interface {
	// Marshal encodes any object to a byte slice.
	Marshal(v any) ([]byte, error)
	// Unmarshal decodes data back into the provided object pointer.
	Unmarshal(data []byte, v any) error
}

// DefaultMarshaler is the package-wide default marshaler using JSON encoding.
var DefaultMarshaler = NewMarshaler()

// BlobMarshaler is used for blob payloads. It defaults to DefaultMarshaler but can be overridden.
var BlobMarshaler = DefaultMarshaler

type defaultMarshaler struct{}

// NewMarshaler returns a Marshaler implemented with the goccy/go-json package.
// It preserves the same API as the standard library while reducing allocations for
// the hot node and value serialization paths used by the B-tree and repository layers.
func NewMarshaler() Marshaler {
	return &defaultMarshaler{}
}

// Marshal encodes any object to a byte slice.
func (m defaultMarshaler) Marshal(v any) ([]byte, error) {
	return goccyjson.Marshal(v)
}

// Unmarshal decodes a byte slice back to its object type.
func (m defaultMarshaler) Unmarshal(data []byte, v any) error {
	return goccyjson.Unmarshal(data, v)
}

// Marshal is a generic helper that marshals values and passes through byte slices without copying.
func Marshal[T any](v T) ([]byte, error) {
	switch any(v).(type) {
	case *[]byte:
		var intf interface{}
		var v2 interface{} = v
		var ba *[]byte = v2.(*[]byte)
		intf = *ba
		return intf.([]byte), nil
	case []byte:
		var intf interface{}
		intf = v
		return intf.([]byte), nil
	default:
		return BlobMarshaler.Marshal(v)
	}
}

// Unmarshal is a generic helper that unmarshals values and passes through byte slices without copying.
func Unmarshal[T any](ba []byte, v *T) error {
	switch any(v).(type) {
	case *[]byte:
		var intf interface{}
		intf = ba
		*v = intf.(T)
		return nil
	case []byte:
		var intf interface{}
		intf = ba
		*v = intf.(T)
		return nil
	default:
		if err := BlobMarshaler.Unmarshal(ba, v); err != nil {
			return err
		}
		return nil
	}
}
