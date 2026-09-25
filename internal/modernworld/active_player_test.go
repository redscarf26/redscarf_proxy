package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestAppendUnitCarriesControlGUIDs(t *testing.T) {
	const mapID = uint16(571)
	legacyGUIDs := []uint64{
		0xf130000001000041, // Charm
		0xf130000001000042, // Summon
		0xf130000001000043, // Critter (owner only)
		0x0000000000000044, // CharmedBy
		0x0000000000000045, // SummonedBy
		0x0000000000000046, // CreatedBy
	}
	fields := map[int]uint32{}
	for index, field := range []int{
		legacyUnitCharm, legacyUnitSummon, legacyUnitCritter,
		legacyUnitCharmedBy, legacyUnitSummonedBy, legacyUnitCreatedBy,
	} {
		fields[field] = uint32(legacyGUIDs[index])
		fields[field+1] = uint32(legacyGUIDs[index] >> 32)
	}
	values := legacyActivePlayerValues{fields: fields}
	body := values.appendUnit(nil, true, false, mapID)
	position := 8 + 8 + 4 + 4 + 5*4
	for index, legacy := range legacyGUIDs {
		low, high, consumed, err := readPackedGUID128(body[position:])
		want := ModernGUIDForLegacy(legacy, mapID)
		if err != nil || low != want.Low || high != want.High {
			t.Fatalf("control GUID %d=%016x:%016x want=%016x:%016x err=%v", index, high, low, want.High, want.Low, err)
		}
		position += consumed
	}
	for index := 0; index < 2; index++ { // DemonCreator, LookAtControllerTarget
		low, high, consumed, err := readPackedGUID128(body[position:])
		if err != nil || low != 0 || high != 0 {
			t.Fatalf("placeholder GUID %d=%016x:%016x err=%v", index, high, low, err)
		}
		position += consumed
	}
}

func TestCreatureGUIDOmitsMapBits(t *testing.T) {
	// Reference capture update-001038-171.bin creates the
	// Skybreaker's deck turret (legacy 0xf110031491000068, entry 201873) on map
	// 631 as High=0x2c00040000c52440, Low=0x68. legacy proxy leaves the map field
	// (bits 29-41) clear, and HermesProxy's Creature/GameObject call sites pass
	// a spawn counter there instead of a map.
	guid := ModernGUIDForLegacy(0xf110031491000068, 631)
	if want := uint64(0x2c00040000c52440); guid.High != want {
		t.Fatalf("turret high GUID = %016x, want %016x", guid.High, want)
	}
	if guid.Low != 0x68 {
		t.Fatalf("turret low GUID = %016x, want 0x68", guid.Low)
	}
	if bits := guid.High >> 29 & 0x1fff; bits != 0 {
		t.Fatalf("creature GUID carries map bits %#x", bits)
	}
}

