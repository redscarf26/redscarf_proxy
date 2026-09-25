package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestKnownSpellsTranslation(t *testing.T) {
	legacy := []byte{1}
	legacy = binary.LittleEndian.AppendUint16(legacy, 2)
	legacy = binary.LittleEndian.AppendUint32(legacy, 133)
	legacy = binary.LittleEndian.AppendUint16(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 168)
	legacy = binary.LittleEndian.AppendUint16(legacy, 7)
	legacy = binary.LittleEndian.AppendUint16(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 133)
	legacy = binary.LittleEndian.AppendUint16(legacy, 9)
	legacy = binary.LittleEndian.AppendUint16(legacy, 11)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1500)
	legacy = binary.LittleEndian.AppendUint32(legacy, 800)
	parsed, err := ParseLegacyKnownSpells(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.InitialLogin || len(parsed.Spells) != 2 || parsed.Spells[0] != 133 || parsed.Spells[1] != 168 {
		t.Fatalf("spells=%v", parsed.Spells)
	}
	if len(parsed.History) != 1 || parsed.History[0] != (SpellHistoryEntry{SpellID: 133, ItemID: 9, Category: 11, RecoveryTime: 1500, CategoryRecoveryTime: 800}) {
		t.Fatalf("history=%v", parsed.History)
	}

	body := EncodeSendKnownSpells(parsed.Spells, parsed.InitialLogin)
	if body[0] != 0x80 || binary.LittleEndian.Uint32(body[1:5]) != 2 || binary.LittleEndian.Uint32(body[5:9]) != 0 {
		t.Fatalf("known spells header %x", body)
	}
	if binary.LittleEndian.Uint32(body[9:13]) != 133 || binary.LittleEndian.Uint32(body[13:17]) != 168 || len(body) != 17 {
		t.Fatalf("known spells body %x", body)
	}
	if EncodeSendKnownSpells(nil, false)[0] != 0 {
		t.Fatal("expected InitialLogin bit clear")
	}

	history := EncodeSendSpellHistory(parsed.History)
	if binary.LittleEndian.Uint32(history[:4]) != 1 || binary.LittleEndian.Uint32(history[4:8]) != 133 || binary.LittleEndian.Uint32(history[8:12]) != 9 {
		t.Fatalf("history ids %x", history)
	}
	if binary.LittleEndian.Uint32(history[12:16]) != 11 || int32(binary.LittleEndian.Uint32(history[16:20])) != 1500 || int32(binary.LittleEndian.Uint32(history[20:24])) != 800 {
		t.Fatalf("history times %x", history)
	}
	if binary.LittleEndian.Uint32(history[24:28]) != math.Float32bits(1) || history[28] != 0 || len(history) != 29 {
		t.Fatalf("history tail %x", history)
	}

	// AzerothCore writes cooldown map size, then skips some entries.
	truncated := []byte{0}
	truncated = binary.LittleEndian.AppendUint16(truncated, 1)
	truncated = binary.LittleEndian.AppendUint32(truncated, 6603)
	truncated = binary.LittleEndian.AppendUint16(truncated, 0)
	truncated = binary.LittleEndian.AppendUint16(truncated, 3)
	truncated = binary.LittleEndian.AppendUint32(truncated, 133)
	truncated = binary.LittleEndian.AppendUint16(truncated, 0)
	truncated = binary.LittleEndian.AppendUint16(truncated, 0)
	truncated = binary.LittleEndian.AppendUint32(truncated, 1500)
	truncated = binary.LittleEndian.AppendUint32(truncated, 0)
	truncated = binary.LittleEndian.AppendUint32(truncated, 168)
	truncated = binary.LittleEndian.AppendUint16(truncated, 0)
	truncated = binary.LittleEndian.AppendUint16(truncated, 0)
	truncated = binary.LittleEndian.AppendUint32(truncated, 800)
	truncated = binary.LittleEndian.AppendUint32(truncated, 0)
	truncatedParsed, err := ParseLegacyKnownSpells(truncated)
	if err != nil {
		t.Fatalf("truncated cooldowns: %v", err)
	}
	if len(truncatedParsed.Spells) != 1 || truncatedParsed.Spells[0] != 6603 || len(truncatedParsed.History) != 2 {
		t.Fatalf("truncated spells=%v history=%d", truncatedParsed.Spells, len(truncatedParsed.History))
	}
}

