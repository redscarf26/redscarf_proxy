package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestKnockbackAckOptionalSpeeds(t *testing.T) {
	m := LegacyMovement{MoveFlags: legacyMoveFalling, JumpXYSpeed: 12, JumpVelocity: -8, JumpSinAngle: 1}
	b := binary.LittleEndian.AppendUint32(EncodeMoveUpdate(m, guildTestResolve(42), GUID128{}), 17)
	for _, speeds := range []bool{false, true} {
		body := append(append([]byte(nil), b...), 0)
		if speeds {
			body[len(body)-1] = 0x80
			body = appendFloat32(appendFloat32(body, 20), -9)
		}
		ack, err := ParseModernKnockBackAck(body)
		if err != nil || ack.Counter != 17 {
			t.Fatalf("ack: %+v %v", ack, err)
		}
		wantXY, wantZ := float32(12), float32(-8)
		if speeds {
			wantXY, wantZ = 20, -9
		}
		wire, err := EncodeLegacyMovementFlagAck(0xF0, 42, 0, ack)
		if err != nil {
			t.Fatal(err)
		}
		r := movementReader{data: wire}
		r.guid64()
		r.u32()
		move, err := r.movementInfo()
		if err != nil || move.JumpXYSpeed != wantXY || move.JumpVelocity != wantZ || r.remaining() != 0 {
			t.Fatalf("legacy movement: %+v %v", move, err)
		}
		for i := range body {
			if _, err := ParseModernKnockBackAck(body[:i]); err == nil {
				t.Fatalf("accepted prefix %d/%d", i, len(body))
			}
		}
		if _, err := ParseModernKnockBackAck(append(body, 0)); err == nil {
			t.Fatal("accepted garbage suffix")
		}
		if _, err := ParseModernMovementFlagAck(body); err == nil {
			t.Fatal("ordinary flag ACK accepted knockback suffix")
		}
	}
}

func TestInterruptExecuteLogMultipleTargetsAndEffects(t *testing.T) {
	b := appendLegacyPackedGUID(nil, 42)
	for _, v := range []uint32{47528, 2, 68, 2} {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	for _, id := range []uint64{51, 52} {
		b = appendLegacyPackedGUID(b, id)
		b = binary.LittleEndian.AppendUint32(b, uint32(id+1000))
	}
	for _, v := range []uint32{24, 1, 6948} {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	log, err := ParseLegacySpellExecuteLog(b)
	if err != nil || len(log.Effects) != 2 || len(log.Effects[0].Interrupts) != 2 || log.Effects[1].TradeSkillItems[0] != 6948 {
		t.Fatalf("%+v %v", log, err)
	}
	packets := EncodeSpellInterruptLogs(log, guildTestResolve)
	if len(packets) != 2 {
		t.Fatal("lost interrupt targets")
	}
	for i, p := range packets {
		r := guildRead(p.Body)
		if p.Opcode != 0x2C1D || r.guid() != guildTestResolve(42) || r.guid() != guildTestResolve(uint64(51+i)) || r.u32() != 47528 || r.u32() != uint32(1051+i) || r.done() != nil {
			t.Fatalf("bad interrupt: %x", p.Body)
		}
	}
	for i := range b {
		if _, err := ParseLegacySpellExecuteLog(b[:i]); err == nil {
			t.Fatalf("accepted truncated effect %d", i)
		}
	}
	if _, err := ParseLegacySpellExecuteLog(append(b, 0)); err == nil {
		t.Fatal("accepted trailing byte")
	}
}

func TestRuntimeMiscWireFormats(t *testing.T) {
	for _, b := range [][]byte{make([]byte, 8), {1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0}} {
		if op, err := ParseRuntimeMiscRequest(CMSGMountSpecialAnim, b); err != nil || op != 0x171 {
			t.Fatal(op, err)
		}
		for i := range b {
			if _, err := ParseRuntimeMiscRequest(CMSGMountSpecialAnim, b[:i]); err == nil {
				t.Fatal("mount prefix accepted")
			}
		}
	}
	if op, err := ParseRuntimeMiscRequest(CMSGEmote, nil); err != nil || op != 0 {
		t.Fatal(op, err)
	}
	if _, err := ParseRuntimeMiscRequest(CMSGEmote, []byte{0}); err == nil {
		t.Fatal("nonempty emote accepted")
	}
	source := guildTestResolve(42)
	r := guildRead(EncodeObjectSound(123, source, GUID128{}, [3]float32{1, 2, 3}))
	if r.u32() != 123 || r.guid() != source || r.guid() != (GUID128{}) {
		t.Fatal("object sound identity")
	}
	for _, f := range []uint32{0x3f800000, 0x40000000, 0x40400000, 0} {
		if r.u32() != f {
			t.Fatal("object sound position")
		}
	}
	if r.done() != nil {
		t.Fatal(r.err)
	}
	if !bytes.Equal(EncodePageTextObject(source), guildAppendGUID(nil, source)) {
		t.Fatal("page GUID")
	}
}

func TestMultipleMovesAtomicValidation(t *testing.T) {
	// Root + water-walk, as emitted by AC Player::SendInitialPacketsAfterAddToMap.
	records := []byte{8, 0xe8, 0, 1, 42, 1, 0, 0, 0, 8, 0xde, 0, 1, 42, 2, 0, 0, 0}
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(records)))
	b = append(b, records...)
	ps, err := ParseLegacyMultipleMoves(b)
	if err != nil || len(ps) != 2 || ps[0].Opcode != 0xe8 || ps[1].Opcode != 0xde || !bytes.Equal(ps[1].Body, []byte{1, 42, 2, 0, 0, 0}) {
		t.Fatal(ps, err)
	}
	for i := range b {
		if ps, err := ParseLegacyMultipleMoves(b[:i]); err == nil || len(ps) != 0 {
			t.Fatal("partial batch escaped")
		}
	}
	bad := append([]byte(nil), b...)
	bad[len(bad)-9] = 255
	if ps, err := ParseLegacyMultipleMoves(bad); err == nil || len(ps) != 0 {
		t.Fatal("malformed second record accepted")
	}
	nested := []byte{3, 0, 0, 0, 2, 0x1e, 5}
	if _, err := ParseLegacyMultipleMoves(nested); err == nil {
		t.Fatal("nested batch accepted")
	}
}
