package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"testing"
)

func TestParseQueryQuestInfo(t *testing.T) {
	entry, err := ParseQueryQuestInfo([]byte{0x21, 0x00, 0x00, 0x00})
	if err != nil || entry != 33 {
		t.Fatalf("entry=%d err=%v", entry, err)
	}
	body := binary.LittleEndian.AppendUint32(nil, 33)
	body = appendPackedGUID128(body, 0, 0)
	entry, err = ParseQueryQuestInfo(body)
	if err != nil || entry != 33 {
		t.Fatalf("with GUID entry=%d err=%v", entry, err)
	}
}

func TestQuestCompletionNPCQueryForwardsToLegacy(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 3)
	body = binary.LittleEndian.AppendUint32(body, 33)
	body = binary.LittleEndian.AppendUint32(body, 76)
	body = binary.LittleEndian.AppendUint32(body, 101)
	quests, err := ParseQueryQuestCompletionNPCs(body)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(quests, []uint32{33, 76, 101}) {
		t.Fatalf("quests %v", quests)
	}
	if got := EncodeLegacyQueryQuestsCompleted(quests); !bytes.Equal(got, body) {
		t.Fatalf("forwarded request %x, want %x", got, body)
	}
}

func TestTranslateMissingQuestQuery(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 33|legacyQuestQueryNotFound)
	body, err := TranslateQuestQueryResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 33 || body[4]&0x80 != 0 {
		t.Fatalf("missing quest header %x", body)
	}
}

func TestTranslateQuestQueryTitle(t *testing.T) {
	legacy := buildLegacyQuestQuery(33, "Wolves", "Kill wolves.", "Details", "Area", "Done", 69, 5)
	body, err := TranslateQuestQueryResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 33 || body[4]&0x80 == 0 {
		t.Fatalf("quest header %x", body[:5])
	}
	if got := body[len(body)-len("WolvesKill wolves.DetailsAreaDone"):]; string(got) != "WolvesKill wolves.DetailsAreaDone" {
		t.Fatalf("strings suffix %q", got)
	}
}

func TestQuestObjectiveKeepsTemplateStorageIndex(t *testing.T) {
	legacy := buildLegacyQuestQuery(33, "Wolves", "Kill wolves.", "Details", "Area", "Done", 69, 5)
	r := movementReader{data: legacy}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	info, err := parseLegacyQuestQuery(&r, 33)
	if err != nil || len(info.Objectives) != 1 || info.Objectives[0].ID == 0 || info.Objectives[0].StorageIndex != 0 || info.Objectives[0].ObjectID != 69 {
		t.Fatalf("first-slot objective %#v err=%v", info.Objectives, err)
	}

	legacy = buildLegacyQuestQueryNPCSlot(33, "Wolves", "Kill wolves.", "Details", "Area", "Done", 1, 69, 5, 0, 0)
	r = movementReader{data: legacy}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	info, err = parseLegacyQuestQuery(&r, 33)
	if err != nil || len(info.Objectives) != 1 || info.Objectives[0].ID == 0 || info.Objectives[0].StorageIndex != 1 || info.Objectives[0].ObjectID != 69 {
		t.Fatalf("slot-1 objective %#v err=%v", info.Objectives, err)
	}
}

// Quest 6681 (The Manor, Ravenholdt) completes through areatrigger 3066, whose
// SAI grants the kill credit AzerothCore stores at RequiredNpcOrGo slot 1 while
// slot 0 stays empty. The client reads ObjectiveProgress[StorageIndex], so the
// wire objective must keep slot 1 or the quest log shows 0/1 forever even
// though the server already flagged the quest complete.
func TestQuestObjectiveKeepsSlotForAreatriggerCreditQuest(t *testing.T) {
	legacy := buildLegacyQuestQueryNPCSlot(6681, "拉文霍德庄园", "把拉文霍德的徽记交给拉文霍德庄园的法赫拉德。", "Details", "Area", "Done", 1, 13936, 1, 17125, 1)
	r := movementReader{data: legacy}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	info, err := parseLegacyQuestQuery(&r, 6681)
	if err != nil || len(info.Objectives) != 2 {
		t.Fatalf("objectives %#v err=%v", info.Objectives, err)
	}
	kill := info.Objectives[0]
	if kill.Type != questObjectiveMonster || kill.ObjectID != 13936 || kill.StorageIndex != 1 || kill.Amount != 1 {
		t.Fatalf("kill objective %#v", kill)
	}
	item := info.Objectives[1]
	if item.Type != questObjectiveItem || item.ObjectID != 17125 || item.StorageIndex != int8(questCreatureObjectives) || item.Amount != 1 {
		t.Fatalf("item objective %#v", item)
	}
	if kill.ID == item.ID {
		t.Fatalf("objective IDs collide: %d", kill.ID)
	}
}

