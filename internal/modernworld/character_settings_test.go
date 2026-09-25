package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestSetTitleRoundTrip(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 42)
	request, err := ParseSetTitle(body)
	if err != nil || request.TitleID != 42 {
		t.Fatalf("set title parsed request=%+v err=%v", request, err)
	}
	if !bytes.Equal(EncodeLegacySetTitle(request), body) {
		t.Fatalf("legacy set-title body = %x, want %x", EncodeLegacySetTitle(request), body)
	}
	if _, err := ParseSetTitle(body[:3]); err == nil {
		t.Fatal("expected short set-title body to error")
	}
}

func TestSetPvPBit(t *testing.T) {
	// The modern client packs a true bit at the MSB of the first byte.
	enabled, err := ParseSetPvP([]byte{0x80})
	if err != nil || !enabled.Enable {
		t.Fatalf("set-pvp enable parsed=%+v err=%v", enabled, err)
	}
	disabled, err := ParseSetPvP([]byte{0x00})
	if err != nil || disabled.Enable {
		t.Fatalf("set-pvp disable parsed=%+v err=%v", disabled, err)
	}
	if !bytes.Equal(EncodeLegacySetPvP(true), []byte{1}) || !bytes.Equal(EncodeLegacySetPvP(false), []byte{0}) {
		t.Fatal("legacy set-pvp one-byte encoding wrong")
	}
	if _, err := ParseSetPvP(nil); err == nil {
		t.Fatal("expected empty set-pvp body to error")
	}
	if _, err := ParseSetPvP([]byte{0x80, 0x00}); err == nil {
		t.Fatal("expected set-pvp body with trailing bytes to error")
	}
}

func TestUnlearnSkillRoundTrip(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 171)
	request, err := ParseUnlearnSkill(body)
	if err != nil || request.SkillLine != 171 {
		t.Fatalf("unlearn parsed request=%+v err=%v", request, err)
	}
	if !bytes.Equal(EncodeLegacyUnlearnSkill(request), body) {
		t.Fatalf("legacy unlearn body = %x, want %x", EncodeLegacyUnlearnSkill(request), body)
	}
}

func TestRemoveGlyphWidensToUint32(t *testing.T) {
	request, err := ParseRemoveGlyph([]byte{4})
	if err != nil || request.GlyphSlot != 4 {
		t.Fatalf("remove-glyph parsed=%+v err=%v", request, err)
	}
	body := EncodeLegacyRemoveGlyph(request)
	if len(body) != 4 || binary.LittleEndian.Uint32(body) != 4 {
		t.Fatalf("legacy remove-glyph body = %x, want uint32 4", body)
	}
	if _, err := ParseRemoveGlyph(nil); err == nil {
		t.Fatal("expected empty remove-glyph body to error")
	}
}
