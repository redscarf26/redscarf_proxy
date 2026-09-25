package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestActualMapDifficulty(t *testing.T) {
	for _, tc := range []struct{ mapID, mode, id, size uint32 }{
		{0, 0, 0, 0}, {530, 0, 0, 0}, {571, 0, 0, 0}, {9999, 1, 0, 0},
		{574, 0, 1, 5}, {574, 1, 2, 5}, {631, 0, 3, 10}, {631, 1, 4, 25},
		{631, 2, 5, 10}, {631, 3, 6, 25}, {409, 0, 9, 40}, {509, 0, 148, 20},
	} {
		got := WorldServerInfoForDifficulty(tc.mapID, MapDifficulty{SpawnMode: tc.mode})
		if got.DifficultyID != tc.id {
			t.Fatalf("%+v: %+v", tc, got)
		}
		if tc.size == 0 {
			if got.InstanceGroupSize != nil {
				t.Fatal("outdoor group size")
			}
		} else if got.InstanceGroupSize == nil || *got.InstanceGroupSize != tc.size {
			t.Fatalf("%+v: size", tc)
		}
	}
	for _, body := range [][]byte{nil, pveWords(4, 0), pveWords(0, 2), pveWords(0, 0, 0)} {
		if _, err := ParseLegacyInstanceDifficulty(body); err == nil {
			t.Fatalf("accepted %x", body)
		}
	}
}

func TestKnockBackCommandAndAck(t *testing.T) {
	b := appendLegacyPackedGUID(nil, 0x42)
	b = binary.LittleEndian.AppendUint32(b, 17)
	for _, v := range []float32{0, 1, 12, -8} {
		b = appendFloat32(b, v)
	}
	k, err := ParseLegacyKnockBack(b)
	if err != nil || k.Counter != 17 || k.VerticalSpeed != -8 {
		t.Fatalf("%+v %v", k, err)
	}
	guid := ModernGUIDForLegacy(0x42, 571)
	out := EncodeModernKnockBack(k, guid)
	_, _, n, err := readPackedGUID128(out)
	if err != nil || binary.LittleEndian.Uint32(out[n:]) != 17 || len(out[n:]) != 20 {
		t.Fatalf("%x %v", out, err)
	}
	m := LegacyMovement{MoveFlags: legacyMoveFalling, X: 1, Y: 2, Z: 3, JumpVelocity: -8, JumpXYSpeed: 12, JumpSinAngle: 1}
	ackBody := binary.LittleEndian.AppendUint32(EncodeMoveUpdate(m, guid, GUID128{}), 17)
	ack, err := ParseModernMovementFlagAck(ackBody)
	if err != nil {
		t.Fatal(err)
	}
	op, ok := LegacyOpcodeForModernMovementFlagAck(CMSGMoveKnockBackAck)
	if !ok || op != 0xF0 {
		t.Fatal(op, ok)
	}
	legacy, err := EncodeLegacyMovementFlagAck(op, 0x42, 0, ack)
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: legacy}
	g, _ := r.guid64()
	counter, _ := r.u32()
	got, err := r.movementInfo()
	if err != nil || g != 0x42 || counter != 17 || got.JumpXYSpeed != 12 || r.remaining() != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	base, err := EncodeLegacyPlayerMovement(ack.Move, 0x42, 0)
	if err != nil {
		t.Fatal(err)
	}
	broadcast := base
	for _, v := range []float32{1, 0, 12, -8} {
		broadcast = appendFloat32(broadcast, v)
	}
	_, updated, err := ParseLegacyKnockBackUpdate(broadcast)
	if err != nil || updated.JumpVelocity != -8 {
		t.Fatal(updated, err)
	}
	for i := 0; i < len(b); i++ {
		if _, err := ParseLegacyKnockBack(b[:i]); err == nil {
			t.Fatalf("accepted prefix %d", i)
		}
	}
}
