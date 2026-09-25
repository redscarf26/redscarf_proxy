package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWrathEnchantVisuals(t *testing.T) {
	// SpellItemEnchantment.dbc field 31, also sent directly by AC's char enum.
	for enchant, want := range map[uint32]uint16{
		3365: 156, 3366: 25, 3367: 161, 3368: 160, 3369: 166, 3370: 1,
		3594: 156, 3595: 161, 3847: 159, 3883: 159,
		3789: 178, 3870: 164,
	} {
		if got := itemEnchantVisual(enchant); got != want {
			t.Errorf("enchant %d visual=%d want=%d", enchant, got, want)
		}
	}
}

func TestPackedVisibleEnchantDelta(t *testing.T) {
	for _, tc := range []struct {
		name   string
		packed uint32
		want   uint16
	}{
		{"razorice", 3370, 1},
		{"fallen crusader", 3368, 160},
		{"temporary only", 13 << 16, 28},
		{"rune and temporary", 3370 | 13<<16, 1},
		{"permanent without visual", 15 | 13<<16, 0},
		{"removed", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := visibleItemEnchantVisual(tc.packed); got != tc.want {
				t.Fatalf("packed=%#x visual=%d want=%d", tc.packed, got, tc.want)
			}
			base := legacyPlayerVisibleItem1 + 15*2
			fields := map[int]uint32{base: 38632, base + 1: tc.packed}
			// Equipping updates the owner; enchant-only updates reach observers.
			for _, active := range []bool{false, true} {
				payload := encodePlayerValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 0, nil, active)
				if len(payload) < 9 || binary.LittleEndian.Uint16(payload[len(payload)-2:]) != tc.want {
					t.Fatalf("active=%v weapon delta=%x want visual=%d", active, payload, tc.want)
				}
				create := (legacyActivePlayerValues{fields: fields}).appendPlayer(nil, 1, ActivePlayerCreateOptions{}, active)
				item := binary.LittleEndian.AppendUint32(nil, 38632)
				item = binary.LittleEndian.AppendUint16(item, 0)
				item = binary.LittleEndian.AppendUint16(item, tc.want)
				if !bytes.Contains(create, item) {
					t.Fatalf("active=%v initial weapon missing %x", active, item)
				}
			}
		})
	}
}

func TestItemEnchantVisualLookup(t *testing.T) {
	if got := itemEnchantVisual(1); got != 61 {
		t.Fatalf("enchant 1 visual=%d want=61", got)
	}
	if got := itemEnchantVisual(3273); got != 166 {
		t.Fatalf("enchant 3273 visual=%d want=166", got)
	}
	if got := itemEnchantVisual(0xffffffff); got != 0 {
		t.Fatalf("unknown enchant visual=%d want=0", got)
	}
}
