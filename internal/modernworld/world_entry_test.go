package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestTranslateInitWorldStates(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 571)
	legacy = binary.LittleEndian.AppendUint32(legacy, 12)
	legacy = binary.LittleEndian.AppendUint32(legacy, 34)
	legacy = binary.LittleEndian.AppendUint16(legacy, 3)
	for _, state := range []WorldState{{100, -7}, {0, 0}, {17223, 9}} {
		legacy = binary.LittleEndian.AppendUint32(legacy, state.Variable)
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(state.Value))
	}
	parsed, body, err := TranslateInitWorldStates(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.MapID != 571 || parsed.ZoneID != 12 || parsed.AreaID != 34 {
		t.Fatalf("unexpected header: %#v", parsed)
	}
	if parsed.States[0] != (WorldState{100, -7}) || parsed.States[1] != (WorldState{17223, 9}) {
		t.Fatalf("legacy states were not retained/deduplicated: %#v", parsed.States[:2])
	}
	if got := int(binary.LittleEndian.Uint32(body[12:])); got != len(parsed.States) {
		t.Fatalf("modern state count = %d, want %d", got, len(parsed.States))
	}
	if len(body) != 16+len(parsed.States)*8 {
		t.Fatalf("modern body has %d bytes", len(body))
	}
	if _, _, err := TranslateInitWorldStates(legacy[:len(legacy)-1]); err == nil {
		t.Fatal("truncated legacy world states unexpectedly parsed")
	}
}

func TestTranslateUpdateWorldState(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 20445)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	body, err := TranslateUpdateWorldState(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 9 || binary.LittleEndian.Uint32(body[:4]) != 20445 || binary.LittleEndian.Uint32(body[4:8]) != 1 || body[8] != 0 {
		t.Fatalf("visible world state %x", body)
	}
	hidden, err := TranslateUpdateWorldState(append(legacy, 1))
	if err != nil {
		t.Fatal(err)
	}
	if hidden[8]&0x80 == 0 {
		t.Fatalf("hidden bit missing: %x", hidden)
	}
	if _, err := TranslateUpdateWorldState(legacy[:7]); err == nil {
		t.Fatal("expected short world-state error")
	}
}

func TestWorldReadyPacketLayouts(t *testing.T) {
	empty := EncodeEmptyUpdateObject(571)
	if len(empty) != 11 || binary.LittleEndian.Uint32(empty) != 0 || binary.LittleEndian.Uint16(empty[4:]) != 571 || empty[6] != 0 || binary.LittleEndian.Uint32(empty[7:]) != 0 {
		t.Fatalf("empty update-object = %x", empty)
	}
	auras := EncodeEmptyAuraUpdateAll(0x42)
	if len(auras) < 7 || auras[0] != 0x80 || auras[1] != 0 {
		t.Fatalf("empty aura-update-all = %x", auras)
	}
	low, high, _, err := readPackedGUID128(auras[2:])
	if err != nil || low != 0x42 || high != uint64(2)<<58|uint64(1)<<42 {
		t.Fatalf("aura GUID = %016x:%016x err=%v", high, low, err)
	}
	phase := EncodeDefaultPhaseShift(0x42)
	low, high, consumed, err := readPackedGUID128(phase)
	if err != nil || low != 0x42 || high != uint64(2)<<58|uint64(1)<<42 || binary.LittleEndian.Uint32(phase[consumed:]) != 8 {
		t.Fatalf("phase shift header = %x err=%v", phase, err)
	}
	if ticks, err := ParseInitActiveMoverComplete([]byte{0x78, 0x56, 0x34, 0x12}); err != nil || ticks != 0x12345678 {
		t.Fatalf("ticks=0x%08x err=%v", ticks, err)
	}
	if _, err := ParseInitActiveMoverComplete(make([]byte, 3)); err == nil {
		t.Fatal("truncated active-mover completion unexpectedly parsed")
	}
}

func TestTranslateLegacyPhaseShiftChange(t *testing.T) {
	body, err := TranslateLegacyPhaseShiftChange([]byte{0x30, 0, 0, 0}, 0x42)
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: body}
	guid, err := r.guid128()
	flags, flagsErr := r.u32()
	count, countErr := r.u32()
	_, personalErr := r.guid128()
	firstFlags, firstFlagsErr := r.u16()
	firstID, firstIDErr := r.u16()
	secondFlags, secondFlagsErr := r.u16()
	secondID, secondIDErr := r.u16()
	if err != nil || flagsErr != nil || countErr != nil || personalErr != nil || firstFlagsErr != nil || firstIDErr != nil || secondFlagsErr != nil || secondIDErr != nil ||
		guid.Low != 0x42 || flags != 0 || count != 2 || firstFlags != 1 || firstID != 173 || secondFlags != 1 || secondID != 174 {
		t.Fatalf("guid=%#v flags/count=%d/%d phases=%d:%d/%d:%d errors=%v/%v/%v/%v/%v/%v/%v/%v body=%x", guid, flags, count, firstFlags, firstID, secondFlags, secondID, err, flagsErr, countErr, personalErr, firstFlagsErr, firstIDErr, secondFlagsErr, secondIDErr, body)
	}
	if _, err := TranslateLegacyPhaseShiftChange([]byte{1, 2, 3}, 0x42); err == nil {
		t.Fatal("short phase-shift change unexpectedly parsed")
	}
}
