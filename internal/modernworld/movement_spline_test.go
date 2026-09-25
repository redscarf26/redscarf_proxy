package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestSplineSpeedRelayMapping(t *testing.T) {
	const legacyCreature = uint64(0xf130005100002345)
	// Run-speed spline (legacy 0x02FE = 766) carries packed guid + float.
	body := appendTestPackedGUID(nil, legacyCreature)
	body = binary.LittleEndian.AppendUint32(body, math.Float32bits(7.0))
	change, handled, err := ParseLegacySpeedChange(legacySMSGSplineSetRunSpeed, body)
	if err != nil || !handled {
		t.Fatalf("spline run-speed handled=%v err=%v", handled, err)
	}
	if change.Kind != speedKindSpline || change.Modern != SMSGMoveSplineSetRunSpeed ||
		change.GUID != legacyCreature || change.Speed != 7.0 {
		t.Fatalf("run speed change=%#v", change)
	}

	// Every spline speed legacy opcode resolves to the matching modern one.
	cases := map[uint16]uint16{
		legacySMSGSplineSetRunSpeed:        SMSGMoveSplineSetRunSpeed,
		legacySMSGSplineSetRunBackSpeed:    SMSGMoveSplineSetRunBackSpeed,
		legacySMSGSplineSetSwimSpeed:       SMSGMoveSplineSetSwimSpeed,
		legacySMSGSplineSetWalkSpeed:       SMSGMoveSplineSetWalkSpeed,
		legacySMSGSplineSetSwimBackSpeed:   SMSGMoveSplineSetSwimBackSpeed,
		legacySMSGSplineSetTurnRate:        SMSGMoveSplineSetTurnRate,
		legacySMSGSplineSetFlightSpeed:     SMSGMoveSplineSetFlightSpeed,
		legacySMSGSplineSetFlightBackSpeed: SMSGMoveSplineSetFlightBackSpeed,
		legacySMSGSplineSetPitchRate:       SMSGMoveSplineSetPitchRate,
	}
	for legacyOpcode, modern := range cases {
		body := appendTestPackedGUID(nil, legacyCreature)
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(1))
		change, handled, err := ParseLegacySpeedChange(legacyOpcode, body)
		if err != nil || !handled || change.Modern != modern {
			t.Fatalf("opcode 0x%04x handled=%v modern=%d want=%d err=%v", legacyOpcode, handled, change.Modern, modern, err)
		}
	}
}

func TestSplineMovementFlagRelayMapping(t *testing.T) {
	const legacyCreature = uint64(0xf130005100002345)
	// Spline flags carry only a packed GUID (no counter).
	body := appendTestPackedGUID(nil, legacyCreature)
	change, handled, err := ParseLegacyMovementFlagChange(legacySMSGSplineRoot, body)
	if err != nil || !handled {
		t.Fatalf("spline root handled=%v err=%v", handled, err)
	}
	if change.Modern != SMSGMoveSplineRoot || change.HasCounter || change.GUID != legacyCreature {
		t.Fatalf("spline root change=%#v", change)
	}
	encoded, err := EncodeModernMovementFlagChange(change, GUID128{Low: 0x2345, High: 3 << 58})
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: encoded}
	guid, err := r.guid128()
	if err != nil || guid != (GUID128{Low: 0x2345, High: 3 << 58}) || r.remaining() != 0 {
		t.Fatalf("spline root modern guid=%#v remaining=%d err=%v", guid, r.remaining(), err)
	}

	// All spline flag opcodes resolve to a spline (no-counter) modern packet.
	cases := map[uint16]uint16{
		legacySMSGSplineRoot:           SMSGMoveSplineRoot,
		legacySMSGSplineUnroot:         SMSGMoveSplineUnroot,
		legacySMSGSplineDisableGravity: SMSGMoveSplineDisableGravity,
		legacySMSGSplineEnableGravity:  SMSGMoveSplineEnableGravity,
		legacySMSGSplineSetFeatherFall: SMSGMoveSplineSetFeatherFall,
		legacySMSGSplineSetNormalFall:  SMSGMoveSplineSetNormalFall,
		legacySMSGSplineSetHover:       SMSGMoveSplineSetHover,
		legacySMSGSplineUnsetHover:     SMSGMoveSplineUnsetHover,
		legacySMSGSplineSetWaterWalk:   SMSGMoveSplineSetWaterWalk,
		legacySMSGSplineSetLandWalk:    SMSGMoveSplineSetLandWalk,
		legacySMSGSplineSetFlying:      SMSGMoveSplineSetFlying,
		legacySMSGSplineUnsetFlying:    SMSGMoveSplineUnsetFlying,
	}
	for legacyOpcode, modern := range cases {
		body := appendTestPackedGUID(nil, legacyCreature)
		change, handled, err := ParseLegacyMovementFlagChange(legacyOpcode, body)
		if err != nil || !handled || change.Modern != modern || change.HasCounter {
			t.Fatalf("opcode 0x%04x handled=%v modern=%d want=%d err=%v", legacyOpcode, handled, change.Modern, modern, err)
		}
	}
}

func TestAchievementDeletedRoundTrip(t *testing.T) {
	id, err := ParseLegacyAchievementDeleted(binary.LittleEndian.AppendUint32(nil, 0x1234))
	if err != nil || id != 0x1234 {
		t.Fatalf("achievement-deleted id=%x err=%v", id, err)
	}
	if _, err := ParseLegacyAchievementDeleted([]byte{1, 2, 3}); err == nil {
		t.Fatal("achievement-deleted with wrong length must error")
	}
	body := EncodeAchievementDeleted(0x1234)
	if !isUint32At(body, 0, 0x1234) || !isUint32At(body, 4, 0) || len(body) != 8 {
		t.Fatalf("modern achievement-deleted=%x", body)
	}
}
