package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestFormatLegacyValuesFields(t *testing.T) {
	got := FormatLegacyValuesFields(map[int]uint32{
		legacyPlayerFlags: 0x30,
		legacyUnitHealth:  1,
		legacyUnitFlags:   0x8,
	})
	want := "24=0x1,59=0x8,150=0x30"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeUnitValuesUpdate(t *testing.T) {
	changed := map[int]uint32{
		legacyUnitHealth:       321,
		legacyUnitLevel:        80,
		legacyUnitDynamicFlags: 1,
	}
	body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0xf130000001000042,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 571, ObjectType: 3,
		GUID: ModernGUIDForLegacy(0xf130000001000042, 571), Fields: changed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if translated != 3 {
		t.Fatalf("translated fields = %d, want 3", translated)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x21 {
		t.Fatalf("changed mask = 0x%x, want 0x21", got)
	}
	position := 4
	if values[position] != 0x50 || binary.LittleEndian.Uint32(values[position+1:]) != 4 {
		t.Fatalf("object delta = %x", values[position:position+5])
	}
	position += 5
	if values[position] != 0x01 || string(values[position+1:position+5]) != string([]byte{0xc0, 0x00, 0x00, 0x21}) {
		t.Fatalf("unit changed-mask = %x", values[position:position+5])
	}
	position += 5
	if got := binary.LittleEndian.Uint64(values[position:]); got != 321 {
		t.Fatalf("health = %d, want 321", got)
	}
	position += 8
	if binary.LittleEndian.Uint32(values[position:]) != 80 || binary.LittleEndian.Uint32(values[position+4:]) != 80 {
		t.Fatalf("level/effective-level = %x", values[position:position+8])
	}
}

func TestEncodePlayerStunnedFlagUpdate(t *testing.T) {
	const legacyFlags = uint32(0x000c0008) // player-controlled, stunned, in combat
	changed := map[int]uint32{legacyUnitFlags: legacyFlags}
	body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0x42,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 36, ObjectType: 4, Active: true,
		GUID: ModernGUIDForLegacy(0x42, 36), Fields: changed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if translated != 1 {
		t.Fatalf("translated fields = %d, want 1", translated)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x20 {
		t.Fatalf("changed mask = 0x%x, want Unit section only", got)
	}
	r := movementReader{data: values[4:]}
	blocksMask, err := r.bits(8)
	if err != nil || blocksMask != 1<<0|1<<1 {
		t.Fatalf("Unit blocks mask=%#x err=%v", blocksMask, err)
	}
	block0, err := r.bits(32)
	if err != nil || block0 != 1|1<<9 {
		t.Fatalf("Unit block 0=%#x err=%v", block0, err)
	}
	block1, err := r.bits(32)
	if err != nil || block1 != 1|1<<(41-32) {
		t.Fatalf("Unit block 1=%#x err=%v", block1, err)
	}
	r.align()
	stateAnimID, err := r.u32()
	if err != nil || stateAnimID != modernUnitAnimStun {
		t.Fatalf("modern UnitData.StateAnimID=%d want=%d err=%v", stateAnimID, modernUnitAnimStun, err)
	}
	flags, err := r.u32()
	if err != nil || flags != legacyFlags {
		t.Fatalf("modern UnitData.Flags=%#x want=%#x err=%v", flags, legacyFlags, err)
	}
}

func TestEncodeUnitTargetValuesUpdate(t *testing.T) {
	legacyTarget := uint64(0xf1300000c7000f14)
	fields := map[int]uint32{
		legacyUnitTarget:     uint32(legacyTarget),
		legacyUnitTarget + 1: uint32(legacyTarget >> 32),
	}
	payload := encodeUnitValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 3, 571)
	r := movementReader{data: payload}
	blocksMask, err := r.bits(8)
	if err != nil || blocksMask != 1 {
		t.Fatalf("Unit blocks mask=%#x err=%v", blocksMask, err)
	}
	block0, err := r.bits(32)
	if err != nil || block0 != 1|1<<19 {
		t.Fatalf("Unit block 0=%#x err=%v", block0, err)
	}
	r.align()
	low, high, consumed, err := readPackedGUID128(r.data[r.offset:])
	want := ModernGUIDForLegacy(legacyTarget, 571)
	if err != nil || low != want.Low || high != want.High {
		t.Fatalf("UnitData.Target=%016x:%016x want=%016x:%016x err=%v", high, low, want.High, want.Low, err)
	}
	r.offset += consumed
	if r.remaining() != 0 {
		t.Fatalf("UnitData.Target delta has %d trailing bytes", r.remaining())
	}
}

func TestEncodeUnitCharmValuesUpdate(t *testing.T) {
	legacyCharm := uint64(0xf130000007fb231c)
	fields := map[int]uint32{
		legacyUnitCharm:     uint32(legacyCharm),
		legacyUnitCharm + 1: uint32(legacyCharm >> 32),
	}
	payload := encodeUnitValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 4, 1)
	r := movementReader{data: payload}
	blocksMask, err := r.bits(8)
	if err != nil || blocksMask != 1 {
		t.Fatalf("Unit blocks mask=%#x err=%v", blocksMask, err)
	}
	block0, err := r.bits(32)
	if err != nil || block0 != 1|1<<11 {
		t.Fatalf("Unit block 0=%#x err=%v", block0, err)
	}
	r.align()
	low, high, consumed, err := readPackedGUID128(r.data[r.offset:])
	want := ModernGUIDForLegacy(legacyCharm, 1)
	if err != nil || low != want.Low || high != want.High {
		t.Fatalf("UnitData.Charm=%016x:%016x want=%016x:%016x err=%v", high, low, want.High, want.Low, err)
	}
	r.offset += consumed
	if r.remaining() != 0 {
		t.Fatalf("UnitData.Charm delta has %d trailing bytes", r.remaining())
	}
}

func TestEncodeActivePlayerFarsightValuesUpdate(t *testing.T) {
	legacyEye := uint64(0xf1300000010000aa)
	fields := map[int]uint32{
		legacyPlayerFarsight:     uint32(legacyEye),
		legacyPlayerFarsight + 1: uint32(legacyEye >> 32),
	}
	payload := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 1)
	r := movementReader{data: payload}
	mask0, err := r.u32()
	if err != nil || mask0 != 1 {
		t.Fatalf("mask0=0x%x err=%v", mask0, err)
	}
	mask1, _ := r.bits(16)
	block0, _ := r.bits(32)
	r.align()
	if mask1 != 0 || block0 != 1|1<<26 {
		t.Fatalf("mask1=0x%x block0=0x%x, want parent 0 + Farsight bit 26", mask1, block0)
	}
	low, high, consumed, err := readPackedGUID128(r.data[r.offset:])
	want := ModernGUIDForLegacy(legacyEye, 1)
	if err != nil || low != want.Low || high != want.High {
		t.Fatalf("Farsight=%016x:%016x want=%016x:%016x err=%v", high, low, want.High, want.Low, err)
	}
	r.offset += consumed
	if r.remaining() != 0 {
		t.Fatalf("Farsight delta has %d trailing bytes", r.remaining())
	}
}

