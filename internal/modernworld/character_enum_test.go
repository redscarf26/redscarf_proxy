package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func TestTranslateCharacterEnum(t *testing.T) {
	legacy := []byte{1}
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x42)
	legacy = append(legacy, "Rabbit"...)
	legacy = append(legacy, 0)
	legacy = append(legacy, 1, 8, 1, 2, 3, 4, 5, 6, 80)
	legacy = binary.LittleEndian.AppendUint32(legacy, 12)
	legacy = binary.LittleEndian.AppendUint32(legacy, 571)
	for _, coordinate := range []float32{1.25, -2.5, 3.75} {
		legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(coordinate))
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	legacy = binary.LittleEndian.AppendUint32(legacy, characterFlagDeclined|0x10)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x1234)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 100)
	legacy = binary.LittleEndian.AppendUint32(legacy, 20)
	legacy = binary.LittleEndian.AppendUint32(legacy, 30)
	for index := 0; index < legacyCharacterVisualItems; index++ {
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(1000+index))
		legacy = append(legacy, byte(index))
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(2000+index))
	}

	characters, err := ParseLegacyCharacterEnum(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(characters) != 1 || characters[0].Name != "Rabbit" || characters[0].Level != 80 || characters[0].VisualItems[22].DisplayID != 1022 {
		t.Fatalf("unexpected parsed character: %#v", characters)
	}

	now := time.Unix(1_700_000_000, 0)
	modern, err := TranslateCharacterEnum(legacy, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(modern) != 713 || modern[0] != 0x82 {
		t.Fatalf("unexpected modern enum length/header: %d %x", len(modern), modern[:1])
	}
	if got := binary.LittleEndian.Uint32(modern[1:5]); got != 1 {
		t.Fatalf("character count = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(modern[5:9]); got != 80 {
		t.Fatalf("max level = %d, want 80", got)
	}
	if got := binary.LittleEndian.Uint32(modern[42:46]); got != 5 {
		t.Fatalf("customization count = %d, want 5", got)
	}
	if option, choice := binary.LittleEndian.Uint32(modern[614:618]), binary.LittleEndian.Uint32(modern[618:622]); option != 14 || choice != 17217 {
		t.Fatalf("first serialized customization = %d/%d, want 14/17217", option, choice)
	}
	nameStart := len(modern) - 50 - len("Rabbit")
	if string(modern[nameStart:nameStart+len("Rabbit")]) != "Rabbit" {
		t.Fatalf("character name not found at expected tail: %x", modern[len(modern)-70:])
	}
}

func TestParseLegacyCharacterEnumRejectsTruncation(t *testing.T) {
	if _, err := ParseLegacyCharacterEnum([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected truncated character enum to fail")
	}
}

func TestAppendPackedGUID128(t *testing.T) {
	got := appendPackedGUID128(nil, 0x010200, 0x0300000000000004)
	want := []byte{0x06, 0x81, 0x02, 0x01, 0x04, 0x03}
	if string(got) != string(want) {
		t.Fatalf("packed guid = %x, want %x", got, want)
	}
}

func TestModernCustomizations(t *testing.T) {
	got := modernCustomizations(LegacyCharacter{Race: 1, Sex: 0, Skin: 1, Face: 2, HairStyle: 3, HairColor: 4, FacialHair: 5})
	want := [][2]uint32{{9, 17161}, {10, 17174}, {11, 17187}, {12, 17200}, {13, 17211}}
	if len(got) != len(want) {
		t.Fatalf("customization count = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("customization %d = %v, want %v", index, got[index], want[index])
		}
	}
}
