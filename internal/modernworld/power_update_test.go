package modernworld

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestPowerUpdateTranslation(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = append(legacy, 6)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1234)
	guid, power, err := ParseLegacyPowerUpdate(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if guid != 0x42 || power.Type != 6 || power.Power != 1234 {
		t.Fatalf("guid=%x power=%#v", guid, power)
	}
	modernGUID := GUID128{Low: 0x42, High: 1}
	body, err := EncodePowerUpdate(modernGUID, []PowerValue{power, {Power: 50, Type: 0}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[consumed:]) != 2 || binary.LittleEndian.Uint32(body[consumed+4:]) != 1234 || body[consumed+8] != 6 {
		t.Fatalf("modern power body=%x", body)
	}
}

func TestHealthUpdateTranslation(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0xfedcba98)
	guid, health, err := ParseLegacyHealthUpdate(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if guid != 0xf130000001000043 || health != 0xfedcba98 {
		t.Fatalf("guid=%x health=%x", guid, health)
	}
	body, err := EncodeHealthUpdate(GUID128{Low: 0x43, High: 1}, health)
	if err != nil {
		t.Fatal(err)
	}
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != consumed+8 || binary.LittleEndian.Uint64(body[consumed:]) != uint64(health) {
		t.Fatalf("modern health body=%x", body)
	}
}

func TestPowerUpdatesFromValues(t *testing.T) {
	powers := PowerUpdatesFromValues(map[int]uint32{
		legacyUnitPower1:     100,
		legacyUnitPower1 + 3: 75,
		legacyUnitMaxPower1:  999,
	})
	if len(powers) != 2 || powers[0] != (PowerValue{Power: 100, Type: 0}) || powers[1] != (PowerValue{Power: 75, Type: 3}) {
		t.Fatalf("powers=%#v", powers)
	}
}

func TestUpdateComboPointsToPowerUpdate(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0xf130000001000043)
	legacy = append(legacy, 3)
	guid, count, err := ParseLegacyUpdateComboPoints(legacy)
	if err != nil || guid != 0xf130000001000043 || count != 3 {
		t.Fatalf("guid=%x count=%d err=%v", guid, count, err)
	}
	body, err := EncodeComboPoints(GUID128{Low: 0x42, High: 1}, count)
	if err != nil {
		t.Fatal(err)
	}
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[consumed:]) != 1 || binary.LittleEndian.Uint32(body[consumed+4:]) != 3 || body[consumed+8] != PowerComboPoints {
		t.Fatalf("combo power body=%x", body)
	}
}

func TestPetUpdateComboPointsAndValues(t *testing.T) {
	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = appendLegacyPackedGUID(legacy, 0xf130000001000043)
	legacy = append(legacy, 4)
	unit, target, count, err := ParseLegacyPetUpdateComboPoints(legacy)
	if err != nil || unit != 0x42 || target != 0xf130000001000043 || count != 4 {
		t.Fatalf("unit=%x target=%x count=%d err=%v", unit, target, count, err)
	}
	player := GUID128{Low: 0x42, High: 1}
	targetGUID := ModernGUIDForLegacy(target, 0)
	body, err := EncodeComboPointValues(player, targetGUID, count, 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body) != 1 {
		t.Fatalf("update count %x", body[:4])
	}
	payload := valuesUpdatePayload(t, body)
	if binary.LittleEndian.Uint32(payload)&0x20 == 0 {
		t.Fatalf("changed mask 0x%x missing unit section", binary.LittleEndian.Uint32(payload))
	}
	if binary.LittleEndian.Uint32(payload)&0x80 != 0 {
		t.Fatalf("changed mask 0x%x should not include ActivePlayer", binary.LittleEndian.Uint32(payload))
	}
	if comboPowerSlot(4) != 1 || comboPowerSlot(11) != 3 {
		t.Fatalf("combo slots rogue=%d druid=%d", comboPowerSlot(4), comboPowerSlot(11))
	}
	if PlayerClassFromFields(map[int]uint32{legacyUnitBytes0: uint32(4) << 8}) != 4 {
		t.Fatal("player class from bytes0")
	}
	fields := map[int]uint32{legacyUnitLevel: 19}
	if PlayerLevelFromFields(fields) != 19 {
		t.Fatal("player level was not read from UNIT_FIELD_LEVEL")
	}
	SetLegacyPlayerLevel(fields, 20)
	if PlayerLevelFromFields(fields) != 20 {
		t.Fatal("cached player level was not advanced")
	}
	if !containsLE32(payload, uint32(count)) {
		t.Fatalf("combo values missing count, payload=%x", payload)
	}
	// Unit section: 8-bit block mask, then one 32-bit word per marked block.
	// Only blocks 3 (ComboTarget 112 + its parent 96 + Power parent 116) and
	// 4 (Power[1] at bit 138) may be present; anything else means the client
	// reads a field the payload does not carry.
	if unitMask := payload[4]; unitMask != 0x18 {
		t.Fatalf("combo unit blocks = 0x%x, want 0x18 (blocks 3+4)", unitMask)
	}
	// Blocks go through bitWriter.writeBits, which is MSB first.
	block3 := binary.BigEndian.Uint32(payload[5:])
	wantBlock3 := uint32(1<<(unitComboTargetParentBit-96) | 1<<(unitComboTargetBit-96) | 1<<(unitPowerParentBit-96))
	if block3 != wantBlock3 {
		t.Fatalf("combo block3 = 0x%08x, want 0x%08x", block3, wantBlock3)
	}
	block4 := binary.BigEndian.Uint32(payload[9:])
	if wantBlock4 := uint32(1 << (unitPower0Bit + 1 - 128)); block4 != wantBlock4 {
		t.Fatalf("combo block4 = 0x%08x, want 0x%08x (Power[1])", block4, wantBlock4)
	}
}

