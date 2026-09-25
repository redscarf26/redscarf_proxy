package modernworld

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestParseStableMasterAndNumberRequests(t *testing.T) {
	master := GUID128{Low: 0x1234, High: uint64(3) << 58}
	body := appendPackedGUID128(nil, master.Low, master.High)
	got, err := ParseStableMaster(body)
	if err != nil || got != master {
		t.Fatalf("stable master=%#v err=%v", got, err)
	}
	if _, err := ParseStableMaster(append(body, 0)); err == nil {
		t.Fatal("trailing bytes on stable master")
	}

	numberBody := binary.LittleEndian.AppendUint32(nil, 77)
	numberBody = appendPackedGUID128(numberBody, master.Low, master.High)
	request, err := ParseStablePetNumberRequest(numberBody)
	if err != nil || request.PetNumber != 77 || request.Master != master {
		t.Fatalf("number request=%#v err=%v", request, err)
	}
	if _, err := ParseStablePetNumberRequest(numberBody[:3]); err == nil {
		t.Fatal("short number request")
	}
}

func TestEncodeLegacyStableRequests(t *testing.T) {
	const guid = uint64(0xf130005100002345)
	if got := EncodeLegacyStableMaster(guid); binary.LittleEndian.Uint64(got) != guid || len(got) != 8 {
		t.Fatalf("stable master body=%x", got)
	}
	got := EncodeLegacyStablePetNumber(guid, 9)
	if binary.LittleEndian.Uint64(got) != guid || binary.LittleEndian.Uint32(got[8:]) != 9 || len(got) != 12 {
		t.Fatalf("stable number body=%x", got)
	}
}

func TestParseLegacyStabledPets(t *testing.T) {
	const master = uint64(0xf130005100001111)
	body := binary.LittleEndian.AppendUint64(nil, master)
	body = append(body, 2, 4)
	body = binary.LittleEndian.AppendUint32(body, 8)
	body = binary.LittleEndian.AppendUint32(body, 26125)
	body = binary.LittleEndian.AppendUint32(body, 80)
	body = append(body, "哈奇"...)
	body = append(body, 0, 1)
	body = binary.LittleEndian.AppendUint32(body, 9)
	body = binary.LittleEndian.AppendUint32(body, 3098)
	body = binary.LittleEndian.AppendUint32(body, 40)
	body = append(body, "Wolf"...)
	body = append(body, 0, 2)

	list, err := ParseLegacyStabledPets(body)
	if err != nil {
		t.Fatal(err)
	}
	if list.Master != master || list.NumStableSlots != 4 || len(list.Pets) != 2 {
		t.Fatalf("list=%#v", list)
	}
	if list.Pets[0].PetNumber != 8 || list.Pets[0].CreatureID != 26125 || list.Pets[0].Level != 80 ||
		list.Pets[0].Name != "哈奇" || list.Pets[0].Flags != 1 || list.Pets[0].PetSlot != 6 {
		t.Fatalf("current pet=%#v", list.Pets[0])
	}
	if list.Pets[1].PetNumber != 9 || list.Pets[1].Name != "Wolf" || list.Pets[1].Flags != 3 || list.Pets[1].PetSlot != 6 {
		t.Fatalf("stabled pet=%#v", list.Pets[1])
	}

	empty := binary.LittleEndian.AppendUint64(nil, master)
	empty = append(empty, 0, 2)
	got, err := ParseLegacyStabledPets(empty)
	if err != nil || got.NumStableSlots != 2 || len(got.Pets) != 0 {
		t.Fatalf("empty list=%#v err=%v", got, err)
	}
	if _, err := ParseLegacyStabledPets(append(body, 0)); err == nil {
		t.Fatal("trailing bytes")
	}
	if _, err := ParseLegacyStabledPets(body[:9]); err == nil {
		t.Fatal("truncated list")
	}
}