func TestQuestQueryKeepsChineseTitle(t *testing.T) {
	legacy := buildLegacyQuestQuery(76, "玉石矿洞", "Explore.", "Details", "Area", "Done", 0, 0)
	r := movementReader{data: legacy}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	info, err := parseLegacyQuestQuery(&r, 76)
	if err != nil || info.LogTitle != "玉石矿洞" {
		t.Fatalf("title %q err=%v", info.LogTitle, err)
	}
	if len(info.Objectives) != 1 || info.Objectives[0].Flags != questObjectiveFlagHidden {
		t.Fatalf("empty non-exploration quest needs a hidden objective: %#v", info.Objectives)
	}
	body, err := TranslateQuestQueryResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	wantText := []byte("玉石矿洞Explore.DetailsAreaDone")
	if !bytes.HasSuffix(body, wantText) {
		t.Fatalf("modern query text suffix %q, want %q", body[len(body)-min(len(body), len(wantText)):], wantText)
	}
}

func TestQuestQueryAddsExplorationObjectiveWithoutChangingText(t *testing.T) {
	legacy := buildLegacyQuestQuery(
		76,
		"玉石矿洞",
		"查看玉石矿洞，然后回到闪金镇去向治安官杜汉报告。",
		"现在我们还需要一个人去更远处的玉石矿洞侦察。",
		"去查看一下玉石矿洞，确定那里是否有狗头人。",
		"侦察玉石矿洞",
		0,
		0,
	)
	// QuestFlags is the second uint32 after RewardKillHonorMultiplier.
	binary.LittleEndian.PutUint32(legacy[80:84], legacyQuestFlagExploration|0x8)

	r := movementReader{data: legacy}
	if _, err := r.u32(); err != nil {
		t.Fatal(err)
	}
	info, err := parseLegacyQuestQuery(&r, 76)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Objectives) != 1 {
		t.Fatalf("exploration objectives = %#v, want one AreaTrigger", info.Objectives)
	}
	objective := info.Objectives[0]
	if objective.ID == 0 || objective.Type != questObjectiveAreaTrigger || objective.StorageIndex != 0 || objective.ObjectID != 0 || objective.Amount != 1 || objective.Description != "" {
		t.Fatalf("exploration objective = %#v", objective)
	}

	body, err := TranslateQuestQueryResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	wantText := []byte(info.LogTitle + info.LogDescription + info.QuestDescription + info.AreaDescription + info.QuestCompletionLog)
	if !bytes.HasSuffix(body, wantText) {
		t.Fatalf("modern exploration text suffix %q, want %q", body[len(body)-min(len(body), len(wantText)):], wantText)
	}
}

func TestMixedExplorationQuestCompletion(t *testing.T) {
	for _, npc := range []uint32{0, 17998} {
		// Drain Schematics: holding item 24330 must not satisfy the separate
		// exploration requirement. Also cover a kill in slot 0 for ID collisions.
		legacy := buildLegacyQuestQueryNPCSlot(9731, "抽水泵结构图", "寻找排水口", "", "", "", 0, npc, 1, 24330, 1)
		binary.LittleEndian.PutUint32(legacy[80:84], 132)
		r := movementReader{data: legacy[4:]}
		info, err := parseLegacyQuestQuery(&r, 9731)
		if err != nil {
			t.Fatal(err)
		}
		wantCount := 2
		if npc != 0 {
			wantCount++
		}
		if len(info.Objectives) != wantCount {
			t.Fatalf("mixed objectives: %+v", info.Objectives)
		}
		ids := map[uint32]bool{}
		for _, objective := range info.Objectives {
			if ids[objective.ID] {
				t.Fatalf("duplicate objective ID: %d", objective.ID)
			}
			ids[objective.ID] = true
		}
		area := info.Objectives[len(info.Objectives)-1]
		if area.Type != questObjectiveAreaTrigger || area.StorageIndex != 0 || area.Amount != 1 || area.Flags != 0 {
			t.Fatalf("missing visible exploration requirement: %+v", area)
		}
		item := info.Objectives[len(info.Objectives)-2]
		if item.Type != questObjectiveItem || item.ObjectID != 24330 || item.Amount != 1 || item.StorageIndex != 4 {
			t.Fatalf("changed item requirement: %+v", item)
		}
		id, hasState, err := HasLegacyQuestStateObjective(legacy)
		if err != nil || id != 9731 || !hasState {
			t.Fatalf("mixed quest not registered for state updates: %d %v %v", id, hasState, err)
		}
		tracked := map[uint32]struct{}{id: {}}
		for _, state := range []uint32{0, legacyQuestStateComplete, 0, 2} {
			wire := appendQuestLogSlot(nil, id, state, 1, 0, 0, tracked)
			flags := binary.LittleEndian.Uint32(wire[12:16])
			if got := flags&modernQuestObjectiveCompleteBit0 != 0; got != (state == legacyQuestStateComplete) {
				t.Fatalf("state %d has incorrect exploration completion: %x", state, flags)
			}
			if binary.LittleEndian.Uint16(wire[16:18]) != 1 {
				t.Fatal("exploration flag overwrote kill progress")
			}
		}
	}
}

