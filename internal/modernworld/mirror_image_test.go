package modernworld

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestParseGetMirrorImageData(t *testing.T) {
	guid := ModernGUIDForLegacy(0xf130000001000042, 631)
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	got, err := ParseGetMirrorImageData(body)
	if err != nil || got != guid {
		t.Fatalf("guid-only: got %#v err=%v", got, err)
	}
	withDisplay := binary.LittleEndian.AppendUint32(append([]byte(nil), body...), 49)
	got, err = ParseGetMirrorImageData(withDisplay)
	if err != nil || got != guid {
		t.Fatalf("guid+display: got %#v err=%v", got, err)
	}
	if _, err := ParseGetMirrorImageData(append(body, 1)); err == nil {
		t.Fatal("expected trailing-byte rejection")
	}
}

func TestTranslateLegacyMirrorImageComponentedData(t *testing.T) {
	const legacyGUID = uint64(0xf130000001000042)
	unit := ModernGUIDForLegacy(legacyGUID, 631)
	legacy := EncodeLegacyUnpackedGUID(legacyGUID)
	legacy = binary.LittleEndian.AppendUint32(legacy, 49)
	legacy = append(legacy, 1, 0, 8, 1, 2, 3, 4, 5) // human male mage appearance
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	for index := 0; index < legacyMirrorImageItemSlots; index++ {
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(1000+index))
	}

	opcode, body, err := TranslateLegacyMirrorImageData(legacy, func(guid uint64) GUID128 {
		if guid != legacyGUID {
			t.Fatalf("resolve guid = 0x%x", guid)
		}
		return unit
	})
	if err != nil {
		t.Fatal(err)
	}
	if opcode != SMSGMirrorImageComponentedData {
		t.Fatalf("opcode = %d, want componented %d", opcode, SMSGMirrorImageComponentedData)
	}

	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	if low != unit.Low || high != unit.High {
		t.Fatalf("unit = 0x%x/0x%x, want 0x%x/0x%x", low, high, unit.Low, unit.High)
	}
	rest := body[consumed:]
	if binary.LittleEndian.Uint32(rest[:4]) != 49 || rest[4] != 1 || rest[5] != 0 || rest[6] != 8 {
		t.Fatalf("display/race/gender/class = %x", rest[:7])
	}
	if binary.LittleEndian.Uint32(rest[7:11]) != 5 {
		t.Fatalf("customization count = %d, want 5", binary.LittleEndian.Uint32(rest[7:11]))
	}
	guildLow, guildHigh, guildBytes, err := readPackedGUID128(rest[11:])
	if err != nil {
		t.Fatal(err)
	}
	wantGuild := modernMirrorGuildGUID(7)
	if guildLow != wantGuild.Low || guildHigh != wantGuild.High {
		t.Fatalf("guild = %d/%d, want %d/%d", guildLow, guildHigh, wantGuild.Low, wantGuild.High)
	}
	afterGuild := rest[11+guildBytes:]
	if binary.LittleEndian.Uint32(afterGuild[:4]) != modernMirrorImageItemCount {
		t.Fatalf("item count = %d, want %d", binary.LittleEndian.Uint32(afterGuild[:4]), modernMirrorImageItemCount)
	}
	pairs := afterGuild[4:]
	want := [][2]uint32{{9, 17161}, {10, 17174}, {11, 17187}, {12, 17200}, {13, 17211}}
	for index, pair := range want {
		option := binary.LittleEndian.Uint32(pairs[index*8 : index*8+4])
		choice := binary.LittleEndian.Uint32(pairs[index*8+4 : index*8+8])
		if option != pair[0] || choice != pair[1] {
			t.Fatalf("customization %d = %d/%d, want %d/%d", index, option, choice, pair[0], pair[1])
		}
	}
	items := pairs[len(want)*8:]
	if len(items) != legacyMirrorImageMaxItemSlots*4 {
		t.Fatalf("item bytes = %d, want %d slots", len(items), legacyMirrorImageMaxItemSlots)
	}
	if binary.LittleEndian.Uint32(items[:4]) != 1000 || binary.LittleEndian.Uint32(items[10*4:11*4]) != 1010 {
		t.Fatalf("item displays = %x", items)
	}
	for index := legacyMirrorImageItemSlots; index < legacyMirrorImageMaxItemSlots; index++ {
		if binary.LittleEndian.Uint32(items[index*4:(index+1)*4]) != 0 {
			t.Fatalf("padding slot %d = %d", index, binary.LittleEndian.Uint32(items[index*4:]))
		}
	}
}

