package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	maxQuestPOIQueries = 175
	maxLegacyQuestPOIs = 25
	maxQuestPOIBlobs   = 64
	maxQuestPOIPoints  = 4096
)

type QuestPOIPoint struct {
	X int16
	Y int16
	Z int16
}

type QuestPOIBlob struct {
	BlobIndex      int32
	ObjectiveIndex int32
	MapID          int32
	UIMapID        int32
	Flags          int32
	Points         []QuestPOIPoint
}

type QuestPOIData struct {
	QuestID int32
	Blobs   []QuestPOIBlob
}

func ParseQuestPOIQuery(body []byte) ([]uint32, error) {
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read quest-POI count: %w", err)
	}
	if count > maxQuestPOIQueries {
		return nil, fmt.Errorf("quest-POI count %d exceeds %d", count, maxQuestPOIQueries)
	}
	quests := make([]uint32, 0, count)
	for index := uint32(0); index < count; index++ {
		questID, readErr := r.u32()
		if readErr != nil {
			return nil, fmt.Errorf("read quest-POI quest %d: %w", index, readErr)
		}
		quests = append(quests, questID)
	}
	// The 3.4.3 client sends the active count followed by a fixed-width quest-log
	// array. legacy proxy consumes only the active IDs and ignores the unused slots.
	if r.remaining()%4 != 0 {
		return nil, fmt.Errorf("quest-POI query has %d trailing bytes", r.remaining())
	}
	if _, err := r.take(r.remaining()); err != nil {
		return nil, fmt.Errorf("skip unused quest POI slots: %w", err)
	}
	return quests, nil
}

func EncodeLegacyQuestPOIQuery(quests []uint32) []byte {
	if len(quests) > maxLegacyQuestPOIs {
		quests = quests[:maxLegacyQuestPOIs]
	}
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(quests)))
	for _, questID := range quests {
		body = binary.LittleEndian.AppendUint32(body, questID)
	}
	return body
}

func ParseLegacyQuestPOIResponse(body []byte) ([]QuestPOIData, error) {
	r := movementReader{data: body}
	questCount, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read quest-POI response count: %w", err)
	}
	if questCount > maxLegacyQuestPOIs {
		return nil, fmt.Errorf("quest-POI response count %d exceeds %d", questCount, maxLegacyQuestPOIs)
	}
	quests := make([]QuestPOIData, 0, questCount)
	for questIndex := uint32(0); questIndex < questCount; questIndex++ {
		var quest QuestPOIData
		if quest.QuestID, err = r.i32(); err != nil {
			return nil, fmt.Errorf("read quest-POI quest %d ID: %w", questIndex, err)
		}
		blobCount, readErr := r.u32()
		if readErr != nil {
			return nil, fmt.Errorf("read quest-POI quest %d blob count: %w", questIndex, readErr)
		}
		if blobCount > maxQuestPOIBlobs {
			return nil, fmt.Errorf("quest-POI blob count %d exceeds %d", blobCount, maxQuestPOIBlobs)
		}
		quest.Blobs = make([]QuestPOIBlob, 0, blobCount)
		for blobIndex := uint32(0); blobIndex < blobCount; blobIndex++ {
			var blob QuestPOIBlob
			if blob.BlobIndex, err = r.i32(); err != nil {
				return nil, fmt.Errorf("read quest-POI blob %d index: %w", blobIndex, err)
			}
			if blob.ObjectiveIndex, err = r.i32(); err != nil {
				return nil, fmt.Errorf("read quest-POI blob %d objective: %w", blobIndex, err)
			}
			if blob.MapID, err = r.i32(); err != nil {
				return nil, fmt.Errorf("read quest-POI blob %d map: %w", blobIndex, err)
			}
			if blob.UIMapID, err = r.i32(); err != nil {
				return nil, fmt.Errorf("read quest-POI blob %d area: %w", blobIndex, err)
			}
			if blob.Flags, err = r.i32(); err != nil {
				return nil, fmt.Errorf("read quest-POI blob %d floor: %w", blobIndex, err)
			}
			if _, err = r.u32(); err != nil {
				return nil, fmt.Errorf("read quest-POI blob %d unknown 3: %w", blobIndex, err)
			}
			if _, err = r.u32(); err != nil {
				return nil, fmt.Errorf("read quest-POI blob %d unknown 4: %w", blobIndex, err)
			}
			pointCount, readErr := r.u32()
			if readErr != nil {
				return nil, fmt.Errorf("read quest-POI blob %d point count: %w", blobIndex, readErr)
			}
			if pointCount > maxQuestPOIPoints {
				return nil, fmt.Errorf("quest-POI point count %d exceeds %d", pointCount, maxQuestPOIPoints)
			}
			blob.Points = make([]QuestPOIPoint, 0, pointCount)
			for pointIndex := uint32(0); pointIndex < pointCount; pointIndex++ {
				x, readErr := r.i32()
				if readErr != nil {
					return nil, fmt.Errorf("read quest-POI point %d x: %w", pointIndex, readErr)
				}
				y, readErr := r.i32()
				if readErr != nil {
					return nil, fmt.Errorf("read quest-POI point %d y: %w", pointIndex, readErr)
				}
				blob.Points = append(blob.Points, QuestPOIPoint{X: int16(x), Y: int16(y)})
			}
			quest.Blobs = append(quest.Blobs, blob)
		}
		quests = append(quests, quest)
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("quest-POI response has %d trailing bytes", r.remaining())
	}
	return quests, nil
}

func EncodeQuestPOIResponse(quests []QuestPOIData) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(quests)))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(quests)))
	for _, quest := range quests {
		body = binary.LittleEndian.AppendUint32(body, uint32(quest.QuestID))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(quest.Blobs)))
		for _, blob := range quest.Blobs {
			objectiveID := uint32(0)
			if blob.ObjectiveIndex >= 0 && blob.ObjectiveIndex < 24 {
				objectiveID = questObjectiveWireID(uint32(quest.QuestID), int8(blob.ObjectiveIndex))
			}
			for _, value := range []uint32{
				uint32(blob.BlobIndex), uint32(blob.ObjectiveIndex), objectiveID, 0,
				uint32(blob.MapID), uint32(blob.UIMapID), 0, uint32(blob.Flags),
				0, 0, 0, 0, uint32(len(blob.Points)),
			} {
				body = binary.LittleEndian.AppendUint32(body, value)
			}
			for _, point := range blob.Points {
				body = binary.LittleEndian.AppendUint16(body, uint16(point.X))
				body = binary.LittleEndian.AppendUint16(body, uint16(point.Y))
				body = binary.LittleEndian.AppendUint16(body, uint16(point.Z))
			}
			bits := newBitWriter(body)
			bits.writeBit(false) // AlwaysAllowMergingBlobs
			body = bits.flush()
		}
	}
	return body
}