func TestEncodeUnitTargetValuesUpdateCanClearTarget(t *testing.T) {
	changed := map[int]uint32{legacyUnitTarget: 0, legacyUnitTarget + 1: 0}
	payload := encodeUnitValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 3, 571)
	if len(payload) != 7 { // block selectors plus an empty PackedGuid128
		t.Fatalf("clear target payload bytes=%d body=%x", len(payload), payload)
	}
	low, high, _, err := readPackedGUID128(payload[5:])
	if err != nil || low != 0 || high != 0 {
		t.Fatalf("cleared UnitData.Target=%016x:%016x err=%v", high, low, err)
	}
}

func TestEncodeUnitChannelValuesUpdate(t *testing.T) {
	const (
		legacyChannelTarget = uint64(0xf130000001000043)
		channelSpell        = uint32(19674)
	)
	fields := map[int]uint32{
		legacyUnitChannelObject:     uint32(legacyChannelTarget & 0xffffffff),
		legacyUnitChannelObject + 1: uint32(legacyChannelTarget >> 32),
		legacyUnitChannelSpell:      channelSpell,
	}
	payload := encodeUnitValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 4, 571)
	r := movementReader{data: payload}
	blocksMask, err := r.bits(8)
	if err != nil || blocksMask != 1 {
		t.Fatalf("Unit blocks mask=%#x err=%v", blocksMask, err)
	}
	block0, err := r.bits(32)
	if err != nil || block0 != 1|1<<4|1<<22 {
		t.Fatalf("Unit block 0=%#x err=%v", block0, err)
	}
	channelObjectsSize, err := r.bits(32)
	if err != nil || channelObjectsSize != 1 {
		t.Fatalf("ChannelObjects size=%d err=%v", channelObjectsSize, err)
	}
	elementChanged, err := r.bit()
	if err != nil || !elementChanged {
		t.Fatalf("ChannelObjects element mask=%v err=%v", elementChanged, err)
	}
	r.align()
	low, high, consumed, err := readPackedGUID128(r.data[r.offset:])
	want := ModernGUIDForLegacy(legacyChannelTarget, 571)
	if err != nil || low != want.Low || high != want.High {
		t.Fatalf("UnitData.ChannelObjects[0]=%016x:%016x want=%016x:%016x err=%v", high, low, want.High, want.Low, err)
	}
	r.offset += consumed
	spellID, spellErr := r.u32()
	visualID, visualErr := r.u32()
	if spellErr != nil || visualErr != nil || spellID != channelSpell || visualID != KnownSpellVisual(channelSpell) {
		t.Fatalf("UnitData.ChannelData spell=%d visual=%d want=%d/%d err=%v/%v", spellID, visualID, channelSpell, KnownSpellVisual(channelSpell), spellErr, visualErr)
	}
	if r.remaining() != 0 {
		t.Fatalf("Unit channel delta has %d trailing bytes", r.remaining())
	}

	guid := ModernGUIDForLegacy(0xf130000001000044, 631)
	body, err := EncodeUnitChannelValuesUpdate(guid, 631, 3, 69705, legacyChannelTarget)
	if err != nil || len(body) == 0 {
		t.Fatalf("channel values update: %v %d", err, len(body))
	}
	ApplyLegacyChannelState(fields, 69705, legacyChannelTarget)
	if LegacyChannelObject(fields) != legacyChannelTarget || fields[legacyUnitChannelSpell] != 69705 {
		t.Fatalf("cached channel state %#v", fields)
	}
	if SpellChannelObject(SpellCastData{HitTargets: []uint64{legacyChannelTarget}}) != legacyChannelTarget {
		t.Fatal("spell go hit should be the channel object")
	}
}

func TestEncodeUnitChannelValuesUpdateCanClearState(t *testing.T) {
	fields := map[int]uint32{
		legacyUnitChannelObject:     0,
		legacyUnitChannelObject + 1: 0,
		legacyUnitChannelSpell:      0,
	}
	payload := encodeUnitValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 4, 571)
	r := movementReader{data: payload}
	blocksMask, err := r.bits(8)
	if err != nil || blocksMask != 1 {
		t.Fatalf("Unit blocks mask=%#x err=%v", blocksMask, err)
	}
	block0, err := r.bits(32)
	if err != nil || block0 != 1|1<<4|1<<22 {
		t.Fatalf("Unit block 0=%#x err=%v", block0, err)
	}
	channelObjectsSize, err := r.bits(32)
	if err != nil || channelObjectsSize != 0 {
		t.Fatalf("cleared ChannelObjects size=%d err=%v", channelObjectsSize, err)
	}
	r.align()
	spellID, spellErr := r.u32()
	visualID, visualErr := r.u32()
	if spellErr != nil || visualErr != nil || spellID != 0 || visualID != 0 || r.remaining() != 0 {
		t.Fatalf("cleared UnitData.ChannelData spell=%d visual=%d remaining=%d err=%v/%v", spellID, visualID, r.remaining(), spellErr, visualErr)
	}
}

func TestEncodePlayerStunnedFlagClearClearsStateAnim(t *testing.T) {
	const legacyFlags = uint32(0x00080008) // player-controlled, in combat
	payload := encodeUnitValuesDelta(legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitFlags: legacyFlags,
	}}, map[int]uint32{legacyUnitFlags: legacyFlags}, 4, 0)
	r := movementReader{data: payload}
	blocksMask, err := r.bits(8)
	if err != nil || blocksMask != 1<<0|1<<1 {
		t.Fatalf("Unit blocks mask=%#x err=%v", blocksMask, err)
	}
	block0, err := r.bits(32)
	if err != nil || block0 != 1|1<<9 {
		t.Fatalf("Unit block 0=%#x err=%v", block0, err)
	}
	block1, err := r.bits(32)
	if err != nil || block1 != 1|1<<(41-32) {
		t.Fatalf("Unit block 1=%#x err=%v", block1, err)
	}
	r.align()
	stateAnimID, err := r.u32()
	if err != nil || stateAnimID != 0 {
		t.Fatalf("modern UnitData.StateAnimID=%d want=0 err=%v", stateAnimID, err)
	}
	flags, err := r.u32()
	if err != nil || flags != legacyFlags {
		t.Fatalf("modern UnitData.Flags=%#x want=%#x err=%v", flags, legacyFlags, err)
	}
}

func TestEncodeUnitCharacterSheetValuesUpdate(t *testing.T) {
	changed := map[int]uint32{
		legacyUnitPowerCostModifier0: 9,
		legacyUnitMaxPower1:          99,
		legacyUnitStat0:              17,
		legacyUnitResistance0:        23,
	}
	fields := cloneFields(changed)
	fields[legacyUnitBytes0] = 8 << 8 // Mage: mana is modern power slot zero.
	section := encodeUnitValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 4, 0)
	r := movementReader{data: section}
	blocksMask, err := r.bits(8)
	if err != nil || blocksMask != 1<<3|1<<4|1<<5|1<<6 {
		t.Fatalf("blocks mask=%#x err=%v", blocksMask, err)
	}
	block3, _ := r.bits(32)
	block4, _ := r.bits(32)
	block5, _ := r.bits(32)
	block6, _ := r.bits(32)
	if block3 != 1<<20 || block4 != 1<<19 {
		t.Fatalf("max-power blocks=%#x/%#x", block3, block4)
	}
	if block5 != 1<<14|1<<15|1<<30|1<<31 {
		t.Fatalf("stat/resistance block=%#x", block5)
	}
	if block6 != 1<<6 {
		t.Fatalf("power-cost block=%#x", block6)
	}
	r.align()
	for _, want := range []uint32{99, 17, 23, 9} {
		got, readErr := r.u32()
		if readErr != nil || got != want {
			t.Fatalf("character-sheet value=%d want=%d err=%v", got, want, readErr)
		}
	}
	if r.remaining() != 0 {
		t.Fatalf("character-sheet delta has %d trailing bytes", r.remaining())
	}
}