func TestTranslateLegacyMirrorImageCreatureData(t *testing.T) {
	const legacyGUID = uint64(0xf130000001000043)
	unit := ModernGUIDForLegacy(legacyGUID, 580)
	legacy := EncodeLegacyUnpackedGUID(legacyGUID)
	legacy = binary.LittleEndian.AppendUint32(legacy, 11686)
	opcode, body, err := TranslateLegacyMirrorImageData(legacy, func(uint64) GUID128 { return unit })
	if err != nil {
		t.Fatal(err)
	}
	if opcode != SMSGMirrorImageCreatureData {
		t.Fatalf("opcode = %d, want creature %d", opcode, SMSGMirrorImageCreatureData)
	}
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	if low != unit.Low || high != unit.High {
		t.Fatalf("unit = %d/%d", low, high)
	}
	rest := body[consumed:]
	if len(rest) != 8 || binary.LittleEndian.Uint32(rest[:4]) != 11686 || binary.LittleEndian.Uint32(rest[4:]) != 0 {
		t.Fatalf("creature payload = %x", rest)
	}
}

func TestIsLegacyMirrorImageCreature(t *testing.T) {
	fields := map[int]uint32{legacyUnitFlags2: 0x810}
	if !IsLegacyMirrorImageCreature(3, fields) {
		t.Fatal("expected cloned creature")
	}
	if IsLegacyMirrorImageCreature(4, fields) {
		t.Fatal("players are not clone NPCs")
	}
	if IsLegacyMirrorImageCreature(3, map[int]uint32{legacyUnitFlags2: 0x800}) {
		t.Fatal("plain creature")
	}
	if IsLegacyMirrorImageCreature(3, nil) {
		t.Fatal("empty fields")
	}
}

func TestEncodeLegacyGetMirrorImageData(t *testing.T) {
	const guid = uint64(0xf130000001000042)
	got := EncodeLegacyGetMirrorImageData(guid)
	want := EncodeLegacyUnpackedGUID(guid)
	if string(got) != string(want) {
		t.Fatalf("legacy cmsg = %x, want %x", got, want)
	}
	if len(got) != 8 {
		t.Fatalf("legacy cmsg has %d bytes, want unpacked 8", len(got))
	}
}

func TestTranslateLegacyMirrorImageMatchesCapture(t *testing.T) {
	const legacyGUID = uint64(0x27cf)
	unit := GUID128{Low: 0x27cf, High: 0x20000400001e7c00}
	legacy := EncodeLegacyUnpackedGUID(legacyGUID)
	legacy = binary.LittleEndian.AppendUint32(legacy, 50)
	legacy = append(legacy, 1, 1, 8, 2, 1, 11, 5, 2)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	for _, display := range []uint32{0, 0, 2163, 12647, 0, 9924, 9929, 0, 0, 0, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, display)
	}

	_, body, err := TranslateLegacyMirrorImageData(legacy, func(uint64) GUID128 { return unit })
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString("03a6cf277c1e0420320000000101080500000000000c0000000e000000414300000f0000004c4300001000000065430000110000007243000012000000794300000000000000000000730800006731000000000000c4260000c9260000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(want) {
		t.Fatalf("capture mismatch\n got %x\nwant %x", body, want)
	}
}
