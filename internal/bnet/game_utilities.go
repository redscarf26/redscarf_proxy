package bnet

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// Variant is the subset of bgs.protocol.Variant used by the WotLK Classic
// GameUtilities realm-list exchange. Pointer fields retain protobuf presence.
type Variant struct {
	BoolValue   *bool
	IntValue    *int64
	FloatValue  *float64
	StringValue *string
	BlobValue   []byte
	HasBlob     bool
	UintValue   *uint64
}

type Attribute struct {
	Name  string
	Value Variant
}

func StringVariant(value string) Variant { return Variant{StringValue: &value} }
func BlobVariant(value []byte) Variant {
	return Variant{BlobValue: append([]byte(nil), value...), HasBlob: true}
}
func UintVariant(value uint64) Variant { return Variant{UintValue: &value} }
func IntVariant(value int64) Variant   { return Variant{IntValue: &value} }

func EncodeClientRequest(attributes []Attribute) []byte {
	return encodeAttributes(attributes)
}

func DecodeClientRequest(data []byte) ([]Attribute, error) {
	return decodeAttributes(data)
}

func EncodeClientResponse(attributes []Attribute) []byte {
	return encodeAttributes(attributes)
}

func DecodeClientResponse(data []byte) ([]Attribute, error) {
	return decodeAttributes(data)
}

func EncodeGetAllValuesRequest(attributeKey string) []byte {
	return appendBytesField(nil, 1, []byte(attributeKey))
}

func DecodeGetAllValuesRequest(data []byte) (string, error) {
	var key string
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, _ uint64) error {
		if number != 1 {
			return nil
		}
		if typ != protowire.BytesType {
			return fmt.Errorf("attribute_key wire type %d", typ)
		}
		key = string(value)
		return nil
	})
	return key, err
}

func EncodeGetAllValuesResponse(values []Variant) []byte {
	var data []byte
	for _, value := range values {
		data = appendBytesField(data, 1, encodeVariant(value))
	}
	return data
}

func DecodeGetAllValuesResponse(data []byte) ([]Variant, error) {
	var values []Variant
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, _ uint64) error {
		if number != 1 {
			return nil
		}
		if typ != protowire.BytesType {
			return fmt.Errorf("attribute_value wire type %d", typ)
		}
		variant, err := decodeVariant(value)
		if err != nil {
			return err
		}
		values = append(values, variant)
		return nil
	})
	return values, err
}

func FindAttribute(attributes []Attribute, name string) (Variant, bool) {
	for _, attribute := range attributes {
		if attribute.Name == name {
			return attribute.Value, true
		}
	}
	return Variant{}, false
}

func encodeAttributes(attributes []Attribute) []byte {
	var data []byte
	for _, attribute := range attributes {
		encoded := appendBytesField(nil, 1, []byte(attribute.Name))
		encoded = appendBytesField(encoded, 2, encodeVariant(attribute.Value))
		data = appendBytesField(data, 1, encoded)
	}
	return data
}

func decodeAttributes(data []byte) ([]Attribute, error) {
	var attributes []Attribute
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, _ uint64) error {
		if number != 1 {
			return nil
		}
		if typ != protowire.BytesType {
			return fmt.Errorf("attribute wire type %d", typ)
		}
		attribute, err := decodeAttribute(value)
		if err != nil {
			return err
		}
		attributes = append(attributes, attribute)
		return nil
	})
	return attributes, err
}

func decodeAttribute(data []byte) (Attribute, error) {
	var attribute Attribute
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, _ uint64) error {
		switch number {
		case 1:
			if typ != protowire.BytesType {
				return fmt.Errorf("attribute name wire type %d", typ)
			}
			attribute.Name = string(value)
		case 2:
			if typ != protowire.BytesType {
				return fmt.Errorf("attribute value wire type %d", typ)
			}
			variant, err := decodeVariant(value)
			if err != nil {
				return err
			}
			attribute.Value = variant
		}
		return nil
	})
	return attribute, err
}

func encodeVariant(value Variant) []byte {
	var data []byte
	if value.BoolValue != nil {
		if *value.BoolValue {
			data = appendVarintField(data, 2, 1)
		} else {
			data = appendVarintField(data, 2, 0)
		}
	}
	if value.IntValue != nil {
		data = appendVarintField(data, 3, uint64(*value.IntValue))
	}
	if value.FloatValue != nil {
		data = protowire.AppendTag(data, 4, protowire.Fixed64Type)
		data = protowire.AppendFixed64(data, math.Float64bits(*value.FloatValue))
	}
	if value.StringValue != nil {
		data = appendBytesField(data, 5, []byte(*value.StringValue))
	}
	if value.HasBlob {
		data = appendBytesField(data, 6, value.BlobValue)
	}
	if value.UintValue != nil {
		data = appendVarintField(data, 9, *value.UintValue)
	}
	return data
}

func decodeVariant(data []byte) (Variant, error) {
	var variant Variant
	err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, scalar uint64) error {
		switch number {
		case 2:
			if typ != protowire.VarintType {
				return fmt.Errorf("bool_value wire type %d", typ)
			}
			parsed := scalar != 0
			variant.BoolValue = &parsed
		case 3:
			if typ != protowire.VarintType {
				return fmt.Errorf("int_value wire type %d", typ)
			}
			parsed := int64(scalar)
			variant.IntValue = &parsed
		case 4:
			if typ != protowire.Fixed64Type {
				return fmt.Errorf("float_value wire type %d", typ)
			}
			parsed := math.Float64frombits(scalar)
			variant.FloatValue = &parsed
		case 5:
			if typ != protowire.BytesType {
				return fmt.Errorf("string_value wire type %d", typ)
			}
			parsed := string(value)
			variant.StringValue = &parsed
		case 6:
			if typ != protowire.BytesType {
				return fmt.Errorf("blob_value wire type %d", typ)
			}
			variant.BlobValue = append([]byte(nil), value...)
			variant.HasBlob = true
		case 9:
			if typ != protowire.VarintType {
				return fmt.Errorf("uint_value wire type %d", typ)
			}
			parsed := scalar
			variant.UintValue = &parsed
		}
		return nil
	})
	return variant, err
}