func TestEncodeGameObjectValuesUpdateCarriesDoorStateComponents(t *testing.T) {
	raw := uint32(5 | 3<<8 | 7<<16 | 255<<24)
	changed := map[int]uint32{legacyGameObjectBytes1: raw}
	section := encodeGameObjectValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 36)
	r := movementReader{data: section}
	mask, err := r.bits(20)
	wantMask := uint32(1 | 1<<15 | 1<<16 | 1<<17 | 1<<18)
	if err != nil || mask != wantMask {
		t.Fatalf("game-object mask=%#x want=%#x err=%v", mask, wantMask, err)
	}
	r.align()
	state, _ := r.u8()
	typeID, _ := r.u8()
	percentHealth, _ := r.u8()
	artKit, _ := r.u32()
	if state != 5 || typeID != 3 || percentHealth != 255 || artKit != 7 {
		t.Fatalf("state=%d type=%d health=%d art-kit=%d", state, typeID, percentHealth, artKit)
	}
	if r.remaining() != 0 {
		t.Fatalf("game-object delta has %d trailing bytes", r.remaining())
	}
}

func TestEncodeGameObjectValuesUpdatePreservesAnimationProgress(t *testing.T) {
	changed := map[int]uint32{legacyGameObjectDynamic: 0xffff0009}
	fields := map[int]uint32{
		legacyGameObjectDynamic: 0xffff0009,
		legacyGameObjectBytes1:  5,
	}
	body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0xf110000001000042,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 36, ObjectType: 5,
		GUID: ModernGUIDForLegacy(0xf110000001000042, 36), Fields: fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	if translated != 1 {
		t.Fatalf("translated fields = %d, want 1", translated)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x01 {
		t.Fatalf("changed mask = 0x%x, want object section only", got)
	}
	if len(values) != 9 || values[4] != 0x50 {
		t.Fatalf("object delta = %x, want dynamic-flags mask and uint32", values[4:])
	}
	if got := binary.LittleEndian.Uint32(values[5:]); got != 0xffff0024 {
		t.Fatalf("modern GameObject dynamic flags = 0x%x, want 0xffff0024", got)
	}
}

func TestEncodeTransportValuesUpdateKeepsActiveTransportBit(t *testing.T) {
	// legacy proxy keeps the modern transport-active bit (0x40) set in the low word of
	// a transport root's ObjectData.DynamicFlags for the lifetime of the object,
	// so a Values delta must re-emit it even when the legacy realm sends an
	// explicit zero.  Without it the 3.4.3 client re-enters the WMO's inactive
	// load path (transport_icebreaker_ship.wmo, FileData 116306).
	tests := []struct {
		name   string
		guid   uint64
		legacy uint32
		want   uint32
	}{
		{name: "moving ship sends zero dynamic", guid: 0x1fc0000000000001, legacy: 0, want: 0x40},
		{name: "airship platform keeps completed progress", guid: 0xf120000000000001, legacy: 0xffff0000, want: 0xffff0040},
		{name: "ship keeps path progress", guid: 0x1fc000000000000a, legacy: 0x47c70000, want: 0x47c70040},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := map[int]uint32{legacyGameObjectDynamic: test.legacy}
			body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
				Type: LegacyUpdateValues, GUID: test.guid,
				Values: LegacyUpdateValuesBlock{Fields: changed},
			}, ValuesUpdateOptions{
				MapID: 36, ObjectType: 5,
				GUID: ModernGUIDForLegacy(test.guid, 36), Fields: changed,
			})
			if err != nil {
				t.Fatal(err)
			}
			if translated != 1 {
				t.Fatalf("translated fields = %d, want 1", translated)
			}
			values := valuesUpdatePayload(t, body)
			if got := binary.LittleEndian.Uint32(values); got != 0x01 {
				t.Fatalf("changed mask = 0x%x, want object section only", got)
			}
			if len(values) != 9 || values[4] != 0x50 {
				t.Fatalf("object delta = %x, want dynamic-flags mask and uint32", values[4:])
			}
			if got := binary.LittleEndian.Uint32(values[5:]); got != test.want {
				t.Fatalf("transport dynamic flags = 0x%x, want active transport bit 0x%x", got, test.want)
			}
		})
	}
}

