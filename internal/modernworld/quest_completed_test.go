package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestParseAndMarkCompletedQuests(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 4)
	// QuestV2 maps these quest IDs to UniqueBitFlag 1, 64, 65 and 54391.
	for _, questID := range []uint32{39, 98, 99, 78753} {
		body = binary.LittleEndian.AppendUint32(body, questID)
	}
	quests, err := ParseLegacyQueryQuestsCompletedResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	blocks := QuestCompletedBlocks(quests)
	if blocks[0] != uint64(1)|uint64(1)<<63 || blocks[1] != 1 || blocks[849] != uint64(1)<<54 {
		t.Fatalf("completed blocks first=0x%x second=0x%x high=0x%x", blocks[0], blocks[1], blocks[849])
	}
	if index, _, changed := MarkQuestCompleted(blocks, ^uint32(0)); index != -1 || changed {
		t.Fatalf("unknown quest changed block %d", index)
	}
	if _, err := ParseLegacyQueryQuestsCompletedResponse(body[:len(body)-1]); err == nil {
		t.Fatal("truncated completed-quest response was accepted")
	}
}

func TestQuestCompletedUses54261UniqueBitFlag(t *testing.T) {
	blocks := QuestCompletedBlocks([]uint32{1})
	// Quest ID 1 has UniqueBitFlag 46698 in build 54261. Direct quest-ID
	// indexing would incorrectly set block 0 instead of block 729.
	if blocks[0] != 0 || blocks[729] != uint64(1)<<41 {
		t.Fatalf("quest 1 blocks block0=0x%x block729=0x%x", blocks[0], blocks[729])
	}
}

func TestEncodeQuestCompletedActivePlayerDelta(t *testing.T) {
	player := ModernGUIDForLegacy(0x42, 571)
	body, err := EncodeQuestCompletedUpdate(571, player, map[int]uint64{
		0:   2,
		874: uint64(1) << 63,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := valuesUpdatePayload(t, body)
	if got := binary.LittleEndian.Uint32(values); got != 0x80 {
		t.Fatalf("values changed mask = 0x%x, want ActivePlayerData", got)
	}
	r := movementReader{data: values[8:]}
	mask1, _ := r.bits(16)
	block19, _ := r.bits(32)
	block47, _ := r.bits(32)
	r.align()
	if mask0 := binary.LittleEndian.Uint32(values[4:]); mask0 != 1<<19 {
		t.Fatalf("ActivePlayer mask0 = 0x%x, want block 19", mask0)
	}
	if mask1 != 1<<15 || block19 != 0x30000000 || block47 != 0x80 {
		t.Fatalf("ActivePlayer masks mask1=0x%x block19=0x%x block47=0x%x", mask1, block19, block47)
	}
	first, err := r.u64()
	if err != nil {
		t.Fatal(err)
	}
	last, err := r.u64()
	if err != nil {
		t.Fatal(err)
	}
	if first != 2 || last != uint64(1)<<63 || r.remaining() != 0 {
		t.Fatalf("quest values first=0x%x last=0x%x remaining=%d", first, last, r.remaining())
	}
}

func TestActivePlayerCreateIncludesCompletedQuestBlocks(t *testing.T) {
	values := legacyActivePlayerValues{fields: map[int]uint32{}}
	empty := values.appendActivePlayer(nil, 571, nil)
	blocks := make([]uint64, QuestCompletedBlockCount)
	blocks[0] = 2
	blocks[874] = uint64(1) << 63
	completed := values.appendActivePlayer(nil, 571, blocks)
	if len(empty) != len(completed) {
		t.Fatalf("ActivePlayer create length changed from %d to %d", len(empty), len(completed))
	}
	differences := 0
	for index := range empty {
		if empty[index] != completed[index] {
			differences++
		}
	}
	if differences != 2 {
		t.Fatalf("completed quest blocks changed %d bytes, want 2", differences)
	}
}
