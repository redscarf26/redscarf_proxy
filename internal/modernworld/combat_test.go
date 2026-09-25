package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAttackStartStopTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0x42)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130000001000043)
	attacker, victim, err := ParseLegacyUnpackedGUIDPair(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if attacker != 0x42 || victim != 0xf130000001000043 {
		t.Fatalf("attacker=%x victim=%x", attacker, victim)
	}
	body := EncodeAttackStart(GUID128{Low: 0x42, High: 1}, GUID128{Low: 0x43, High: 1})
	first, second, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	third, fourth, rest, err := readPackedGUID128(body[consumed:])
	if err != nil || rest+consumed != len(body) || first != 0x42 || third != 0x43 || second == 0 || fourth == 0 {
		t.Fatalf("modern attack-start %x err=%v", body, err)
	}

	stopLegacy := appendLegacyPackedGUID(nil, 0x42)
	stopLegacy = appendLegacyPackedGUID(stopLegacy, 0xf130000001000043)
	stopLegacy = append(stopLegacy, 1, 0, 0, 0)
	_, _, nowDead, err := ParseLegacyAttackStop(stopLegacy)
	if err != nil || !nowDead {
		t.Fatalf("nowDead=%v err=%v", nowDead, err)
	}
	stop := EncodeAttackStop(GUID128{Low: 1, High: 1}, GUID128{Low: 2, High: 1}, true)
	if stop[len(stop)-1]&0x80 == 0 {
		t.Fatalf("now-dead bit missing: %x", stop)
	}
	attackerOnly := appendLegacyPackedGUID(nil, 0xf130000001000043)
	attacker, victim, nowDead, err = ParseLegacyAttackStop(attackerOnly)
	if err != nil || attacker != 0xf130000001000043 || victim != 0 || nowDead {
		t.Fatalf("attacker-only stop attacker=%x victim=%x nowDead=%v err=%v", attacker, victim, nowDead, err)
	}
}

func TestAIReactionAndSelection(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0x99)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	guid, reaction, err := ParseLegacyAIReaction(legacy)
	if err != nil || guid != 0x99 || reaction != 2 {
		t.Fatalf("guid=%x reaction=%d err=%v", guid, reaction, err)
	}
	body := EncodeAIReaction(GUID128{Low: 0x99, High: 1}, 2)
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil || binary.LittleEndian.Uint32(body[consumed:]) != 2 {
		t.Fatalf("modern reaction %x err=%v", body, err)
	}
	empty, err := ParsePackedGUID128Exact(nil)
	if err != nil || empty.Low != 0 {
		t.Fatalf("empty selection %#v err=%v", empty, err)
	}
	got, err := ParsePackedGUID128Exact(appendPackedGUID128(nil, 7, 1))
	if err != nil || got.Low != 7 {
		t.Fatalf("selection %#v err=%v", got, err)
	}
	if _, err := ParsePackedGUID128Exact(append(appendPackedGUID128(nil, 7, 1), 0)); err == nil {
		t.Fatal("expected trailing-byte error")
	}
}

func TestEncodeLegacyPackedGUIDRoundTrip(t *testing.T) {
	encoded := EncodeLegacyPackedGUID(0xf130000001000043)
	got, second, err := ParseLegacyPackedGUIDPair(append(encoded, EncodeLegacyPackedGUID(1)...))
	if err != nil || got != 0xf130000001000043 || second != 1 {
		t.Fatalf("got=%x second=%x err=%v", got, second, err)
	}
}

func TestSetSheathedAndAttackSwingError(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	bits := newBitWriter(body)
	bits.writeBit(true)
	body = bits.flush()
	state, err := ParseSetSheathed(body)
	if err != nil || state != 1 {
		t.Fatalf("state=%d err=%v", state, err)
	}
	encoded, err := EncodeAttackSwingError(AttackSwingNotInRange)
	if err != nil || encoded[0]&0xe0 != 0x00 {
		t.Fatalf("not-in-range %x err=%v", encoded, err)
	}
	facing, err := EncodeAttackSwingError(AttackSwingBadFacing)
	if err != nil || facing[0]&0xe0 != 0x20 {
		t.Fatalf("bad-facing %x err=%v", facing, err)
	}
	// AnimKitID (uint32 zero) then the WotLK stand state byte.
	stand, err := TranslateStandStateUpdate([]byte{1})
	if err != nil || !bytes.Equal(stand, []byte{0, 0, 0, 0, 1}) {
		t.Fatalf("stand=%x err=%v", stand, err)
	}
}

func TestAttackerStateUpdateRoundTrip(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 0x00000002)
	legacy = appendLegacyPackedGUID(legacy, 0x42)
	legacy = appendLegacyPackedGUID(legacy, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 10)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = appendFloat32(legacy, 10)
	legacy = binary.LittleEndian.AppendUint32(legacy, 10)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	update, err := ParseLegacyAttackerStateUpdate(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if update.Attacker != 0x42 || update.Victim != 0xf130000001000043 || update.Damage != 10 || len(update.SubDamage) != 1 || update.SubDamage[0].IntDamage != 10 {
		t.Fatalf("parsed %#v", update)
	}
	body := EncodeAttackerStateUpdate(GUID128{Low: 0x42, High: 1}, GUID128{Low: 0x43, High: 1}, update)
	if body[0]&0x80 != 0 {
		t.Fatalf("log-data bit should be clear: %x", body)
	}
	size := binary.LittleEndian.Uint32(body[1:5])
	if int(size) != len(body)-5 || size < 40 {
		t.Fatalf("inner size=%d body=%d", size, len(body))
	}
}

func TestThreatAndPartyKillTranslation(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = appendLegacyPackedGUID(legacy, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1500)
	unit, entries, err := ParseLegacyThreatUpdate(legacy)
	if err != nil || unit != 0xf130000001000043 || len(entries) != 1 || entries[0].Threat != 1500 {
		t.Fatalf("unit=%x entries=%v err=%v", unit, entries, err)
	}
	body := EncodeThreatUpdate(GUID128{Low: 0x43, High: 1}, entries, []GUID128{{Low: 0x42, High: 1}})
	if len(body) < 16 {
		t.Fatalf("threat body %d", len(body))
	}
	clearBody := EncodeThreatClear(GUID128{Low: 0x43, High: 1})
	if len(clearBody) < 2 {
		t.Fatal("clear body empty")
	}
	kill := EncodePartyKillLog(GUID128{Low: 1, High: 1}, GUID128{Low: 2, High: 1})
	if len(kill) < 4 {
		t.Fatal("party-kill body empty")
	}
}