func TestEncodeGameObjectStateChangeDoesNotRewriteDynamicFlags(t *testing.T) {
	const guid = uint64(0xf110000001000042)
	// A door flipping from READY(1) to ACTIVE(0) arrives as a bytes1 change
	// only.  legacy proxy carries that State on its own descriptor field and leaves
	// ObjectData.DynamicFlags alone; mirroring State into the low-word flags
	// made the door render closed instead of open.
	changed := map[int]uint32{legacyGameObjectBytes1: 0 | 2<<8 | 255<<24}
	fields := map[int]uint32{
		legacyGameObjectDynamic: 0xffff0009,
		legacyGameObjectBytes1:  changed[legacyGameObjectBytes1],
	}
	body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: guid,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 36, ObjectType: 5,
		GUID: ModernGUIDForLegacy(guid, 36), Fields: fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	if translated != 1 {
		t.Fatalf("translated fields = %d, want 1", translated)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x100 {
		t.Fatalf("changed mask = %#x, want GameObject section only", got)
	}
	// Game-object delta starts with the 20-bit mask then State/TypeID/
	// PercentHealth/ArtKit bytes; the ObjectData section must be absent.
	section := values[4:]
	r := movementReader{data: section}
	mask, err := r.bits(20)
	if err != nil {
		t.Fatal(err)
	}
	if mask&(1<<2) != 0 {
		t.Fatalf("game-object mask %#x includes ObjectData dynamic flags", mask)
	}
	r.align()
	state, _ := r.u8()
	if state != 0 {
		t.Fatalf("state = %d, want 0 (ACTIVE)", state)
	}
}

func TestActivePlayerGlyphValuesUpdate(t *testing.T) {
	fields := map[int]uint32{legacyPlayerCoinage: 123, legacyPlayerGlyphsEnabled: 0x3f}
	for slot := 0; slot < 6; slot++ {
		fields[legacyPlayerGlyphSlot1+slot] = uint32(21 + slot)
		fields[legacyPlayerGlyph1+slot] = uint32(531 + slot)
	}
	body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 7,
		Values: LegacyUpdateValuesBlock{Fields: fields},
	}, ValuesUpdateOptions{
		ObjectType: 4, Active: true, GUID: ModernGUIDForLegacy(7, 0), Fields: fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := valuesUpdatePayload(t, body)
	if binary.LittleEndian.Uint32(payload) != 0x80 {
		t.Fatalf("glyph update must contain ActivePlayer section: %x", payload)
	}
	r := movementReader{data: payload[4:]}
	mask0, _ := r.u32()
	mask1, _ := r.bits(16)
	block0, _ := r.bits(32)
	block3, _ := r.bits(32)
	block47, _ := r.bits(32)
	if mask0 != 9 || mask1 != 0x8000 || block0 != 0x10000001 || block3 != 0x01000040 || block47 != 0x001fff00 {
		t.Fatalf("glyph masks: %x/%x blocks %x/%x/%x", mask0, mask1, block0, block3, block47)
	}
	r.align()
	if coin, err := r.u64(); err != nil || coin != 123 {
		t.Fatalf("coin=%d err=%v", coin, err)
	}
	if enabled, err := r.u8(); err != nil || enabled != 0x3f {
		t.Fatalf("enabled=%x err=%v", enabled, err)
	}
	if hasStable, err := r.bits(1); err != nil || hasStable != 0 {
		t.Fatalf("HasPetStable=%d err=%v", hasStable, err)
	}
	r.align()
	// WPP343 reads GlyphSlots[i] and Glyphs[i] together. Distinct values
	// make the old array-by-array writer fail here rather than silently swap IDs.
	for slot := uint32(0); slot < 6; slot++ {
		for _, base := range []uint32{21, 531} {
			if got, err := r.u32(); err != nil || got != base+slot {
				t.Fatalf("glyph slot=%d base=%d got=%d err=%v", slot, base, got, err)
			}
		}
	}
	if r.remaining() != 0 {
		t.Fatalf("glyph update has %d trailing bytes", r.remaining())
	}
}

func TestActivePlayerGlyphRemovalValuesUpdate(t *testing.T) {
	for slot := 0; slot < 6; slot++ {
		changed := map[int]uint32{legacyPlayerGlyph1 + slot: 0}
		fields := cloneFields(changed)
		fields[legacyPlayerGlyph1+(slot+1)%6] = 531 // unchanged socket must stay untouched
		section := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 0)
		r := movementReader{data: section}
		mask0, _ := r.u32()
		mask1, _ := r.bits(16)
		block47, _ := r.bits(32)
		if mask0 != 0 || mask1 != 0x8000 || block47 != 1<<8|1<<(15+slot) {
			t.Fatalf("clear socket %d masks=%x/%x block=%x", slot, mask0, mask1, block47)
		}
		r.align()
		if glyph, err := r.u32(); err != nil || glyph != 0 || r.remaining() != 0 {
			t.Fatalf("clear socket %d glyph=%d remaining=%d err=%v", slot, glyph, r.remaining(), err)
		}
	}
	changed := map[int]uint32{legacyPlayerGlyphsEnabled: 0}
	section := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 0)
	// High block mask empty, block 3 contains parent 102 and field 120,
	// followed by GlyphsEnabled and the padded HasPetStable bit.
	want := []byte{8, 0, 0, 0, 0, 0, 1, 0, 0, 0x40, 0, 0}
	if !bytes.Equal(section, want) {
		t.Fatalf("clear enabled=%x want=%x", section, want)
	}
}

func TestActivePlayerNoReagentCostValuesUpdate(t *testing.T) {
	changed := map[int]uint32{legacyPlayerNoReagentCost1 + 2: 1}
	section := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 0)
	r := movementReader{data: section}
	mask0, _ := r.u32()
	mask1, _ := r.bits(16)
	block19, _ := r.bits(32)
	// Parent 615 and element 618 share block 19 (bits 7 and 10).
	if mask0 != 1<<19 || mask1 != 0 || block19 != 1<<7|1<<10 {
		t.Fatalf("levitate mask=%x/%x block=%x", mask0, mask1, block19)
	}
	r.align()
	if got, err := r.u32(); err != nil || got != 1 || r.remaining() != 0 {
		t.Fatalf("mask word=%d remaining=%d err=%v", got, r.remaining(), err)
	}

	cleared := map[int]uint32{legacyPlayerNoReagentCost1 + 2: 0}
	clearedSection := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: cleared}, cleared, 0)
	if len(clearedSection) == 0 {
		t.Fatal("clearing the reagent mask must still reach the client")
	}

	all := map[int]uint32{
		legacyPlayerNoReagentCost1:     0x11,
		legacyPlayerNoReagentCost1 + 1: 0x22,
		legacyPlayerNoReagentCost1 + 2: 1,
		legacyPlayerGlyph1 + 4:         0x1cb,
	}
	combined := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: all}, all, 0)
	r = movementReader{data: combined}
	mask0, _ = r.u32()
	mask1, _ = r.bits(16)
	block19, _ = r.bits(32)
	block47, _ := r.bits(32)
	if mask0 != 1<<19 || mask1 != 0x8000 || block19 != 1<<7|1<<8|1<<9|1<<10 || block47 != 1<<8|1<<(15+4) {
		t.Fatalf("combined masks=%x/%x blocks=%x/%x", mask0, mask1, block19, block47)
	}
	r.align()
	for _, want := range []uint32{0x11, 0x22, 1, 0x1cb} {
		if got, err := r.u32(); err != nil || got != want {
			t.Fatalf("payload=%d want=%d err=%v", got, want, err)
		}
	}
	if r.remaining() != 0 {
		t.Fatalf("combined update has %d trailing bytes", r.remaining())
	}
}

func TestActivePlayerCreateWritesNoReagentCostMask(t *testing.T) {
	empty := legacyActivePlayerValues{}.appendActivePlayer(nil, 571, nil)
	masked := legacyActivePlayerValues{fields: map[int]uint32{
		legacyPlayerNoReagentCost1:     0x11,
		legacyPlayerNoReagentCost1 + 1: 0x22,
		legacyPlayerNoReagentCost1 + 2: 1,
	}}.appendActivePlayer(nil, 571, nil)
	if len(empty) != len(masked) || len(empty) != 14253 {
		t.Fatalf("CREATE length empty=%d masked=%d want 14253", len(empty), len(masked))
	}
	pos := -1
	for i := range empty {
		if empty[i] != masked[i] {
			pos = i
			break
		}
	}
	want := []byte{0x11, 0, 0, 0, 0x22, 0, 0, 0, 1, 0, 0, 0}
	if pos < 0 || pos+16 > len(masked) || !bytes.Equal(masked[pos:pos+12], want) {
		t.Fatalf("CREATE reagent mask at %d = %x, want %x", pos, masked[pos:pos+12], want)
	}
	if !bytes.Equal(masked[pos+12:pos+16], empty[pos+12:pos+16]) {
		t.Fatal("fourth NoReagentCostMask word must stay zero")
	}
	for i := pos + 12; i < len(empty); i++ {
		if empty[i] != masked[i] {
			t.Fatalf("CREATE reagent mask spilled to offset %d", i)
		}
	}
}

