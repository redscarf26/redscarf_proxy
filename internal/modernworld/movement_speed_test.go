package modernworld

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestForceRunSpeedChangeToMoveSetSpeed(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	legacy = append(legacy, 0) // WotLK extra byte only on FORCE_RUN
	legacy = appendFloat32(legacy, 4.5)
	change, handled, err := ParseLegacySpeedChange(legacySMSGForceRunSpeedChange, legacy)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if change.Modern != SMSGMoveSetRunSpeed || change.GUID != 0x42 || change.Counter != 7 || change.Speed != 4.5 {
		t.Fatalf("change=%#v", change)
	}
	guid := GUID128{Low: 0x42, High: 1}
	body, err := EncodeModernSpeedChange(change, guid)
	if err != nil {
		t.Fatal(err)
	}
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil || low != 0x42 || high != 1 {
		t.Fatalf("guid=%x:%x err=%v", low, high, err)
	}
	if binary.LittleEndian.Uint32(body[consumed:]) != 7 {
		t.Fatalf("counter=%x", body[consumed:consumed+4])
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(body[consumed+4:])) != 4.5 {
		t.Fatalf("speed bits=%x", body[consumed+4:])
	}
}

func TestForceWalkAndSplineSpeedChange(t *testing.T) {
	walk := appendLegacyPackedGUID(nil, 0x43)
	walk = binary.LittleEndian.AppendUint32(walk, 3)
	walk = appendFloat32(walk, 2.5)
	change, handled, err := ParseLegacySpeedChange(legacySMSGForceWalkSpeedChange, walk)
	if err != nil || !handled || change.Modern != SMSGMoveSetWalkSpeed || change.Counter != 3 {
		t.Fatalf("walk handled=%v err=%v change=%#v", handled, err, change)
	}
	runBack, handled, err := ParseLegacySpeedChange(legacySMSGForceRunBackSpeedChange, walk)
	if err != nil || !handled || runBack.Modern != SMSGMoveSetRunBackSpeed {
		t.Fatalf("run-back mapped to %d handled=%v err=%v", runBack.Modern, handled, err)
	}
	spline := appendLegacyPackedGUID(nil, 0x44)
	spline = appendFloat32(spline, 7)
	spl, handled, err := ParseLegacySpeedChange(legacySMSGSplineSetRunSpeed, spline)
	if err != nil || !handled || spl.Modern != SMSGMoveSplineSetRunSpeed || spl.Counter != 0 {
		t.Fatalf("spline handled=%v err=%v change=%#v", handled, err, spl)
	}
	body, err := EncodeModernSpeedChange(spl, GUID128{Low: 0x44, High: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != consumed+4 {
		t.Fatalf("spline body should be guid+float, got %d extra", len(body)-consumed-4)
	}
}

func TestSpeedAckRoundTrip(t *testing.T) {
	move := LegacyMovement{MoveTime: 99, X: 1, Y: 2, Z: 3, Orientation: 0.5, TransportSeat: -1}
	mover := GUID128{Low: 0x42, High: 1}
	status := appendModernMovementInfoResolved(nil, mover.Low, mover.High, &move, 0, 0)
	status = binary.LittleEndian.AppendUint32(status, 11)
	status = appendFloat32(status, 3.5)
	ack, err := ParseModernSpeedAck(status)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Move.Mover != mover || ack.Counter != 11 || ack.Speed != 3.5 {
		t.Fatalf("ack=%#v", ack)
	}
	legacy, err := EncodeLegacyForceSpeedAck(0x42, 0, ack)
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
	if err != nil {
		t.Fatal(err)
	}
	speed, err := r.f32()
	if err != nil || speed != 3.5 || r.remaining() != 0 || decoded.MoveTime != 99 {
		t.Fatalf("speed=%v remaining=%d move=%#v err=%v", speed, r.remaining(), decoded, err)
	}
	legacyOpcode, ok := LegacyOpcodeForModernSpeedAck(CMSGMoveForceRunSpeedAck)
	if !ok || legacyOpcode != legacyCMSGForceRunSpeedChangeAck {
		t.Fatalf("ack opcode map = %d %v", legacyOpcode, ok)
	}
}

func TestSpeedChangeRejectsUnknownAndMalformed(t *testing.T) {
	if _, handled, err := ParseLegacySpeedChange(0x0001, nil); handled || err != nil {
		t.Fatalf("unknown opcode handled=%v err=%v", handled, err)
	}
	if _, _, err := ParseLegacySpeedChange(legacySMSGForceRunSpeedChange, []byte{1}); err == nil || !strings.Contains(err.Error(), "GUID") {
		t.Fatalf("malformed err=%v", err)
	}
	if _, err := EncodeModernSpeedChange(legacySpeedChange{Kind: speedKindForce}, GUID128{}); err == nil {
		t.Fatal("empty GUID should fail")
	}
}

func TestClassic54261SpeedOpcodesMatchAshamaneOffset(t *testing.T) {
	if SMSGMoveUpdate != 11744 || SMSGMoveSetActiveMover != 11733 || SMSGMoveTeleport != 11780 {
		t.Fatal("anchor opcodes moved; re-check Ashamane+50 mapping")
	}
	if SMSGMoveSetRunSpeed != 11760 || SMSGMoveSetWalkSpeed != 11766 || SMSGMoveSplineSetRunSpeed != 11751 {
		t.Fatalf("set/spline opcodes drifted: run=%d walk=%d spline=%d", SMSGMoveSetRunSpeed, SMSGMoveSetWalkSpeed, SMSGMoveSplineSetRunSpeed)
	}
	if SMSGMoveUpdateRunSpeed != 0x2DD6 || SMSGMoveUpdatePitchRate != 0x2DDE {
		t.Fatalf("update-speed opcodes drifted: run=%d pitch=%d", SMSGMoveUpdateRunSpeed, SMSGMoveUpdatePitchRate)
	}
	if CMSGMoveForcePitchRateAck != 14898 {
		t.Fatalf("pitch ack=%d, want 14898", CMSGMoveForcePitchRateAck)
	}
}