func TestEncodeActivePlayerCreateStructure(t *testing.T) {
	fields := map[int]uint32{
		legacyObjectScale:           0x3f800000,
		legacyUnitBytes0:            0x010001,
		legacyUnitHealth:            900,
		legacyUnitMaxHealth:         1000,
		legacyUnitLevel:             80,
		legacyUnitDisplayID:         49,
		legacyUnitNativeDisplayID:   49,
		legacyUnitFaction:           1,
		legacyUnitBoundingRadius:    0x3ec72b02,
		legacyUnitCombatReach:       0x3fc00000,
		legacyUnitMaxHealthModifier: 0x3f800000,
		legacyUnitHoverHeight:       0x3f800000,
		legacyPlayerBytes:           0x02030100,
		legacyPlayerBytes2:          0x02010004,
		legacyPlayerBytes3:          0,
		legacyPlayerXP:              12345,
		legacyPlayerNextLevelXP:     1680000,
		legacyPlayerMaxLevel:        80,
	}
	move := &LegacyMovement{
		UpdateFlags:     legacyUpdateSelf | legacyUpdateLiving,
		MoveFlags:       0x00000401, // forward + disable gravity
		MoveTime:        0x11223344,
		X:               1.25,
		Y:               -2.5,
		Z:               3.75,
		Orientation:     0.5,
		WalkSpeed:       2.5,
		RunSpeed:        7,
		RunBackSpeed:    4.5,
		SwimSpeed:       4.722222,
		SwimBackSpeed:   2.5,
		FlightSpeed:     7,
		FlightBackSpeed: 4.5,
		TurnRate:        3.141593,
		PitchRate:       3.141593,
	}
	actions := []int32{0x01020304, 0x05060708}
	update := LegacyObjectUpdate{
		Type:       LegacyUpdateCreateObject2,
		GUID:       0x11223344,
		ObjectType: 4,
		Movement:   move,
		Values:     LegacyUpdateValuesBlock{Fields: fields},
	}
	body, err := EncodeActivePlayerCreate(update, ActivePlayerCreateOptions{
		MapID:         571,
		VirtualRealm:  0x20000001,
		ActionButtons: actions,
		Now:           time.Unix(1_700_000_000, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(body); got != 1 {
		t.Fatalf("object update count = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(body[4:]); got != 571 {
		t.Fatalf("map ID = %d, want 571", got)
	}
	if body[6] != 0 {
		t.Fatalf("destroy/out-of-range bit byte = 0x%02x, want 0", body[6])
	}
	objectLength := int(binary.LittleEndian.Uint32(body[7:]))
	if objectLength != len(body)-11 {
		t.Fatalf("object data length = %d, actual %d", objectLength, len(body)-11)
	}

	object := body[11:]
	if object[0] != 2 {
		t.Fatalf("modern update type = %d, want CreateObject2", object[0])
	}
	low, high, guidBytes, err := readPackedGUID128(object[1:])
	if err != nil {
		t.Fatal(err)
	}
	if low != 0x11223344 || high != uint64(2)<<58|uint64(1)<<42 {
		t.Fatalf("modern player GUID = %016x:%016x", high, low)
	}
	position := 1 + guidBytes
	if object[position] != 7 {
		t.Fatalf("modern object type = %d, want ActivePlayer", object[position])
	}
	position++
	if got := object[position : position+3]; got[0] != 0x10 || got[1] != 0x02 || got[2] != 0x80 {
		t.Fatalf("create bits = % x, want 10 02 80", got)
	}
	position += 3
	_, _, movementGUIDBytes, err := readPackedGUID128(object[position:])
	if err != nil {
		t.Fatal(err)
	}
	position += movementGUIDBytes
	if got := binary.LittleEndian.Uint32(object[position:]); got != 0x00000201 {
		t.Fatalf("modern movement flags = 0x%08x, want 0x00000201", got)
	}
	if binary.LittleEndian.Uint32(object[position+4:]) != 0x200 || binary.LittleEndian.Uint32(object[position+8:]) != 0 {
		t.Fatal("modern movement extra flags do not match legacy proxy")
	}
	position += 12 + 4 + 6*4 + 2*4 + 1
	position += 9*4 + 4 + 18*4 + 1 + 4
	if object[position] != 0x20 {
		t.Fatalf("active-player optional bits = 0x%02x, want 0x20 (HasActionButtons)", object[position])
	}
	position++
	position += modernActionButtonCount * 4
	valuesLength := int(binary.LittleEndian.Uint32(object[position:]))
	position += 4
	if valuesLength != len(object)-position {
		t.Fatalf("values length = %d, actual %d", valuesLength, len(object)-position)
	}
	if object[position] != 0x03 {
		t.Fatalf("update-field flags = 0x%02x, want owner+party", object[position])
	}
	if valuesLength != 17345 {
		t.Fatalf("active-player values bytes = %d, want fixed descriptor length 17345", valuesLength)
	}

	values := legacyActivePlayerValues{fields: fields}
	if got := len(values.appendObject(nil)); got != 12 {
		t.Fatalf("Object descriptor bytes = %d, want 12", got)
	}
	if got := len(values.appendUnit(nil, true, false, 0)); got != 825 {
		t.Fatalf("Unit descriptor bytes = %d, want 825", got)
	}
	if got := len(values.appendUnit(nil, false, true, 0)); got != 519 {
		t.Fatalf("non-owner Unit descriptor bytes = %d, want 519", got)
	}
	ownerPlayerBytes := len(values.appendPlayer(nil, update.GUID, ActivePlayerCreateOptions{VirtualRealm: 1, Now: time.Unix(1_700_000_000, 0)}, true))
	if ownerPlayerBytes != 2254 {
		t.Fatalf("owner Player descriptor bytes = %d, want 2254", ownerPlayerBytes)
	}
	nonOwnerPlayerBytes := len(values.appendPlayer(nil, update.GUID, ActivePlayerCreateOptions{VirtualRealm: 1}, false))
	if nonOwnerPlayerBytes != 654 {
		t.Fatalf("non-owner Player descriptor bytes = %d, want 654", nonOwnerPlayerBytes)
	}
	if activeBytes := len(values.appendActivePlayer(nil, 571, nil)); activeBytes != 14253 {
		t.Fatalf("ActivePlayer descriptor bytes = %d, want 14253", activeBytes)
	}
}

func TestActivePlayerCreateWritesOverrideSpellsID(t *testing.T) {
	empty := legacyActivePlayerValues{}.appendActivePlayer(nil, 571, nil)
	frenzy := legacyActivePlayerValues{fields: map[int]uint32{legacyPlayerFieldBytes2: 0x040000f1}}.appendActivePlayer(nil, 571, nil)
	if len(empty) != len(frenzy) || len(empty) != 14253 {
		t.Fatalf("CREATE length empty=%d frenzy=%d want 14253", len(empty), len(frenzy))
	}
	diffs := 0
	var pos int
	for i := range empty {
		if empty[i] != frenzy[i] {
			diffs++
			pos = i
		}
	}
	if diffs != 1 || frenzy[pos] != 0xf1 {
		t.Fatalf("CREATE OverrideSpellsID diffs=%d pos=%d byte=%#x", diffs, pos, frenzy[pos])
	}
}

func TestCreateDescriptorsKeepFixedDynamicFieldCounts(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitBytes0:            0x00000101,
		legacyPlayerBytes:           0,
		legacyPlayerKnownTitles:     0x11223344,
		legacyPlayerKnownTitles + 1: 0x55667788,
	}}

	player := values.appendPlayer(nil, 1, ActivePlayerCreateOptions{VirtualRealm: 1}, true)
	position := 0
	for index := 0; index < 3; index++ {
		_, _, consumed, err := readPackedGUID128(player[position:])
		if err != nil {
			t.Fatal(err)
		}
		position += consumed
	}
	position += 5 * 4
	if got := binary.LittleEndian.Uint32(player[position:]); got != 36 {
		t.Fatalf("PlayerData customization count = %d, want legacy proxy's fixed 36", got)
	}

	active := values.appendActivePlayer(nil, 571, nil)
	position = 0
	for index := 0; index < 141+2; index++ {
		_, _, consumed, err := readPackedGUID128(active[position:])
		if err != nil {
			t.Fatal(err)
		}
		position += consumed
	}
	if got := binary.LittleEndian.Uint32(active[position:]); got != 3 {
		t.Fatalf("ActivePlayer known-title count = %d, want WotLK's fixed 3", got)
	}
	wantTitle := binary.LittleEndian.AppendUint64(nil, 0x5566778811223344)
	if !bytes.Contains(active, wantTitle) {
		t.Fatalf("ActivePlayer descriptor did not preserve known-title block %x", wantTitle)
	}
}

func TestAppendUnitForwardsCreateTargetGUID(t *testing.T) {
	const legacyTarget = uint64(0x1)
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitTarget:     uint32(legacyTarget),
		legacyUnitTarget + 1: uint32(legacyTarget >> 32),
	}}
	body := values.appendUnit(nil, false, true, 571)
	position := 8 + 8 + 4 + 4 + 5*4
	for index := 0; index < 9; index++ {
		low, high, consumed, err := readPackedGUID128(body[position:])
		if err != nil {
			t.Fatalf("GUID %d: %v", index, err)
		}
		position += consumed
		if index != 7 {
			if low != 0 || high != 0 {
				t.Fatalf("GUID %d unexpectedly non-empty: %016x:%016x", index, high, low)
			}
			continue
		}
		want := ModernGUIDForLegacy(legacyTarget, 571)
		if low != want.Low || high != want.High {
			t.Fatalf("UnitData.Target=%016x:%016x want=%016x:%016x", high, low, want.High, want.Low)
		}
	}
}

