package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestQuestInteractionRequests(t *testing.T) {
	giver := GUID128{Low: 0x42, High: 0x2000000000000001}
	body := appendPackedGUID128(nil, giver.Low, giver.High)
	body = binary.LittleEndian.AppendUint32(body, 1234)
	bits := newBitWriter(body)
	bits.writeBit(true)
	complete, err := ParseQuestGiverCompleteQuest(bits.flush())
	if err != nil || complete.Giver != giver || complete.QuestID != 1234 || !complete.FromScript {
		t.Fatalf("complete=%+v err=%v", complete, err)
	}

	rewardBody := appendPackedGUID128(nil, giver.Low, giver.High)
	rewardBody = binary.LittleEndian.AppendUint32(rewardBody, 1234)
	bits = newBitWriter(rewardBody)
	bits.writeBits(0, 2)
	rewardBody = bits.flush()
	rewardBody = appendItemInstance(rewardBody, 5678, 0, 0)
	rewardBody = binary.LittleEndian.AppendUint32(rewardBody, 2)
	reward, err := ParseQuestGiverChooseReward(rewardBody)
	if err != nil || reward.ItemID != 5678 || reward.Quantity != 2 {
		t.Fatalf("reward=%+v err=%v", reward, err)
	}
	choices := [questRewardChoiceCount]uint32{111, 5678}
	choice, err := QuestRewardChoiceIndex(reward.ItemID, choices)
	if err != nil || choice != 1 {
		t.Fatalf("choice=%d err=%v", choice, err)
	}
	legacy := EncodeLegacyQuestChooseReward(0xf130001234000042, 1234, choice)
	if len(legacy) != 16 || binary.LittleEndian.Uint32(legacy[12:]) != 1 {
		t.Fatalf("legacy choice=%x", legacy)
	}
}

func TestQuestGiverHelloQueryAndAcceptRequests(t *testing.T) {
	giver := GUID128{Low: 0x42, High: uint64(8) << 58}
	packed := appendPackedGUID128(nil, giver.Low, giver.High)
	if parsed, err := ParseQuestGiverHello(packed); err != nil || parsed != giver {
		t.Fatalf("hello parsed=%+v err=%v", parsed, err)
	}

	queryBody := append(append([]byte(nil), packed...), 0x34, 0x12, 0, 0)
	bits := newBitWriter(queryBody)
	bits.writeBit(true)
	query, err := ParseQuestGiverQueryQuest(bits.flush())
	if err != nil || query.Giver != giver || query.QuestID != 0x1234 || !query.RespondToGiver {
		t.Fatalf("query parsed=%+v err=%v", query, err)
	}
	legacyQuery := EncodeLegacyQuestGiverQuery(0xf130000001000042, query.QuestID, query.RespondToGiver)
	if len(legacyQuery) != 13 || legacyQuery[12] != 1 {
		t.Fatalf("legacy query=%x", legacyQuery)
	}

	acceptBody := append(append([]byte(nil), packed...), 0x78, 0x56, 0, 0)
	bits = newBitWriter(acceptBody)
	bits.writeBit(false)
	accept, err := ParseQuestGiverAcceptQuest(bits.flush())
	if err != nil || accept.Giver != giver || accept.QuestID != 0x5678 || accept.StartCheat {
		t.Fatalf("accept parsed=%+v err=%v", accept, err)
	}
	legacyAccept := EncodeLegacyQuestGiverAccept(0xf130000001000042, accept.QuestID, accept.StartCheat)
	if len(legacyAccept) != 16 || binary.LittleEndian.Uint32(legacyAccept[12:]) != 0 {
		t.Fatalf("legacy accept=%x", legacyAccept)
	}
}