func TestAssignStablePetDisplays(t *testing.T) {
	list := LegacyStabledPets{Pets: []LegacyStabledPet{
		{CreatureID: 26125, Flags: 1},
		{CreatureID: 3098, Flags: 2},
		{CreatureID: 3098, Flags: 2},
	}}
	missing := AssignStablePetDisplays(&list, map[uint32]uint32{3098: 447}, 155)
	if list.Pets[0].DisplayID != 155 || list.Pets[1].DisplayID != 447 || list.Pets[2].DisplayID != 447 {
		t.Fatalf("displays=%#v", list.Pets)
	}
	if len(missing) != 0 {
		t.Fatalf("missing=%v", missing)
	}
	list.Pets[0].DisplayID = 0
	list.Pets[1].DisplayID = 0
	list.Pets[2].DisplayID = 0
	missing = AssignStablePetDisplays(&list, nil, 0)
	if len(missing) != 2 || missing[0] != 26125 || missing[1] != 3098 {
		t.Fatalf("missing entries=%v", missing)
	}
}

func TestEncodePetGuidsAndResultAndSound(t *testing.T) {
	guid := GUID128{Low: 0x42, High: uint64(10) << 58}
	body := EncodePetGuids([]GUID128{guid})
	if binary.LittleEndian.Uint32(body) != 1 {
		t.Fatalf("pet guids count=%d", binary.LittleEndian.Uint32(body))
	}
	low, high, consumed, err := readPackedGUID128(body[4:])
	if err != nil || low != guid.Low || high != guid.High || 4+consumed != len(body) {
		t.Fatalf("pet guids body=%x err=%v", body, err)
	}
	if got := EncodePetGuids(nil); binary.LittleEndian.Uint32(got) != 0 || len(got) != 4 {
		t.Fatalf("empty pet guids=%x", got)
	}

	if _, err := ParseLegacyPetStableResult(nil); err == nil {
		t.Fatal("empty stable result")
	}
	result, err := ParseLegacyPetStableResult([]byte{0x08})
	if err != nil || result != 0x08 {
		t.Fatalf("result=%d err=%v", result, err)
	}
	if got := EncodePetStableResult(0x0A); len(got) != 1 || got[0] != 0x0A {
		t.Fatalf("encode result=%x", got)
	}
	if !PetStableResultSucceeded(PetStableSuccessStable) || !PetStableResultSucceeded(PetStableSuccessUnstable) || !PetStableResultSucceeded(PetStableSuccessBuySlot) {
		t.Fatal("success codes must reload the 54261 VALUES list")
	}
	if PetStableResultSucceeded(0x01) || PetStableResultSucceeded(0x06) {
		t.Fatal("failure codes must not reload the list")
	}

	const unit = uint64(0xf140000008000001)
	soundBody := binary.LittleEndian.AppendUint64(nil, unit)
	soundBody = binary.LittleEndian.AppendUint32(soundBody, 2)
	sound, err := ParseLegacyPetActionSound(soundBody)
	if err != nil || sound.UnitGUID != unit || sound.Action != 2 {
		t.Fatalf("sound=%#v err=%v", sound, err)
	}
	modern := ModernGUIDForLegacy(unit, 1)
	encoded := EncodePetActionSound(modern, 2)
	low, high, consumed, err = readPackedGUID128(encoded)
	if err != nil || low != modern.Low || high != modern.High {
		t.Fatalf("action sound GUID err=%v", err)
	}
	if binary.LittleEndian.Uint32(encoded[consumed:]) != 2 || consumed+4 != len(encoded) {
		t.Fatalf("action sound=%x", encoded)
	}

	packed := EncodeLegacyPackedGUID(unit)
	packed = binary.LittleEndian.AppendUint32(packed, 3)
	sound, err = ParseLegacyPetActionSound(packed)
	if err != nil || sound.UnitGUID != unit || sound.Action != 3 {
		t.Fatalf("packed sound=%#v err=%v", sound, err)
	}
	if _, err := ParseLegacyPetActionSound(packed[:len(packed)-1]); err == nil {
		t.Fatal("truncated packed action sound")
	}
}

