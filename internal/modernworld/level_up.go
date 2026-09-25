package modernworld

import (
	"encoding/binary"
	"fmt"
)

const SMSGLevelUpInfo = uint16(9961)

type LevelUpInfo struct {
	Level                int32
	HealthDelta          int32
	PowerDelta           [10]int32
	StatDelta            [5]int32
	NumNewTalents        int32
	NumNewPvpTalentSlots int32
}

func ParseLegacyLevelUpInfo(body []byte) (LevelUpInfo, error) {
	var info LevelUpInfo
	// WotLK 3.3.5a: level + health + 7 powers + 5 stats.
	if len(body) != 14*4 {
		return info, fmt.Errorf("level-up info is %d bytes, want 56", len(body))
	}
	r := movementReader{data: body}
	var err error
	if info.Level, err = r.i32(); err != nil {
		return info, fmt.Errorf("read level-up level: %w", err)
	}
	if info.HealthDelta, err = r.i32(); err != nil {
		return info, fmt.Errorf("read level-up health: %w", err)
	}
	for index := 0; index < 7; index++ {
		if info.PowerDelta[index], err = r.i32(); err != nil {
			return info, fmt.Errorf("read level-up power %d: %w", index, err)
		}
	}
	for index := range info.StatDelta {
		if info.StatDelta[index], err = r.i32(); err != nil {
			return info, fmt.Errorf("read level-up stat %d: %w", index, err)
		}
	}
	return info, nil
}

func EncodeLevelUpInfo(info LevelUpInfo) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(info.Level))
	body = binary.LittleEndian.AppendUint32(body, uint32(info.HealthDelta))
	for _, delta := range info.PowerDelta {
		body = binary.LittleEndian.AppendUint32(body, uint32(delta))
	}
	for _, delta := range info.StatDelta {
		body = binary.LittleEndian.AppendUint32(body, uint32(delta))
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(info.NumNewTalents))
	return binary.LittleEndian.AppendUint32(body, uint32(info.NumNewPvpTalentSlots))
}

// SetLevelUpTalentDelta reconstructs the two build-54261 fields that are not
// present in the 3.3.5a packet. NumTalentsAtLevel.db2 awards one point per
// level from 10 for normal classes and from 56 for death knights. PvP talent
// slots do not exist in WotLK Classic and therefore remain zero.
func SetLevelUpTalentDelta(info *LevelUpInfo, previousLevel int32, class byte) {
	if info == nil {
		return
	}
	if previousLevel <= 0 {
		previousLevel = info.Level - 1
		if previousLevel < 1 {
			previousLevel = 1
		}
	}
	info.NumNewTalents = talentsAtLevel(info.Level, class) - talentsAtLevel(previousLevel, class)
	info.NumNewPvpTalentSlots = 0
}

func talentsAtLevel(level int32, class byte) int32 {
	// The final 3.4.3 table ends at level 80; TrinityCore uses that last row
	// for custom levels above the client cap as well.
	if level > 80 {
		level = 80
	}
	firstTalentLevel := int32(10)
	if class == 6 { // Death knight
		firstTalentLevel = 56
	}
	if level < firstTalentLevel {
		return 0
	}
	return level - firstTalentLevel + 1
}
