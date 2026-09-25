package modernworld

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestModernPlayerMovementToLegacyRoundTrip(t *testing.T) {
	legacyFlags := uint32(0x00000001 | legacyMoveFalling | legacyMoveSwimming | legacyMoveSplineElevation)
	move := LegacyMovement{
		MoveFlags: legacyFlags, MoveTime: 12345,
		X: 1, Y: 2, Z: 3, Orientation: -0.5,
		Pitch: 0.25, FallTime: 400, JumpVelocity: 8,
		JumpSinAngle: 0.1, JumpCosAngle: 0.9, JumpXYSpeed: 5,
		SplineElevation: 1.5, TransportSeat: -1,
	}
	mover := GUID128{Low: 0x42, High: uint64(2)<<58 | uint64(1)<<42}
	modernBody := appendModernMovementInfoResolved(nil, mover.Low, mover.High, &move, 0, 0)

	parsed, err := ParseModernPlayerMovement(modernBody)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Mover != mover || parsed.Move.MoveFlags != modernMovementFlags(legacyFlags) || parsed.Move.MoveTime != 12345 {
		t.Fatalf("unexpected modern movement: %#v", parsed)
	}
	if parsed.Move.Orientation < 5.78 || parsed.Move.JumpVelocity != 8 || parsed.Move.SplineElevation != 1.5 {
		t.Fatalf("movement details were not preserved: %#v", parsed.Move)
	}

	legacyBody, err := EncodeLegacyPlayerMovement(parsed, 0x42, 0)
	if err != nil {
		t.Fatal(err)
	}
	legacyMover, decoded, err := ParseLegacyPlayerMovement(legacyBody)
	if err != nil {
		t.Fatal(err)
	}
	if legacyMover != 0x42 || decoded.MoveFlags != legacyFlags || decoded.MoveTime != move.MoveTime || decoded.JumpXYSpeed != 5 {
		t.Fatalf("unexpected legacy movement: mover=%x move=%#v", legacyMover, decoded)
	}
}

func TestPlayerMovementTransportRoundTrip(t *testing.T) {
	move := LegacyMovement{
		MoveFlags: legacyMoveOnTransport | 1, MoveExtra: legacyMoveInterpolate,
		MoveTime: 55, X: 1, Y: 2, Z: 3, Orientation: 0.5,
		TransportX: 4, TransportY: 5, TransportZ: 6, TransportOrientation: -1,
		TransportTime: 66, TransportTime2: 44, TransportSeat: 2,
	}
	legacyBody := appendLegacyPackedGUID(nil, 0x42)
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, move.MoveFlags)
	legacyBody = binary.LittleEndian.AppendUint16(legacyBody, move.MoveExtra)
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, move.MoveTime)
	for _, value := range []float32{move.X, move.Y, move.Z, move.Orientation} {
		legacyBody = appendFloat32(legacyBody, value)
	}
	legacyBody = appendLegacyPackedGUID(legacyBody, 0xf110000001000099)
	for _, value := range []float32{move.TransportX, move.TransportY, move.TransportZ, move.TransportOrientation} {
		legacyBody = appendFloat32(legacyBody, value)
	}
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, move.TransportTime)
	legacyBody = append(legacyBody, byte(move.TransportSeat))
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, move.TransportTime2)
	legacyBody = binary.LittleEndian.AppendUint32(legacyBody, 0) // fall time

	moverGUID, parsedLegacy, err := ParseLegacyPlayerMovement(legacyBody)
	if err != nil {
		t.Fatal(err)
	}
	transport := GUID128{Low: 0x99, High: 0x1234}
	modernBody := EncodeMoveUpdate(parsedLegacy, GUID128{Low: moverGUID, High: 1}, transport)
	parsedModern, err := ParseModernPlayerMovement(modernBody)
	if err != nil {
		t.Fatal(err)
	}
	if parsedModern.Transport != transport || parsedModern.Move.TransportSeat != 2 || parsedModern.Move.TransportTime != 66 {
		t.Fatalf("unexpected modern transport movement: %#v", parsedModern)
	}
}