func TestEncodeActivePlayerCharacterSheetValuesUpdate(t *testing.T) {
	fields := map[int]uint32{
		legacyPlayerExpertise:         7,
		legacyPlayerBlockPercent:      math.Float32bits(25.5),
		legacyPlayerCritPercent:       math.Float32bits(18.75),
		legacyPlayerSpellCrit1:        math.Float32bits(6.5),
		legacyPlayerDamagePos1:        33,
		legacyPlayerCombatRating1 + 5: 44, // Melee hit rating.
	}
	section := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 0)
	r := movementReader{data: section}
	blocksMask, err := r.u32()
	if err != nil || blocksMask != 1<<0|1<<1|1<<8|1<<17|1<<18 {
		t.Fatalf("blocks mask=%#x err=%v", blocksMask, err)
	}
	mask1, _ := r.bits(16)
	block0, _ := r.bits(32)
	block1, _ := r.bits(32)
	block8, _ := r.bits(32)
	block17, _ := r.bits(32)
	block18, _ := r.bits(32)
	if mask1 != 0 || block0 != 1 || block1 != 1<<4|1<<6|1<<9|1<<14 ||
		block8 != 1<<13|1<<14|1<<21 || block17 != 1<<30 || block18 != 1<<4 {
		t.Fatalf("active masks mask1=%#x blocks=%#x/%#x/%#x/%#x/%#x", mask1, block0, block1, block8, block17, block18)
	}
	r.align()
	for _, want := range []uint32{
		math.Float32bits(7), math.Float32bits(25.5), math.Float32bits(18.75),
		math.Float32bits(6.5), 33, 44,
	} {
		got, readErr := r.u32()
		if readErr != nil || got != want {
			t.Fatalf("active character-sheet value=%#x want=%#x err=%v", got, want, readErr)
		}
	}
	if r.remaining() != 0 {
		t.Fatalf("active character-sheet delta has %d trailing bytes", r.remaining())
	}
}

func TestEncodeCreatureValuesUpdateClearsLootableDynamicFlag(t *testing.T) {
	changed := map[int]uint32{legacyUnitDynamicFlags: 0}
	body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0xf130000001000042,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 571, ObjectType: 3,
		GUID: ModernGUIDForLegacy(0xf130000001000042, 571), Fields: changed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if translated != 1 {
		t.Fatalf("translated fields = %d, want 1", translated)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x01 {
		t.Fatalf("changed mask = 0x%x, want object section only", got)
	}
	if len(values) != 9 || values[4] != 0x50 {
		t.Fatalf("object delta = %x, want dynamic-flags mask and uint32", values[4:])
	}
	if got := binary.LittleEndian.Uint32(values[5:]); got != 0 {
		t.Fatalf("modern dynamic flags = 0x%x, want cleared", got)
	}
}

func TestEncodeActivePlayerInventoryAndCoinageUpdate(t *testing.T) {
	itemGUID := uint64(0x4000000000000043)
	changed := map[int]uint32{
		legacyPlayerCoinage:         123456,
		legacyPlayerInvSlotHead:     uint32(itemGUID),
		legacyPlayerInvSlotHead + 1: uint32(itemGUID >> 32),
	}
	body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0x42,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 571, ObjectType: 4, Active: true,
		GUID: ModernGUIDForLegacy(0x42, 571), Fields: changed,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x80 {
		t.Fatalf("changed mask = 0x%x, want 0x80", got)
	}
	section := values[4:]
	if got := binary.LittleEndian.Uint32(section); got != 0x09 {
		t.Fatalf("ActivePlayer blocks mask = 0x%x, want 0x9", got)
	}
	if string(section[6:10]) != string([]byte{0x10, 0, 0, 1}) || string(section[10:14]) != string([]byte{0x30, 0, 0, 0}) {
		t.Fatalf("ActivePlayer block words = %x", section[6:14])
	}
	position := 14
	if got := binary.LittleEndian.Uint64(section[position:]); got != 123456 {
		t.Fatalf("coinage = %d, want 123456", got)
	}
	position += 8
	low, high, _, err := readPackedGUID128(section[position:])
	if err != nil {
		t.Fatal(err)
	}
	want := ModernGUIDForLegacy(itemGUID, 571)
	if low != want.Low || high != want.High {
		t.Fatalf("inventory GUID = %016x:%016x, want %016x:%016x", high, low, want.High, want.Low)
	}
}

func TestVisibleItemEnchantDeltaUsesItemVisual(t *testing.T) {
	base := legacyPlayerVisibleItem1
	changed := map[int]uint32{base + 1: 3273}
	fields := map[int]uint32{base: 12345, base + 1: 3273}
	payload := encodePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 0, nil, false)
	if len(payload) < 9 {
		t.Fatalf("visible-item delta too short: %x", payload)
	}
	item := payload[len(payload)-9:]
	if item[0] != 0xf0 {
		t.Fatalf("visible-item mask byte=%x, want 0xf0 (4-bit 0x0f flushed)", item[0])
	}
	if binary.LittleEndian.Uint32(item[1:]) != 12345 || binary.LittleEndian.Uint16(item[5:]) != 0 || binary.LittleEndian.Uint16(item[7:]) != 166 {
		t.Fatalf("visible-item payload=%x", item)
	}
}

func TestItemEnchantmentDeltaUsesSixBitMask(t *testing.T) {
	// Temp-enchant slot 1: ID + duration + charges. legacy proxy writes those three
	// field bits in a 6-bit ItemEnchantment mask (byte 0x3c after flush).
	base := legacyItemEnchantment + 3
	changed := map[int]uint32{base: 13, base + 1: 3600000, base + 2: 0}
	fields := map[int]uint32{base: 13, base + 1: 3600000, base + 2: 0}
	payload := encodeItemValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 0)
	if len(payload) < 11 {
		t.Fatalf("item-enchantment delta too short: %x", payload)
	}
	body := payload[len(payload)-11:]
	if body[0] != 0x3c {
		t.Fatalf("enchantment mask byte=%x, want 0x3c (6-bit 0x0f flushed)", body[0])
	}
	if binary.LittleEndian.Uint32(body[1:]) != 13 || binary.LittleEndian.Uint32(body[5:]) != 3600000 || binary.LittleEndian.Uint16(body[9:]) != 0 {
		t.Fatalf("enchantment payload=%x", body)
	}
}

func TestActivePlayerVisibleItemEnchantDeltaIsSkipped(t *testing.T) {
	// AzerothCore pushes the owner's PLAYER_VISIBLE_ITEM enchant on every
	// whetstone. That owner-only visual update disconnects 3.4.3 (error 7).
	base := legacyPlayerVisibleItem1
	changed := map[int]uint32{base + 1: 3273, 70: 0x42c8a3d6, 71: 0x430947ae}
	fields := map[int]uint32{base: 12345, base + 1: 3273, 70: 0x42c8a3d6, 71: 0x430947ae}
	section := encodePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 0, nil, true)
	if len(section) != 0 {
		t.Fatalf("active player enchant-only change produced a Player section: %x", section)
	}
}

func TestActivePlayerVisibleItemIDDeltaIsForwarded(t *testing.T) {
	base := legacyPlayerVisibleItem1
	changed := map[int]uint32{base: 19019, base + 1: 0}
	fields := map[int]uint32{base: 19019, base + 1: 0}
	section := encodePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 0, nil, true)
	if len(section) == 0 {
		t.Fatal("active player ItemID change produced no Player section")
	}
}

func TestUnitVirtualItemValuesDelta(t *testing.T) {
	changed := map[int]uint32{legacyUnitVirtualItem1: 19019}
	fields := map[int]uint32{legacyUnitVirtualItem1: 19019}
	section := encodeUnitValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 4, 0)
	if len(section) == 0 {
		t.Fatal("virtual-item change produced no Unit section")
	}
}