func TestRogueCreateExposesComboMaxPower(t *testing.T) {
	_, maximum := modernClassPowers(legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitPower1 + 3:    80,
		legacyUnitMaxPower1 + 3: 100,
	}}, 4)
	if maximum[0] != 100 {
		t.Fatalf("energy max=%d", maximum[0])
	}
	if maximum[1] != legacyComboPointsMax {
		t.Fatalf("combo max=%d, want %d", maximum[1], legacyComboPointsMax)
	}
	const (
		uniqueEnergy    = uint32(0xa1a1a1a1)
		uniqueEnergyMax = uint32(0xb2b2b2b2)
	)
	wired := legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitBytes0:        uint32(4)<<8 | uint32(3)<<24, // rogue, display power energy
		legacyUnitPower1 + 3:    uniqueEnergy,
		legacyUnitMaxPower1 + 3: uniqueEnergyMax,
	}}.appendUnit(nil, true, false, 0)
	// UnitData::WriteCreate: race, class, playerClass, sex, displayPower, then
	// OverrideDisplayPowerID, 10 owner-only PowerRegen* float pairs, then
	// (Power[i], MaxPower[i], ModPowerRegen[i]) per index.
	marker := -1
	for index := 0; index+5 <= len(wired); index++ {
		if wired[index] == 0 && wired[index+1] == 4 && wired[index+2] == 0 && wired[index+3] == 0 && wired[index+4] == 3 {
			marker = index
			break
		}
	}
	if marker < 0 {
		t.Fatal("race/class/sex/displayPower bytes not found in CREATE unit blob")
	}
	energyAt := marker + 5 + 4 + 10*2*4
	if got := binary.LittleEndian.Uint32(wired[energyAt:]); got != uniqueEnergy {
		t.Fatalf("Power[0] = 0x%x, want energy 0x%x", got, uniqueEnergy)
	}
	if got := binary.LittleEndian.Uint32(wired[energyAt+4:]); got != uniqueEnergyMax {
		t.Fatalf("MaxPower[0] = 0x%x, want 0x%x (arrays must interleave)", got, uniqueEnergyMax)
	}
	if got := binary.LittleEndian.Uint32(wired[energyAt+16:]); got != legacyComboPointsMax {
		t.Fatalf("MaxPower[1] = %d, want %d", got, legacyComboPointsMax)
	}
	_, mageMax := modernClassPowers(legacyActivePlayerValues{fields: map[int]uint32{
		legacyUnitMaxPower1: 2000,
	}}, 8)
	if mageMax[1] != 0 {
		t.Fatalf("mage should not grow a combo power slot, max1=%d", mageMax[1])
	}
}

func containsLE32(body []byte, want uint32) bool {
	for index := 0; index+4 <= len(body); index++ {
		if binary.LittleEndian.Uint32(body[index:index+4]) == want {
			return true
		}
	}
	return false
}

func TestPowerUpdateRejectsMalformed(t *testing.T) {
	if _, _, err := ParseLegacyPowerUpdate([]byte{1}); err == nil || !strings.Contains(err.Error(), "power GUID") {
		t.Fatalf("parse error=%v", err)
	}
	if _, err := EncodePowerUpdate(GUID128{Low: 1}, nil); err == nil || !strings.Contains(err.Error(), "1..16") {
		t.Fatalf("empty encode error=%v", err)
	}
	if _, err := EncodePowerUpdate(GUID128{}, []PowerValue{{}}); err == nil || !strings.Contains(err.Error(), "GUID is empty") {
		t.Fatalf("GUID encode error=%v", err)
	}
	if _, _, err := ParseLegacyHealthUpdate([]byte{1}); err == nil || !strings.Contains(err.Error(), "health GUID") {
		t.Fatalf("health parse error=%v", err)
	}
	if _, err := EncodeHealthUpdate(GUID128{}, 1); err == nil || !strings.Contains(err.Error(), "GUID is empty") {
		t.Fatalf("health encode error=%v", err)
	}
}