func TestAppendUnitForwardsCreateChannelState(t *testing.T) {
	const (
		legacyChannelTarget = uint64(0xf130000001000043)
		channelSpell        = uint32(19674)
	)
	fields := map[int]uint32{
		legacyUnitChannelObject:     uint32(legacyChannelTarget & 0xffffffff),
		legacyUnitChannelObject + 1: uint32(legacyChannelTarget >> 32),
		legacyUnitChannelSpell:      channelSpell,
	}
	values := legacyActivePlayerValues{fields: fields}
	body := values.appendUnit(nil, false, true, 571)

	position := 8 + 8 + 4 + 4 + 5*4
	for index := 0; index < 9; index++ {
		_, _, consumed, err := readPackedGUID128(body[position:])
		if err != nil {
			t.Fatalf("GUID %d: %v", index, err)
		}
		position += consumed
	}
	position += 8 // BattlePetDBID
	if got := binary.LittleEndian.Uint32(body[position:]); got != channelSpell {
		t.Fatalf("UnitData.ChannelData.SpellID=%d want=%d", got, channelSpell)
	}
	if got := binary.LittleEndian.Uint32(body[position+4:]); got != KnownSpellVisual(channelSpell) {
		t.Fatalf("UnitData.ChannelData.SpellXSpellVisualID=%d want=%d", got, KnownSpellVisual(channelSpell))
	}

	emptyValues := legacyActivePlayerValues{fields: map[int]uint32{legacyUnitChannelSpell: channelSpell}}
	emptyBody := emptyValues.appendUnit(nil, false, true, 571)
	channelCountPosition := len(emptyBody) - 18
	if got := binary.LittleEndian.Uint32(body[channelCountPosition:]); got != 1 {
		t.Fatalf("UnitData.ChannelObjects count=%d want=1", got)
	}
	low, high, _, err := readPackedGUID128(body[len(emptyBody):])
	want := ModernGUIDForLegacy(legacyChannelTarget, 571)
	if err != nil || low != want.Low || high != want.High {
		t.Fatalf("UnitData.ChannelObjects[0]=%016x:%016x want=%016x:%016x err=%v", high, low, want.High, want.Low, err)
	}
}