func TestHasLegacyQuestStateObjective(t *testing.T) {
	exploration := buildLegacyQuestQuery(
		76,
		"玉石矿洞",
		"查看玉石矿洞，然后回到闪金镇去向治安官杜汉报告。",
		"去更远处的玉石矿洞侦察。",
		"侦察玉石矿洞",
		"向治安官杜汉报告。",
		0,
		0,
	)
	binary.LittleEndian.PutUint32(exploration[80:84], legacyQuestFlagExploration|0x8)
	id, only, err := HasLegacyQuestStateObjective(exploration)
	if err != nil || id != 76 || !only {
		t.Fatalf("exploration-only id=%d only=%v err=%v", id, only, err)
	}

	// Script/dialogue quests use hidden objectives, but must still receive
	// completion flag updates, including the repush after a template query.
	dialogue := buildLegacyQuestQuery(12670, "血色收割", "", "", "", "", 0, 0)
	if id, only, err = HasLegacyQuestStateObjective(dialogue); err != nil || id != 12670 || !only {
		t.Fatalf("dialogue quest id=%d only=%v err=%v", id, only, err)
	}
	horseman := buildLegacyQuestQuery(12687, "进入暗影界", "骑手的挑战", "Details", "Area", "Done", 0, 0)
	if id, only, err = HasLegacyQuestStateObjective(horseman); err != nil || id != 12687 || !only {
		t.Fatalf("horseman quest id=%d only=%v err=%v", id, only, err)
	}
	r := movementReader{data: horseman}
	if _, err = r.u32(); err != nil {
		t.Fatal(err)
	}
	info, err := parseLegacyQuestQuery(&r, 12687)
	if err != nil || len(info.Objectives) != 1 || info.Objectives[0].Flags != questObjectiveFlagHidden {
		t.Fatalf("12687 needs a hidden completion objective: %#v err=%v", info.Objectives, err)
	}

	mixed := buildLegacyQuestQueryNPCSlot(6681, "拉文霍德庄园", "目标", "详情", "区域", "完成", 1, 13936, 1, 17125, 1)
	if _, only, err = HasLegacyQuestStateObjective(mixed); err != nil || only {
		t.Fatalf("slot-credit quest marked area-only (only=%v err=%v)", only, err)
	}
}

func TestQuestQueryChineseStringLengthsMatchClientLayout(t *testing.T) {
	info := questQueryInfo{
		QuestID:            76,
		LogTitle:           "玉石矿洞",
		LogDescription:     "查看玉石矿洞，然后回到闪金镇。",
		QuestDescription:   "去更远处的玉石矿洞侦察。",
		AreaDescription:    "侦察玉石矿洞",
		QuestCompletionLog: "向治安官杜汉报告。",
	}
	body := encodeQuestQueryResponse(info.QuestID, true, info)

	// legacy proxy 54261 starts these nine length fields at byte 477. They occupy
	// 89 MSB-first bits, then ReadyForTranslation, and flush to 12 bytes
	// before the string data.
	const lengthOffset = 477
	reader := questTestBitReader{data: body[lengthOffset : lengthOffset+12]}
	lengths := []int{
		int(reader.readBits(9)),
		int(reader.readBits(12)),
		int(reader.readBits(12)),
		int(reader.readBits(9)),
		int(reader.readBits(10)),
		int(reader.readBits(8)),
		int(reader.readBits(10)),
		int(reader.readBits(8)),
		int(reader.readBits(11)),
	}
	wantLengths := []int{
		len(info.LogTitle),
		len(info.LogDescription),
		len(info.QuestDescription),
		len(info.AreaDescription),
		0, 0, 0, 0,
		len(info.QuestCompletionLog),
	}
	if !slices.Equal(lengths, wantLengths) {
		t.Fatalf("string lengths = %v, want %v", lengths, wantLengths)
	}
	if reader.readBits(1) != 0 {
		t.Fatal("ReadyForTranslation must be 0 to match legacy proxy/Hermes")
	}

	data := body[lengthOffset+12:]
	wantStrings := []string{
		info.LogTitle,
		info.LogDescription,
		info.QuestDescription,
		info.AreaDescription,
		info.QuestCompletionLog,
	}
	for index, want := range wantStrings {
		length := len(want)
		if len(data) < length || string(data[:length]) != want {
			t.Fatalf("string %d = %q, want %q", index, data[:min(len(data), length)], want)
		}
		data = data[length:]
	}
	if len(data) != 0 {
		t.Fatalf("unexpected trailing query text: %x", data)
	}
}

func TestDialogueAndScriptObjectivesCarryHiddenCompletion(t *testing.T) {
	for _, questID := range []uint32{12700, 12670, 12687} {
		legacy := buildLegacyQuestQuery(questID, "交谈任务", "", "", "", "", 0, 0)
		binary.LittleEndian.PutUint32(legacy[80:84], 136)
		r := movementReader{data: legacy[4:]}
		info, err := parseLegacyQuestQuery(&r, questID)
		if err != nil || len(info.Objectives) != 1 {
			t.Fatalf("quest %d: objectives=%v err=%v", questID, info.Objectives, err)
		}
		objective := info.Objectives[0]
		wire := encodeQuestObjective(nil, objective)
		if wire[4] != questObjectiveAreaTrigger || wire[5] != 0 || binary.LittleEndian.Uint32(wire[14:18]) != 8 {
			t.Fatalf("quest %d: hidden flag objective encoding %x", questID, wire)
		}
		stateOnly := map[uint32]struct{}{questID: {}}
		if got := translateQuestLogStateFlags(questID, 0, stateOnly); got != 0 {
			t.Fatalf("quest %d: incomplete became complete: %x", questID, got)
		}
		if got := translateQuestLogStateFlags(questID, legacyQuestStateComplete, stateOnly); got != legacyQuestStateComplete|modernQuestObjectiveCompleteBit0 {
			t.Fatalf("quest %d: missing completion bit: %x", questID, got)
		}
	}
}

