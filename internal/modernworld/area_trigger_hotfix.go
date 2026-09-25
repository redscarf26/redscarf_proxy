package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
)

const areaTriggerTableHash = uint32(441483745)

// AreaTrigger3.csv is the WotLK compatibility table shipped with legacy proxy/Hermes.
// In particular, record 4354 restores the Blasted Lands Dark Portal volume.
func loadAreaTriggerHotfixes() ([]HotfixRecord, error) {
	rows, err := readHotfixCSV("hotfixdata/AreaTrigger3.csv")
	if err != nil {
		return nil, err
	}
	records := make([]HotfixRecord, 0, len(rows))
	for i, row := range rows {
		if len(row) != 18 {
			return nil, fmt.Errorf("area trigger row %d has %d fields", i+1, len(row))
		}
		content := appendCString(nil, row[0])
		var recordID uint32
		for col := 1; col < len(row); col++ {
			switch col {
			case 1, 2, 3, 9, 10, 11, 12, 13:
				v, err := strconv.ParseFloat(row[col], 32)
				if err != nil {
					return nil, fmt.Errorf("area trigger row %d column %d: %w", i+1, col, err)
				}
				content = binary.LittleEndian.AppendUint32(content, math.Float32bits(float32(v)))
			default:
				bits := 16
				if col == 4 {
					bits = 32
				}
				if col == 6 || col == 14 || col == 17 {
					bits = 8
				}
				v, err := strconv.ParseUint(row[col], 10, bits)
				if err != nil {
					return nil, fmt.Errorf("area trigger row %d column %d: %w", i+1, col, err)
				}
				switch bits {
				case 32:
					recordID = uint32(v)
					content = binary.LittleEndian.AppendUint32(content, recordID)
				case 16:
					content = binary.LittleEndian.AppendUint16(content, uint16(v))
				case 8:
					content = append(content, byte(v))
				}
			}
		}
		pushID := uint32(100001 + i)
		records = append(records, HotfixRecord{PushID: pushID, UniqueID: pushID, TableHash: areaTriggerTableHash, RecordID: recordID, Content: content})
	}
	return records, nil
}
