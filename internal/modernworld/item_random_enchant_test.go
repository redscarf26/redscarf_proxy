package modernworld

import (
	"encoding/binary"
	"testing"
)

// The Legion Coif (-40) has agility, stamina and attack power in legacy
// slots 7..9. Modern slot 7 is Use and must remain empty. Include both
// remaining property slots and the ordinary enchantments as boundary cases.
func TestItemRandomEnchantCreateSlots(t *testing.T) {
	ids := []uint32{13, 14, 15, 16, 17, 18, 19, 2802, 2803, 2825, 123, 124}
	want := []uint32{13, 14, 15, 16, 17, 18, 19, 0, 2802, 2803, 2825, 123, 124}
	fields := map[int]uint32{legacyItemPropertySeed: 49, legacyItemRandom: 0xffffffd8}
	for slot, id := range ids {
		base := legacyItemEnchantment + slot*3
		fields[base], fields[base+1], fields[base+2] = id, id+1, id+2
	}
	for _, owner := range []bool{false, true} {
		body := (legacyActivePlayerValues{fields: fields}).appendItem(nil, 0, owner)
		offset := 8 + 4 // four empty packed GUIDs and flags
		if owner {
			offset += 28
		}
		for slot, id := range want {
			row := body[offset+slot*12:][:12]
			duration, charges := uint32(0), uint16(0)
			if id != 0 {
				duration, charges = id+1, uint16(id+2)
			}
			if binary.LittleEndian.Uint32(row) != id || binary.LittleEndian.Uint32(row[4:]) != duration || binary.LittleEndian.Uint16(row[8:]) != charges || binary.LittleEndian.Uint16(row[10:]) != 0 {
				t.Fatalf("owner=%v slot=%d: %x, want enchant %d", owner, slot, row, id)
			}
		}
		tail := body[offset+13*12:]
		if binary.LittleEndian.Uint32(tail) != 49 || binary.LittleEndian.Uint32(tail[4:]) != 0xffffffd8 {
			t.Fatal("random property metadata shifted")
		}
	}
}

func TestItemRandomEnchantDeltaSlots(t *testing.T) {
	for slot, modernSlot := range []int{0, 1, 2, 3, 4, 5, 6, 8, 9, 10, 11, 12} {
		if slot >= 2 && slot <= 4 {
			continue // socket slots also emit the separately tested Gems dynamic field
		}
		for _, id := range []uint32{2802, 0} { // include clearing an existing enchant
			base := legacyItemEnchantment + slot*3
			fields := map[int]uint32{base: id, base + 1: 50, base + 2: 2}
			body := encodeItemValuesDelta(legacyActivePlayerValues{fields: fields}, fields, 0)
			r := movementReader{data: body}
			presence, _ := r.bits(2)
			var masks [2]uint32
			for i := range masks {
				if presence&(1<<i) != 0 {
					masks[i], _ = r.bits(32)
				}
			}
			var want [2]uint32
			want[0] = 1 << 29
			bit := 30 + modernSlot
			want[bit/32] |= 1 << uint(bit%32)
			if masks != want {
				t.Fatalf("slot=%d masks=%x want=%x", slot, masks, want)
			}
			r.align()
			mask, _ := r.u8()
			got, _ := r.u32()
			duration, _ := r.u32()
			charges, _ := r.u16()
			if mask != 0x3c || got != id || duration != 50 || charges != 2 || r.remaining() != 0 {
				t.Fatalf("slot=%d bad payload %x", slot, body)
			}
		}
	}
}
