package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseLegacyInspectResult(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x43)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3)
	legacy = append(legacy, 1, 0, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2111)
	legacy = append(legacy, 2, 1)
	legacy = binary.LittleEndian.AppendUint16(legacy, 456)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)

	result, err := ParseLegacyInspectResult(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Target != 0x43 || result.Unspent != 3 || result.ActiveSpec != 0 {
		t.Fatalf("header=%+v", result)
	}
	if len(result.Specs) != 1 || len(result.Specs[0].Talents) != 1 || result.Specs[0].Talents[0] != (LegacyInspectTalent{ID: 2111, Rank: 2}) {
		t.Fatalf("talents=%+v", result.Specs)
	}
	if len(result.Specs[0].Glyphs) != 1 || result.Specs[0].Glyphs[0] != 456 {
		t.Fatalf("glyphs=%+v", result.Specs[0].Glyphs)
	}
}

func TestEncodeInspectResultWritesTalentGroups(t *testing.T) {
	result := LegacyInspectResult{
		Unspent:    3,
		ActiveSpec: 0,
		Specs: []LegacyInspectSpec{{
			Talents: []LegacyInspectTalent{{ID: 2111, Rank: 2}},
			Glyphs:  []uint16{456},
		}},
		Items: []LegacyInspectItem{{Index: 0, ItemID: 25}},
	}
	metadata := InspectMetadata{
		GUID:            GUID128{Low: 1, High: 1 << 58},
		Name:            "A",
		Sex:             1,
		Race:            1,
		Class:           1,
		LifetimeMaxRank: 14,
	}
	body := EncodeInspectResult(result, metadata, func(uint64) GUID128 { return GUID128{} })

	talentID := make([]byte, 4)
	binary.LittleEndian.PutUint32(talentID, 2111)
	idx := bytes.Index(body, talentID)
	if idx < 0 {
		t.Fatalf("talent id missing in %x", body)
	}
	if body[idx+4] != 2 {
		t.Fatalf("talent rank = %d, want 2", body[idx+4])
	}

	// legacy proxy Write343: Unspent/Active/SpecCount, then talentCount u8+u32,
	// glyph slots always 6 u8+u32, SpecID, then talents.
	if idx < 20 {
		t.Fatal("inspect talent header truncated")
	}
	group := body[idx-20 : idx]
	if binary.LittleEndian.Uint32(group[0:4]) != 3 {
		t.Fatalf("unspent=%d", binary.LittleEndian.Uint32(group[0:4]))
	}
	if group[4] != 0 || binary.LittleEndian.Uint32(group[5:9]) != 1 {
		t.Fatalf("active/spec-count header=%x", group[4:9])
	}
	if group[9] != 1 || binary.LittleEndian.Uint32(group[10:14]) != 1 {
		t.Fatalf("talent count header=%x", group[9:14])
	}
	if group[14] != inspectGlyphSlots || binary.LittleEndian.Uint32(group[15:19]) != inspectGlyphSlots {
		t.Fatalf("glyph count header=%x", group[14:19])
	}
	if group[19] != inspectTalentSpecID {
		t.Fatalf("spec id=%d, want %d", group[19], inspectTalentSpecID)
	}

	glyphStart := idx + 5
	if glyph := binary.LittleEndian.Uint16(body[glyphStart:]); glyph != 456 {
		t.Fatalf("glyph[0]=%d, want 456", glyph)
	}
	for slot := 1; slot < inspectGlyphSlots; slot++ {
		offset := glyphStart + slot*2
		if glyph := binary.LittleEndian.Uint16(body[offset:]); glyph != 0 {
			t.Fatalf("glyph[%d]=%d, want 0", slot, glyph)
		}
	}

	// DisplayInfo is followed by PvpTalentsCount=0, not the glyph count. A
	// non-zero leading count would be consumed as PvP talent IDs and skip the
	// nested talent groups that enable the inspect talent/glyph tabs.
	itemID := make([]byte, 4)
	binary.LittleEndian.PutUint32(itemID, 25)
	itemAt := bytes.Index(body, itemID)
	if itemAt < 0 {
		t.Fatal("inspect item id missing")
	}
}

func TestEncodeInspectResultPvpTalentCountIsZero(t *testing.T) {
	metadata := InspectMetadata{GUID: GUID128{Low: 1}, Name: "A", Sex: 1, Race: 1, Class: 1}
	withGlyphs := EncodeInspectResult(LegacyInspectResult{
		Specs: []LegacyInspectSpec{{Glyphs: []uint16{456, 457}}},
	}, metadata, func(uint64) GUID128 { return GUID128{} })
	empty := EncodeInspectResult(LegacyInspectResult{}, metadata, func(uint64) GUID128 { return GUID128{} })

	displayLen := inspectDisplayLen(empty)
	if got := binary.LittleEndian.Uint32(withGlyphs[displayLen:]); got != 0 {
		t.Fatalf("pvp talent count=%d, want 0", got)
	}
	if withGlyphs[displayLen+8] != 0 {
		t.Fatalf("lifetime rank shifted, byte=%d", withGlyphs[displayLen+8])
	}
}

func inspectDisplayLen(emptyBody []byte) int {
	trailer := 4 + 4 + 1 + 2 + 2 + 4 + 4 + 4 + 1 + 4
	optional := newBitWriter(nil)
	optional.writeBit(false)
	optional.writeBit(false)
	trailer += len(optional.flush())
	for i := 0; i < 6; i++ {
		trailer += 1 + 12*4
		bits := newBitWriter(nil)
		bits.writeBit(false)
		trailer += len(bits.flush())
	}
	return len(emptyBody) - trailer
}
