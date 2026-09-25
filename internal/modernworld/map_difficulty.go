package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Map metadata is restricted to stock 3.3.5 maps. Unknown/custom maps do not
// inherit dungeon semantics merely because their numeric ID is above one.
func IsLegacyInstanceMap(id uint32) bool {
	switch id {
	case 33, 34, 36, 43, 47, 48, 70, 90, 109, 129, 189, 209, 229, 230, 249,
		269, 289, 309, 329, 349, 389, 409, 429, 469, 509, 531, 532, 533, 534,
		540, 542, 543, 544, 545, 546, 547, 548, 550, 552, 553, 554, 555, 556,
		557, 558, 560, 564, 565, 568, 574, 575, 576, 578, 580, 585, 595, 599,
		600, 601, 602, 603, 604, 608, 615, 616, 619, 624, 631, 632, 649, 650,
		658, 668, 724:
		return true
	}
	return false
}

type MapDifficulty struct {
	SpawnMode     uint32
	DynamicHeroic bool
}

func ParseLegacyInstanceDifficulty(body []byte) (MapDifficulty, error) {
	if len(body) != 8 {
		return MapDifficulty{}, fmt.Errorf("instance difficulty has %d bytes, want 8", len(body))
	}
	mode, dynamic := binary.LittleEndian.Uint32(body), binary.LittleEndian.Uint32(body[4:])
	if mode > 3 || dynamic > 1 {
		return MapDifficulty{}, fmt.Errorf("invalid instance difficulty %d/%d", mode, dynamic)
	}
	return MapDifficulty{mode, dynamic != 0}, nil
}

func WorldServerInfoForDifficulty(mapID uint32, difficulty MapDifficulty) WorldServerInfo {
	if !IsLegacyInstanceMap(mapID) {
		return WorldServerInfo{}
	}
	id := InstanceDifficultyForMap(mapID, difficulty.SpawnMode)
	// SpawnMode is already the actual map mode; the dynamic flag is not a
	// second difficulty ID and must not turn a normal mode into heroic.
	var size uint32
	switch id {
	case 1, 2:
		size = 5
	case 3, 5:
		size = 10
	case 4, 6:
		size = 25
	case 9:
		size = 40
	case 148:
		size = 20
	default:
		return WorldServerInfo{}
	}
	return WorldServerInfo{DifficultyID: id, InstanceGroupSize: &size}
}
