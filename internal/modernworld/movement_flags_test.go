package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestMovementFlagChangeTranslation(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)

	tests := []struct {
		legacy     uint16
		modern     uint16
		hasCounter bool
	}{
		{legacySMSGMoveWaterWalk, SMSGMoveSetWaterWalk, true},
		{legacySMSGMoveLandWalk, SMSGMoveSetLandWalk, true},
		{legacySMSGForceMoveRoot, SMSGMoveRoot, true},
		{legacySMSGForceMoveUnroot, SMSGMoveUnroot, true},
		{legacySMSGMoveSetCanFly, SMSGMoveSetCanFly, true},
		{legacySMSGMoveUnsetCanFly, SMSGMoveUnsetCanFly, true},
		{legacySMSGMoveDisableGravity, SMSGMoveDisableGravity, true},
		{legacySMSGMoveEnableGravity, SMSGMoveEnableGravity, true},
		{legacySMSGMoveSetFeatherFall, SMSGMoveSetFeatherFall, true},
		{legacySMSGMoveSetNormalFall, SMSGMoveSetNormalFall, true},
	}
	for _, test := range tests {
		change, handled, err := ParseLegacyMovementFlagChange(test.legacy, legacy)
		if err != nil || !handled || change.Modern != test.modern || change.GUID != 0x42 || change.Counter != 7 || change.HasCounter != test.hasCounter {
			t.Fatalf("legacy=%#x handled=%v err=%v change=%#v", test.legacy, handled, err, change)
		}
		body, err := EncodeModernMovementFlagChange(change, GUID128{Low: 0x42, High: 1})
		if err != nil {
			t.Fatal(err)
		}
		low, high, consumed, err := readPackedGUID128(body)
		if err != nil || low != 0x42 || high != 1 || binary.LittleEndian.Uint32(body[consumed:]) != 7 {
			t.Fatalf("modern body=%x low=%x high=%x consumed=%d err=%v", body, low, high, consumed, err)
		}
	}
}

func TestSplineMovementFlagChangeTranslation(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0xf130000001000042)
	tests := []struct {
		legacy uint16
		modern uint16
	}{
		{legacySMSGSplineStartSwim, SMSGMoveSplineStartSwim},
		{legacySMSGSplineStopSwim, SMSGMoveSplineStopSwim},
		{legacySMSGSplineSetRunMode, SMSGMoveSplineSetRunMode},
		{legacySMSGSplineSetWalkMode, SMSGMoveSplineSetWalkMode},
	}
	for _, test := range tests {
		change, handled, err := ParseLegacyMovementFlagChange(test.legacy, legacy)
		if err != nil || !handled || change.Modern != test.modern || change.GUID != 0xf130000001000042 || change.HasCounter {
			t.Fatalf("legacy=%#x handled=%v err=%v change=%#v", test.legacy, handled, err, change)
		}
		body, err := EncodeModernMovementFlagChange(change, GUID128{Low: 0x42, High: 1})
		if err != nil {
			t.Fatal(err)
		}
		low, high, consumed, err := readPackedGUID128(body)
		if err != nil || low != 0x42 || high != 1 || consumed != len(body) {
			t.Fatalf("modern body=%x low=%x high=%x consumed=%d err=%v", body, low, high, consumed, err)
		}
	}
}

