package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeUnitAndOtherPlayerCreates(t *testing.T) {
	move := &LegacyMovement{
		UpdateFlags:     legacyUpdateLiving,
		MoveTime:        123,
		X:               1,
		Y:               2,
		Z:               3,
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
	fields := map[int]uint32{
		legacyObjectEntry:  1,
		legacyUnitLevel:    80,
		legacyUnitBytes0:   0x010001,
		legacyPlayerBytes:  0x02030100,
		legacyPlayerBytes2: 0x02010004,
		legacyPlayerBytes3: 0,
	}
	tests := []struct {
		name              string
		update            LegacyObjectUpdate
		encode            func(LegacyObjectUpdate, ActivePlayerCreateOptions) ([]byte, error)
		modernType        byte
		wantLow, wantHigh uint64
		wantValues        int
		movementAfterGUID int
	}{
		{
			name: "creature unit",
			update: LegacyObjectUpdate{Type: LegacyUpdateCreateObject2, GUID: 0xf130000001000042, ObjectType: 3,
				Movement: move, Values: LegacyUpdateValuesBlock{Fields: fields}},
			encode:            EncodeUnitCreate,
			modernType:        5,
			wantLow:           0x42,
			wantHigh:          uint64(8)<<58 | uint64(1)<<42 | uint64(1)<<6,
			wantValues:        1 + 12 + 519,
			movementAfterGUID: 166,
		},
		{
			name: "other player",
			update: LegacyObjectUpdate{Type: LegacyUpdateCreateObject2, GUID: 0x43, ObjectType: 4,
				Movement: move, Values: LegacyUpdateValuesBlock{Fields: fields}},
			encode:            EncodePlayerCreate,
			modernType:        6,
			wantLow:           0x43,
			wantHigh:          uint64(2)<<58 | uint64(1)<<42,
			wantValues:        1 + 12 + 519 + 651,
			movementAfterGUID: 170,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := test.encode(test.update, ActivePlayerCreateOptions{MapID: 571, VirtualRealm: 1})
			if err != nil {
				t.Fatal(err)
			}
			if binary.LittleEndian.Uint32(body) != 1 || binary.LittleEndian.Uint16(body[4:]) != 571 {
				t.Fatalf("bad UpdateObject header: % x", body[:11])
			}
			object := body[11:]
			low, high, guidBytes, err := readPackedGUID128(object[1:])
			if err != nil {
				t.Fatal(err)
			}
			if low != test.wantLow || high != test.wantHigh {
				t.Fatalf("modern GUID = %016x:%016x, want %016x:%016x", high, low, test.wantHigh, test.wantLow)
			}
			position := 1 + guidBytes
			if object[position] != test.modernType {
				t.Fatalf("modern object type = %d, want %d", object[position], test.modernType)
			}
			position += 1 + 3
			_, _, movementGUIDBytes, err := readPackedGUID128(object[position:])
			if err != nil {
				t.Fatal(err)
			}
			position += movementGUIDBytes + test.movementAfterGUID
			valuesLength := int(binary.LittleEndian.Uint32(object[position:]))
			position += 4
			if valuesLength != test.wantValues || valuesLength != len(object)-position {
				t.Fatalf("values bytes = %d, want %d (remaining %d)", valuesLength, test.wantValues, len(object)-position)
			}
			if object[position] != 0 {
				t.Fatalf("non-owner update-field flags = 0x%02x, want 0", object[position])
			}
		})
	}
}