type questTestBitReader struct {
	data []byte
	bit  int
}

func (r *questTestBitReader) readBits(count int) uint32 {
	var value uint32
	for range count {
		value <<= 1
		value |= uint32((r.data[r.bit/8] >> (7 - (r.bit % 8))) & 1)
		r.bit++
	}
	return value
}

func TestQuestObjectiveEncodingMatches54261(t *testing.T) {
	objective := questObjective{
		ID:           1234,
		Type:         questObjectiveMonster,
		StorageIndex: 2,
		ObjectID:     69,
		Amount:       5,
		Description:  "Wolves",
	}
	body := encodeQuestObjective(nil, objective)
	if len(body) != 31+len(objective.Description) {
		t.Fatalf("objective size = %d, want %d", len(body), 31+len(objective.Description))
	}
	if body[4] != objective.Type || body[5] != byte(objective.StorageIndex) {
		t.Fatalf("type/storage bytes = %x, want %02x%02x", body[4:6], objective.Type, byte(objective.StorageIndex))
	}
	if got := binary.LittleEndian.Uint32(body[:4]); got != objective.ID {
		t.Fatalf("objective ID = %d, want %d", got, objective.ID)
	}
	if got := int32(binary.LittleEndian.Uint32(body[6:10])); got != objective.ObjectID {
		t.Fatalf("object id = %d, want %d", got, objective.ObjectID)
	}
	if body[30] != byte(len(objective.Description)) || !bytes.Equal(body[31:], []byte(objective.Description)) {
		t.Fatalf("description encoding = %x", body[30:])
	}
}

func TestLegacyQuestGiverStatusQueryIsUnpacked(t *testing.T) {
	guid := uint64(0xf130000001000043)
	got := EncodeLegacyUnpackedGUID(guid)
	if len(got) != 8 || binary.LittleEndian.Uint64(got) != guid {
		t.Fatalf("quest-giver status query must be unpacked uint64, got %x", got)
	}
	packed := EncodeLegacyPackedGUID(guid)
	if len(packed) >= 8 && binary.LittleEndian.Uint64(packed[:8]) == guid {
		t.Fatal("packed GUID accidentally matches unpacked encoding")
	}
}

func TestQuestGiverStatusConversion(t *testing.T) {
	if ConvertQuestGiverStatus343(8) != 1024 {
		t.Fatalf("available status %d", ConvertQuestGiverStatus343(8))
	}
	if ConvertQuestGiverStatus343(9) != 4096 {
		t.Fatalf("reward2 status %d, want legacy proxy expansion-80 value", ConvertQuestGiverStatus343(9))
	}
	if ConvertQuestGiverStatus343(10) != 4096 {
		t.Fatalf("reward status %d must retain its completion POI", ConvertQuestGiverStatus343(10))
	}
	legacy := append(EncodeLegacyUnpackedGUID(0xf130000001000043), 8)
	guid, status, err := TranslateQuestGiverStatus(legacy)
	if err != nil || guid != 0xf130000001000043 || status != 1024 {
		t.Fatalf("guid=%#x status=%d err=%v", guid, status, err)
	}
	modern := EncodeQuestGiverStatus(GUID128{Low: 0x43, High: 1}, status)
	if binary.LittleEndian.Uint64(modern[len(modern)-8:]) != 1024 {
		t.Fatalf("modern status %x", modern)
	}
}

func TestQuestGiverStatusMultipleConversion(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 2)
	legacy = append(legacy, EncodeLegacyUnpackedGUID(0xf130000001000043)...)
	legacy = append(legacy, 8)
	legacy = append(legacy, EncodeLegacyUnpackedGUID(0xf130000002000044)...)
	legacy = append(legacy, 10)

	guids, statuses, err := TranslateQuestGiverStatusMultiple(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(guids, []uint64{0xf130000001000043, 0xf130000002000044}) {
		t.Fatalf("guids=%#x", guids)
	}
	if !slices.Equal(statuses, []uint64{1024, 4096}) {
		t.Fatalf("statuses=%v", statuses)
	}
}

func TestQuestGiverStatusMultipleEmpty(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 0)
	guids, statuses, err := TranslateQuestGiverStatusMultiple(legacy)
	if err != nil || len(guids) != 0 || len(statuses) != 0 {
		t.Fatalf("empty multiple guids=%v statuses=%v err=%v", guids, statuses, err)
	}
	body := EncodeQuestGiverStatusMultiple(nil, nil)
	if binary.LittleEndian.Uint32(body) != 0 || len(body) != 4 {
		t.Fatalf("empty multiple body %x", body)
	}
}

