package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestQuestPOITranslation(t *testing.T) {
	query := binary.LittleEndian.AppendUint32(nil, 2)
	query = binary.LittleEndian.AppendUint32(query, 37)
	query = binary.LittleEndian.AppendUint32(query, 52)
	quests, err := ParseQuestPOIQuery(query)
	if err != nil || len(quests) != 2 || quests[1] != 52 {
		t.Fatalf("quests=%v err=%v", quests, err)
	}
	if legacy := EncodeLegacyQuestPOIQuery(quests); len(legacy) != 12 || binary.LittleEndian.Uint32(legacy[8:]) != 52 {
		t.Fatalf("legacy query=%x", legacy)
	}
	paddedQuery := binary.LittleEndian.AppendUint32(nil, 2)
	paddedQuery = binary.LittleEndian.AppendUint32(paddedQuery, 37)
	paddedQuery = binary.LittleEndian.AppendUint32(paddedQuery, 52)
	for len(paddedQuery) < 4+maxLegacyQuestPOIs*4 {
		paddedQuery = binary.LittleEndian.AppendUint32(paddedQuery, 0)
	}
	paddedQuests, err := ParseQuestPOIQuery(paddedQuery)
	if err != nil || len(paddedQuests) != 2 || paddedQuests[1] != 52 {
		t.Fatalf("padded quests=%v err=%v", paddedQuests, err)
	}
	if _, err := ParseQuestPOIQuery(append(paddedQuery, 0)); err == nil {
		t.Fatal("expected unaligned quest-POI padding error")
	}

	legacy := binary.LittleEndian.AppendUint32(nil, 1)
	for _, value := range []uint32{52, 1, 3, 1, 0, 12, 0, 0, 0, 2, 100, 200, 300, 400} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	response, err := ParseLegacyQuestPOIResponse(legacy)
	if err != nil || len(response) != 1 || len(response[0].Blobs) != 1 || len(response[0].Blobs[0].Points) != 2 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	modern := EncodeQuestPOIResponse(response)
	if binary.LittleEndian.Uint32(modern[:4]) != 1 || binary.LittleEndian.Uint32(modern[4:8]) != 1 {
		t.Fatalf("modern response=%x", modern)
	}
	objectiveID := binary.LittleEndian.Uint32(modern[24:28])
	if objectiveID != questObjectiveWireID(52, 1) || objectiveID == 0 {
		t.Fatalf("objective ID=%d body=%x", objectiveID, modern)
	}
}
