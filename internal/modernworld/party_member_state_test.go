package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPartyMountAuraFilteredLikeLegacyProxy(t *testing.T) {
	// Reproduce the two spells in the 2026-09-09 crash-session roster.
	// legacy proxy retains 19884 but filters mount 10793 entirely.
	legacy := binary.LittleEndian.AppendUint64(nil, 3)
	for _, spell := range []uint32{19884, 10793} {
		legacy = binary.LittleEndian.AppendUint32(legacy, spell)
		legacy = append(legacy, legacyAuraPositive|legacyAuraNoCaster|legacyAuraEffect0|legacyAuraEffect1)
	}
	r := movementReader{data: legacy}
	auras, err := parseLegacyPartyMemberAuras(&r, "party-member")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0xac, 0x4d, 0, 0, 3, 1, 3, 0, 0, 0, 0, 0, 0, 0,
	}
	if got := appendPartyMemberAuraEntries(nil, auras); !bytes.Equal(got, want) {
		t.Fatalf("remote aura wire metadata = %x, want %x", got, want)
	}
}

func TestPartyMemberPartialStateTranslation54261(t *testing.T) {
	const (
		memberGUID = uint64(0x43)
		petGUID    = uint64(0xf140000001000044)
	)
	flags := uint32(0x000001 | 0x000002 | 0x000004 | 0x000008 | 0x000010 | 0x000020 |
		0x000040 | 0x000080 | 0x000100 | 0x000200 | 0x000400 | 0x000800 | 0x001000 |
		0x002000 | 0x004000 | 0x040000 | 0x080000)
	legacy := appendLegacyPackedGUID(nil, memberGUID)
	legacy = binary.LittleEndian.AppendUint32(legacy, flags)
	legacy = binary.LittleEndian.AppendUint16(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 123)
	legacy = binary.LittleEndian.AppendUint32(legacy, 456)
	legacy = append(legacy, 0)
	legacy = binary.LittleEndian.AppendUint16(legacy, 78)
	legacy = binary.LittleEndian.AppendUint16(legacy, 100)
	legacy = binary.LittleEndian.AppendUint16(legacy, 80)
	legacy = binary.LittleEndian.AppendUint16(legacy, 1519)
	legacy = binary.LittleEndian.AppendUint16(legacy, 0xfff4)
	legacy = binary.LittleEndian.AppendUint16(legacy, 34)
	legacy = binary.LittleEndian.AppendUint64(legacy, uint64(1)<<2)
	legacy = binary.LittleEndian.AppendUint32(legacy, 21562)
	legacy = append(legacy, legacyAuraPositive|legacyAuraEffect0)
	legacy = binary.LittleEndian.AppendUint64(legacy, petGUID)
	legacy = append(legacy, "Wolf"...)
	legacy = append(legacy, 0)
	legacy = binary.LittleEndian.AppendUint16(legacy, 1234)
	legacy = binary.LittleEndian.AppendUint32(legacy, 55)
	legacy = binary.LittleEndian.AppendUint32(legacy, 66)
	legacy = binary.LittleEndian.AppendUint64(legacy, uint64(1)<<1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 12345)
	legacy = append(legacy, legacyAuraNegative|legacyAuraEffect0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3)

	state, err := ParseLegacyPartyMemberPartialState(legacy)
	if err != nil {
		t.Fatal(err)
	}
	state.MemberGUID = ModernGUIDForLegacy(memberGUID, 0)
	petModern := ModernGUIDForLegacy(petGUID, 0)
	state.Pet.GUID = &petModern
	if state.Status == nil || *state.Status != 1 || state.CurrentHealth == nil || *state.CurrentHealth != 123 ||
		state.Position == nil || state.Position[0] != -12 || state.Pet == nil || state.Pet.Name == nil || *state.Pet.Name != "Wolf" ||
		state.Auras == nil || len(*state.Auras) != 1 || state.Pet.Auras == nil || len(*state.Pet.Auras) != 1 {
		t.Fatalf("unexpected partial state: %#v", state)
	}

	modern := EncodePartyMemberPartialState(state)
	r := movementReader{data: modern}
	presence := make([]bool, 22)
	for index := range presence {
		presence[index], err = r.bit()
		if err != nil {
			t.Fatal(err)
		}
	}
	// 0..2 flags; then Status, PowerType, Health, Power, Level, Zone,
	// Position, Vehicle, Auras and Pet are represented as modern option bits.
	for _, index := range []int{4, 5, 7, 8, 9, 10, 11, 13, 16, 17, 18, 19} {
		if !presence[index] {
			t.Fatalf("modern presence bit %d is clear: %08b", index, modern[:3])
		}
	}
	for _, index := range []int{0, 1, 2, 3, 6, 12, 14, 15, 20, 21} {
		if presence[index] {
			t.Fatalf("modern presence bit %d is set: %08b", index, modern[:3])
		}
	}
	r.align()
	petPresence := make([]bool, 6)
	for index := range petPresence {
		petPresence[index], err = r.bit()
		if err != nil || !petPresence[index] {
			t.Fatalf("pet presence bit %d=%v err=%v", index, petPresence[index], err)
		}
	}
	r.align()
	petNameLength, err := r.bits(8)
	if err != nil {
		t.Fatal(err)
	}
	petName, err := r.stringN(int(petNameLength))
	if err != nil || petName != "Wolf" {
		t.Fatalf("pet name=%q err=%v", petName, err)
	}
	decodedPetGUID, _ := r.guid128()
	model, _ := r.i32()
	petHealth, _ := r.i32()
	petMaxHealth, _ := r.i32()
	petAuraCount, _ := r.u32()
	petAuraSpell, _ := r.i32()
	petAuraFlags, _ := r.u16()
	_, _ = r.u32()
	petPoints, _ := r.u32()
	if decodedPetGUID != petModern || model != 1234 || petHealth != 55 || petMaxHealth != 66 || petAuraCount != 1 || petAuraSpell != 12345 || petAuraFlags&auraFlagNegative == 0 || petPoints != 0 {
		t.Fatalf("bad pet partial GUID=%#v model=%d hp=%d/%d auras=%d spell=%d flags=%x points=%d", decodedPetGUID, model, petHealth, petMaxHealth, petAuraCount, petAuraSpell, petAuraFlags, petPoints)
	}

	decodedMemberGUID, _ := r.guid128()
	status, _ := r.u16()
	powerType, _ := r.u8()
	health, _ := r.i32()
	maxHealth, _ := r.i32()
	currentPower, _ := r.u16()
	maxPower, _ := r.u16()
	level, _ := r.u16()
	zone, _ := r.u16()
	positionX, _ := r.u16()
	positionY, _ := r.u16()
	positionZ, _ := r.u16()
	vehicleSeat, _ := r.i32()
	auraCount, _ := r.u32()
	auraSpell, _ := r.i32()
	auraFlags, _ := r.u16()
	activeFlags, _ := r.u32()
	points, _ := r.u32()
	if decodedMemberGUID != state.MemberGUID || status != 1 || powerType != 0 || health != 123 || maxHealth != 456 ||
		currentPower != 78 || maxPower != 100 || level != 80 || zone != 1519 || int16(positionX) != -12 || positionY != 34 || positionZ != 0 ||
		vehicleSeat != 3 || auraCount != 1 || auraSpell != 21562 || auraFlags&(auraFlagPositive|auraFlagCancelable) == 0 || activeFlags != 1 || points != 0 || r.remaining() != 0 {
		t.Fatalf("bad modern state guid=%#v status=%d power=%d hp=%d/%d resource=%d/%d level=%d zone=%d pos=%d/%d/%d vehicle=%d aura=%d/%d/%x/%d/%d remaining=%d body=%x",
			decodedMemberGUID, status, powerType, health, maxHealth, currentPower, maxPower, level, zone, int16(positionX), positionY, positionZ,
			vehicleSeat, auraCount, auraSpell, auraFlags, activeFlags, points, r.remaining(), modern)
	}
	if _, err := ParseLegacyPartyMemberPartialState(legacy[:len(legacy)-1]); err == nil {
		t.Fatal("partial-state parser accepted a truncated vehicle seat")
	}
}

