package bnet

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestAccountStateResponseContainsPrivacy(t *testing.T) {
	response := EncodeAccountStateResponse()
	state := nestedField(t, response, 1)
	privacy := nestedField(t, state, 2)
	values := scalarFields(t, privacy)
	if values[3] != 0 || values[4] != 0 || values[5] != 1 {
		t.Fatalf("unexpected privacy values: %#v", values)
	}
	if len(nestedField(t, response, 2)) == 0 {
		t.Fatal("missing account field tags")
	}
}

func TestGameAccountStateResponseContainsNameAndStatus(t *testing.T) {
	response := EncodeGameAccountStateResponse("TESTER")
	state := nestedField(t, response, 1)
	level := nestedField(t, state, 1)
	if got := string(nestedField(t, level, 8)); got != "TESTER" {
		t.Fatalf("account name %q", got)
	}
	levelScalars := scalarFields(t, level)
	if levelScalars[9] != wowProgram {
		t.Fatalf("program %#x", levelScalars[9])
	}
	status := scalarFields(t, nestedField(t, state, 3))
	if status[4] != 0 || status[5] != 0 || status[7] != wowProgram {
		t.Fatalf("unexpected game status: %#v", status)
	}
}

func nestedField(t *testing.T, data []byte, wanted protowire.Number) []byte {
	t.Helper()
	var result []byte
	if err := walkFields(data, func(number protowire.Number, typ protowire.Type, value []byte, _ uint64) error {
		if number == wanted && typ == protowire.BytesType {
			result = append([]byte(nil), value...)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func scalarFields(t *testing.T, data []byte) map[protowire.Number]uint64 {
	t.Helper()
	result := make(map[protowire.Number]uint64)
	if err := walkFields(data, func(number protowire.Number, typ protowire.Type, _ []byte, scalar uint64) error {
		if typ == protowire.VarintType || typ == protowire.Fixed32Type || typ == protowire.Fixed64Type {
			result[number] = scalar
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}