func TestQuestUpdateCompleteAndAddKill(t *testing.T) {
	questID, err := ParseLegacyQuestUpdateID([]byte{0x0d, 0x65, 0x00, 0x00})
	if err != nil || questID != 25869 {
		t.Fatalf("complete id=%d err=%v", questID, err)
	}
	if got := EncodeQuestUpdateID(25869); binary.LittleEndian.Uint32(got) != 25869 || SMSGQuestUpdateComplete != 10889 {
		t.Fatalf("complete encode %x opcode=%d", got, SMSGQuestUpdateComplete)
	}

	legacy := binary.LittleEndian.AppendUint32(nil, 12416)
	legacy = binary.LittleEndian.AppendUint32(legacy, 27685)
	legacy = binary.LittleEndian.AppendUint32(legacy, 6)
	legacy = binary.LittleEndian.AppendUint32(legacy, 12)
	legacy = append(legacy, EncodeLegacyPackedGUID(0xf13006c5000000ab)...)
	credit, err := ParseLegacyQuestUpdateAddKill(legacy)
	if err != nil || credit.QuestID != 12416 || credit.ObjectID != 27685 || credit.Count != 6 || credit.Required != 12 || credit.ObjectiveType != questObjectiveMonster {
		t.Fatalf("add-kill %#v err=%v", credit, err)
	}
	if credit.Victim != 0xf13006c5000000ab {
		t.Fatalf("victim %#x", credit.Victim)
	}
	body := EncodeQuestUpdateAddCredit(GUID128{Low: 0xab, High: 1}, credit)
	if len(body) < 13 {
		t.Fatalf("add-credit too short %x", body)
	}
	tail := body[len(body)-13:]
	if binary.LittleEndian.Uint32(tail[:4]) != 12416 || binary.LittleEndian.Uint32(tail[4:8]) != 27685 {
		t.Fatalf("add-credit quest/object %x", tail)
	}
	if binary.LittleEndian.Uint16(tail[8:10]) != 6 || binary.LittleEndian.Uint16(tail[10:12]) != 12 || tail[12] != questObjectiveMonster {
		t.Fatalf("add-credit counts %x", tail)
	}

	goEntry := binary.LittleEndian.AppendUint32(nil, 26013)
	goEntry = binary.LittleEndian.AppendUint32(goEntry, 193195|legacyQuestUpdateGOFlag)
	goEntry = binary.LittleEndian.AppendUint32(goEntry, 1)
	goEntry = binary.LittleEndian.AppendUint32(goEntry, 1)
	goCredit, err := ParseLegacyQuestUpdateAddKill(goEntry)
	if err != nil || goCredit.ObjectID != 193195 || goCredit.ObjectiveType != questObjectiveGameObject {
		t.Fatalf("go credit %#v err=%v", goCredit, err)
	}
}

func TestQuestFailureNotifications(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 12416)
	legacy = binary.LittleEndian.AppendUint32(legacy, 50)
	questID, reason, err := ParseLegacyQuestGiverQuestFailed(legacy)
	if err != nil || questID != 12416 || reason != 50 {
		t.Fatalf("quest=%d reason=%d err=%v", questID, reason, err)
	}
	if SMSGQuestGiverInvalidQuest != 10885 || SMSGQuestGiverQuestFailed != 10886 || SMSGQuestLogFull != 10887 {
		t.Fatalf("quest failure opcodes invalid=%d failed=%d log-full=%d", SMSGQuestGiverInvalidQuest, SMSGQuestGiverQuestFailed, SMSGQuestLogFull)
	}
	body := EncodeQuestGiverQuestFailed(questID, reason)
	if binary.LittleEndian.Uint32(body[:4]) != questID || binary.LittleEndian.Uint32(body[4:]) != 51 {
		t.Fatalf("quest-giver-failed body=%x", body)
	}
	for _, invalid := range [][]byte{legacy[:7], append(append([]byte(nil), legacy...), 0)} {
		if _, _, err := ParseLegacyQuestGiverQuestFailed(invalid); err == nil {
			t.Fatalf("invalid quest-giver-failed body accepted: %x", invalid)
		}
	}
	if err := ParseLegacyQuestLogFull(nil); err != nil {
		t.Fatalf("empty quest-log-full rejected: %v", err)
	}
	if err := ParseLegacyQuestLogFull([]byte{0}); err == nil {
		t.Fatal("non-empty quest-log-full accepted")
	}
}

func TestQuestShareOpcodes(t *testing.T) {
	if CMSGQuestConfirmAccept != 0x349E || CMSGQuestPushResult != 0x34A0 {
		t.Fatalf("quest-share CMSG opcodes confirm=%d push=%d", CMSGQuestConfirmAccept, CMSGQuestPushResult)
	}
	if SMSGQuestConfirmAccept != 10895 || SMSGQuestPushResult != 10896 || SMSGQuestGiverInvalidQuest != 10885 {
		t.Fatalf("quest-share SMSG opcodes confirm=%d push=%d invalid=%d", SMSGQuestConfirmAccept, SMSGQuestPushResult, SMSGQuestGiverInvalidQuest)
	}
}

