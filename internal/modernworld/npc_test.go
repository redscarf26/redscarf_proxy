package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestEncodeNpcInteractionSpiritHealer(t *testing.T) {
	guid := GUID128{Low: 0x85, High: uint64(8)<<58 | uint64(1)<<42}
	body := EncodeNpcInteraction(guid, PlayerInteractionSpiritHealer, true)
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil || low != guid.Low || high != guid.High {
		t.Fatalf("guid %#v consumed=%d err=%v body=%x", guid, consumed, err, body)
	}
	rest := body[consumed:]
	if int32(binary.LittleEndian.Uint32(rest[:4])) != PlayerInteractionSpiritHealer {
		t.Fatalf("interaction %x", rest)
	}
	if rest[4]&0x80 == 0 {
		t.Fatalf("success bit missing: %x", rest)
	}
}

func TestLegacyUnitUsesSpellClick(t *testing.T) {
	cannon := uint64(0xf150008fe600023f)
	if !LegacyUnitUsesSpellClick(cannon, 0) {
		t.Fatal("vehicle GUID without gossip should spell-click")
	}
	if !LegacyUnitUsesSpellClick(0xf130000001000001, legacyNPCFlagSpellClick) {
		t.Fatal("explicit SPELLCLICK flag should spell-click")
	}
	if LegacyUnitUsesSpellClick(cannon, legacyNPCFlagGossip) {
		t.Fatal("vehicle with only gossip should keep gossip")
	}
	if LegacyUnitUsesSpellClick(0xf130000001000001, legacyNPCFlagGossip) {
		t.Fatal("creature gossip should stay gossip")
	}
}

func TestLegacyHealthDroppedToZero(t *testing.T) {
	if !LegacyHealthDroppedToZero(map[int]uint32{legacyUnitHealth: 0}) {
		t.Fatal("health 0 should count as death")
	}
	if LegacyHealthDroppedToZero(map[int]uint32{legacyUnitHealth: 1}) {
		t.Fatal("health 1 is not death")
	}
	if LegacyHealthDroppedToZero(map[int]uint32{legacyUnitFlags: 0}) {
		t.Fatal("missing health field is not death")
	}
}

func TestEncodeNpcInteractionBinder(t *testing.T) {
	guid := GUID128{Low: 0x85, High: uint64(8)<<58 | uint64(1)<<42}
	body := EncodeNpcInteraction(guid, PlayerInteractionBinder, true)
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	rest := body[consumed:]
	if int32(binary.LittleEndian.Uint32(rest[:4])) != PlayerInteractionBinder || rest[4]&0x80 == 0 {
		t.Fatalf("binder interaction malformed: %x", rest)
	}
}

func TestParseLegacyPackedGUID(t *testing.T) {
	unpacked := EncodeLegacyUnpackedGUID(0xf130000001000042)
	got, err := ParseLegacyPackedGUID(unpacked)
	if err != nil || got != 0xf130000001000042 {
		t.Fatalf("unpacked %#x err=%v", got, err)
	}
	packed := EncodeLegacyPackedGUID(0x42)
	got, err = ParseLegacyPackedGUID(packed)
	if err != nil || got != 0x42 {
		t.Fatalf("packed %#x err=%v", got, err)
	}
}

func TestParseTalkToGossipAndSpiritHealer(t *testing.T) {
	body := appendPackedGUID128(nil, 9, 1)
	guid, err := ParseTalkToGossip(body)
	if err != nil || guid.Low != 9 {
		t.Fatalf("talk %#v err=%v", guid, err)
	}
	guid, err = ParseSpiritHealerActivate(body)
	if err != nil || guid.Low != 9 {
		t.Fatalf("activate %#v err=%v", guid, err)
	}
	guid, err = ParseBinderActivate(body)
	if err != nil || guid != (GUID128{Low: 9, High: 1}) {
		t.Fatalf("binder %#v err=%v", guid, err)
	}
}