func TestEncodePetStableValuesUpdate(t *testing.T) {
	player := GUID128{Low: 7, High: 1}
	master := GUID128{Low: 0x1111, High: uint64(3) << 58}
	list := LegacyStabledPets{
		Master:         0xf130005100001111,
		NumStableSlots: 4,
		Pets: []LegacyStabledPet{
			{PetNumber: 8, CreatureID: 26125, DisplayID: 155, Level: 80, Name: "哈奇", Flags: 1, PetSlot: 6},
			{PetNumber: 9, CreatureID: 3098, DisplayID: 447, Level: 40, Name: "Wolf", Flags: 2, PetSlot: 5},
		},
	}
	body, err := EncodePetStableValuesUpdate(571, player, master, list)
	if err != nil {
		t.Fatal(err)
	}
	payload := valuesUpdatePayload(t, body)
	if binary.LittleEndian.Uint32(payload) != 0x80 {
		t.Fatalf("section mask=%x", payload[:4])
	}
	r := movementReader{data: payload[4:]}
	mask0, _ := r.u32()
	mask1, _ := r.bits(16)
	block3, _ := r.bits(32)
	if mask0 != 8 || mask1 != 0 || block3 != 0x0c000040 {
		t.Fatalf("masks mask0=%x mask1=%x block3=%x", mask0, mask1, block3)
	}
	r.align()
	if slots, err := r.u8(); err != nil || slots != 4 {
		t.Fatalf("slots=%d err=%v", slots, err)
	}
	if hasStable, err := r.bits(1); err != nil || hasStable != 1 {
		t.Fatalf("HasPetStable=%d err=%v", hasStable, err)
	}
	r.align()
	infoMask, err := r.bits(3)
	if err != nil || infoMask != 7 {
		t.Fatalf("stable info mask=%d err=%v", infoMask, err)
	}
	count, err := r.bits(32)
	if err != nil || count != 2 {
		t.Fatalf("pet count=%d err=%v", count, err)
	}
	for i := 0; i < 2; i++ {
		changed, err := r.bit()
		if err != nil || !changed {
			t.Fatalf("pet %d changed=%v err=%v", i, changed, err)
		}
	}
	r.align()
	wantWire := []struct {
		slot, creature, model, level uint32
		flags                        uint8
		name                         string
	}{
		{6, 26125, 155, 80, 1, "哈奇"},
		{6, 3098, 447, 40, 3, "Wolf"},
	}
	for i, want := range wantWire {
		petMask, err := r.bits(8)
		if err != nil || petMask != 0xff {
			t.Fatalf("pet %d mask=%x err=%v", i, petMask, err)
		}
		r.align()
		if slot, err := r.u32(); err != nil || slot != want.slot {
			t.Fatalf("pet %d slot=%d err=%v", i, slot, err)
		}
		if creature, err := r.u32(); err != nil || creature != want.creature {
			t.Fatalf("pet %d creature=%d err=%v", i, creature, err)
		}
		if model, err := r.u32(); err != nil || model != want.model {
			t.Fatalf("pet %d model=%d err=%v", i, model, err)
		}
		if level, err := r.u32(); err != nil || level != want.level {
			t.Fatalf("pet %d level=%d err=%v", i, level, err)
		}
		if flags, err := r.u8(); err != nil || flags != want.flags {
			t.Fatalf("pet %d flags=%d err=%v", i, flags, err)
		}
		if pad, err := r.u8(); err != nil || pad != 0 {
			t.Fatalf("pet %d name pad=%d err=%v", i, pad, err)
		}
		nameLen, err := r.bits(8)
		if err != nil || int(nameLen) != len(want.name) {
			t.Fatalf("pet %d nameLen=%d err=%v", i, nameLen, err)
		}
		r.align()
		name, err := r.stringN(int(nameLen))
		if err != nil || name != want.name {
			t.Fatalf("pet %d name=%q err=%v", i, name, err)
		}
	}
	low, high, consumed, err := readPackedGUID128(r.data[r.offset:])
	if err != nil || low != master.Low || high != master.High || consumed != r.remaining() {
		t.Fatalf("master GUID remaining=%d err=%v", r.remaining(), err)
	}

	empty := LegacyStabledPets{NumStableSlots: 2}
	body, err = EncodePetStableValuesUpdate(0, player, master, empty)
	if err != nil {
		t.Fatal(err)
	}
	payload = valuesUpdatePayload(t, body)
	r = movementReader{data: payload[4:]}
	_, _ = r.u32()
	_, _ = r.bits(16)
	_, _ = r.bits(32)
	r.align()
	if slots, _ := r.u8(); slots != 2 {
		t.Fatalf("empty slots=%d", slots)
	}
	if hasStable, _ := r.bits(1); hasStable != 1 {
		t.Fatal("empty list must still set HasPetStable")
	}
	r.align()
	_, _ = r.bits(3)
	count, _ = r.bits(32)
	if count != 0 {
		t.Fatalf("empty pet count=%d", count)
	}
	r.align()
	low, high, consumed, err = readPackedGUID128(r.data[r.offset:])
	if err != nil || low != master.Low || high != master.High || consumed != r.remaining() {
		t.Fatalf("empty master remaining=%d err=%v", r.remaining(), err)
	}

	if _, err := EncodePetStableValuesUpdate(0, GUID128{}, master, empty); err == nil {
		t.Fatal("empty player GUID")
	}
}