func TestParseQuestConfirmAccept(t *testing.T) {
	questID, err := ParseQuestConfirmAccept(EncodeLegacyQuestID(12416))
	if err != nil || questID != 12416 {
		t.Fatalf("quest=%d err=%v", questID, err)
	}
	for _, invalid := range [][]byte{nil, {1, 2, 3}, {1, 2, 3, 4, 5}} {
		if _, err := ParseQuestConfirmAccept(invalid); err == nil {
			t.Fatalf("invalid quest-confirm-accept body accepted: %x", invalid)
		}
	}
}

func TestQuestPushReasonMapping(t *testing.T) {
	if questPushTooFar != 4 || questPushBusy != 5 || questPushDead != 6 ||
		questPushLogFull != 7 || questPushOnQuest != 8 || questPushAlreadyDone != 9 ||
		questPushNotDaily != 10 || questPushTimerExpired != 11 || questPushNotInParty != 12 {
		t.Fatalf("54261 quest-push wires drifted: tooFar=%d busy=%d dead=%d logFull=%d onQuest=%d done=%d notDaily=%d timer=%d party=%d",
			questPushTooFar, questPushBusy, questPushDead, questPushLogFull, questPushOnQuest,
			questPushAlreadyDone, questPushNotDaily, questPushTimerExpired, questPushNotInParty)
	}
	for _, tc := range []struct {
		legacy uint8
		modern uint8
	}{
		{questShareSharing, questPushSuccess},
		{questShareInvalid, questPushInvalid},
		{questShareAccepted, questPushAccepted},
		{questShareDeclined, questPushDeclined},
		{questShareBusy, questPushBusy},
		{questShareLogFull, questPushLogFull},
		{questShareHaveQuest, questPushOnQuest},
		{questShareFinish, questPushAlreadyDone},
		{questShareNotDaily, questPushNotDaily},
		{questShareTimer, questPushTimerExpired},
		{questShareNotInParty, questPushNotInParty},
	} {
		if got := ModernQuestPushReason(tc.legacy); got != tc.modern {
			t.Fatalf("legacy %d mapped to modern %d, want %d", tc.legacy, got, tc.modern)
		}
		if got := LegacyQuestPushReason(tc.modern); got != tc.legacy {
			t.Fatalf("modern %d mapped to legacy %d, want %d", tc.modern, got, tc.legacy)
		}
	}
	if got := ModernQuestPushReason(questShareHaveQuest); got != questPushOnQuest || got == questPushDead || got == questPushLogFull || got == questPushNotDaily || got == questPushTimerExpired {
		t.Fatalf("HaveQuest mapped to %d, want OnQuest=%d", got, questPushOnQuest)
	}
	if got := ModernQuestPushReason(questShareBusy); got != 5 || got == questPushTooFar {
		t.Fatalf("Busy mapped to %d, want 5", got)
	}
	if got := ModernQuestPushReason(questShareLogFull); got != 7 || got == questPushDead {
		t.Fatalf("LogFull mapped to %d, want 7", got)
	}
	if got := ModernQuestPushReason(questShareAccepted); got != questPushAccepted || got == questPushDeclined {
		t.Fatalf("Accept mapped to %d, want Accepted=%d not Declined=%d", got, questPushAccepted, questPushDeclined)
	}
	acceptBody := EncodeQuestPushResult(GUID128{Low: 0x43, High: 1}, questShareAccepted)
	if acceptBody[len(acceptBody)-1] != questPushAccepted {
		t.Fatalf("S2C accept byte %d, want %d", acceptBody[len(acceptBody)-1], questPushAccepted)
	}
	if got := LegacyQuestPushReason(questPushDead); got != questShareBusy {
		t.Fatalf("modern Dead=%d mapped to %d, want Busy=%d", questPushDead, got, questShareBusy)
	}
	if got := LegacyQuestPushReason(questPushTooFar); got != questShareBusy {
		t.Fatalf("modern TooFar=%d mapped to %d, want Busy=%d", questPushTooFar, got, questShareBusy)
	}
	if got := ModernQuestPushReason(11); got != questPushBusy {
		t.Fatalf("unknown legacy reason mapped to %d, want Busy", got)
	}
}

func TestQuestPushResultKeepsQuestID(t *testing.T) {
	sender := GUID128{Low: 0x43, High: 1}
	modern := appendPackedGUID128(nil, sender.Low, sender.High)
	modern = binary.LittleEndian.AppendUint32(modern, 12416)
	modern = append(modern, questPushOnQuest)
	request, err := ParseQuestPushResult(modern)
	if err != nil || request.Sender != sender || request.QuestID != 12416 || request.Result != questPushOnQuest {
		t.Fatalf("request %#v err=%v", request, err)
	}
	legacy := EncodeLegacyQuestPushResult(0x43, request.QuestID, request.Result)
	if len(legacy) != 13 {
		t.Fatalf("legacy quest-push-result has %d bytes, want 13", len(legacy))
	}
	if binary.LittleEndian.Uint64(legacy[:8]) != 0x43 {
		t.Fatalf("legacy sender %#x", legacy[:8])
	}
	if binary.LittleEndian.Uint32(legacy[8:12]) != 12416 {
		t.Fatalf("QuestID dropped: %x", legacy)
	}
	if legacy[12] != questShareHaveQuest {
		t.Fatalf("legacy result %d, want HaveQuest=%d", legacy[12], questShareHaveQuest)
	}
	for _, invalid := range [][]byte{modern[:len(modern)-1], append(append([]byte(nil), modern...), 0)} {
		if _, err := ParseQuestPushResult(invalid); err == nil {
			t.Fatalf("invalid quest-push-result body accepted: %x", invalid)
		}
	}
}