func TestDeathKnightCreateCarriesRuneBlock(t *testing.T) {
	encode := func(class byte) []byte {
		fields := map[int]uint32{
			legacyObjectScale:           0x3f800000,
			legacyUnitBytes0:            uint32(1) | uint32(class)<<8 | uint32(1)<<16,
			legacyUnitHealth:            900,
			legacyUnitMaxHealth:         1000,
			legacyUnitLevel:             55,
			legacyUnitDisplayID:         49,
			legacyUnitNativeDisplayID:   49,
			legacyUnitFaction:           1,
			legacyUnitBoundingRadius:    0x3ec72b02,
			legacyUnitCombatReach:       0x3fc00000,
			legacyUnitMaxHealthModifier: 0x3f800000,
			legacyUnitHoverHeight:       0x3f800000,
			legacyPlayerBytes:           0x02030100,
			legacyPlayerBytes2:          0x02010004,
			legacyPlayerMaxLevel:        80,
		}
		update := LegacyObjectUpdate{
			Type:       LegacyUpdateCreateObject2,
			GUID:       0x11223344,
			ObjectType: 4,
			Movement: &LegacyMovement{
				UpdateFlags: legacyUpdateSelf | legacyUpdateLiving,
				X:           1, Y: 2, Z: 3,
				WalkSpeed: 2.5, RunSpeed: 7, RunBackSpeed: 4.5,
				SwimSpeed: 4.72, SwimBackSpeed: 2.5, FlightSpeed: 7,
				FlightBackSpeed: 4.5, TurnRate: 3.14, PitchRate: 3.14,
			},
			Values: LegacyUpdateValuesBlock{Fields: fields},
		}
		body, err := EncodeActivePlayerCreate(update, ActivePlayerCreateOptions{MapID: 609, Now: time.Unix(1, 0)})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	warrior := encode(1)
	dk := encode(6)
	runeBody := EncodeRuneResync(DefaultDeathKnightRuneState())
	if bytes.Contains(warrior, runeBody) {
		t.Fatal("warrior create unexpectedly contains the death-knight rune block")
	}
	if !bytes.Contains(dk, runeBody) {
		t.Fatalf("death-knight create missing rune block %x", runeBody)
	}
	if len(dk) != len(warrior)+len(runeBody) {
		t.Fatalf("death-knight create is %d bytes, warrior %d, rune block %d", len(dk), len(warrior), len(runeBody))
	}
}

func TestEncodeActivePlayerCreateRejectsNonPlayerUpdates(t *testing.T) {
	tests := []LegacyObjectUpdate{
		{Type: LegacyUpdateValues, GUID: 1, ObjectType: 4, Movement: &LegacyMovement{UpdateFlags: legacyUpdateLiving}},
		{Type: LegacyUpdateCreateObject2, GUID: 1, ObjectType: 3, Movement: &LegacyMovement{UpdateFlags: legacyUpdateLiving}},
		{Type: LegacyUpdateCreateObject2, GUID: 0, ObjectType: 4, Movement: &LegacyMovement{UpdateFlags: legacyUpdateLiving}},
		{Type: LegacyUpdateCreateObject2, GUID: 1, ObjectType: 4, Movement: &LegacyMovement{}},
	}
	for index, update := range tests {
		if _, err := EncodeActivePlayerCreate(update, ActivePlayerCreateOptions{}); err == nil {
			t.Fatalf("case %d unexpectedly succeeded", index)
		}
	}
}

func TestActionButtonsLegacyAndModernLayouts(t *testing.T) {
	legacy := make([]byte, 1+144*4)
	legacy[0] = 1
	binary.LittleEndian.PutUint32(legacy[1:], 0x12034567)
	binary.LittleEndian.PutUint32(legacy[5:], 0x7f000001)
	buttons, reason, err := ParseLegacyActionButtons(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if reason != 1 || len(buttons) != 144 || uint32(buttons[0]) != 0x12034567 {
		t.Fatalf("parsed reason=%d buttons=%d first=0x%08x", reason, len(buttons), uint32(buttons[0]))
	}
	modern := EncodeUpdateActionButtons(buttons, reason)
	if len(modern) != 1441 || modern[len(modern)-1] != 1 {
		t.Fatalf("modern action body has %d bytes and reason %d", len(modern), modern[len(modern)-1])
	}
	created := EncodeCreateActionButtons(buttons)
	if len(created) != modernActionButtonCount*4 || binary.LittleEndian.Uint32(created) != 0x12034567 {
		t.Fatalf("create action buttons: bytes=%d first=0x%08x", len(created), binary.LittleEndian.Uint32(created))
	}
	if got := binary.LittleEndian.Uint64(modern); got != 0x12034567 {
		t.Fatalf("modern first action = 0x%016x", got)
	}
	if got := binary.LittleEndian.Uint64(modern[8:]); got != 0x7f000001 {
		t.Fatalf("modern second action = 0x%016x", got)
	}
	packed := uint32(0x80000001)
	item := EncodeUpdateActionButtons([]int32{int32(packed)}, 1)
	if got := binary.LittleEndian.Uint64(item); got != 0x80000001 {
		t.Fatalf("item slot was shifted out of the low dword, got 0x%016x", got)
	}
	if _, _, err := ParseLegacyActionButtons(legacy[:len(legacy)-1]); err == nil {
		t.Fatal("truncated legacy action buttons unexpectedly parsed")
	}
}

func TestSetActionButtonAndBarToggles(t *testing.T) {
	mask, err := ParseSetActionBarToggles([]byte{0x1f})
	if err != nil || mask != 0x1f {
		t.Fatalf("mask=%d err=%v", mask, err)
	}
	// Modern packed value 0x80034567 is split by Hermes as Action=0x4567,
	// Type=0x8003. Recombining it must retain the item action type.
	hermes := []byte{0x67, 0x45, 0x03, 0x80, 3}
	change, err := ParseSetActionButton(hermes)
	if err != nil || change.Index != 3 || change.Packed != 0x80034567 {
		t.Fatalf("hermes change=%#v err=%v", change, err)
	}
	packed := binary.LittleEndian.AppendUint64(nil, 0x12034567)
	packed = append(packed, 7)
	change, err = ParseSetActionButton(packed)
	if err != nil || change.Index != 7 || change.Packed != 0x12034567 {
		t.Fatalf("uint64 change=%#v err=%v", change, err)
	}
	modernPacked := binary.LittleEndian.AppendUint64(nil, 0x8000000000034567)
	modernPacked = append(modernPacked, 8)
	modernChange, err := ParseSetActionButton(modernPacked)
	if err != nil || modernChange.Index != 8 || modernChange.Packed != 0x80034567 {
		t.Fatalf("modern uint64 change=%#v err=%v", modernChange, err)
	}
	legacy := EncodeLegacySetActionButton(change)
	if len(legacy) != 5 || legacy[0] != 7 || binary.LittleEndian.Uint32(legacy[1:]) != 0x12034567 {
		t.Fatalf("legacy set-action-button %x", legacy)
	}
}

func TestGhostCreateUsesAliveHealthAndDropsLegacyGhostUnitFlag(t *testing.T) {
	v := legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitHealth:    0,
		legacyUnitMaxHealth: 100,
		legacyUnitFlags:     legacyUnitFlagGhost | 0x8,
		legacyPlayerFlags:   legacyPlayerFlagGhost,
	}}
	if !v.isLegacyGhost() {
		t.Fatal("expected ghost")
	}
	if got := modernUnitHealth(v); got != 1 {
		t.Fatalf("ghost health = %d, want 1", got)
	}
	if got := modernUnitFlags(v.field(legacyUnitFlags)); got != 0x8 {
		t.Fatalf("modern unit flags = %#x, want 0x8", got)
	}
	body := v.appendUnit(nil, true, false, 0)
	if got := binary.LittleEndian.Uint64(body[:8]); got != 1 {
		t.Fatalf("encoded health = %d, want 1", got)
	}
}