func TestEncodePetStableValuesUpdateMatchesCapture(t *testing.T) {
	// Reference capture seq 565 / seq 524+687.
	// legacy proxy prefixes the ActivePlayer section with Unit+Player empty masks
	// (section 0xE0 + two 00 bytes). Glyph/quest updates already prove 0x80
	// ActivePlayer-only is accepted; lock the section that carries PetStable.
	// Four uint32s are PetSlot, CreatureID, DisplayID, Level. PetNumber is
	// omitted; AC number 6 in the second word would load Kobold Vermin.
	player := GUID128{Low: 0x03, High: 0x0804000000000000}
	master := GUID128{Low: 0x3b84, High: 0x20000400001a6080}
	name, err := hex.DecodeString("e9b281e4bcafe696af")
	if err != nil {
		t.Fatal(err)
	}
	current := LegacyStabledPets{
		NumStableSlots: 0,
		Pets: []LegacyStabledPet{
			{PetNumber: 6, CreatureID: 521, DisplayID: 11412, Level: 69, Name: string(name), Flags: 1},
		},
	}
	body, err := EncodePetStableValuesUpdate(571, player, master, current)
	if err != nil {
		t.Fatal(err)
	}
	payload := valuesUpdatePayload(t, body)
	wantCurrent, err := hex.DecodeString("0800000000000c0000400080e000000030ff0600000009020000942c000045000000010009e9b281e4bcafe696af03a7843b80601a0420")
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(payload) != 0x80 {
		t.Fatalf("section mask=%x", payload[:4])
	}
	if got := payload[4:]; !bytes.Equal(got, wantCurrent) {
		t.Fatalf("current section=%x\nwant            %x", got, wantCurrent)
	}

	stabled := LegacyStabledPets{
		NumStableSlots: 3,
		Pets: []LegacyStabledPet{
			{PetNumber: 6, CreatureID: 521, DisplayID: 11412, Level: 69, Name: string(name), Flags: 2, PetSlot: 5},
		},
	}
	body, err = EncodePetStableValuesUpdate(571, player, master, stabled)
	if err != nil {
		t.Fatal(err)
	}
	payload = valuesUpdatePayload(t, body)
	wantStabled, err := hex.DecodeString("0800000000000c0000400380e000000030ff0600000009020000942c000045000000030009e9b281e4bcafe696af03a7843b80601a0420")
	if err != nil {
		t.Fatal(err)
	}
	if got := payload[4:]; !bytes.Equal(got, wantStabled) {
		t.Fatalf("stabled section=%x\nwant            %x", got, wantStabled)
	}
}

func TestLegacySummonAndDisplayHelpers(t *testing.T) {
	summon := uint64(0xf140000008000001)
	fields := map[int]uint32{
		legacyUnitSummon:     uint32(summon),
		legacyUnitSummon + 1: uint32(summon >> 32),
		legacyUnitDisplayID:  155,
	}
	if got := LegacySummonGUID(fields); got != summon {
		t.Fatalf("summon=%x", got)
	}
	if got := LegacyUnitDisplayID(fields); got != 155 {
		t.Fatalf("display=%d", got)
	}
	if LegacySummonGUID(nil) != 0 || LegacyUnitDisplayID(nil) != 0 {
		t.Fatal("nil fields")
	}
}