func TestQuestRequestItemsRoundTrip(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130001234000042)
	legacy = binary.LittleEndian.AppendUint32(legacy, 100)
	legacy = append(legacy, "Test Quest"...)
	legacy = append(legacy, 0)
	legacy = append(legacy, "Bring the ore."...)
	legacy = append(legacy, 0)
	for _, value := range []uint32{10, 20, 1, 0x40, 2, 500, 1, 2770, 3, 12345, 3, 0, 0, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	quest, err := ParseLegacyQuestGiverRequestItems(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if quest.QuestID != 100 || quest.Title != "Test Quest" || len(quest.Collect) != 1 || quest.Collect[0].ItemID != 2770 || quest.StatusFlags != 223 {
		t.Fatalf("quest=%+v", quest)
	}
	giver := ModernGUIDForLegacy(quest.Giver, 0)
	modern := EncodeQuestGiverRequestItems(giver, quest)
	r := movementReader{data: modern}
	gotGUID, err := r.guid128()
	if err != nil || gotGUID != giver {
		t.Fatalf("guid=%+v err=%v", gotGUID, err)
	}
	creatureID, _ := r.u32()
	if creatureID != 0x1234 {
		t.Fatalf("creature id=%x", creatureID)
	}
}

func TestQuestOfferRewardAndComplete(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130001234000042)
	legacy = binary.LittleEndian.AppendUint32(legacy, 200)
	legacy = append(legacy, "Reward Quest"...)
	legacy = append(legacy, 0)
	legacy = append(legacy, "Choose wisely."...)
	legacy = append(legacy, 0, 1, 1) // terminator, auto-launch, QuestFlags low byte
	legacy = append(legacy, 0, 0, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2) // suggested party
	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // emote count
	legacy = binary.LittleEndian.AppendUint32(legacy, 50)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2) // choice count
	for _, value := range []uint32{1001, 1, 0, 1002, 2, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // fixed reward count
	for _, value := range []uint32{2001, 3, 0, 10000, 5000, 20} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1))
	for _, value := range []uint32{0, 123, 0, 9, 2, 0, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	for index := 0; index < questRewardFactionCount; index++ {
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(100+index))
	}
	for index := 0; index < questRewardFactionCount; index++ {
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(10+index))
	}
	for index := 0; index < questRewardFactionCount; index++ {
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(20+index))
	}
	quest, err := ParseLegacyQuestGiverOfferReward(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if quest.ChoiceCount != 2 || quest.ChoiceItems[1].ItemID != 1002 || quest.RewardItems[0].ItemID != 2001 || quest.SpellID != 123 {
		t.Fatalf("quest=%+v", quest)
	}
	if modern := EncodeQuestGiverOfferReward(ModernGUIDForLegacy(quest.Giver, 0), quest); len(modern) < 200 {
		t.Fatalf("modern offer is only %d bytes", len(modern))
	}

	completeBody := make([]byte, 0, 24)
	for _, value := range []uint32{200, 5000, 10000, 20, 0, 0} {
		completeBody = binary.LittleEndian.AppendUint32(completeBody, value)
	}
	complete, err := ParseLegacyQuestGiverQuestComplete(completeBody)
	if err != nil {
		t.Fatal(err)
	}
	modernComplete := EncodeQuestGiverQuestComplete(complete)
	if binary.LittleEndian.Uint32(modernComplete[:4]) != 200 || binary.LittleEndian.Uint64(modernComplete[8:16]) != 10000 {
		t.Fatalf("modern complete=%x", modernComplete)
	}
	if modernComplete[24] != 0 {
		t.Fatalf("modern complete reopens quest interaction: flags=%08b", modernComplete[24])
	}
}

func TestQuestGiverQuestDetailsTranslation(t *testing.T) {
	legacyGiver := uint64(0xf130001234000042)
	legacyInform := uint64(0x0000000000000043)
	legacy := binary.LittleEndian.AppendUint64(nil, legacyGiver)
	legacy = binary.LittleEndian.AppendUint64(legacy, legacyInform)
	legacy = binary.LittleEndian.AppendUint32(legacy, 61)
	legacy = appendCString(legacy, "新的任务")
	legacy = appendCString(legacy, "请帮助我们。")
	legacy = appendCString(legacy, "完成任务目标。")
	legacy = append(legacy, 1) // auto-launched
	legacy = binary.LittleEndian.AppendUint32(legacy, 8)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	legacy = append(legacy, 0) // unused WotLK byte

	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // choice count
	for _, value := range []uint32{1001, 1, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // fixed count
	for _, value := range []uint32{2001, 2, 0, 100, 50, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1))
	// Quest details omits the reward-flags uint32 used by OfferReward.
	for _, value := range []uint32{123, 0, 9, 2, 0, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	for index := range questRewardFactionCount * 3 {
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(index))
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // description emote count
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	legacy = binary.LittleEndian.AppendUint32(legacy, 50)

	quest, err := ParseLegacyQuestGiverQuestDetails(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if quest.QuestID != 61 || quest.Title != "新的任务" || !quest.AutoLaunched || quest.Rewards.ChoiceItems[0].ItemID != 1001 || quest.DescEmotes[0].Type != 7 {
		t.Fatalf("quest details = %+v", quest)
	}
	giver := ModernGUIDForLegacy(legacyGiver, 0)
	inform := ModernGUIDForLegacy(legacyInform, 0)
	modern := EncodeQuestGiverQuestDetails(giver, inform, quest)
	r := movementReader{data: modern}
	gotGiver, _ := r.guid128()
	gotInform, _ := r.guid128()
	questID, _ := r.u32()
	if gotGiver != giver || gotInform != inform || questID != 61 || !bytes.Contains(modern, []byte("新的任务")) || !bytes.Contains(modern, []byte("请帮助我们。")) {
		t.Fatalf("modern quest-details giver=%+v inform=%+v quest=%d bytes=%x", gotGiver, gotInform, questID, modern)
	}
}
