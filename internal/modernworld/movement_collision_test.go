package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestCollisionHeightTranslation(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 17)
	legacy = appendFloat32(legacy, 2.25)
	change, err := ParseLegacyCollisionHeightChange(legacy)
	if err != nil || change.GUID != 0x42 || change.Counter != 17 || change.Height != 2.25 {
		t.Fatalf("change=%#v err=%v", change, err)
	}

	mover := GUID128{Low: 0x42, High: 1}
	modern := EncodeModernCollisionHeightChange(change, mover)
	r := movementReader{data: modern}
	decodedMover, _ := r.guid128()
	counter, _ := r.u32()
	height, _ := r.f32()
	scale, _ := r.f32()
	reason, _ := r.u8()
	mountDisplay, _ := r.u32()
	duration, _ := r.i32()
	if decodedMover != mover || counter != 17 || height != 2.25 || scale != 1 || reason != 2 ||
		mountDisplay != 0 || duration != 2000 || r.remaining() != 0 {
		t.Fatalf("modern collision mover=%#v counter=%d height=%v scale=%v reason=%d mount=%d duration=%d remaining=%d body=%x",
			decodedMover, counter, height, scale, reason, mountDisplay, duration, r.remaining(), modern)
	}
}

func TestCollisionHeightAckTranslation(t *testing.T) {
	move := LegacyMovement{MoveTime: 99, X: 1, Y: 2, Z: 3, Orientation: 0.5, TransportSeat: -1}
	mover := GUID128{Low: 0x42, High: 1}
	body := appendModernMovementInfoResolved(nil, mover.Low, mover.High, &move, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 17)
	body = appendFloat32(body, 2.25)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = append(body, 2)
	ack, err := ParseModernCollisionHeightAck(body)
	if err != nil || ack.Move.Mover != mover || ack.Counter != 17 || ack.Height != 2.25 {
		t.Fatalf("ack=%#v err=%v", ack, err)
	}

	legacy, err := EncodeLegacyCollisionHeightAck(0x42, 0, ack)
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: legacy}
	guid, _ := r.guid64()
	counter, _ := r.u32()
	decodedMove, err := r.movementInfo()
	height, _ := r.f32()
	if err != nil || guid != 0x42 || counter != 17 || decodedMove.MoveTime != 99 || height != 2.25 || r.remaining() != 0 {
		t.Fatalf("legacy ack guid=%x counter=%d move=%#v height=%v remaining=%d err=%v body=%x",
			guid, counter, decodedMove, height, r.remaining(), err, legacy)
	}
}