func TestAppendUnitClearsMirrorImageIdentity(t *testing.T) {
	fields := map[int]uint32{
		legacyUnitBytes0:    0x01010801, // race 1, class 8, sex 1, power 1
		legacyUnitFlags2:    0x810,      // UNIT_FLAG2_MIRROR_IMAGE | default
		legacyUnitDisplayID: 50,
	}
	clone := legacyActivePlayerValues{fields: fields}.appendUnit(nil, false, true, 0)
	plainFields := map[int]uint32{
		legacyUnitBytes0:    0x01010801,
		legacyUnitFlags2:    0x800,
		legacyUnitDisplayID: 50,
	}
	plain := legacyActivePlayerValues{fields: plainFields}.appendUnit(nil, false, true, 0)

	findMarker := func(body []byte, want []byte) int {
		for index := 0; index+len(want) <= len(body); index++ {
			if string(body[index:index+len(want)]) == string(want) {
				return index
			}
		}
		return -1
	}
	if findMarker(plain, []byte{1, 8, 0, 1, 1}) < 0 {
		t.Fatal("plain unit missing race/class/sex")
	}
	if findMarker(clone, []byte{1, 8, 0, 1, 1}) >= 0 {
		t.Fatal("clone still carried player race bytes")
	}
	if findMarker(clone, []byte{0, 0, 0, 0, 1}) < 0 {
		t.Fatal("clone did not wipe race/class/sex")
	}
	flagBytes := func(value uint32) []byte {
		raw := make([]byte, 4)
		binary.LittleEndian.PutUint32(raw, value)
		return raw
	}
	if !bytes.Contains(clone, flagBytes(0x810)) {
		t.Fatal("clone create dropped UNIT_FLAG2_MIRROR_IMAGE; 54261 will not query appearance")
	}
}

