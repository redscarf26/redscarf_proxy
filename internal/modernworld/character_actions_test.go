package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestParseAndEncodeCreateCharacter(t *testing.T) {
	name := "Rabbit"
	body := []byte{byte(len(name) << 2), 0}
	body = append(body, 1, 8, 0)
	body = binary.LittleEndian.AppendUint32(body, 5)
	body = append(body, name...)
	for _, pair := range [][2]uint32{{9, 17162}, {10, 17175}, {11, 17188}, {12, 17201}, {13, 17212}} {
		body = binary.LittleEndian.AppendUint32(body, pair[0])
		body = binary.LittleEndian.AppendUint32(body, pair[1])
	}
	request, err := ParseCreateCharacter(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Name != name || request.Race != 1 || request.Class != 8 || request.Sex != 0 || request.Skin != 2 || request.Face != 3 || request.HairStyle != 4 || request.HairColor != 5 || request.FacialHair != 6 {
		t.Fatalf("unexpected create request: %#v", request)
	}
	legacy := EncodeLegacyCreateCharacter(request)
	wantTail := []byte{0, 1, 8, 0, 2, 3, 4, 5, 6, 0}
	if len(legacy) != len(name)+len(wantTail) || string(legacy[:len(name)]) != name || string(legacy[len(name):]) != string(wantTail) {
		t.Fatalf("unexpected legacy create body: %x", legacy)
	}
}

func TestCharacterActionResultsAndGUID(t *testing.T) {
	high := uint64(2)<<58 | uint64(1)<<42
	packed := appendPackedGUID128(nil, 0x42, high)
	legacy, err := ParseCharacterGUID(packed)
	if err != nil || legacy != 0x42 {
		t.Fatalf("legacy GUID=%x err=%v", legacy, err)
	}
	created := EncodeCreateCharacterResult(47, legacy)
	if created[0] != 24 || string(created[1:]) != string(packed) {
		t.Fatalf("unexpected create result: %x", created)
	}
	if got := EncodeDeleteCharacterResult(71); len(got) != 1 || got[0] != 63 {
		t.Fatalf("unexpected delete result: %x", got)
	}
	if got := EncodeRandomCharacterNameUnavailable(); string(got) != string([]byte{0, 0}) {
		t.Fatalf("unexpected random-name result: %x", got)
	}
}

func TestParseCreateCharacterRejectsMalformedInput(t *testing.T) {
	if _, err := ParseCreateCharacter([]byte{0}); err == nil {
		t.Fatal("expected short create packet to fail")
	}
	if _, err := ParseCharacterGUID([]byte{1}); err == nil {
		t.Fatal("expected short packed GUID to fail")
	}
}