func TestEncodeUnitCreateUsesOwnerFieldsForViewerTotem(t *testing.T) {
	const (
		ownerGUID = uint64(4)
		spellID   = uint32(8071)
		mapID     = uint16(530)
	)
	move := &LegacyMovement{
		UpdateFlags:     legacyUpdateLiving,
		MoveTime:        123,
		X:               1,
		Y:               2,
		Z:               3,
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
	fields := map[int]uint32{
		legacyObjectEntry:        5873,
		legacyUnitCreatedBySpell: spellID,
		legacyUnitSummonedBy:     uint32(ownerGUID),
		legacyUnitCreatedBy:      uint32(ownerGUID),
		legacyUnitFlags:          0x8,
		legacyUnitLevel:          4,
	}
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject2, GUID: 0xf130016ee1000ee1, ObjectType: 3,
		Movement: move, Values: LegacyUpdateValuesBlock{Fields: fields},
	}
	body, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: mapID, OwnerGUID: ownerGUID, VirtualRealm: 1})
	if err != nil {
		t.Fatal(err)
	}
	object := body[11:]
	_, _, guidBytes, err := readPackedGUID128(object[1:])
	if err != nil {
		t.Fatal(err)
	}
	position := 1 + guidBytes + 1 + 3
	_, _, movementGUIDBytes, err := readPackedGUID128(object[position:])
	if err != nil {
		t.Fatal(err)
	}
	position += movementGUIDBytes + 166
	valuesLength := int(binary.LittleEndian.Uint32(object[position:]))
	position += 4
	if valuesLength != len(object)-position {
		t.Fatalf("values bytes = %d, remaining %d", valuesLength, len(object)-position)
	}
	if object[position] != ownedUnitCreateFlags {
		t.Fatalf("owned totem visibility flags = 0x%02x, want 0x%02x", object[position], ownedUnitCreateFlags)
	}
	values := legacyActivePlayerValues{fields: fields}
	wantValues := 1 + 12 + len(values.appendUnit(nil, true, true, mapID))
	if valuesLength != wantValues {
		t.Fatalf("owned totem values bytes = %d, want owner UnitData length %d", valuesLength, wantValues)
	}

	unowned, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: mapID, VirtualRealm: 1})
	if err != nil {
		t.Fatal(err)
	}
	unownedObject := unowned[11:]
	_, _, unownedGUIDBytes, err := readPackedGUID128(unownedObject[1:])
	if err != nil {
		t.Fatal(err)
	}
	unownedPos := 1 + unownedGUIDBytes + 1 + 3
	_, _, unownedMoveGUID, err := readPackedGUID128(unownedObject[unownedPos:])
	if err != nil {
		t.Fatal(err)
	}
	unownedPos += unownedMoveGUID + 166
	unownedValues := int(binary.LittleEndian.Uint32(unownedObject[unownedPos:]))
	unownedPos += 4
	if unownedObject[unownedPos] != 0 {
		t.Fatalf("totem without viewer OwnerGUID flags = 0x%02x, want public 0", unownedObject[unownedPos])
	}
	wantPublic := 1 + 12 + len(values.appendUnit(nil, false, true, mapID))
	if unownedValues != wantPublic {
		t.Fatalf("public totem values bytes = %d, want %d", unownedValues, wantPublic)
	}
}

func TestEncodePublicCreateValidation(t *testing.T) {
	base := LegacyObjectUpdate{Type: LegacyUpdateCreateObject2, GUID: 1, ObjectType: 3,
		Movement: &LegacyMovement{UpdateFlags: legacyUpdateLiving}}
	bad := []LegacyObjectUpdate{
		{Type: LegacyUpdateValues, GUID: 1, ObjectType: 3, Movement: base.Movement},
		{Type: LegacyUpdateCreateObject2, GUID: 1, ObjectType: 4, Movement: base.Movement},
		{Type: LegacyUpdateCreateObject2, ObjectType: 3, Movement: base.Movement},
		{Type: LegacyUpdateCreateObject2, GUID: 1, ObjectType: 3, Movement: &LegacyMovement{}},
		{Type: LegacyUpdateCreateObject2, GUID: 0xdead000000000001, ObjectType: 3, Movement: base.Movement},
	}
	for index, update := range bad {
		if _, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{}); err == nil {
			t.Fatalf("case %d unexpectedly succeeded", index)
		}
	}
}

func TestEncodeUnitCreateSetsHoverAnimFromHoverFlag(t *testing.T) {
	move := &LegacyMovement{
		UpdateFlags:     legacyUpdateLiving,
		MoveFlags:       legacyMoveHover,
		MoveTime:        123,
		X:               1,
		Y:               2,
		Z:               3,
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
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject2, GUID: 0xf130000001000042, ObjectType: 3,
		Movement: move, Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{legacyObjectEntry: 37026}},
	}
	body, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 631, VirtualRealm: 1})
	if err != nil {
		t.Fatal(err)
	}
	object := body[11:]
	_, _, guidBytes, err := readPackedGUID128(object[1:])
	if err != nil {
		t.Fatal(err)
	}
	bits := object[1+guidBytes+1 : 1+guidBytes+4]
	if bits[0] != 0x30 || bits[1] != 0 || bits[2] != 0 {
		t.Fatalf("create bits = % x, want 30 00 00 (living + PlayHoverAnim)", bits)
	}
	if legacyMovePlaysHoverAnim(legacyMoveDisableGravity) {
		t.Fatal("DisableGravity is transport/levitate, not the hover pose")
	}
	if !legacyMovePlaysHoverAnim(legacyMoveHover) {
		t.Fatal("MOVEFLAG_HOVER should play hover anim")
	}
	if legacyMovePlaysHoverAnim(1) {
		t.Fatal("forward-only should not play hover anim")
	}

	move.MoveFlags = legacyMoveDisableGravity
	body, err = EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 631, VirtualRealm: 1})
	if err != nil {
		t.Fatal(err)
	}
	object = body[11:]
	_, _, guidBytes, err = readPackedGUID128(object[1:])
	if err != nil {
		t.Fatal(err)
	}
	bits = object[1+guidBytes+1 : 1+guidBytes+4]
	if bits[0] != 0x10 || bits[1] != 0 || bits[2] != 0 {
		t.Fatalf("DisableGravity create bits = % x, want 10 00 00 (living only)", bits)
	}

	move.VehicleID = 79
	body, err = EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 631, VirtualRealm: 1})
	if err != nil {
		t.Fatal(err)
	}
	object = body[11:]
	_, _, guidBytes, err = readPackedGUID128(object[1:])
	if err != nil {
		t.Fatal(err)
	}
	bits = object[1+guidBytes+1 : 1+guidBytes+4]
	// bit 2 PlayHoverAnim, bit 3 living, bit 8 vehicle.
	if bits[0] != 0x30 || bits[1] != 0x80 || bits[2] != 0 {
		t.Fatalf("vehicle DisableGravity create bits = % x, want 30 80 00 (living + hover + vehicle)", bits)
	}
	if createMovementPlaysHoverAnim(&LegacyMovement{MoveFlags: legacyMoveDisableGravity}) {
		t.Fatal("DisableGravity without VehicleID must not play hover anim")
	}
	if !createMovementPlaysHoverAnim(&LegacyMovement{MoveFlags: legacyMoveDisableGravity, VehicleID: 79}) {
		t.Fatal("DisableGravity vehicles must play hover anim")
	}
	if createMovementPlaysHoverAnim(&LegacyMovement{MoveFlags: legacyMoveOnTransport, VehicleID: 554}) {
		t.Fatal("ICC cannons are ONTRANSPORT-only; legacy proxy create bits are living+vehicle without Hover")
	}
}

