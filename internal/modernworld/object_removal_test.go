package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestEncodeObjectRemovals(t *testing.T) {
	destroyed := ModernGUIDForLegacy(0xf110000001000042, 571)
	outOfRange := ModernGUIDForLegacy(0x43, 571)
	body := EncodeObjectRemovals(571, []GUID128{destroyed}, []GUID128{{}, outOfRange})
	if binary.LittleEndian.Uint32(body) != 0 || binary.LittleEndian.Uint16(body[4:]) != 571 || body[6] != 0x80 {
		t.Fatalf("bad removal header: % x", body[:7])
	}
	if got := binary.LittleEndian.Uint16(body[7:]); got != 1 {
		t.Fatalf("destroyed count = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(body[9:]); got != 2 {
		t.Fatalf("total removal count = %d, want 2", got)
	}
	position := 13
	for index, want := range []GUID128{destroyed, outOfRange} {
		low, high, consumed, err := readPackedGUID128(body[position:])
		if err != nil {
			t.Fatal(err)
		}
		if low != want.Low || high != want.High {
			t.Fatalf("GUID %d = %016x:%016x, want %016x:%016x", index, high, low, want.High, want.Low)
		}
		position += consumed
	}
	if position+4 != len(body) || binary.LittleEndian.Uint32(body[position:]) != 0 {
		t.Fatalf("bad trailing object-data length at %d of %d", position, len(body))
	}
	if got := EncodeObjectRemovals(571, nil, nil); len(got) != 11 || binary.LittleEndian.Uint32(got) != 0 {
		t.Fatalf("empty removals encoded as %x", got)
	}
}
