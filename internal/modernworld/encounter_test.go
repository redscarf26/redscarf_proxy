package modernworld

import (
	"bytes"
	"testing"
)

func TestEncounterUnitFrames(t *testing.T) {
	for kind := uint32(0); kind < 3; kind++ {
		in := append(pveWords(kind), 1, 0x42, 7)
		calls := 0
		resolve := func(guid uint64) GUID128 {
			calls++
			if guid != 0x42 {
				t.Fatalf("legacy GUID %x", guid)
			}
			return GUID128{Low: 0x1234, High: 0x5678}
		}
		got, err := TranslateEncounterUnit(in, resolve)
		want := []byte{3, 3, 0x34, 0x12, 0x78, 0x56}
		if kind != 1 {
			want = append(want, 7)
		}
		if err != nil || got.Opcode != 0x27b0+uint16(kind) || !bytes.Equal(got.Body, want) || calls != 1 {
			t.Fatalf("event %d = %#v %v calls=%d", kind, got, err, calls)
		}
		for n := 0; n < len(in); n++ {
			if _, err := TranslateEncounterUnit(in[:n], resolve); err == nil {
				t.Fatalf("accepted truncated event %x", in[:n])
			}
		}
		if _, err := TranslateEncounterUnit(append(in, 0), resolve); err == nil {
			t.Fatal("accepted trailing byte")
		}
	}
	for kind := uint32(3); kind <= 8; kind++ {
		if _, err := TranslateEncounterUnit(pveWords(kind), nil); err == nil {
			t.Fatalf("silently accepted unaudited event %d", kind)
		}
	}
}

func TestEncodeInstanceEncounterStartAndEnd(t *testing.T) {
	if SMSGInstanceEncounterStart != 0x27B6 || SMSGInstanceEncounterEnd != 0x27BA {
		t.Fatalf("opcodes start=%#x end=%#x", SMSGInstanceEncounterStart, SMSGInstanceEncounterEnd)
	}
	start := EncodeInstanceEncounterStart(true)
	if len(start) != 17 || start[16]&0x80 == 0 {
		t.Fatalf("encounter start %x", start)
	}
	idle := EncodeInstanceEncounterStart(false)
	if len(idle) != 17 || idle[16]&0x80 != 0 {
		t.Fatalf("idle encounter start %x", idle)
	}
}
