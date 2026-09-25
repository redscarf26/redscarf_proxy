package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestSpellEmptyControls(t *testing.T) {
	if err := ParseEmptyControl(nil); err != nil {
		t.Fatalf("empty control: %v", err)
	}
	if err := ParseEmptyControl([]byte{0}); err == nil {
		t.Fatal("non-empty control must error")
	}
	if err := ParseSelfRes(binary.LittleEndian.AppendUint32(nil, 20608)); err != nil {
		t.Fatalf("self-res: %v", err)
	}
	if err := ParseSelfRes([]byte{1, 2, 3}); err == nil {
		t.Fatal("self-res with wrong length must error")
	}
}

func TestParseCancelChannelling(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 0x15)
	body = binary.LittleEndian.AppendUint32(body, 1)
	spellID, err := ParseCancelChannelling(body)
	if err != nil || spellID != 0x15 {
		t.Fatalf("cancel channelling spell=%x err=%v", spellID, err)
	}
	if _, err := ParseCancelChannelling(append(body, 0)); err == nil {
		t.Fatal("cancel channelling with trailing bytes must error")
	}
}

func TestParseSpellClick(t *testing.T) {
	guid := GUID128{Low: 0x1234, High: 3 << 58}
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	bits := newBitWriter(body)
	bits.writeBit(true) // TryAutoDismount
	body = bits.flush()
	got, err := ParseSpellClick(body)
	if err != nil || got != guid {
		t.Fatalf("spell-click=%#v err=%v", got, err)
	}
}

func TestTotemDestroyedRoundTrip(t *testing.T) {
	totem := GUID128{Low: 0x99, High: 3 << 58}
	body := []byte{2} // slot 2 (fire totem = slot 0 indexed, this is arbitrary in test)
	body = appendPackedGUID128(body, totem.Low, totem.High)
	slot, err := ParseTotemDestroyed(body)
	if err != nil || slot != 2 {
		t.Fatalf("totem slot=%d err=%v", slot, err)
	}
	if encoded := EncodeLegacyTotemDestroyed(slot); len(encoded) != 1 || encoded[0] != 2 {
		t.Fatalf("legacy totem=%x", encoded)
	}
	bad := []byte{9}
	bad = appendPackedGUID128(bad, totem.Low, totem.High)
	if _, err := ParseTotemDestroyed(bad); err == nil {
		t.Fatal("totem slot out of range must error")
	}
}

func TestPetLearnTalentRoundTrip(t *testing.T) {
	pet := GUID128{Low: 0x7777, High: 3 << 58}
	body := appendPackedGUID128(nil, pet.Low, pet.High)
	body = binary.LittleEndian.AppendUint32(body, 0x1234)
	body = binary.LittleEndian.AppendUint16(body, 2)
	request, err := ParsePetLearnTalent(body)
	if err != nil || request.Pet != pet || request.Talent != 0x1234 || request.Rank != 2 {
		t.Fatalf("pet-learn-talent=%#v err=%v", request, err)
	}
	legacy := EncodeLegacyPetLearnTalent(0x4000000000007777, 0x1234, 2)
	if !isUint64At(legacy, 0, 0x4000000000007777) || !isUint32At(legacy, 8, 0x1234) || !isUint32At(legacy, 12, 2) {
		t.Fatalf("legacy pet-learn-talent=%x", legacy)
	}
}

func TestEncodeLegacyPetCastSpellPrependsPet(t *testing.T) {
	legacy := EncodeLegacyPetCastSpell(0x4000000000005555, SpellCastRequest{SpellID: 883}, 0, 0, 0, 0)
	// guid64 + [cast count 0][spell][flags 0][targets flags u32]
	if !isUint64At(legacy, 0, 0x4000000000005555) || legacy[8] != 0 || !isUint32At(legacy, 9, 883) {
		t.Fatalf("legacy pet cast=%x", legacy)
	}
}