func TestEncodeActivePlayerSkillUpdate(t *testing.T) {
	changed := map[int]uint32{legacyPlayerSkill1 + 1: uint32(300)<<16 | 151}
	body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0x42,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 0, ObjectType: 4, Active: true,
		GUID: ModernGUIDForLegacy(0x42, 0), Fields: changed,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := valuesUpdatePayload(t, body)
	section := values[4:]
	r := movementReader{data: section}
	mask0, err := r.u32()
	if err != nil || mask0 != 3 {
		t.Fatalf("active blocks mask=%x err=%v", mask0, err)
	}
	mask1, _ := r.bits(16)
	block0, _ := r.bits(32)
	block1, _ := r.bits(32)
	r.align()
	if mask1 != 0 || block0 != 1 || block1 != 1 {
		t.Fatalf("active masks mask1=%x blocks=%x/%x", mask1, block0, block1)
	}
	skillMask0, err := r.u32()
	if err != nil || skillMask0 != 0x00010001 {
		t.Fatalf("skill mask0=%x err=%v", skillMask0, err)
	}
	skillMask1, _ := r.bits(25)
	root, _ := r.bits(32)
	rankMask, _ := r.bits(32)
	maxMask, _ := r.bits(32)
	r.align()
	if skillMask1 != 1 || root != 1 || rankMask != 2 || maxMask != 2 {
		t.Fatalf("skill masks mask1=%x root=%x rank=%x max=%x", skillMask1, root, rankMask, maxMask)
	}
	rank, _ := r.u16()
	maxRank, _ := r.u16()
	if rank != 151 || maxRank != 300 || r.remaining() != 0 {
		t.Fatalf("skill rank=%d max=%d remaining=%d", rank, maxRank, r.remaining())
	}
}

func TestEncodeItemAndContainerValuesUpdates(t *testing.T) {
	fields := map[int]uint32{
		legacyItemDurability:     25,
		legacyContainerNumSlots:  16,
		legacyContainerSlot1:     0x44,
		legacyContainerSlot1 + 1: 0x40000000,
	}
	body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0x4000000000000043,
		Values: LegacyUpdateValuesBlock{Fields: fields},
	}, ValuesUpdateOptions{
		MapID: 571, ObjectType: 2,
		GUID: ModernGUIDForLegacy(0x4000000000000043, 571), Fields: fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x06 {
		t.Fatalf("changed mask = 0x%x, want item+container", got)
	}
	if len(values) < 20 {
		t.Fatalf("item/container delta is only %d bytes", len(values))
	}
}

func TestEncodeValuesUpdateDropsUnsupportedDelta(t *testing.T) {
	body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{9999: 1}},
	}, ValuesUpdateOptions{ObjectType: 6, GUID: GUID128{Low: 1}, Fields: map[int]uint32{9999: 1}})
	if err != nil || body != nil || translated != 0 {
		t.Fatalf("unsupported delta returned body=%x translated=%d err=%v", body, translated, err)
	}
}

func TestActivePlayerCreateIncludesInventoryGUIDs(t *testing.T) {
	itemGUID := uint64(0x4000000000000043)
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyPlayerInvSlotHead:     uint32(itemGUID),
		legacyPlayerInvSlotHead + 1: uint32(itemGUID >> 32),
	}}
	body := values.appendActivePlayer(nil, 571, nil)
	low, high, _, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	want := ModernGUIDForLegacy(itemGUID, 571)
	if low != want.Low || high != want.High {
		t.Fatalf("first inventory GUID = %016x:%016x, want %016x:%016x", high, low, want.High, want.Low)
	}
}

func valuesUpdatePayload(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 20 || binary.LittleEndian.Uint32(body) != 1 {
		t.Fatalf("bad UpdateObject body: %x", body)
	}
	object := body[11:]
	if object[0] != 0 {
		t.Fatalf("modern update type = %d, want Values", object[0])
	}
	_, _, guidBytes, err := readPackedGUID128(object[1:])
	if err != nil {
		t.Fatal(err)
	}
	position := 1 + guidBytes
	length := int(binary.LittleEndian.Uint32(object[position:]))
	position += 4
	if length != len(object)-position {
		t.Fatalf("values length = %d, remaining %d", length, len(object)-position)
	}
	return object[position:]
}

func TestGhostHealthValuesUpdate(t *testing.T) {
	changed := map[int]uint32{legacyUnitHealth: 0}
	fields := map[int]uint32{
		legacyUnitHealth:  0,
		legacyPlayerFlags: legacyPlayerFlagGhost,
	}
	body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0x42,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 0, ObjectType: 4, GUID: GUID128{Low: 0x42, High: uint64(2)<<58 | uint64(1)<<42},
		Fields: fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	if translated != 1 {
		t.Fatalf("translated fields = %d, want 1", translated)
	}
	values := valuesUpdatePayload(t, body)
	if len(values) < 16 {
		t.Fatalf("values too short: %x", values)
	}
	found := false
	for i := 0; i+8 <= len(values); i++ {
		if binary.LittleEndian.Uint64(values[i:i+8]) == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ghost health 1 not in values update: %x", values)
	}
}

func TestGhostPlayerFlagsValuesUpdate(t *testing.T) {
	for _, test := range []struct {
		name  string
		flags uint32
	}{
		{name: "enter ghost", flags: legacyPlayerFlagGhost},
		{name: "leave ghost", flags: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := map[int]uint32{legacyPlayerFlags: test.flags}
			section := encodePlayerValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 0, nil, false)
			r := movementReader{data: section}
			blocksMask, err := r.bits(4)
			if err != nil || blocksMask != 1 {
				t.Fatalf("blocks mask = %#x, err=%v", blocksMask, err)
			}
			block0, err := r.bits(32)
			if err != nil || block0 != (1|1<<7|1<<8) {
				t.Fatalf("block 0 = %#x, err=%v", block0, err)
			}
			noQuestLogMask, err := r.bits(1)
			if err != nil || noQuestLogMask != 1 {
				t.Fatalf("no-quest-log-mask = %d, err=%v", noQuestLogMask, err)
			}
			r.align()
			flags, err := r.u32()
			if err != nil || flags != test.flags {
				t.Fatalf("modern player flags = %#x, want %#x, err=%v", flags, test.flags, err)
			}
			flagsEx, err := r.u32()
			if err != nil || flagsEx != 0 || r.remaining() != 0 {
				t.Fatalf("flagsEx=%#x remaining=%d err=%v", flagsEx, r.remaining(), err)
			}
		})
	}
}

func TestModernUnitBytes1PutsStealthOnAnimTier(t *testing.T) {
	stand, pet, vis, anim := modernUnitBytes1(0x20000)
	if stand != 0 || pet != 0 || vis != 2 || anim != 2 {
		t.Fatalf("creep bytes1 = %d %d %d %d, want 0 0 2 2", stand, pet, vis, anim)
	}
	stand, pet, vis, anim = modernUnitBytes1(0)
	if stand != 0 || pet != 0 || vis != 0 || anim != 0 {
		t.Fatalf("clear bytes1 = %d %d %d %d", stand, pet, vis, anim)
	}
}