func TestEncodeUnitCreateMatchesICCCannon(t *testing.T) {
	// Reference capture seq 103 at offset 18183: Alliance cannon
	// 0xf150008fe600023f bolted to the Skybreaker, vehicle 554, from the run
	// where the live client boarded it. Boarding depends on the create bits
	// (living + vehicle, Hover clear). Forcing Hover on this ONTRANSPORT-only
	// gun made 3.4 show a gear that selected the unit and never sent a click.
	want, err := os.ReadFile(filepath.Join("testdata", "icc-cannon-20260917.bin"))
	if err != nil {
		t.Fatal(err)
	}
	f := math.Float32frombits
	move := &LegacyMovement{
		UpdateFlags:          legacyUpdateLiving | legacyUpdateVehicle,
		MoveFlags:            legacyMoveOnTransport,
		MoveExtra:            0x3b,
		MoveTime:             398902,
		X:                    f(0xc3e1ea4e),
		Y:                    f(0x451bb0cd),
		Z:                    f(0x433f91bb),
		Orientation:          f(0x3fc360ac),
		TransportGUID:        0x1fc0000000000015,
		TransportX:           f(0xc0c4fc7a),
		TransportY:           f(0xc1c9e8dc),
		TransportZ:           f(0x41ada3d7),
		TransportOrientation: f(0x4096cbe6),
		TransportSeat:        -1,
		VehicleID:            554,
		VehicleOrientation:   f(0x4096cbe6),
		WalkSpeed:            2.5,
		RunSpeed:             7,
		RunBackSpeed:         4.5,
		SwimSpeed:            f(0x40971c71),
		SwimBackSpeed:        2.5,
		FlightSpeed:          7,
		FlightBackSpeed:      4.5,
		TurnRate:             f(0x40490fe0),
		PitchRate:            f(0x4048f5c3),
	}
	update := LegacyObjectUpdate{
		Type: LegacyUpdateCreateObject1, GUID: 0xf150008fe600023f, ObjectType: 3,
		Movement: move,
		Values: LegacyUpdateValuesBlock{Fields: map[int]uint32{
			legacyObjectEntry:   36838,
			legacyUnitNPCFlags:  0x01000000,
			legacyUnitDisplayID: 29488,
			legacyObjectScale:   math.Float32bits(1),
		}},
	}
	body, err := EncodeUnitCreate(update, ActivePlayerCreateOptions{MapID: 631})
	if err != nil {
		t.Fatal(err)
	}
	got := body[11:]
	if len(got) < 14 || len(want) < 14 {
		t.Fatalf("cannon create is too short: ours=%d captured=%d", len(got), len(want))
	}
	if !bytes.Equal(got[:11], want[:11]) {
		t.Fatalf("cannon guid prefix\ngot  % x\nwant % x", got[:11], want[:11])
	}
	if got[11] != 0x10 || got[12] != 0x80 || got[13] != 0 {
		t.Fatalf("cannon create bits = %02x %02x %02x, want legacy proxy 10 80 00 (living+vehicle, no Hover)", got[11], got[12], got[13])
	}
}
