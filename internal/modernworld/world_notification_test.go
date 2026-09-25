package modernworld

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWorldNotifications(t *testing.T) {
	last := binary.LittleEndian.AppendUint32(nil, 580)
	out, err := TranslateLegacyWorldNotification(0x0320, last)
	if err != nil || out.Opcode != 0x2688 || !bytes.Equal(out.Body, last) {
		t.Fatalf("last instance: %+v %v", out, err)
	}
	for _, bad := range [][]byte{nil, last[:3], append(append([]byte(nil), last...), 0)} {
		if _, err := TranslateLegacyWorldNotification(0x0320, bad); err == nil {
			t.Fatal("accepted malformed instance")
		}
	}
	fixed := binary.LittleEndian.AppendUint32(nil, 6)
	fixed = appendFloat32(fixed, 123.5)
	fixed = appendFloat32(fixed, -45)
	fixed = binary.LittleEndian.AppendUint32(fixed, 7)
	fixed = binary.LittleEndian.AppendUint32(fixed, 9)
	for _, name := range []string{"", "银行", strings.Repeat("中", 30), "a" + strings.Repeat("中", 30)} {
		body := append(append([]byte(nil), fixed...), []byte(name)...)
		body = append(body, 0)
		out, err := TranslateLegacyWorldNotification(0x0224, body)
		if err != nil || out.Opcode != 0x2798 {
			t.Fatalf("POI: %+v %v", out, err)
		}
		if binary.LittleEndian.Uint32(out.Body) != 1 || !bytes.Equal(out.Body[4:16], fixed[:12]) || binary.LittleEndian.Uint32(out.Body[16:20]) != 0 || !bytes.Equal(out.Body[20:28], fixed[12:]) || binary.LittleEndian.Uint32(out.Body[28:32]) != 0 {
			t.Fatalf("POI fixed fields=%x", out.Body)
		}
		text := string(out.Body[33:])
		if int(out.Body[32]>>2) != len(text) || len(text) > 63 || !utf8.ValidString(text) || !strings.HasPrefix(name, text) {
			t.Fatalf("POI name=%q", text)
		}
		for i := 0; i < len(body); i++ {
			if _, err := TranslateLegacyWorldNotification(0x0224, body[:i]); err == nil {
				t.Fatalf("accepted prefix %d", i)
			}
		}
		if _, err := TranslateLegacyWorldNotification(0x0224, append(body, 0)); err == nil {
			t.Fatal("accepted trailing POI byte")
		}
	}
}

func TestObserverMovementNotifications(t *testing.T) {
	for _, opcode := range []uint16{0x00ec, 0x00ed, 0x0518} {
		flags := uint32(0)
		if opcode == 0x00ec {
			flags = legacyMoveRoot
		}
		body := appendLegacyPackedGUID(nil, 0x42)
		body = binary.LittleEndian.AppendUint32(body, flags)
		body = binary.LittleEndian.AppendUint16(body, 0)
		body = binary.LittleEndian.AppendUint32(body, 123)
		for _, f := range []float32{1, 2, 3, 0.5} {
			body = appendFloat32(body, f)
		}
		body = binary.LittleEndian.AppendUint32(body, 0)
		if opcode == 0x0518 {
			body = appendFloat32(body, 2.75)
		}
		guid, move, err := ParseLegacyPlayerMovementForOpcode(opcode, body)
		if err != nil || guid != 0x42 || !IsLegacyPlayerMovementOpcode(opcode) {
			t.Fatalf("opcode=%x move=%+v err=%v", opcode, move, err)
		}
		mover := ModernGUIDForLegacy(guid, 0)
		var encoded []byte
		if opcode == 0x0518 {
			encoded = EncodeMoveUpdateCollisionHeight(move, mover, GUID128{})
		} else {
			encoded = EncodeMoveUpdate(move, mover, GUID128{})
		}
		r := movementReader{data: encoded}
		got, err := r.modernMovementStats()
		if err != nil || got.Mover != mover || got.Move.MoveFlags != modernMovementFlags(flags) || got.Move.X != 1 || got.Move.MoveTime != 123 {
			t.Fatalf("decoded=%+v err=%v", got, err)
		}
		if opcode == 0x0518 {
			h, _ := r.f32()
			scale, _ := r.f32()
			if h != 2.75 || scale != 1 {
				t.Fatalf("height=%v scale=%v", h, scale)
			}
		}
		if r.remaining() != 0 {
			t.Fatal("trailing modern bytes")
		}
		for i := 0; i < len(body); i++ {
			if _, _, err := ParseLegacyPlayerMovementForOpcode(opcode, body[:i]); err == nil {
				t.Fatalf("accepted prefix %x/%d", opcode, i)
			}
		}
		if _, _, err := ParseLegacyPlayerMovementForOpcode(opcode, append(body, 0)); err == nil {
			t.Fatal("accepted trailing movement byte")
		}
	}
}
