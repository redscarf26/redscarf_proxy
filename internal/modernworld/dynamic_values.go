package modernworld

import "encoding/binary"

func encodeDynamicObjectValuesDelta(values legacyActivePlayerValues, changed map[int]uint32, mapID uint16) []byte {
	var mask uint32 = 1
	var data []byte
	_, lo := changed[6]
	_, hi := changed[7]
	if lo || hi {
		mask |= 2
		data = values.appendLegacyGUID(data, 6, mapID)
	}
	if _, ok := changed[8]; ok {
		mask |= 4
		data = append(data, byte(values.field(8)))
	}
	if _, ok := changed[9]; ok {
		mask |= 8 | 16
		data = binary.LittleEndian.AppendUint32(data, KnownSpellVisual(values.field(9)))
		data = binary.LittleEndian.AppendUint32(data, values.field(9))
	}
	if _, ok := changed[10]; ok {
		mask |= 32
		data = binary.LittleEndian.AppendUint32(data, values.field(10))
	}
	if _, ok := changed[11]; ok {
		mask |= 64
		data = binary.LittleEndian.AppendUint32(data, values.field(11))
	}
	if mask == 1 {
		return nil
	}
	bits := newBitWriter(nil)
	bits.writeBits(mask, 7)
	return append(bits.flush(), data...)
}
