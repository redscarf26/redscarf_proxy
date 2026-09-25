package modernworld

import (
	"encoding/binary"
	"fmt"
)

// MergeUpdateObjectBodies combines single-object SMSG_UPDATE_OBJECT bodies
// into one envelope.  Hermes emits a single update packet for a group of
// creates; keeping the object-data bytes untouched lets the caller use the
// same encoder while matching that packet boundary.
//
// The function deliberately accepts only create/value bodies produced by
// encodeUpdateObjects.  Removal updates have a different section layout and
// must not be accidentally concatenated with object data.
func MergeUpdateObjectBodies(mapID uint16, bodies ...[]byte) ([]byte, error) {
	if len(bodies) == 0 {
		return EncodeEmptyUpdateObject(mapID), nil
	}

	objects := make([][]byte, 0, len(bodies))
	totalSize := 0
	for index, body := range bodies {
		if len(body) < 11 {
			return nil, fmt.Errorf("update-object body %d is only %d bytes", index+1, len(body))
		}
		if count := binary.LittleEndian.Uint32(body[:4]); count != 1 {
			return nil, fmt.Errorf("update-object body %d contains %d objects, want 1", index+1, count)
		}
		if got := binary.LittleEndian.Uint16(body[4:6]); got != mapID {
			return nil, fmt.Errorf("update-object body %d map id %d, want %d", index+1, got, mapID)
		}
		// The one-byte removal bitset follows the map id.  A create body must
		// have its removal bit clear; otherwise bytes 7 onward are not a data
		// size and cannot be merged safely.
		if body[6]&0x80 != 0 {
			return nil, fmt.Errorf("update-object body %d contains removals", index+1)
		}
		dataSize := int(binary.LittleEndian.Uint32(body[7:11]))
		if dataSize != len(body)-11 {
			return nil, fmt.Errorf("update-object body %d data size %d, want %d", index+1, dataSize, len(body)-11)
		}
		objects = append(objects, body[11:])
		totalSize += dataSize
	}

	// Keep the same size guard as the packet encoder's uint32 length field and
	// avoid an integer overflow on a malformed caller-provided body list.
	if totalSize < 0 || uint64(totalSize) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("merged update-object data is too large: %d bytes", totalSize)
	}
	return encodeUpdateObjects(mapID, objects...), nil
}
