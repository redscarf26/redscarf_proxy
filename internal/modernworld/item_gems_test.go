package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestGemItemIDFromEnchant(t *testing.T) {
	if got := gemItemIDFromEnchant(3054); got != 30555 {
		t.Fatalf("enchant 3054 mapped to %d, want 30555 Glowing Tanzanite", got)
	}
	if got := gemItemIDFromEnchant(3055); got != 30556 {
		t.Fatalf("enchant 3055 mapped to %d, want 30556 Glinting Fire Opal", got)
	}
	if got := gemItemIDFromEnchant(0); got != 0 {
		t.Fatalf("empty enchant mapped to %d", got)
	}
	if got := gemItemIDFromEnchant(1); got != 0 {
		t.Fatalf("unknown enchant mapped to %d", got)
	}
}

func TestItemSocketedGemsOmitsTrailingEmpty(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{
		itemSocketEnchantField(0): 3054,
		itemSocketEnchantField(2): 3055,
	}}
	got := itemSocketedGems(values)
	want := []uint32{30555, 0, 30556}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("gems=%v want %v", got, want)
	}
	if got := itemSocketedGems(legacyActivePlayerValues{}); got != nil {
		t.Fatalf("empty item gems=%v", got)
	}
}

func TestAppendSocketedGemsCreateMatchesLogin(t *testing.T) {
	// Reference capture update-000102-37116.bin: each SocketedGem
	// create is int32 ItemID + 16×uint16 BonusListIDs + uint8 Context.
	body := appendSocketedGemsCreate(nil, []uint32{30555, 30556, 30556})
	if len(body) != 3*37 {
		t.Fatalf("create gems have %d bytes, want 111", len(body))
	}
	if binary.LittleEndian.Uint32(body[0:]) != 30555 || body[36] != 0 {
		t.Fatalf("first gem=%x", body[:37])
	}
	if binary.LittleEndian.Uint32(body[37:]) != 30556 || binary.LittleEndian.Uint32(body[74:]) != 30556 {
		t.Fatalf("second/third gem=%x", body[37:])
	}
	for _, b := range body[4:36] {
		if b != 0 {
			t.Fatalf("bonus/context padding was %x", body[4:37])
		}
	}
}

func TestOwnerItemCreateWritesSocketedGems(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{
		legacyItemOwner:           0x42,
		itemSocketEnchantField(0): 3054,
		itemSocketEnchantField(1): 3055,
		itemSocketEnchantField(2): 3055,
	}}
	data := values.appendItem(nil, 571, true)
	empty := legacyActivePlayerValues{fields: map[int]uint32{legacyItemOwner: 0x42}}.appendItem(nil, 571, true)
	if len(empty) != 263 {
		t.Fatalf("empty owner item is %d bytes, want 263", len(empty))
	}
	if len(data) != len(empty)+3*37 {
		t.Fatalf("socketed item is %d bytes, empty is %d (want +111)", len(data), len(empty))
	}
	// legacy proxy: Gems.size, owner uint32, two uint32s, owner uint16, THEN gem bodies, modifiers.
	modOff := len(data) - 1
	gems := data[modOff-3*37 : modOff]
	if binary.LittleEndian.Uint32(gems[0:]) != 30555 || binary.LittleEndian.Uint32(gems[37:]) != 30556 || binary.LittleEndian.Uint32(gems[74:]) != 30556 {
		t.Fatalf("create gem bodies=%x", gems)
	}
	if data[modOff] != 0 {
		t.Fatalf("modifiers trailing=%x", data[modOff:])
	}
	sizeOff := modOff - 3*37 - 2 - 8 - 4 - 4
	if binary.LittleEndian.Uint32(data[sizeOff:]) != 3 {
		t.Fatalf("Gems.size at %d = %d, want 3 (item=%x)", sizeOff, binary.LittleEndian.Uint32(data[sizeOff:]), data)
	}
	if binary.LittleEndian.Uint16(data[modOff-3*37-2:]) != 0 {
		t.Fatalf("owner uint16 before gems = %x", data[modOff-3*37-2:modOff-3*37])
	}
}

func TestOwnerItemCreatePlacesGemsAfterTrailingScalars(t *testing.T) {
	data := legacyActivePlayerValues{fields: map[int]uint32{
		legacyItemOwner:           0x42,
		itemSocketEnchantField(0): 3054,
	}}.appendItem(nil, 571, true)
	empty := legacyActivePlayerValues{fields: map[int]uint32{legacyItemOwner: 0x42}}.appendItem(nil, 571, true)
	if len(data) != len(empty)+37 {
		t.Fatalf("one-gem item is %d bytes, empty is %d", len(data), len(empty))
	}
	// Walk back from the flushed ItemModList byte: gem body, owner uint16,
	// two uint32s, DEBUGItemLevel, Gems.size. ItemID 30555 is 0x0000775b; if
	// it were written before the uint16 the client would eat 0x775b as that
	// scalar and the tooltip sockets would stay empty.
	modOff := len(data) - 1
	gemOff := modOff - 37
	const trailing = 4 + 8 + 2
	sizeOff := gemOff - trailing - 4
	if binary.LittleEndian.Uint32(data[sizeOff:]) != 1 {
		t.Fatalf("Gems.size = %d, want 1", binary.LittleEndian.Uint32(data[sizeOff:]))
	}
	if binary.LittleEndian.Uint16(data[gemOff-2:]) != 0 {
		t.Fatalf("uint16 immediately before gems is %x; ItemID leaked into scalars", data[gemOff-2:gemOff])
	}
	if binary.LittleEndian.Uint32(data[gemOff:]) != 30555 {
		t.Fatalf("gem ItemID at %d = %d, want 30555", gemOff, binary.LittleEndian.Uint32(data[gemOff:]))
	}
}

func TestItemGemsValuesDeltaWritesDynamicField(t *testing.T) {
	base := itemSocketEnchantField(0)
	changed := map[int]uint32{base: 3054}
	fields := map[int]uint32{base: 3054}
	payload := encodeItemValuesDelta(legacyActivePlayerValues{fields: fields}, changed, 0)
	gems := encodeSocketedGemsUpdate([]uint32{30555})
	if len(payload) < len(gems) {
		t.Fatalf("socket gem delta too short: %x", payload)
	}
	if !bytes.Contains(payload, gems) {
		t.Fatalf("item gem delta=%x does not contain SocketedGem update %x", payload, gems)
	}
}
