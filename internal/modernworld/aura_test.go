package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestAuraDispositionTranslation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		legacy byte
		want   uint16
	}{
		{"received paladin aura", legacyAuraEffect0, auraFlagPositive},
		{"self paladin aura", legacyAuraEffect0 | legacyAuraPositive | legacyAuraNoCaster, auraFlagPositive | auraFlagCancelable | auraFlagNoCaster},
		{"debuff", legacyAuraEffect0 | legacyAuraNegative | legacyAuraDuration, auraFlagNegative | auraFlagDuration},
		{"negative takes precedence", legacyAuraEffect0 | legacyAuraPositive | legacyAuraNegative, auraFlagNegative},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := EncodeLegacyPackedGUID(0x42)
			body = append(body, 2)
			body = binary.LittleEndian.AppendUint32(body, 465)
			body = append(body, tc.legacy, 80, 1)
			if tc.legacy&legacyAuraNoCaster == 0 {
				body = append(body, EncodeLegacyPackedGUID(0x99)...)
			}
			if tc.legacy&legacyAuraDuration != 0 {
				body = binary.LittleEndian.AppendUint32(body, 10000)
				body = binary.LittleEndian.AppendUint32(body, 9000)
			}
			for _, all := range []bool{false, true} {
				_, auras, err := ParseLegacyAuraUpdate(body, all)
				if err != nil || len(auras) != 1 {
					t.Fatalf("auras=%v err=%v", auras, err)
				}
				encoded := EncodeAuraUpdate(GUID128{Low: 0x42}, 631, all, auras)
				_, _, packed, err := readPackedGUID128(encoded[4:])
				if err != nil {
					t.Fatal(err)
				}
				if got := binary.LittleEndian.Uint16(encoded[4+packed+8:]); got != tc.want {
					t.Fatalf("wire flags=%#x want=%#x", got, tc.want)
				}
			}
		})
	}
}