func TestStunnedCreateIncludesExplicitStateAnimation(t *testing.T) {
	v := legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitFlags: legacyUnitFlagStunned,
	}}
	body := v.appendUnit(nil, false, false, 0)
	if got := binary.LittleEndian.Uint32(body[32:36]); got != modernUnitAnimStun {
		t.Fatalf("StateAnimID = %d, want %d", got, modernUnitAnimStun)
	}
}

func TestModernPlayerLocalFlags(t *testing.T) {
	if got := modernPlayerLocalFlags(0x1f0008, true); got != modernLocalFlagNoReleaseWindow {
		t.Fatalf("ghost leftover release-timer = %#x, want no-release-window %#x", got, modernLocalFlagNoReleaseWindow)
	}
	if got := modernPlayerLocalFlags(0x08, false); got != modernLocalFlagReleaseTimer {
		t.Fatalf("alive release timer = %#x, want %#x", got, modernLocalFlagReleaseTimer)
	}
	if got := modernPlayerLocalFlags(0x08, true); got != modernLocalFlagNoReleaseWindow {
		t.Fatalf("ghost should drop release timer, got %#x", got)
	}
	if got := modernPlayerLocalFlags(0x10, true); got != modernLocalFlagNoReleaseWindow {
		t.Fatalf("ghost no-release-window = %#x, want %#x", got, modernLocalFlagNoReleaseWindow)
	}
	if got := modernPlayerLocalFlags(0x02, false); got != modernLocalFlagTrackStealthed {
		t.Fatalf("track stealthed = %#x, want %#x", got, modernLocalFlagTrackStealthed)
	}
	fields := map[int]uint32{legacyPlayerFieldBytes: 0x1f0008, legacyPlayerFlags: legacyPlayerFlagGhost}
	if got := PlayerLocalFlags(fields); got != modernLocalFlagNoReleaseWindow {
		t.Fatalf("PlayerLocalFlags = %#x, want %#x", got, modernLocalFlagNoReleaseWindow)
	}
}

