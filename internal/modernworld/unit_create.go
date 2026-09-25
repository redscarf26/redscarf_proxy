package modernworld

import (
	"encoding/binary"
	"fmt"
)

// EncodeUnitCreate translates a legacy Creature, Pet, or Vehicle CreateObject
// into a build-54261 Unit create, including living movement and create-time
// splines (legacy proxy ObjectUpdateBuilder343.writeCreateObjectSplineDataBlock).
func EncodeUnitCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions) ([]byte, error) {
	return encodePublicUnitOrPlayerCreate(update, options, 3)
}

// EncodePlayerCreate translates another (non-active) player's legacy
// CreateObject into a build-54261 Player create.
func EncodePlayerCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions) ([]byte, error) {
	return encodePublicUnitOrPlayerCreate(update, options, 4)
}

func encodePublicUnitOrPlayerCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions, wantObjectType uint8) ([]byte, error) {
	if update.Type != LegacyUpdateCreateObject1 && update.Type != LegacyUpdateCreateObject2 {
		return nil, fmt.Errorf("public object update type is %d, want legacy CreateObject", update.Type)
	}
	if update.ObjectType != wantObjectType {
		return nil, fmt.Errorf("legacy object type is %d, want %d", update.ObjectType, wantObjectType)
	}
	if update.GUID == 0 {
		return nil, fmt.Errorf("public object GUID is empty")
	}
	if update.Movement == nil || update.Movement.UpdateFlags&legacyUpdateLiving == 0 {
		return nil, fmt.Errorf("public object create has no living movement block")
	}
	if options.VirtualRealm == 0 {
		options.VirtualRealm = 1
	}
	low, high := modernLegacyCreateGUID(update, options.MapID)
	if low == 0 && high == 0 {
		return nil, fmt.Errorf("legacy GUID type 0x%04x is not mapped", uint16(update.GUID>>48))
	}

	objectData := make([]byte, 0, 2<<10)
	objectData = append(objectData, byte(update.Type-1))
	objectData = appendPackedGUID128(objectData, low, high)
	if wantObjectType == 4 {
		objectData = append(objectData, 6) // Player
	} else {
		objectData = append(objectData, 5) // Unit
	}
	var err error
	objectData, err = appendCreateMovement(objectData, update, low, high, options.MapID, false, wantObjectType == 4, nil, nil, options.Now)
	if err != nil {
		return nil, err
	}

	values := legacyActivePlayerValues{fields: update.Values.Fields}
	if update.Movement != nil {
		values.moveFlags = update.Movement.MoveFlags
		values.vehicleID = update.Movement.VehicleID
	}
	// legacy proxy's ObjectUpdateBuilder343 writes UpdateFieldFlag.Owner|UnitAll
	// (0x05) for the viewing player's totems, pets, and other summons, and
	// the matching owner-only UnitData (PowerRegen, ChannelObjects slot,
	// stats, …). Public NPCs stay at flags 0. Captured in
	// Reference capture (Stoneskin Totem 5873). The extra shaman
	// totem action bar is MultiCastActionBarFrame (known spells + action
	// buttons), not this unit create.
	owner := wantObjectType == 3 && unitOwnedByViewer(values, options.OwnerGUID)
	valuesData := make([]byte, 0, 1<<10)
	if owner {
		valuesData = append(valuesData, ownedUnitCreateFlags)
	} else {
		valuesData = append(valuesData, 0)
	}
	valuesData = values.appendObject(valuesData)
	valuesData = values.appendUnit(valuesData, owner, wantObjectType == 3, options.MapID)
	if wantObjectType == 4 {
		valuesData = values.appendPlayer(valuesData, update.GUID, options, false)
	}
	objectData = binary.LittleEndian.AppendUint32(objectData, uint32(len(valuesData)))
	objectData = append(objectData, valuesData...)
	return encodeUpdateObjects(options.MapID, objectData), nil
}

const (
	// ownedUnitCreateFlags is legacy proxy's visibility byte for a unit the viewer
	// owns: Owner (0x01) | UnitAll (0x04). Active-player creates use 0x03
	// (Owner|PartyMember) instead; do not reuse that value here.
	ownedUnitCreateFlags byte = 0x05
)

func unitOwnedByViewer(values legacyActivePlayerValues, ownerGUID uint64) bool {
	if ownerGUID == 0 {
		return false
	}
	return values.legacyGUID(legacyUnitSummonedBy) == ownerGUID ||
		values.legacyGUID(legacyUnitCreatedBy) == ownerGUID
}

func modernLegacyCreateGUID(update LegacyObjectUpdate, mapID uint16) (uint64, uint64) {
	// Keep HighGuid::Pet's legacy entry component (pet_number). legacy proxy's
	// WowGuid64.To128 does the same, and later uses the modern GUID entry for
	// CMSG_QUERY_PET_NAME. Replacing it with creature_template.entry makes the
	// created Unit differ from the owner's Summon GUID and from pet packets,
	// which leaves the portrait and action bar detached from the visible pet.
	return modernLegacyGUID(update.GUID, mapID)
}
