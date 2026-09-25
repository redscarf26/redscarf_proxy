package modernworld

import "encoding/binary"

// GUID128 is the build-54261 low/high representation used by object lifecycle
// packets. It is exposed so the relay can remember corrected Pet GUIDs between
// CreateObject and later removal packets.
type GUID128 struct {
	Low  uint64
	High uint64
}

func ModernGUIDForLegacy(guid uint64, mapID uint16) GUID128 {
	low, high := modernLegacyGUID(guid, mapID)
	return GUID128{Low: low, High: high}
}

func ModernGUIDForCreate(update LegacyObjectUpdate, mapID uint16) GUID128 {
	low, high := modernLegacyCreateGUID(update, mapID)
	return GUID128{Low: low, High: high}
}

// ModernItemHigh is the build-54261 GUID128 high bits that modernLegacyGUID
// assigns to every legacy Item object (legacy HighGuid 0x4000/0x4700). The low
// half carries the item counter, so the mapping is invertible without the proxy
// ever having seen a create object for the item (bag items never get one).
const ModernItemHigh = uint64(3)<<58 | uint64(1)<<42

// LegacyItemGUIDFromModern recovers the legacy 64-bit Item GUID from the modern
// GUID128 an inventory item was assigned. It is the deterministic inverse of
// modernLegacyGUID for items and is used to forward modern item controls (for
// example auctioning a weapon straight out of a bag) whose item object was never
// created on the world so it has no session.objectGUIDs entry.
func LegacyItemGUIDFromModern(guid GUID128) uint64 {
	if guid.Low == 0 || guid.High != ModernItemHigh {
		return 0
	}
	return uint64(0x4000)<<48 | uint64(uint32(guid.Low))
}

// EncodeObjectRemovals builds the removal section of SMSG_UPDATE_OBJECT.
// Destroyed GUIDs must precede ordinary out-of-range GUIDs on the wire.
func EncodeObjectRemovals(mapID uint16, destroyed, outOfRange []GUID128) []byte {
	destroyed = nonEmptyGUIDs(destroyed)
	outOfRange = nonEmptyGUIDs(outOfRange)
	if len(destroyed) == 0 && len(outOfRange) == 0 {
		return encodeUpdateObjects(mapID)
	}
	body := make([]byte, 0, 17+(len(destroyed)+len(outOfRange))*12)
	body = binary.LittleEndian.AppendUint32(body, 0) // object-update count
	body = binary.LittleEndian.AppendUint16(body, mapID)
	bits := newBitWriter(body)
	bits.writeBit(true)
	body = bits.flush()
	body = binary.LittleEndian.AppendUint16(body, uint16(len(destroyed)))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(destroyed)+len(outOfRange)))
	for _, guid := range destroyed {
		body = appendPackedGUID128(body, guid.Low, guid.High)
	}
	for _, guid := range outOfRange {
		body = appendPackedGUID128(body, guid.Low, guid.High)
	}
	return binary.LittleEndian.AppendUint32(body, 0) // object-data byte count
}

func nonEmptyGUIDs(source []GUID128) []GUID128 {
	result := make([]GUID128, 0, len(source))
	for _, guid := range source {
		if guid.Low != 0 || guid.High != 0 {
			result = append(result, guid)
		}
	}
	return result
}
