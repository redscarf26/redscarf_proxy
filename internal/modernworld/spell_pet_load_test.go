package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestPetLoadSpellGo(t *testing.T) {
	for _, owner := range []uint64{7, 0x12345678} {
		body := appendLegacyPackedGUID(nil, owner)
		body = appendLegacyPackedGUID(body, owner)
		body = append(body, 0)
		body = binary.LittleEndian.AppendUint32(body, 46584)
		body = binary.LittleEndian.AppendUint32(body, 0x100)
		body = binary.LittleEndian.AppendUint32(body, 0)
		cast, err := ParseLegacySpellStartOrGo(body, true)
		if err != nil || !cast.PetLoadCooldown || cast.CasterGUID != owner || cast.SpellID != 46584 || len(cast.HitTargets) != 0 || cast.Target.Flags != 0 {
			t.Fatalf("owner %x: %#v, %v", owner, cast, err)
		}
		if owner == 7 && len(body) != 17 {
			t.Fatalf("packet length = %d", len(body))
		}
		if _, err := ParseLegacySpellStartOrGo(body, false); err == nil {
			t.Fatal("header-only START accepted")
		}
		for end := 0; end < len(body); end++ {
			if _, err := ParseLegacySpellStartOrGo(body[:end], true); err == nil {
				t.Fatalf("truncated header accepted at %d", end)
			}
		}
		for _, offset := range []int{len(body) - 13, len(body) - 12, len(body) - 8, len(body) - 4, 1} {
			bad := append([]byte(nil), body...)
			if offset == len(body)-12 {
				binary.LittleEndian.PutUint32(bad[offset:], 0)
			} else {
				bad[offset] ^= 1
			}
			if _, err := ParseLegacySpellStartOrGo(bad, true); err == nil {
				t.Fatalf("invalid header accepted at %d", offset)
			}
		}
		if _, err := ParseLegacySpellStartOrGo(append(append([]byte(nil), body...), 0), true); err == nil {
			t.Fatal("partial normal GO accepted")
		}
		// The same header with a full empty hit/miss/target tail is ordinary GO.
		full := append(append([]byte(nil), body...), 0, 0, 0, 0, 0, 0)
		if cast, err := ParseLegacySpellStartOrGo(full, true); err != nil || cast.PetLoadCooldown {
			t.Fatalf("normal GO misclassified: %#v %v", cast, err)
		}
	}
}