func TestMovementFlagAckRoundTrip(t *testing.T) {
	move := LegacyMovement{MoveFlags: legacyMoveWaterWalking, MoveTime: 99, X: 1, Y: 2, Z: 3, Orientation: 0.5, TransportSeat: -1}
	mover := GUID128{Low: 0x42, High: 1}
	body := appendModernMovementInfoResolved(nil, mover.Low, mover.High, &move, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 11)
	ack, err := ParseModernMovementFlagAck(body)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Move.Mover != mover || ack.Counter != 11 {
		t.Fatalf("ack=%#v", ack)
	}
	legacy, err := EncodeLegacyMovementFlagAck(legacyCMSGMoveWaterWalkAck, 0x42, 0, ack)
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: legacy}
	guid, err := r.guid64()
	if err != nil || guid != 0x42 {
		t.Fatalf("guid=%x err=%v", guid, err)
	}
	counter, err := r.u32()
	if err != nil || counter != 11 {
		t.Fatalf("counter=%d err=%v", counter, err)
	}
	decoded, err := r.movementInfo()
	applied, appliedErr := r.u32()
	if err != nil || appliedErr != nil || r.remaining() != 0 || decoded.MoveTime != 99 || applied != 1 {
		t.Fatalf("remaining=%d applied=%d move=%#v err=%v appliedErr=%v", r.remaining(), applied, decoded, err, appliedErr)
	}

	root, err := EncodeLegacyMovementFlagAck(legacyCMSGForceMoveRootAck, 0x42, 0, ack)
	if err != nil || len(root) != len(legacy)-4 {
		t.Fatalf("root ACK bytes=%d, water-walk ACK bytes=%d, err=%v", len(root), len(legacy), err)
	}

	for modern, wantLegacy := range map[uint16]uint32{
		CMSGMoveForceRootAck:      legacyCMSGForceMoveRootAck,
		CMSGMoveForceUnrootAck:    legacyCMSGForceMoveUnrootAck,
		CMSGMoveWaterWalkAck:      legacyCMSGMoveWaterWalkAck,
		CMSGMoveSetCanFlyAck:      legacyCMSGMoveSetCanFlyAck,
		CMSGMoveGravityDisableAck: legacyCMSGMoveGravityDisableAck,
		CMSGMoveGravityEnableAck:  legacyCMSGMoveGravityEnableAck,
		CMSGMoveFeatherFallAck:    legacyCMSGMoveFeatherFallAck,
	} {
		got, ok := LegacyOpcodeForModernMovementFlagAck(modern)
		if !ok || got != wantLegacy {
			t.Fatalf("modern=%#x legacy=%#x ok=%v, want %#x", modern, got, ok, wantLegacy)
		}
	}

	flyMove := LegacyMovement{MoveFlags: legacyMoveCanFly, MoveTime: 99, X: 1, Y: 2, Z: 3, Orientation: 0.5, TransportSeat: -1}
	flyBody := appendModernMovementInfoResolved(nil, mover.Low, mover.High, &flyMove, 0, 0)
	flyBody = binary.LittleEndian.AppendUint32(flyBody, 11)
	flyAck, err := ParseModernMovementFlagAck(flyBody)
	if err != nil {
		t.Fatal(err)
	}
	flyLegacy, err := EncodeLegacyMovementFlagAck(legacyCMSGMoveSetCanFlyAck, 0x42, 0, flyAck)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(flyLegacy[len(flyLegacy)-4:]) != 1 {
		t.Fatalf("can-fly ACK applied=%d, want 1; body=%x", binary.LittleEndian.Uint32(flyLegacy[len(flyLegacy)-4:]), flyLegacy)
	}
	fallMove := LegacyMovement{MoveFlags: legacyMoveFallingSlow, MoveTime: 99, X: 1, Y: 2, Z: 3, Orientation: 0.5, TransportSeat: -1}
	fallBody := appendModernMovementInfoResolved(nil, mover.Low, mover.High, &fallMove, 0, 0)
	fallBody = binary.LittleEndian.AppendUint32(fallBody, 11)
	fallAck, err := ParseModernMovementFlagAck(fallBody)
	if err != nil {
		t.Fatal(err)
	}
	if modernMovementFlags(legacyMoveFallingSlow) != 0x08000000 || modernMovementFlagsToLegacy(0x08000000)&legacyMoveFallingSlow == 0 {
		t.Fatal("falling-slow flag did not map to CanSafeFall")
	}
	fallLegacy, err := EncodeLegacyMovementFlagAck(legacyCMSGMoveFeatherFallAck, 0x42, 0, fallAck)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(fallLegacy[len(fallLegacy)-4:]) != 1 {
		t.Fatalf("feather-fall ACK applied=%d, want 1; body=%x", binary.LittleEndian.Uint32(fallLegacy[len(fallLegacy)-4:]), fallLegacy)
	}
	if SMSGMoveSetCanFly != 11781 || SMSGMoveUnsetCanFly != 11782 {
		t.Fatalf("can-fly opcodes set=%d unset=%d, want 11781/11782", SMSGMoveSetCanFly, SMSGMoveUnsetCanFly)
	}
}

func TestMovementFlagChangeRejectsMalformed(t *testing.T) {
	if _, handled, err := ParseLegacyMovementFlagChange(1, nil); handled || err != nil {
		t.Fatalf("unknown handled=%v err=%v", handled, err)
	}
	if _, _, err := ParseLegacyMovementFlagChange(legacySMSGMoveLandWalk, []byte{1}); err == nil {
		t.Fatal("truncated movement-flag packet should fail")
	}
	if _, err := EncodeModernMovementFlagChange(MovementFlagChange{}, GUID128{}); err == nil {
		t.Fatal("empty GUID should fail")
	}
}

func TestEncodeSetPlayHoverAnim(t *testing.T) {
	if SMSGSetPlayHoverAnim != 0x25BA {
		t.Fatalf("SMSG_SET_PLAY_HOVER_ANIM=%#x, want 0x25BA", SMSGSetPlayHoverAnim)
	}
	body := EncodeSetPlayHoverAnim(GUID128{Low: 0x42}, true)
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil || low != 0x42 || high != 0 || consumed >= len(body) {
		t.Fatalf("guid low=%x high=%x consumed=%d err=%v body=%x", low, high, consumed, err, body)
	}
	if body[consumed] != 0x80 {
		t.Fatalf("play bit byte=%02x, want 0x80", body[consumed])
	}
	off := EncodeSetPlayHoverAnim(GUID128{Low: 0x42}, false)
	if off[consumed] != 0 {
		t.Fatalf("clear bit byte=%02x, want 0", off[consumed])
	}
	play, ok := PlayHoverAnimForSplineOpcode(legacySMSGSplineSetHover)
	if !ok || !play {
		t.Fatalf("set-hover play=%v ok=%v", play, ok)
	}
	play, ok = PlayHoverAnimForSplineOpcode(legacySMSGSplineUnsetHover)
	if !ok || play {
		t.Fatalf("unset-hover play=%v ok=%v", play, ok)
	}
	if _, ok := PlayHoverAnimForSplineOpcode(legacySMSGSplineRoot); ok {
		t.Fatal("root should not emit SetPlayHoverAnim")
	}
}