func TestModernMovementFlagsDropsLegacyOnlyBits(t *testing.T) {
	legacy := uint32(0x80000000 | 0x40000000 | 0x10000000 | 0x08000000 | 0x00000400 | 0x00000200 | 1)
	if got, want := modernMovementFlags(legacy), uint32(0x10000000|0x04000000|0x00000200|1); got != want {
		t.Fatalf("modern flags = 0x%08x, want 0x%08x", got, want)
	}
}

func TestQuestLogStateFlagsPreserveLegacyState(t *testing.T) {
	incomplete := questLogProgress(3, 0)
	if incomplete[0] != 3 || incomplete[4] != 0 {
		t.Fatalf("incomplete %#v", incomplete)
	}
	for _, test := range []struct {
		name           string
		questID, state uint32
		want           uint32
	}{
		{name: "empty slot", state: 1, want: 0},
		{name: "incomplete", questID: 76, want: 0},
		{name: "complete", questID: 76, state: 1, want: 1},
		{name: "failed", questID: 76, state: 2, want: 2},
		{name: "preserve unknown legacy bits", questID: 76, state: 0x80000001, want: 0x80000001},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := translateQuestLogStateFlags(test.questID, test.state, nil); got != test.want {
				t.Fatalf("state flags = 0x%x, want 0x%x", got, test.want)
			}
		})
	}
	wire := appendQuestLogSlot(nil, 76, 1, 0, 0, 0, nil)
	if got := binary.LittleEndian.Uint32(wire[12:16]); got != 1 {
		t.Fatalf("wire StateFlags = 0x%x, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(wire[16:18]); got != 0 {
		t.Fatalf("exploration completion must not manufacture ObjectiveProgress, got %d", got)
	}
}

// Pure exploration/event quests carry completion only as the legacy slot
// COMPLETE state; the modern client ticks their synthesized StorageIndex 0
// AreaTrigger objective from QuestLog.StateFlags bit 8. The bit must be added
// only for those quests (never for counter-driven quests) and only once the
// slot is actually COMPLETE.
func TestQuestLogStateFlagsMarksExplorationObjectiveBit(t *testing.T) {
	areaOnly := map[uint32]struct{}{76: {}}
	for _, test := range []struct {
		name           string
		questID, state uint32
		areaOnlyQuests map[uint32]struct{}
		want           uint32
	}{
		{name: "exploration complete", questID: 76, state: 1, areaOnlyQuests: areaOnly, want: 0x101},
		{name: "exploration pending", questID: 76, areaOnlyQuests: areaOnly, want: 0},
		{name: "exploration failed", questID: 76, state: 2, areaOnlyQuests: areaOnly, want: 2},
		{name: "counter quest complete", questID: 77, state: 1, areaOnlyQuests: areaOnly, want: 1},
		{name: "unknown quest complete", questID: 77, state: 1, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := translateQuestLogStateFlags(test.questID, test.state, test.areaOnlyQuests); got != test.want {
				t.Fatalf("state flags = 0x%x, want 0x%x", got, test.want)
			}
		})
	}
	wire := appendQuestLogSlot(nil, 76, 1, 0, 0, 0, areaOnly)
	if got := binary.LittleEndian.Uint32(wire[12:16]); got != 0x101 {
		t.Fatalf("wire StateFlags = 0x%x, want 0x101", got)
	}
	if got := binary.LittleEndian.Uint16(wire[16:18]); got != 0 {
		t.Fatalf("area objective completion must not manufacture ObjectiveProgress, got %d", got)
	}
}