func TestModernUnitBytes1MapsHoverBitfield(t *testing.T) {
	_, _, _, anim := modernUnitBytes1(0x02000000)
	if anim != 2 {
		t.Fatalf("HOVER bitfield AnimTier=%d, want 2", anim)
	}
	_, _, _, anim = modernUnitBytes1(0x03000000)
	if anim != 2 {
		t.Fatalf("ALWAYS_STAND|HOVER AnimTier=%d, want Hover 2", anim)
	}
	_, _, _, anim = modernUnitBytes1(0x04000000)
	if anim != 3 {
		t.Fatalf("fly bitfield AnimTier=%d, want Fly 3", anim)
	}
}

func TestStealthVisFlagsValuesUpdate(t *testing.T) {
	const creep = uint32(0x20000) // UNIT_FIELD_BYTES_1 vis flag 0x02
	changed := map[int]uint32{legacyUnitBytes1: creep}
	body, translated, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0x42,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 0, ObjectType: 4, Active: true,
		GUID:   GUID128{Low: 0x42, High: uint64(2)<<58 | uint64(1)<<42},
		Fields: changed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if translated != 1 {
		t.Fatalf("translated fields = %d, want 1", translated)
	}
	values := valuesUpdatePayload(t, body)
	if binary.LittleEndian.Uint32(values)&0x20 == 0 {
		t.Fatalf("changed mask 0x%x missing unit section", binary.LittleEndian.Uint32(values))
	}
	found := false
	for index := 0; index+4 <= len(values); index++ {
		if values[index] == 0 && values[index+1] == 0 && values[index+2] == 2 && values[index+3] == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("stand/vis/anim bytes not 00 00 02 02: %x", values)
	}
}

func TestStealthValuesAreUnitOnly(t *testing.T) {
	changed := map[int]uint32{
		legacyUnitBytes1:        0x20001,
		legacyUnitDynamicFlags:  0,
		legacyUnitNPCFlags:      0,
		legacyUnitBytes2:        0x1e000000,
		legacyPlayerFieldBytes2: 0x20000000,
	}
	body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0x42,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		MapID: 0, ObjectType: 4, Active: true,
		GUID:   GUID128{Low: 0x42, High: uint64(2)<<58 | uint64(1)<<42},
		Fields: changed,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x20 {
		t.Fatalf("stealth changed mask = 0x%x, want unit-only 0x20", got)
	}
	foundForm := false
	for index := 0; index+4 <= len(values); index++ {
		if values[index] == 0 && values[index+1] == 0 && values[index+2] == 0 && values[index+3] == 0x1e {
			foundForm = true
		}
	}
	if !foundForm {
		t.Fatalf("shapeshift form missing from payload=%x", values)
	}
}

func TestUnitDeltaMarksArrayParentBits(t *testing.T) {
	// NpcFlags[0] is bit 114 under parent 113, not under the block-aligned
	// parent 96 that the first four blocks use.
	if got := unitParentBit(114); got != 113 {
		t.Fatalf("parent of NpcFlags[0] = %d, want 113", got)
	}
	if got := unitParentBit(unitPower0Bit); got != unitPowerParentBit {
		t.Fatalf("parent of Power[0] = %d, want %d", got, unitPowerParentBit)
	}
	changed := map[int]uint32{legacyUnitNPCFlags: 3}
	payload := encodeUnitValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 3, 0)
	if payload[0] != 1<<3 {
		t.Fatalf("npc-flags blocks = 0x%x, want block 3 only", payload[0])
	}
	block := binary.BigEndian.Uint32(payload[1:])
	if want := uint32(1<<(113-96) | 1<<(114-96)); block != want {
		t.Fatalf("npc-flags block3 = 0x%08x, want 0x%08x", block, want)
	}

	cleared := map[int]uint32{legacyUnitNPCFlags: 0}
	payload = encodeUnitValuesDelta(legacyActivePlayerValues{fields: cleared}, cleared, 3, 0)
	if len(payload) != 9 || !bytes.Equal(payload[len(payload)-4:], make([]byte, 4)) {
		t.Fatalf("cleared creature npc-flags payload = %x, want mask plus zero value", payload)
	}
	if playerPayload := encodeUnitValuesDelta(legacyActivePlayerValues{fields: cleared}, cleared, 4, 0); len(playerPayload) != 0 {
		t.Fatalf("routine player npc-flags zero was not suppressed: %x", playerPayload)
	}
}

func TestAuraVisionActivePlayerDelta(t *testing.T) {
	// PLAYER_FIELD_BYTES2 must not open an ActivePlayer Values section.
	// Stealth always sends 1229, and that extra section is what made the
	// client drop the whole blob (Unit sit/visflags included).
	const stealth = uint32(0x20000000)
	changed := map[int]uint32{legacyPlayerFieldBytes2: stealth | 0x0700}
	payload := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 0)
	if len(payload) != 0 {
		t.Fatalf("aura-vision Values delta = %x, want empty", payload)
	}
}

func TestOverrideSpellsIDActivePlayerDelta(t *testing.T) {
	// Blood Queen Frenzied Bloodthirst writes override set 241 in the low
	// uint16 of PLAYER_FIELD_BYTES2 (0x040000f1). Extra Action reads
	// ActivePlayerData.OverrideSpellsID (bit 105, parent 102), not AuraVision.
	const frenzy = uint32(0x040000f1)
	changed := map[int]uint32{legacyPlayerFieldBytes2: frenzy}
	payload := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 0)
	r, mask := readBatchActiveMask(t, payload)
	if mask[playerAuraVisionParentBit/32]&(1<<(playerAuraVisionParentBit%32)) == 0 {
		t.Fatalf("missing parent 102: block3=%#x", mask[3])
	}
	if mask[playerOverrideSpellsBit/32]&(1<<(playerOverrideSpellsBit%32)) == 0 {
		t.Fatalf("missing OverrideSpellsID bit 105: block3=%#x", mask[3])
	}
	if mask[playerAuraVisionBit/32]&(1<<(playerAuraVisionBit%32)) != 0 {
		t.Fatalf("AuraVision bit 103 must stay clear: block3=%#x", mask[3])
	}
	got, err := r.u32()
	if err != nil || got != 241 {
		t.Fatalf("OverrideSpellsID=%d want 241 err=%v", got, err)
	}
	hasStable, err := r.bits(1)
	if err != nil || hasStable != 0 {
		t.Fatalf("HasPetStable=%d err=%v", hasStable, err)
	}
	r.align()
	if r.remaining() != 0 {
		t.Fatalf("override-spells delta has %d trailing bytes", r.remaining())
	}

	cleared := map[int]uint32{legacyPlayerFieldBytes2: 0x04000000}
	clearPayload := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: cleared}, cleared, 0)
	clearR, _ := readBatchActiveMask(t, clearPayload)
	clearID, err := clearR.u32()
	if err != nil || clearID != 0 {
		t.Fatalf("cleared OverrideSpellsID=%d err=%v", clearID, err)
	}
}