func TestLearnedUnlearnedAndSuperceded(t *testing.T) {
	spellID, err := ParseLegacyLearnedSpell(append(binary.LittleEndian.AppendUint32(nil, 133), 0, 0))
	if err != nil || spellID != 133 {
		t.Fatalf("learned=%d err=%v", spellID, err)
	}
	if got, err := ParseLegacyLearnedSpell(binary.LittleEndian.AppendUint32(nil, 168)); err != nil || got != 168 {
		t.Fatalf("learned without extra=%d err=%v", got, err)
	}
	if _, err := ParseLegacyLearnedSpell([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected short learned-spell to fail")
	}

	learned := EncodeLearnedSpells([]uint32{133}, true)
	if binary.LittleEndian.Uint32(learned[:4]) != 1 || binary.LittleEndian.Uint32(learned[4:8]) != 0 || learned[8] != 0x80 {
		t.Fatalf("learned header %x", learned)
	}
	if binary.LittleEndian.Uint32(learned[9:13]) != 133 || learned[13] != 0 || len(learned) != 14 {
		t.Fatalf("learned body %x", learned)
	}

	unlearnedID, err := ParseLegacyUnlearnedSpell(binary.LittleEndian.AppendUint32(nil, 168))
	if err != nil || unlearnedID != 168 {
		t.Fatalf("unlearned=%d err=%v", unlearnedID, err)
	}
	unlearned := EncodeUnlearnedSpells([]uint32{168}, false)
	if binary.LittleEndian.Uint32(unlearned[:4]) != 1 || binary.LittleEndian.Uint32(unlearned[4:8]) != 168 || unlearned[8] != 0 {
		t.Fatalf("unlearned body %x", unlearned)
	}

	oldSpell, newSpell, err := ParseLegacySupercededSpells(append(binary.LittleEndian.AppendUint32(nil, 133), binary.LittleEndian.AppendUint32(nil, 168)...))
	if err != nil || oldSpell != 133 || newSpell != 168 {
		t.Fatalf("superceded old=%d new=%d err=%v", oldSpell, newSpell, err)
	}
	superceded := EncodeSupercededSpells(168, 133)
	if binary.LittleEndian.Uint32(superceded[:4]) != 1 || binary.LittleEndian.Uint32(superceded[4:8]) != 168 || superceded[8]&0x20 == 0 {
		t.Fatalf("superceded header %x", superceded)
	}
	if binary.LittleEndian.Uint32(superceded[9:13]) != 133 || len(superceded) != 13 {
		t.Fatalf("superceded body %x", superceded)
	}

	unlearnList, err := ParseLegacySendUnlearnSpells(append(binary.LittleEndian.AppendUint32(nil, 1), binary.LittleEndian.AppendUint32(nil, 99)...))
	if err != nil || len(unlearnList) != 1 || unlearnList[0] != 99 {
		t.Fatalf("unlearn list=%v err=%v", unlearnList, err)
	}
	if empty, err := ParseLegacySendUnlearnSpells(binary.LittleEndian.AppendUint32(nil, 0)); err != nil || len(empty) != 0 {
		t.Fatalf("empty unlearn=%v err=%v", empty, err)
	}
	encodedUnlearn := EncodeSendUnlearnSpells([]uint32{99})
	if binary.LittleEndian.Uint32(encodedUnlearn[:4]) != 1 || binary.LittleEndian.Uint32(encodedUnlearn[4:]) != 99 {
		t.Fatalf("send unlearn %x", encodedUnlearn)
	}
	if emptyUnlearn := EncodeSendUnlearnSpells(nil); len(emptyUnlearn) != 4 || binary.LittleEndian.Uint32(emptyUnlearn) != 0 {
		t.Fatalf("empty send unlearn %x", emptyUnlearn)
	}
	if charges := EncodeSendSpellCharges(0); len(charges) != 4 || binary.LittleEndian.Uint32(charges) != 0 {
		t.Fatalf("empty spell charges %x", charges)
	}
}

func TestSpellCooldownTranslation(t *testing.T) {
	packed := EncodeLegacyPackedGUID(0x42)
	packed = append(packed, 1)
	packed = binary.LittleEndian.AppendUint32(packed, 133)
	packed = binary.LittleEndian.AppendUint32(packed, 2500)
	guid, flags, cooldowns, err := ParseLegacySpellCooldown(packed)
	if err != nil || guid != 0x42 || flags != 1 || len(cooldowns) != 1 || cooldowns[0] != (SpellCooldown{SpellID: 133, ForcedCooldown: 2500}) {
		t.Fatalf("packed cooldown guid=%x flags=%d cds=%v err=%v", guid, flags, cooldowns, err)
	}

	unpacked := binary.LittleEndian.AppendUint64(nil, 0x42)
	unpacked = append(unpacked, 0)
	unpacked = binary.LittleEndian.AppendUint32(unpacked, 168)
	unpacked = binary.LittleEndian.AppendUint32(unpacked, 1000)
	guid, flags, cooldowns, err = ParseLegacySpellCooldown(unpacked)
	if err != nil || guid != 0x42 || flags != 0 || len(cooldowns) != 1 || cooldowns[0].SpellID != 168 {
		t.Fatalf("unpacked cooldown guid=%x flags=%d cds=%v err=%v", guid, flags, cooldowns, err)
	}

	body := EncodeSpellCooldown(GUID128{Low: 0x42, High: 1}, 1, cooldowns)
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil || low != 0x42 || high != 1 {
		t.Fatalf("modern cooldown guid %x err=%v", body, err)
	}
	rest := body[consumed:]
	if rest[0] != 1 || binary.LittleEndian.Uint32(rest[1:5]) != 1 || binary.LittleEndian.Uint32(rest[5:9]) != 168 {
		t.Fatalf("modern cooldown body %x", body)
	}
	if binary.LittleEndian.Uint32(rest[9:13]) != 1000 || binary.LittleEndian.Uint32(rest[13:17]) != math.Float32bits(1) {
		t.Fatalf("modern cooldown duration %x", body)
	}
}

func TestClearCooldownAndCooldownEvent(t *testing.T) {
	spellID, guid, err := ParseLegacyCooldownSpellAndGUID(append(binary.LittleEndian.AppendUint32(nil, 133), binary.LittleEndian.AppendUint64(nil, 0x42)...))
	if err != nil || spellID != 133 || guid != 0x42 {
		t.Fatalf("unpacked event spell=%d guid=%x err=%v", spellID, guid, err)
	}
	petGUID := uint64(0xf140000001000043)
	packed := append(binary.LittleEndian.AppendUint32(nil, 168), EncodeLegacyPackedGUID(petGUID)...)
	spellID, guid, err = ParseLegacyCooldownSpellAndGUID(packed)
	if err != nil || spellID != 168 || guid != petGUID || !LegacyGUIDIsPet(guid) {
		t.Fatalf("packed pet spell=%d guid=%x pet=%v err=%v", spellID, guid, LegacyGUIDIsPet(guid), err)
	}

	event := EncodeCooldownEvent(133, false)
	if binary.LittleEndian.Uint32(event[:4]) != 133 || event[4] != 0 {
		t.Fatalf("cooldown event %x", event)
	}
	if EncodeCooldownEvent(1, true)[4]&0x80 == 0 {
		t.Fatal("expected pet bit on cooldown event")
	}
	clear := EncodeClearCooldown(168, true)
	if binary.LittleEndian.Uint32(clear[:4]) != 168 || clear[4]&0x40 == 0 {
		t.Fatalf("clear cooldown %x", clear)
	}
}

func TestKnownSpellsRejectsMalformedInput(t *testing.T) {
	if _, err := ParseLegacyKnownSpells([]byte{0}); err == nil {
		t.Fatal("expected truncated known-spells to fail")
	}
	tooMany := binary.LittleEndian.AppendUint16([]byte{0}, 2049)
	if _, err := ParseLegacyKnownSpells(tooMany); err == nil {
		t.Fatal("expected oversized known-spells to fail")
	}
	if _, err := ParseLegacySendUnlearnSpells(binary.LittleEndian.AppendUint32(nil, 1025)); err == nil {
		t.Fatal("expected oversized send-unlearn to fail")
	}
}