func TestMovementOpcodeMap(t *testing.T) {
	if len(modernToLegacyPlayerMoveOpcode) != 31 {
		t.Fatalf("movement opcode count=%d", len(modernToLegacyPlayerMoveOpcode))
	}
	tests := map[uint16]uint32{
		0x3A33:                  0x046d,
		CMSGMoveStartForward:    0x00b5,
		CMSGMoveStop:            0x00b7,
		CMSGMoveHeartbeat:       0x00ee,
		CMSGMoveStartDescend:    0x03a7,
		CMSGMoveDoubleJump:      0x00da,
		CMSGMoveRemoveForces:    0x00da,
		CMSGMoveSetFacing:       0x00da,
		CMSGMoveChangeTransport: 0x00da,
	}
	for modern, want := range tests {
		got, ok := LegacyOpcodeForModernMovement(modern)
		if !ok || got != want {
			t.Fatalf("modern opcode %d maps to %04x, %v; want %04x", modern, got, ok, want)
		}
	}
	if _, ok := LegacyOpcodeForModernMovement(0xffff); ok || !IsLegacyPlayerMovementOpcode(0x00ee) || !IsLegacyPlayerMovementOpcode(0x00c5) || !IsLegacyPlayerMovementOpcode(0x00cd) || !IsLegacyPlayerMovementOpcode(0x00d1) || !IsLegacyPlayerMovementOpcode(0x03ad) || IsLegacyPlayerMovementOpcode(0xffff) {
		t.Fatal("movement opcode membership mismatch")
	}
}

func TestDismissControlledVehicleMovementPayload(t *testing.T) {
	// Synthetic flying-vehicle fixture, not a captured client packet.
	const vehicle = uint64(0xf150006ffe0272d6)
	mover := ModernGUIDForLegacy(vehicle, 609)
	move := LegacyMovement{MoveFlags: legacyMoveDisableGravity, MoveTime: 12345,
		X: 2200, Y: -5700, Z: 150, Orientation: 1.5, TransportSeat: -1}
	body := EncodeMoveUpdate(move, mover, GUID128{})
	parsed, err := ParseModernPlayerMovement(body)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Mover != mover {
		t.Fatalf("vehicle mover changed: %#v", parsed.Mover)
	}
	opcode, ok := LegacyOpcodeForModernMovement(0x3a33)
	if !ok || opcode != 0x046d {
		t.Fatalf("dismiss maps to %x, %v; want CMSG_DISMISS_CONTROLLED_VEHICLE", opcode, ok)
	}
	legacyBody, err := EncodeLegacyPlayerMovement(parsed, vehicle, 0)
	if err != nil {
		t.Fatal(err)
	}
	guid, decoded, err := ParseLegacyPlayerMovement(legacyBody)
	if err != nil || guid != vehicle || decoded.MoveFlags != move.MoveFlags ||
		decoded.MoveTime != move.MoveTime || decoded.X != move.X || decoded.Y != move.Y || decoded.Z != move.Z {
		t.Fatalf("dismiss movement lost: guid=%x move=%#v err=%v", guid, decoded, err)
	}
	if _, err := ParseModernPlayerMovement(body[:len(body)-1]); err == nil {
		t.Fatal("accepted truncated dismiss movement")
	}
}

func TestMoveSplineDoneRoundTrip(t *testing.T) {
	modern := appendPackedGUID128(nil, 0x42, 1)
	modern = binary.LittleEndian.AppendUint32(modern, 0)
	modern = binary.LittleEndian.AppendUint32(modern, 0)
	modern = binary.LittleEndian.AppendUint32(modern, 0)
	modern = binary.LittleEndian.AppendUint32(modern, 123)
	for _, value := range []float32{1, 2, 3, 0.5, 0, 0} {
		modern = appendFloat32(modern, value)
	}
	modern = binary.LittleEndian.AppendUint32(modern, 0) // removed forces
	modern = binary.LittleEndian.AppendUint32(modern, 0) // move index
	modern = append(modern, 0)                           // eight optional bits
	modern = binary.LittleEndian.AppendUint32(modern, 77)
	movement, splineID, err := ParseMoveSplineDone(modern)
	if err != nil || movement.Mover.Low != 0x42 || splineID != 77 {
		t.Fatalf("movement=%#v spline=%d err=%v", movement, splineID, err)
	}
	legacy, err := EncodeLegacyMoveSplineDone(movement, 0x42, 0, splineID)
	if err != nil || binary.LittleEndian.Uint32(legacy[len(legacy)-4:]) != 77 {
		t.Fatalf("legacy=%x err=%v", legacy, err)
	}
}