func TestGhostAuraUpdateAllTranslation(t *testing.T) {
	legacy := EncodeLegacyPackedGUID(0x42)
	legacy = append(legacy, 0) // slot
	legacy = binary.LittleEndian.AppendUint32(legacy, 8326)
	legacy = append(legacy, legacyAuraEffect0|legacyAuraNoCaster|legacyAuraPositive, 1, 1)
	guid, auras, err := ParseLegacyAuraUpdate(legacy, true)
	if err != nil || guid != 0x42 || len(auras) != 1 {
		t.Fatalf("guid=%x auras=%d err=%v", guid, len(auras), err)
	}
	aura := auras[0]
	if !aura.HasData || aura.SpellID != 8326 || aura.Flags&auraFlagNoCaster == 0 || aura.Flags&auraFlagPositive == 0 || aura.ActiveFlags != 1 {
		t.Fatalf("aura %#v", aura)
	}
	if auras[0].Flags != (auraFlagNoCaster | auraFlagCancelable | auraFlagPositive) {
		t.Fatalf("ghost flags=%#x, want legacy proxy 0x103", auras[0].Flags)
	}

	negativeGhost := []AuraInfo{{HasData: true, SpellID: 8326, Flags: auraFlagNegative | auraFlagNoCaster}}
	MarkLocalPlayerAuraFlags(negativeGhost)
	if negativeGhost[0].Flags != (auraFlagNoCaster | auraFlagCancelable | auraFlagPositive) {
		t.Fatalf("forced ghost flags=%#x, want 0x103", negativeGhost[0].Flags)
	}
	if KnownSpellVisual(8326) != 241720 || KnownSpellVisual(1784) != 237504 ||
		KnownSpellVisual(133) != 236677 || KnownSpellVisual(6304) != 240259 ||
		KnownSpellVisual(11829) != 244071 || KnownSpellVisual(20793) != 244077 ||
		KnownSpellVisual(32950) != 316112 || KnownSpellVisual(423869) != 411089 ||
		KnownSpellVisual(0xffffffff) != 0 || len(spellVisuals()) != 32053 {
		t.Fatalf("ghost visual %d stealth visual %d fireballs %d/%d", KnownSpellVisual(8326), KnownSpellVisual(1784), KnownSpellVisual(133), KnownSpellVisual(20793))
	}
	if !IsStealthSpell(1784) || IsStealthSpell(133) || StealthCooldownMS != 10000 {
		t.Fatalf("stealth spell/cooldown")
	}

	low, high := modernPlayerGUID(0x42)
	const mapID uint16 = 571
	body := EncodeAuraUpdate(GUID128{Low: low, High: high}, mapID, true, auras)
	if body[0] != 0x80 || body[1]&0x40 == 0 {
		t.Fatalf("update-all header %x", body[:2])
	}
	if body[2] != 0 || body[3]&0x80 == 0 {
		t.Fatalf("slot/has-data %x", body)
	}
	castLow, castHigh, packed, err := readPackedGUID128(body[4:])
	if err != nil {
		t.Fatal(err)
	}
	wantCastHigh := uint64(modernHighGuidCast)<<58 | uint64(1)<<42 | uint64(mapID&0x1fff)<<29 | (uint64(8326)&0x7fffff)<<6 | modernSpellCastSourceAura
	if castLow != 0x42 || castHigh != wantCastHigh {
		t.Fatalf("cast guid low=%#x high=%#x, want low=0x42 high=%#x", castLow, castHigh, wantCastHigh)
	}
	spellAt := 4 + packed
	if binary.LittleEndian.Uint32(body[spellAt:spellAt+4]) != 8326 {
		t.Fatalf("spell-id at %d: %x", spellAt, body)
	}
	if binary.LittleEndian.Uint32(body[spellAt+4:spellAt+8]) != 0 {
		t.Fatalf("default visual should be 0, got %x", body[spellAt+4:spellAt+8])
	}

	withVisual := auras
	withVisual[0].VisualID = 0x11223344
	visualBody := EncodeAuraUpdate(GUID128{Low: low, High: high}, mapID, true, withVisual)
	if binary.LittleEndian.Uint32(visualBody[spellAt+4:spellAt+8]) != 0x11223344 {
		t.Fatalf("visual-id at %d: %x", spellAt+4, visualBody)
	}
	empty := EncodeEmptyAuraUpdateAll(0x42)
	if empty[0] != 0x80 || empty[1] != 0 {
		t.Fatalf("empty aura-update-all %x", empty)
	}
}

func TestAuraRemoveAndCaster(t *testing.T) {
	legacy := EncodeLegacyPackedGUID(0x42)
	legacy = append(legacy, 3)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	_, auras, err := ParseLegacyAuraUpdate(legacy, false)
	if err != nil || len(auras) != 1 || auras[0].HasData || auras[0].Slot != 3 {
		t.Fatalf("remove %#v err=%v", auras, err)
	}

	withCaster := EncodeLegacyPackedGUID(0x42)
	withCaster = append(withCaster, 1)
	withCaster = binary.LittleEndian.AppendUint32(withCaster, 139)
	withCaster = append(withCaster, legacyAuraEffect0|legacyAuraPositive|legacyAuraDuration, 80, 1)
	withCaster = append(withCaster, EncodeLegacyPackedGUID(0x99)...)
	withCaster = binary.LittleEndian.AppendUint32(withCaster, 15000)
	withCaster = binary.LittleEndian.AppendUint32(withCaster, 12000)
	_, auras, err = ParseLegacyAuraUpdate(withCaster, true)
	if err != nil || len(auras) != 1 || auras[0].CastUnit.Low != 0x99 || auras[0].Duration == nil || *auras[0].Duration != 15000 || *auras[0].Remaining != 12000 {
		t.Fatalf("caster aura %#v err=%v", auras, err)
	}
}