func TestPartyMemberFullStateTranslation54261(t *testing.T) {
	const memberGUID = uint64(0x43)
	flags := uint32(0x000001 | 0x000002 | 0x000004 | 0x000008 | 0x000010 | 0x000020 |
		0x000040 | 0x000080 | 0x000100)
	legacy := []byte{1} // ForEnemy/full update byte
	legacy = appendLegacyPackedGUID(legacy, memberGUID)
	legacy = binary.LittleEndian.AppendUint32(legacy, flags)
	legacy = binary.LittleEndian.AppendUint16(legacy, 1) // online
	legacy = binary.LittleEndian.AppendUint32(legacy, 123)
	legacy = binary.LittleEndian.AppendUint32(legacy, 456)
	legacy = append(legacy, 0)
	legacy = binary.LittleEndian.AppendUint16(legacy, 78)
	legacy = binary.LittleEndian.AppendUint16(legacy, 100)
	legacy = binary.LittleEndian.AppendUint16(legacy, 80)
	legacy = binary.LittleEndian.AppendUint16(legacy, 1519)
	legacy = binary.LittleEndian.AppendUint16(legacy, 0xfff4)
	legacy = binary.LittleEndian.AppendUint16(legacy, 34)

	full, err := ParseLegacyPartyMemberFullState(legacy)
	if err != nil {
		t.Fatal(err)
	}
	full.Member.MemberGUID = ModernGUIDForLegacy(memberGUID, 0)
	modern := EncodePartyMemberFullState(full, [2]byte{1, 0})
	r := movementReader{data: modern}
	forEnemy, _ := r.bit()
	r.align()
	partyType0, _ := r.u8()
	partyType1, _ := r.u8()
	status, _ := r.u16()
	powerType, _ := r.u8()
	powerDisplay, _ := r.u16()
	health, _ := r.i32()
	maxHealth, _ := r.i32()
	currentPower, _ := r.u16()
	maxPower, _ := r.u16()
	level, _ := r.u16()
	specID, _ := r.u16()
	zone, _ := r.u16()
	wmoGroup, _ := r.u16()
	wmoDoodad, _ := r.i32()
	x, _ := r.u16()
	y, _ := r.u16()
	z, _ := r.u16()
	vehicle, _ := r.i32()
	auraCount, _ := r.u32()
	phaseFlags, _ := r.u32()
	phaseCount, _ := r.u32()
	personalPhase, _ := r.guid128()
	conditionMask, _ := r.u32()
	unused901, _ := r.u32()
	expansionMask, _ := r.u32()
	hasPet, _ := r.bit()
	r.align()
	decodedMember, _ := r.guid128()
	if !forEnemy || partyType0 != 1 || partyType1 != 0 || status != 1 || powerType != 0 || powerDisplay != 0 ||
		health != 123 || maxHealth != 456 || currentPower != 78 || maxPower != 100 || level != 80 || specID != 0 || zone != 1519 ||
		wmoGroup != 0 || wmoDoodad != 0 || int16(x) != -12 || y != 34 || z != 0 || vehicle != 0 || auraCount != 0 ||
		phaseFlags != 8 || phaseCount != 0 || personalPhase != (GUID128{}) || conditionMask != 0 || unused901 != 0 || expansionMask != 0 ||
		hasPet || decodedMember != full.Member.MemberGUID || r.remaining() != 0 {
		t.Fatalf("full state enemy=%v party=%d/%d status=%d power=%d/%d hp=%d/%d resource=%d/%d level=%d spec=%d zone=%d wmo=%d/%d pos=%d/%d/%d vehicle=%d auras=%d phase=%d/%d/%#v ctr=%d/%d/%d pet=%v guid=%#v remaining=%d body=%x",
			forEnemy, partyType0, partyType1, status, powerType, powerDisplay, health, maxHealth, currentPower, maxPower, level, specID, zone,
			wmoGroup, wmoDoodad, int16(x), y, z, vehicle, auraCount, phaseFlags, phaseCount, personalPhase,
			conditionMask, unused901, expansionMask, hasPet, decodedMember, r.remaining(), modern)
	}
	if _, err := ParseLegacyPartyMemberFullState(legacy[:len(legacy)-1]); err == nil {
		t.Fatal("truncated full state was accepted")
	}
}