func TestLegacyRunSpeedMovementConsumesOpcodeTrailer(t *testing.T) {
	move := LegacyMovement{MoveTime: 123, X: 1, Y: 2, Z: 3, Orientation: 0.5, TransportSeat: -1}
	body, err := EncodeLegacyPlayerMovement(PlayerMovement{Move: move}, 0x42, 0)
	if err != nil {
		t.Fatal(err)
	}
	body = appendFloat32(body, 7.0)
	if _, _, err := ParseLegacyPlayerMovement(body); err == nil {
		t.Fatal("opcode-agnostic parser accepted the run-speed trailer")
	}
	mover, parsed, err := ParseLegacyPlayerMovementForOpcode(0x00cd, body)
	if err != nil || mover != 0x42 || parsed.RunSpeed != 7.0 {
		t.Fatalf("mover=%x runSpeed=%v err=%v", mover, parsed.RunSpeed, err)
	}
	if _, _, err := ParseLegacyPlayerMovementForOpcode(0x00ee, body); err == nil {
		t.Fatal("unrelated movement opcode accepted the run-speed trailer")
	}
	modernOpcode, modernBody, ok := EncodeMoveUpdateSpeed(0x00cd, parsed, GUID128{Low: mover, High: 1}, GUID128{})
	if !ok || modernOpcode != SMSGMoveUpdateRunSpeed || len(modernBody) < 4 || math.Float32frombits(binary.LittleEndian.Uint32(modernBody[len(modernBody)-4:])) != 7.0 {
		t.Fatalf("modern opcode=%x body=%x ok=%v", modernOpcode, modernBody, ok)
	}
	if _, err := ParseModernPlayerMovement(modernBody[:len(modernBody)-4]); err != nil {
		t.Fatalf("modern speed MovementInfo: %v", err)
	}
}

func TestMoveTimeSkippedTranslation(t *testing.T) {
	body := appendPackedGUID128(nil, 0x42, 1)
	body = binary.LittleEndian.AppendUint32(body, 250)
	mover, skipped, err := ParseMoveTimeSkipped(body)
	if err != nil || mover.Low != 0x42 || mover.High != 1 || skipped != 250 {
		t.Fatalf("mover=%#v skipped=%d err=%v", mover, skipped, err)
	}
	legacy := EncodeLegacyMoveTimeSkipped(0x42, skipped)
	r := movementReader{data: legacy}
	guid, err := r.guid64()
	if err != nil || guid != 0x42 || binary.LittleEndian.Uint32(legacy[len(legacy)-4:]) != 250 {
		t.Fatalf("legacy=%x guid=%x err=%v", legacy, guid, err)
	}
}

func TestModernPlayerMovementRejectsMalformed(t *testing.T) {
	move := LegacyMovement{MoveTime: 1, TransportSeat: -1}
	body := appendModernMovementInfoResolved(nil, 0x42, 1, &move, 0, 0)
	badCount := append([]byte(nil), body...)
	guidBytes := len(appendPackedGUID128(nil, 0x42, 1))
	// GUID + 3 flag words + time + six floats = removed-force count.
	binary.LittleEndian.PutUint32(badCount[guidBytes+40:], maxRemovedMovementForces+1)
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{name: "truncated", body: body[:len(body)-1], want: "standing bit"},
		{name: "too many forces", body: badCount, want: "exceeds limit"},
		{name: "trailing", body: append(append([]byte(nil), body...), 1), want: "trailing bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseModernPlayerMovement(test.body)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want substring %q", err, test.want)
			}
		})
	}
}

func TestMovementFlagMappingExcludesTransportAndCollision(t *testing.T) {
	legacy := uint32(legacyMoveOnTransport | legacyMoveDisableGravity | legacyMoveRoot | legacyMoveFallingFar | 1)
	modern := modernMovementFlags(legacy)
	if modern&0x100 != 0 || modern&0x200 == 0 || modern&0x400 == 0 || modern&modernMoveFallingFar == 0 {
		t.Fatalf("legacy-to-modern flags=%08x", modern)
	}
	if got := modernMovementFlagsToLegacy(modern | 0x20000000); got != legacy&^legacyMoveOnTransport {
		t.Fatalf("modern-to-legacy flags=%08x want=%08x", got, legacy&^legacyMoveOnTransport)
	}
	if math.IsNaN(float64(clampOrientation(-1))) {
		t.Fatal("orientation clamp returned NaN")
	}
}