func TestQuestPushResultS2CHasNoTitle(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0x43)
	legacy = append(legacy, questShareLogFull)
	sender, result, err := ParseLegacyQuestPushResult(legacy)
	if err != nil || sender != 0x43 || result != questShareLogFull {
		t.Fatalf("sender=%#x result=%d err=%v", sender, result, err)
	}
	guid := GUID128{Low: 0x43, High: 1}
	body := EncodeQuestPushResult(guid, result)
	packed := appendPackedGUID128(nil, guid.Low, guid.High)
	if len(body) != len(packed)+1 {
		t.Fatalf("54261 quest-push-result has %d bytes, want packed GUID plus Result only: %x", len(body), body)
	}
	if !bytes.Equal(body[:len(packed)], packed) || body[len(packed)] != questPushLogFull {
		t.Fatalf("quest-push-result body %x, want LogFull=%d", body, questPushLogFull)
	}

	haveQuest := binary.LittleEndian.AppendUint64(nil, 0x43)
	haveQuest = append(haveQuest, questShareHaveQuest)
	_, haveResult, err := ParseLegacyQuestPushResult(haveQuest)
	if err != nil || haveResult != questShareHaveQuest {
		t.Fatalf("have-quest result=%d err=%v", haveResult, err)
	}
	haveBody := EncodeQuestPushResult(guid, haveResult)
	if haveBody[len(packed)] != questPushOnQuest {
		t.Fatalf("already-on-quest encoded as %d, want OnQuest=%d (not LogFull=%d)", haveBody[len(packed)], questPushOnQuest, questPushLogFull)
	}

	wide := binary.LittleEndian.AppendUint64(nil, 0x43)
	wide = binary.LittleEndian.AppendUint32(wide, uint32(questShareHaveQuest))
	_, wideResult, err := ParseLegacyQuestPushResult(wide)
	if err != nil || wideResult != questShareHaveQuest {
		t.Fatalf("12-byte have-quest result=%d err=%v", wideResult, err)
	}

	for _, invalid := range [][]byte{legacy[:8], append(append([]byte(nil), legacy...), 0), append(legacy[:8], 0, 0, 0, 0, 5)} {
		if _, _, err := ParseLegacyQuestPushResult(invalid); err == nil {
			t.Fatalf("invalid legacy quest-push-result body accepted: %x", invalid)
		}
	}
}

func TestQuestConfirmAcceptFieldOrder(t *testing.T) {
	const title = "Escort the ambassador"
	legacy := binary.LittleEndian.AppendUint32(nil, 12416)
	legacy = append(legacy, title...)
	legacy = append(legacy, 0)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x43)
	questID, gotTitle, initiator, err := ParseLegacyQuestConfirmAccept(legacy)
	if err != nil || questID != 12416 || gotTitle != title || initiator != 0x43 {
		t.Fatalf("quest=%d title=%q initiator=%#x err=%v", questID, gotTitle, initiator, err)
	}
	guid := GUID128{Low: 0x43, High: 1}
	body := EncodeQuestConfirmAccept(questID, guid, gotTitle)
	if binary.LittleEndian.Uint32(body[:4]) != 12416 {
		t.Fatalf("modern quest-confirm-accept ID %x", body[:4])
	}
	if body[4] == 'E' {
		t.Fatalf("modern packet still uses ID→Title→GUID order: %x", body)
	}
	low, high, consumed, err := readPackedGUID128(body[4:])
	if err != nil || low != guid.Low || high != guid.High {
		t.Fatalf("modern initiator %#x/%#x err=%v", low, high, err)
	}
	r := movementReader{data: body[4+consumed:]}
	length, err := r.bits(10)
	if err != nil || int(length) != len(title) {
		t.Fatalf("title length %d err=%v", length, err)
	}
	r.align()
	got, err := r.stringN(int(length))
	if err != nil || got != title || r.remaining() != 0 {
		t.Fatalf("title %q remaining=%d err=%v", got, r.remaining(), err)
	}
	for _, invalid := range [][]byte{legacy[:4], legacy[:len(legacy)-1], append(append([]byte(nil), legacy...), 0)} {
		if _, _, _, err := ParseLegacyQuestConfirmAccept(invalid); err == nil {
			t.Fatalf("invalid quest-confirm-accept body accepted: %x", invalid)
		}
	}
}

