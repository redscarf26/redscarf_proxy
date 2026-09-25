package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGHealthUpdate = uint16(9937)
	SMSGPowerUpdate  = uint16(9938)

	// 3.4.3 Enum.PowerType.ComboPoints is 4 (legacy proxy GetPowerSlotForClass uses
	// internal type 14 only to pick the Power[] slot, not the wire type).
	PowerComboPoints byte = 4
)

type PowerValue struct {
	Power int32
	Type  byte
}

func ParseLegacyHealthUpdate(body []byte) (uint64, uint32, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, 0, fmt.Errorf("read health GUID: %w", err)
	}
	health, err := r.u32()
	if err != nil {
		return 0, 0, fmt.Errorf("read health value: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("health update has %d trailing bytes", r.remaining())
	}
	return guid, health, nil
}

func EncodeHealthUpdate(guid GUID128, health uint32) ([]byte, error) {
	if guid.Low == 0 && guid.High == 0 {
		return nil, fmt.Errorf("health update GUID is empty")
	}
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	return binary.LittleEndian.AppendUint64(body, uint64(health)), nil
}

func ParseLegacyPowerUpdate(body []byte) (uint64, PowerValue, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, PowerValue{}, fmt.Errorf("read power GUID: %w", err)
	}
	powerType, err := r.u8()
	if err != nil {
		return 0, PowerValue{}, fmt.Errorf("read power type: %w", err)
	}
	power, err := r.u32()
	if err != nil {
		return 0, PowerValue{}, fmt.Errorf("read power value: %w", err)
	}
	if r.remaining() != 0 {
		return 0, PowerValue{}, fmt.Errorf("power update has %d trailing bytes", r.remaining())
	}
	return guid, PowerValue{Power: int32(power), Type: powerType}, nil
}

func EncodePowerUpdate(guid GUID128, powers []PowerValue) ([]byte, error) {
	if guid.Low == 0 && guid.High == 0 {
		return nil, fmt.Errorf("power update GUID is empty")
	}
	if len(powers) == 0 || len(powers) > 16 {
		return nil, fmt.Errorf("power update has %d entries, want 1..16", len(powers))
	}
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(powers)))
	for _, power := range powers {
		body = binary.LittleEndian.AppendUint32(body, uint32(power.Power))
		body = append(body, power.Type)
	}
	return body, nil
}

func ParseLegacyUpdateComboPoints(body []byte) (uint64, uint8, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, 0, fmt.Errorf("read combo-points target: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return 0, 0, fmt.Errorf("read combo-points count: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("combo-points has %d trailing bytes", r.remaining())
	}
	return guid, count, nil
}

func ParseLegacyPetUpdateComboPoints(body []byte) (uint64, uint64, uint8, error) {
	r := movementReader{data: body}
	unit, err := r.guid64()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read pet-combo unit: %w", err)
	}
	target, err := r.guid64()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read pet-combo target: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read pet-combo count: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, 0, fmt.Errorf("pet-combo has %d trailing bytes", r.remaining())
	}
	return unit, target, count, nil
}

func EncodeComboPoints(guid GUID128, count uint8) ([]byte, error) {
	return EncodePowerUpdate(guid, []PowerValue{{Power: int32(count), Type: PowerComboPoints}})
}

const (
	// Descriptor bits come from the 3.4.3.54261 descriptor table
	// (3.4.3.54261) cross-checked against TrinityCore wotlk_classic UnitData:
	// ComboTarget is bit 112 (parent 96), Power[i] is bit 137+i (parent 116).
	// Bits 33-40 are the scaling fields and 127-136 are
	// PowerRegenInterruptedFlatModifier: marking either makes the client read
	// a value that is not in the payload and desync the rest of the block.
	unitComboTargetBit       = 112
	unitComboTargetParentBit = 96
	unitPower0Bit            = 137
	unitPowerParentBit       = 116
	legacyComboPointsMax     = 5
)

func comboPowerSlot(class byte) int {
	// legacy proxy GetPowerSlotForClass(class, PowerType=14).
	switch class {
	case 11:
		return 3
	default:
		return 1
	}
}

func PlayerClassFromFields(fields map[int]uint32) byte {
	if fields == nil {
		return 0
	}
	return byte(fields[legacyUnitBytes0] >> 8)
}

func PlayerLevelFromFields(fields map[int]uint32) int32 {
	if fields == nil {
		return 0
	}
	return int32(fields[legacyUnitLevel])
}

func SetLegacyPlayerLevel(fields map[int]uint32, level int32) {
	if fields != nil {
		fields[legacyUnitLevel] = uint32(level)
	}
}

// EncodeComboPointValues is legacy proxy SMSG_UPDATE_COMBO_POINTS: a Values update
// that sets Unit/ActivePlayer ComboTarget plus Power[comboSlot].
func EncodeComboPointValues(player, target GUID128, count uint8, class byte, mapID uint16) ([]byte, error) {
	if player.Low == 0 && player.High == 0 {
		return nil, fmt.Errorf("combo-points player GUID is empty")
	}
	slot := comboPowerSlot(class)
	if slot < 0 || slot > 9 {
		return nil, fmt.Errorf("combo power slot %d is out of range", slot)
	}
	countBytes := binary.LittleEndian.AppendUint32(nil, uint32(count))
	targetBytes := appendPackedGUID128(nil, target.Low, target.High)
	deltas := []valueDelta{
		{unitComboTargetBit, targetBytes},
		{unitPower0Bit + slot, countBytes},
	}
	unitSection := encodeBlockDeltas(deltas, 8, 8, false, func(bits *bitWriter, blocks []uint32) {
		if bits != nil {
			return
		}
		setUnitParentBits(blocks, deltas)
	})
	valuesData := binary.LittleEndian.AppendUint32(nil, 0x20)
	valuesData = append(valuesData, unitSection...)
	objectData := []byte{0} // UpdateTypeModern.Values
	objectData = appendPackedGUID128(objectData, player.Low, player.High)
	objectData = binary.LittleEndian.AppendUint32(objectData, uint32(len(valuesData)))
	objectData = append(objectData, valuesData...)
	return encodeUpdateObjects(mapID, objectData), nil
}

// PowerUpdatesFromValues returns direct UNIT_FIELD_POWER1..7 deltas. WotLK and
// build 54261 use the same global power-type values for these seven entries.
func PowerUpdatesFromValues(changed map[int]uint32) []PowerValue {
	powers := make([]PowerValue, 0, 7)
	for powerType := 0; powerType < 7; powerType++ {
		if value, ok := changed[legacyUnitPower1+powerType]; ok {
			powers = append(powers, PowerValue{Power: int32(value), Type: byte(powerType)})
		}
	}
	return powers
}
