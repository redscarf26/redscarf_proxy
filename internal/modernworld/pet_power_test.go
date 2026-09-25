package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

// hunterPetFields returns legacy values for a tamed hunter pet: UNIT_FIELD_BYTES_0
// carries DisplayPower Focus (0x02020100, matching AzerothCore's CreateBaseAtTamed).
func hunterPetFields(focus, focusMax, happy, happyMax uint32) map[int]uint32 {
	return map[int]uint32{
		legacyUnitBytes0:         0x02020100,
		legacyUnitPower1 + 2:     focus,
		legacyUnitMaxPower1 + 2:  focusMax,
		legacyUnitPower1 + 4:     happy,
		legacyUnitMaxPower1 + 4:  happyMax,
		legacyUnitMaxHealth:      happyMax, // needs a positive cap to be created
		legacyObjectScale:        math.Float32bits(1),
	}
}

func TestHunterPetCreatePowerSlots(t *testing.T) {
	values := legacyActivePlayerValues{fields: hunterPetFields(0x11111111, 0x22222222, 0x00100590, 0x00100590)}
	wired := values.appendUnit(nil, false, true, 0)
	// appendUnit writes race, class, 0, sex, displayPower, then
	// OverrideDisplayPowerID, then (Power[i], MaxPower[i], ModPowerRegen=1) per
	// index for the creature (no owner-only PowerRegen float pairs).
	marker := -1
	for index := 0; index+5 <= len(wired); index++ {
		if wired[index] == 0 && wired[index+1] == 1 && wired[index+2] == 0 && wired[index+3] == 2 && wired[index+4] == 2 {
			marker = index
			break
		}
	}
	if marker < 0 {
		t.Fatal("pet race/class/displayPower marker not found")
	}
	power0 := marker + 5 + 4 // skip OverrideDisplayPowerID
	get := func(offset int) uint32 {
		return binary.LittleEndian.Uint32(wired[offset:])
	}
	if got := get(power0); got != 0x11111111 {
		t.Fatalf("Power[0] (focus) = %#x", got)
	}
	if got := get(power0 + 4); got != 0x22222222 {
		t.Fatalf("MaxPower[0] = %#x", got)
	}
	if got := get(power0 + 12*3); got != 0x00100590 {
		t.Fatalf("Power[3] (happiness) = %#x", got)
	}
	if got := get(power0 + 12*3 + 4); got != 0x00100590 {
		t.Fatalf("MaxPower[3] = %#x", got)
	}
}

func TestUnitPowerSlotPetMapping(t *testing.T) {
	values := legacyActivePlayerValues{fields: hunterPetFields(100, 100, 1, 2)}
	if got := unitPowerSlot(3, values, 1, 2); got != 0 {
		t.Fatalf("pet focus slot = %d, want 0", got)
	}
	if got := unitPowerSlot(3, values, 1, 4); got != 3 {
		t.Fatalf("pet happiness slot = %d, want 3", got)
	}
	if got := unitPowerSlot(3, values, 1, 0); got != -1 {
		t.Fatalf("pet mana slot = %d, want -1", got)
	}
	// A player unit must keep the class layout (druid energy -> slot 0).
	playerValues := legacyActivePlayerValues{fields: map[int]uint32{legacyUnitBytes0: uint32(4)<<8 | uint32(3)<<24}}
	if got := unitPowerSlot(4, playerValues, 4, 3); got != 0 {
		t.Fatalf("druid energy slot = %d, want 0", got)
	}
}

func TestHunterPetHappinessValuesDelta(t *testing.T) {
	const happy = 0x00100590 // 1,050,000 = AzerothCore GetCreatePowers(POWER_HAPPINESS)
	values := legacyActivePlayerValues{fields: hunterPetFields(100, 100, happy, happy)}
	changed := map[int]uint32{legacyUnitPower1 + 4: happy}
	section := encodeUnitValuesDelta(values, changed, 3, 0)
	if len(section) == 0 {
		t.Fatal("hunter-pet happiness change produced no unit delta")
	}
	blockMask := section[0]
	if blockMask != 0x18 {
		t.Fatalf("blockMask = 0x%02x, want 0x18 (blocks 3+4)", blockMask)
	}
	p := 1
	var block3, block4 uint32
	for i := 0; i < 8; i++ {
		if blockMask&(1<<uint(i)) == 0 {
			continue
		}
		v := binary.BigEndian.Uint32(section[p:])
		p += 4
		switch i {
		case 3:
			block3 = v
		case 4:
			block4 = v
		}
	}
	if block3 != 1<<(116-96) {
		t.Fatalf("block3 submask = 0x%08x, want parent 116 (0x%08x)", block3, 1<<(116-96))
	}
	if block4 != 1<<(140-128) {
		t.Fatalf("block4 submask = 0x%08x, want Power[3] bit 140 (0x%08x)", block4, 1<<(140-128))
	}
	if got := binary.LittleEndian.Uint32(section[p:]); got != happy {
		t.Fatalf("happiness value = 0x%x, want 0x%x", got, happy)
	}
}

func TestDeathKnightRunicPowerValuesDelta(t *testing.T) {
	const runic = uint32(80)
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitBytes0:     uint32(6) << 8,
		legacyUnitPower1 + 6: runic,
	}}
	changed := map[int]uint32{legacyUnitPower1 + 6: runic}
	section := encodeUnitValuesDelta(values, changed, 4, 0)
	if len(section) == 0 {
		t.Fatal("death-knight runic power change produced no unit delta")
	}
	blockMask := section[0]
	if blockMask != 0x18 {
		t.Fatalf("blockMask = 0x%02x, want 0x18 (blocks 3+4)", blockMask)
	}
	p := 1
	var block3, block4 uint32
	for i := 0; i < 8; i++ {
		if blockMask&(1<<uint(i)) == 0 {
			continue
		}
		v := binary.BigEndian.Uint32(section[p:])
		p += 4
		switch i {
		case 3:
			block3 = v
		case 4:
			block4 = v
		}
	}
	if block3 != 1<<(116-96) {
		t.Fatalf("block3 submask = 0x%08x, want parent 116", block3)
	}
	if block4 != 1<<(137-128) {
		t.Fatalf("block4 submask = 0x%08x, want Power[0] bit 137", block4)
	}
	if got := binary.LittleEndian.Uint32(section[p:]); got != runic {
		t.Fatalf("runic power = %d, want %d", got, runic)
	}
}

func TestHunterPetMaxPowerValuesDelta(t *testing.T) {
	values := legacyActivePlayerValues{fields: hunterPetFields(100, 100, 1, 0x00100590)}
	changed := map[int]uint32{legacyUnitMaxPower1 + 4: 0x00100590}
	section := encodeUnitValuesDelta(values, changed, 3, 0)
	if len(section) == 0 {
		t.Fatal("hunter-pet happiness max produced no unit delta")
	}
	// MaxPower[3] = descriptor bit 150 -> block 4, sub-bit 22.
	p := 1
	var block4 uint32
	for i := 0; i < 8; i++ {
		if section[0]&(1<<uint(i)) == 0 {
			continue
		}
		v := binary.BigEndian.Uint32(section[p:])
		p += 4
		if i == 4 {
			block4 = v
		}
	}
	if block4 != 1<<(150-128) {
		t.Fatalf("block4 submask = 0x%08x, want MaxPower[3] bit 150 (0x%08x)", block4, 1<<(150-128))
	}
	if got := binary.LittleEndian.Uint32(section[p:]); got != 0x00100590 {
		t.Fatalf("happiness max value = 0x%x", got)
	}
}