func TestQuestGiverInvalidQuestPassesReason(t *testing.T) {
	const reason = uint32(19)
	got, err := ParseLegacyQuestGiverInvalidQuest(binary.LittleEndian.AppendUint32(nil, reason))
	if err != nil || got != reason {
		t.Fatalf("reason=%d err=%v", got, err)
	}
	body := EncodeQuestGiverInvalidQuest(got)
	if len(body) != 10 {
		t.Fatalf("invalid-quest body has %d bytes, want 10: %x", len(body), body)
	}
	if binary.LittleEndian.Uint32(body[:4]) != reason {
		t.Fatalf("reason remapped: %x", body[:4])
	}
	if binary.LittleEndian.Uint32(body[4:8]) != 0 {
		t.Fatalf("ContributionRewardID %x", body[4:8])
	}
	if body[8] != 0x80 || body[9] != 0 {
		t.Fatalf("SendErrorMessage/ReasonText bits %x", body[8:])
	}
	for _, invalid := range [][]byte{{1, 2, 3}, {1, 2, 3, 4, 5}} {
		if _, err := ParseLegacyQuestGiverInvalidQuest(invalid); err == nil {
			t.Fatalf("invalid quest-giver-invalid body accepted: %x", invalid)
		}
	}
}

func TestTranslateWeatherAndProficiency(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 1)
	legacy = appendFloat32(legacy, 0.5)
	legacy = append(legacy, 1)
	body, err := TranslateWeather(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 1 {
		t.Fatalf("weather id %x", body[:4])
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(body[4:8])) != 0.5 {
		t.Fatalf("weather intensity %x", body[4:8])
	}
	if body[8]&0x80 == 0 {
		t.Fatalf("weather abrupt bit missing %x", body)
	}

	prof, err := TranslateSetProficiency([]byte{2, 0xff, 0x00, 0x00, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(prof[:4]) != 0xff || prof[4] != 2 {
		t.Fatalf("proficiency %x", prof)
	}
}

func buildLegacyQuestQuery(id uint32, title, objectives, details, area, completed string, npcID, npcCount uint32) []byte {
	return buildLegacyQuestQueryNPCSlot(id, title, objectives, details, area, completed, 0, npcID, npcCount, 0, 0)
}

func TestQuestItemObjectiveProgressHelpers(t *testing.T) {
	legacy := buildLegacyQuestQueryItem(39, 1234, 6)
	items, err := ParseLegacyQuestItemObjectives(legacy)
	if err != nil || len(items) != 1 || items[0] != (QuestItemObjectiveInfo{QuestID: 39, ItemID: 1234, Required: 6}) {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	quests := ActiveQuestIDsFromLegacyFields(map[int]uint32{legacyPlayerQuestLog1: 39})
	if _, ok := quests[39]; !ok {
		t.Fatalf("active quests=%v", quests)
	}
	entry, count := LegacyItemEntryAndCount(map[int]uint32{legacyObjectEntry: 1234, legacyItemStackCount: 2})
	if entry != 1234 || count != 2 {
		t.Fatalf("item=%d count=%d", entry, count)
	}
	body := EncodeQuestItemProgress(39, 1234, 2, 6)
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil || len(body) < consumed+13 || binary.LittleEndian.Uint32(body[consumed:]) != 39 || body[len(body)-1] != questObjectiveItem {
		t.Fatalf("progress body=%x", body)
	}
}

func buildLegacyQuestQueryItem(id, itemID, itemCount uint32) []byte {
	legacy := binary.LittleEndian.AppendUint32(nil, id)
	for range 17 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = appendFloat32(legacy, 0)
	for range 7 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	for range 4 + 4 + 6 + 6 + 5 + 5 + 5 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = appendFloat32(legacy, 0)
	legacy = appendFloat32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	for _, value := range []string{"Quest", "Collect", "Details", "Area", "Done"} {
		legacy = appendCString(legacy, value)
	}
	for range 4 * 4 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, itemID)
	legacy = binary.LittleEndian.AppendUint32(legacy, itemCount)
	for range 5 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	for range 4 {
		legacy = appendCString(legacy, "")
	}
	return legacy
}

func buildLegacyQuestQueryNPCSlot(id uint32, title, objectives, details, area, completed string, slot int, npcID, npcCount uint32, itemID, itemCount uint32) []byte {
	legacy := binary.LittleEndian.AppendUint32(nil, id)
	for range 17 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = appendFloat32(legacy, 0)
	for range 7 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	for range 4 + 4 + 6 + 6 + 5 + 5 + 5 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = appendFloat32(legacy, 0)
	legacy = appendFloat32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = appendCString(legacy, title)
	legacy = appendCString(legacy, objectives)
	legacy = appendCString(legacy, details)
	legacy = appendCString(legacy, area)
	legacy = appendCString(legacy, completed)
	for index := 0; index < 4; index++ {
		if index == slot {
			legacy = binary.LittleEndian.AppendUint32(legacy, npcID)
			legacy = binary.LittleEndian.AppendUint32(legacy, npcCount)
		} else {
			legacy = binary.LittleEndian.AppendUint32(legacy, 0)
			legacy = binary.LittleEndian.AppendUint32(legacy, 0)
		}
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	for index := range 6 {
		if index == 0 {
			legacy = binary.LittleEndian.AppendUint32(legacy, itemID)
			legacy = binary.LittleEndian.AppendUint32(legacy, itemCount)
		} else {
			legacy = binary.LittleEndian.AppendUint32(legacy, 0)
			legacy = binary.LittleEndian.AppendUint32(legacy, 0)
		}
	}
	legacy = appendCString(legacy, "Wolves slain")
	for range 3 {
		legacy = append(legacy, 0)
	}
	return legacy
}
