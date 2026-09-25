package modernworld

import (
	_ "embed"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// QuestCompletedBlockCount is build 3.4.3.54261's
// ActivePlayerData.QuestCompleted uint64-array length. legacy proxy uses the same 875
// blocks, covering QuestV2 unique bits 1 through 56000.
const QuestCompletedBlockCount = 875

// Quest completion bits are not keyed by quest ID. The 3.4.3 client resolves
// every quest through QuestV2.db2's UniqueBitFlag first. This snapshot is from
// the exact client build supported by the proxy; using questID directly makes
// GetQuestsCompleted report a different quest for nearly every set bit.
//
// Source: https://wago.tools/db2/QuestV2/csv?build=3.4.3.54261
//
//go:embed quest_v2_3_4_3_54261.csv
var questV254261CSV string

var questUniqueBitByID = mustLoadQuestUniqueBits(questV254261CSV)

func mustLoadQuestUniqueBits(data string) map[uint32]uint16 {
	reader := csv.NewReader(strings.NewReader(data))
	header, err := reader.Read()
	if err != nil || len(header) != 2 || header[0] != "ID" || header[1] != "UniqueBitFlag" {
		panic(fmt.Sprintf("load QuestV2 54261 header: %v (%q)", err, header))
	}

	bits := make(map[uint32]uint16, 9000)
	for row := 2; ; row++ {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			panic(fmt.Sprintf("load QuestV2 54261 row %d: %v", row, readErr))
		}
		questID, parseErr := strconv.ParseUint(record[0], 10, 32)
		if parseErr != nil {
			panic(fmt.Sprintf("load QuestV2 54261 quest ID at row %d: %v", row, parseErr))
		}
		uniqueBit, parseErr := strconv.ParseUint(record[1], 10, 16)
		if parseErr != nil || uniqueBit == 0 || uniqueBit > QuestCompletedBlockCount*64 {
			panic(fmt.Sprintf("load QuestV2 54261 unique bit at row %d: %q (%v)", row, record[1], parseErr))
		}
		bits[uint32(questID)] = uint16(uniqueBit)
	}
	return bits
}

func ParseLegacyQueryQuestsCompletedResponse(body []byte) ([]uint32, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("query-quests-completed response has %d bytes, want at least 4", len(body))
	}
	count := binary.LittleEndian.Uint32(body)
	if count > QuestCompletedBlockCount*64 {
		return nil, fmt.Errorf("query-quests-completed count %d exceeds %d", count, QuestCompletedBlockCount*64)
	}
	want := 4 + int(count)*4
	if len(body) != want {
		return nil, fmt.Errorf("query-quests-completed response has %d bytes, want %d for %d quests", len(body), want, count)
	}
	quests := make([]uint32, count)
	for index := range quests {
		quests[index] = binary.LittleEndian.Uint32(body[4+index*4:])
	}
	return quests, nil
}

func QuestCompletedBlocks(quests []uint32) []uint64 {
	blocks := make([]uint64, QuestCompletedBlockCount)
	for _, questID := range quests {
		MarkQuestCompleted(blocks, questID)
	}
	return blocks
}

// MarkQuestCompleted resolves QuestV2.UniqueBitFlag and returns the changed
// ActivePlayerData array element.
func MarkQuestCompleted(blocks []uint64, questID uint32) (index int, value uint64, changed bool) {
	uniqueBit := uint32(questUniqueBitByID[questID])
	if uniqueBit == 0 {
		return -1, 0, false
	}
	zeroBased := uniqueBit - 1
	index = int(zeroBased / 64)
	if index >= QuestCompletedBlockCount || index >= len(blocks) {
		return -1, 0, false
	}
	mask := uint64(1) << (zeroBased % 64)
	old := blocks[index]
	blocks[index] |= mask
	return index, blocks[index], blocks[index] != old
}

// EncodeQuestCompletedUpdate emits an ActivePlayer Values update for the
// specified QuestCompleted array blocks. A zero value is meaningful because it
// clears a previously populated block after an authoritative reload.
func EncodeQuestCompletedUpdate(mapID uint16, player GUID128, changed map[int]uint64) ([]byte, error) {
	if player.Low == 0 && player.High == 0 {
		return nil, fmt.Errorf("quest-completed player GUID is empty")
	}
	deltas := make([]valueDelta, 0, len(changed))
	for index, value := range changed {
		if index < 0 || index >= QuestCompletedBlockCount {
			return nil, fmt.Errorf("quest-completed block %d is outside 0..%d", index, QuestCompletedBlockCount-1)
		}
		deltas = append(deltas, valueDelta{
			bit:  637 + index,
			data: binary.LittleEndian.AppendUint64(nil, value),
		})
	}
	section := encodeActivePlayerFieldDeltas(deltas)
	if len(section) == 0 {
		return nil, nil
	}
	valuesData := binary.LittleEndian.AppendUint32(nil, 0x80) // ActivePlayerData
	valuesData = append(valuesData, section...)
	objectData := []byte{0} // UpdateTypeModern.Values
	objectData = appendPackedGUID128(objectData, player.Low, player.High)
	objectData = binary.LittleEndian.AppendUint32(objectData, uint32(len(valuesData)))
	objectData = append(objectData, valuesData...)
	return encodeUpdateObjects(mapID, objectData), nil
}