func TestEmitOverrideSpellsIDDelta(t *testing.T) {
	if !emitOverrideSpellsIDDelta(0x040000f1) {
		t.Fatal("frenzy override id must emit")
	}
	if emitOverrideSpellsIDDelta(legacyPlayerFieldBytes2Stealth) {
		t.Fatal("stealth AuraVision must not emit")
	}
	if emitOverrideSpellsIDDelta(legacyPlayerFieldBytes2Stealth | 0x0700) {
		t.Fatal("stealth plus byte-1 AuraVision must not emit")
	}
	if !emitOverrideSpellsIDDelta(0x04000000) {
		t.Fatal("cleared override id must emit zero")
	}
}

func TestDeathLocalFlagsActivePlayerDelta(t *testing.T) {
	for _, test := range []struct {
		name    string
		changed map[int]uint32
		fields  map[int]uint32
		want    uint32
	}{
		{
			name:    "resurrection restores release window with unchanged player bytes",
			changed: map[int]uint32{legacyPlayerFlags: 1},
			fields: map[int]uint32{
				legacyPlayerFieldBytes: 0x170008,
				legacyPlayerFlags:      1,
				legacyUnitFlags:        8,
			},
			want: modernLocalFlagReleaseTimer,
		},
		{
			name:    "death enables release spirit",
			changed: map[int]uint32{legacyPlayerFieldBytes: 0x1f0008},
			fields:  map[int]uint32{legacyPlayerFieldBytes: 0x1f0008},
			want:    modernLocalFlagReleaseTimer,
		},
		{
			name:    "entering ghost closes release window",
			changed: map[int]uint32{legacyPlayerFlags: legacyPlayerFlagGhost},
			fields: map[int]uint32{
				legacyPlayerFieldBytes: 0x1f0008,
				legacyPlayerFlags:      legacyPlayerFlagGhost,
			},
			want: modernLocalFlagNoReleaseWindow,
		},
		{
			name:    "unit ghost flag also closes release window",
			changed: map[int]uint32{legacyUnitFlags: legacyUnitFlagGhost | 0x8},
			fields: map[int]uint32{
				legacyPlayerFieldBytes: 0x1f0008,
				legacyUnitFlags:        legacyUnitFlagGhost | 0x8,
			},
			want: modernLocalFlagNoReleaseWindow,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: test.fields}, test.changed, 0)
			r := movementReader{data: payload}
			mask0, err := r.u32()
			if err != nil || mask0 != 0x06 {
				t.Fatalf("blocks mask = %#x, want blocks 1 and 2, err=%v", mask0, err)
			}
			mask1, _ := r.bits(16)
			parent, _ := r.bits(32)
			localFlags, _ := r.bits(32)
			r.align()
			wantMask := uint32(1 << (69 - 64))
			_, packedChanged := test.changed[legacyPlayerFieldBytes]
			if packedChanged {
				wantMask |= 1<<(70-64) | 1<<(71-64) | 1<<(72-64) | 1<<(73-64)
			}
			if mask1 != 0 || parent != 1<<(38-32) || localFlags != wantMask {
				t.Fatalf("mask1=%#x parent=%#x local-flags-mask=%#x", mask1, parent, localFlags)
			}
			got, err := r.u32()
			if packedChanged {
				for _, want := range []byte{0, 0x1f, 0} {
					v, e := r.u8()
					if e != nil || v != want {
						t.Fatalf("packed bytes got=%d want=%d", v, want)
					}
				}
			}
			if err != nil || got != test.want || r.remaining() != 0 {
				t.Fatalf("LocalFlags=%#x, want %#x, remaining=%d, err=%v", got, test.want, r.remaining(), err)
			}
		})
	}
}

func TestWatchedFactionActivePlayerDelta(t *testing.T) {
	// Watching a faction makes AzerothCore echo PLAYER_FIELD_WATCHED_FACTION_INDEX
	// (1230). It must land on the modern WatchedFactionIndex descriptor (bit 92,
	// grouped under parent bit 70) so the portrait rep bar appears immediately
	// instead of only after a relog re-sends the CREATE.
	changed := map[int]uint32{legacyPlayerWatchedFaction: 9}
	payload := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 0)
	r := movementReader{data: payload}
	mask0, err := r.u32()
	if err != nil || mask0 != 1<<2 {
		t.Fatalf("mask0=0x%x err=%v", mask0, err)
	}
	mask1, _ := r.bits(16)
	block2, _ := r.bits(32)
	r.align()
	// block 2 covers descriptor bits 64..95: parent 70 at bit 6 and the watched
	// faction value at bit 92 (= 92-64 = 28).
	if mask1 != 0 || block2 != uint32(1<<6|1<<28) {
		t.Fatalf("mask1=0x%x block2=0x%x", mask1, block2)
	}
	got, err := r.u32()
	if err != nil || got != 9 || r.remaining() != 0 {
		t.Fatalf("watched faction=%d, want 9, remaining=%d err=%v", got, r.remaining(), err)
	}
}

func TestBuybackPriceAndTimestampActivePlayerDelta(t *testing.T) {
	changed := map[int]uint32{
		legacyPlayerBuybackPrice1: 12345,
		legacyPlayerBuybackTime1:  67890,
	}
	payload := encodeActivePlayerValuesDelta(legacyActivePlayerValues{fields: changed}, changed, 0)
	r := movementReader{data: payload}
	mask0, err := r.u32()
	if err != nil || mask0 != 1<<17 {
		t.Fatalf("mask0=0x%x err=%v", mask0, err)
	}
	mask1, _ := r.bits(16)
	block17, _ := r.bits(32)
	r.align()
	wantBlock := uint32(1<<5 | 1<<6 | 1<<18) // parent 549, price 550, timestamp 562
	if mask1 != 0 || block17 != wantBlock {
		t.Fatalf("mask1=0x%x block17=0x%x want=0x%x", mask1, block17, wantBlock)
	}
	price, _ := r.u32()
	timestamp, _ := r.u64()
	if price != 12345 || timestamp != 67890 || r.remaining() != 0 {
		t.Fatalf("price=%d timestamp=%d remaining=%d", price, timestamp, r.remaining())
	}
}

func TestTransportStateTransitionsUpdateDynamicFlags(t *testing.T) {
	for _, guid := range []uint64{0x1fc0000000000015, 0xf120000000000001} {
		for _, state := range []uint32{0, 1, 0} {
			fields := map[int]uint32{legacyGameObjectBytes1: 0xff000f00 | state, legacyGameObjectDynamic: 0x12340001}
			for _, changed := range []map[int]uint32{
				{legacyGameObjectBytes1: fields[legacyGameObjectBytes1]},
				{legacyGameObjectDynamic: fields[legacyGameObjectDynamic]},
			} {
				body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{Type: LegacyUpdateValues, GUID: guid, Values: LegacyUpdateValuesBlock{Fields: changed}},
					ValuesUpdateOptions{MapID: 631, ObjectType: 5, GUID: ModernGUIDForLegacy(guid, 631), Fields: fields})
				if err != nil {
					t.Fatal(err)
				}
				payload := valuesUpdatePayload(t, body)
				want := uint32(0x12340104)
				if state == 1 {
					want = 0x12340044
				}
				if len(payload) < 9 || payload[4] != 0x50 || binary.LittleEndian.Uint32(payload[5:]) != want {
					t.Fatalf("state %d delta %x want flags %x", state, payload, want)
				}
			}
		}
	}
}
