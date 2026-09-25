package bnet

import (
	"bytes"
	"testing"
)

func TestGameUtilitiesAttributesRoundTrip(t *testing.T) {
	attributes := []Attribute{
		{Name: "Command_RealmListRequest_v1_wotlk1", Value: StringVariant("1-1-0")},
		{Name: "Param_ClientInfo", Value: BlobVariant([]byte{1, 2, 3})},
		{Name: "Param_RealmAddress", Value: UintVariant(0x01010001)},
		{Name: "Param_LastPlayedTime", Value: IntVariant(1234)},
	}
	got, err := DecodeClientRequest(EncodeClientRequest(attributes))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(attributes) {
		t.Fatalf("got %d attributes, want %d", len(got), len(attributes))
	}
	if got[0].Value.StringValue == nil || *got[0].Value.StringValue != "1-1-0" {
		t.Fatalf("unexpected string variant: %#v", got[0].Value)
	}
	if !got[1].Value.HasBlob || !bytes.Equal(got[1].Value.BlobValue, []byte{1, 2, 3}) {
		t.Fatalf("unexpected blob variant: %#v", got[1].Value)
	}
	if got[2].Value.UintValue == nil || *got[2].Value.UintValue != 0x01010001 {
		t.Fatalf("unexpected uint variant: %#v", got[2].Value)
	}
	if got[3].Value.IntValue == nil || *got[3].Value.IntValue != 1234 {
		t.Fatalf("unexpected int variant: %#v", got[3].Value)
	}
}

func TestGetAllValuesRoundTrip(t *testing.T) {
	const key = "Command_RealmListRequest_v1_wotlk1"
	decodedKey, err := DecodeGetAllValuesRequest(EncodeGetAllValuesRequest(key))
	if err != nil || decodedKey != key {
		t.Fatalf("key=%q err=%v", decodedKey, err)
	}
	values, err := DecodeGetAllValuesResponse(EncodeGetAllValuesResponse([]Variant{StringVariant("1-1-0")}))
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].StringValue == nil || *values[0].StringValue != "1-1-0" {
		t.Fatalf("unexpected values: %#v", values)
	}
}
