package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestGossipSelectOptionRoundTrip(t *testing.T) {
	giver := GUID128{Low: 0x1234, High: uint64(8) << 58}
	body := appendPackedGUID128(nil, giver.Low, giver.High)
	body = binary.LittleEndian.AppendUint32(body, 77)
	body = binary.LittleEndian.AppendUint32(body, 3)
	bits := newBitWriter(body)
	bits.writeBits(4, 8)
	body = append(bits.flush(), "code"...)

	request, err := ParseGossipSelectOption(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Giver != giver || request.GossipID != 77 || request.OptionIndex != 3 || request.PromotionCode != "code" {
		t.Fatalf("parsed gossip select = %+v", request)
	}
	legacy := EncodeLegacyGossipSelectOption(0xf130000001000042, request.GossipID, request.OptionIndex, request.PromotionCode)
	if len(legacy) != 21 || binary.LittleEndian.Uint32(legacy[8:]) != 77 || binary.LittleEndian.Uint32(legacy[12:]) != 3 || string(legacy[16:]) != "code\x00" {
		t.Fatalf("legacy gossip select = %x", legacy)
	}
}

func TestLegacyGossipMessageTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000042)
	legacy = binary.LittleEndian.AppendUint32(legacy, 55)
	legacy = binary.LittleEndian.AppendUint32(legacy, 68)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	legacy = append(legacy, 2, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 123)
	legacy = append(legacy, "传送我\x00确定吗？\x00"...)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 12)
	legacy = binary.LittleEndian.AppendUint32(legacy, 4)
	legacy = binary.LittleEndian.AppendUint32(legacy, 10)
	legacy = binary.LittleEndian.AppendUint32(legacy, 8)
	legacy = append(legacy, 1)
	legacy = append(legacy, "完成任务\x00"...)

	message, err := ParseLegacyGossipMessage(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if message.GossipID != 55 || message.TextID != 68 || len(message.Options) != 1 || len(message.Quests) != 1 {
		t.Fatalf("parsed gossip = %+v", message)
	}
	if message.Options[0].Text != "传送我" || message.Quests[0].Title != "完成任务" || !message.Quests[0].Repeatable {
		t.Fatalf("gossip contents = %+v / %+v", message.Options[0], message.Quests[0])
	}

	giver := ModernGUIDForLegacy(message.Giver, 571)
	modern := EncodeGossipMessage(giver, message)
	r := movementReader{data: modern}
	gotGUID, _ := r.guid128()
	menuID, _ := r.u32()
	friendship, _ := r.u32()
	optionCount, _ := r.u32()
	questCount, _ := r.u32()
	hasText, _ := r.bit()
	suppressed, _ := r.bit()
	r.align()
	if gotGUID != giver || menuID != 55 || friendship != 0 || optionCount != 1 || questCount != 1 || !hasText || suppressed {
		t.Fatalf("modern gossip header guid=%+v menu=%d friendship=%d options=%d quests=%d bits=%v/%v", gotGUID, menuID, friendship, optionCount, questCount, hasText, suppressed)
	}
	index, _ := r.u32()
	icon, _ := r.u8()
	flags, _ := r.u8()
	cost, _ := r.u32()
	_, _ = r.u32()
	_, _ = r.u32()
	secondIndex, _ := r.u32()
	textLen, _ := r.bits(12)
	confirmLen, _ := r.bits(12)
	_, _ = r.bits(2)
	_, _ = r.bit()
	_, _ = r.bit()
	r.align()
	treasureCount, _ := r.u32()
	text, _ := r.stringN(int(textLen))
	confirm, _ := r.stringN(int(confirmLen))
	textID, _ := r.u32()
	questID, _ := r.u32()
	_, _ = r.u32()
	questType, _ := r.u32()
	questLevel, _ := r.u32()
	maxLevel, _ := r.u32()
	questFlags, _ := r.u32()
	_, _ = r.u32()
	repeatable, _ := r.bit()
	_, _ = r.bit()
	titleLen, _ := r.bits(9)
	title, _ := r.stringN(int(titleLen))
	if index != 7 || secondIndex != 7 || icon != 2 || flags != 1 || cost != 123 || treasureCount != 0 || text != "传送我" || confirm != "确定吗？" {
		t.Fatalf("modern option index=%d/%d icon=%d flags=%d cost=%d treasure=%d text=%q confirm=%q", index, secondIndex, icon, flags, cost, treasureCount, text, confirm)
	}
	if textID != 68 || questID != 12 || questType != 4 || questLevel != 10 || maxLevel != 255 || questFlags != 8 || !repeatable || title != "完成任务" || r.remaining() != 0 {
		t.Fatalf("modern quest text=%d id=%d type=%d level=%d max=%d flags=%d repeat=%v title=%q remaining=%d", textID, questID, questType, questLevel, maxLevel, questFlags, repeatable, title, r.remaining())
	}
}

func TestEncodeGossipComplete(t *testing.T) {
	if body := EncodeGossipComplete(); len(body) != 1 || body[0] != 0 {
		t.Fatalf("gossip complete = %x", body)
	}
}

func TestLegacyQuestGiverQuestListTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000042)
	legacy = append(legacy, "有什么可以帮你？\x00"...)
	legacy = binary.LittleEndian.AppendUint32(legacy, 100)
	legacy = binary.LittleEndian.AppendUint32(legacy, 66)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1234)
	legacy = binary.LittleEndian.AppendUint32(legacy, 4)
	legacy = binary.LittleEndian.AppendUint32(legacy, 20)
	legacy = binary.LittleEndian.AppendUint32(legacy, 8)
	legacy = append(legacy, 0)
	legacy = append(legacy, "需要交付的任务\x00"...)

	list, err := ParseLegacyQuestGiverQuestList(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if list.Greeting != "有什么可以帮你？" || list.EmoteDelay != 100 || list.EmoteType != 66 || len(list.Quests) != 1 || list.Quests[0].QuestID != 1234 {
		t.Fatalf("quest list = %+v", list)
	}
	modern := EncodeQuestGiverQuestList(ModernGUIDForLegacy(list.Giver, 571), list)
	r := movementReader{data: modern}
	_, _ = r.guid128()
	delay, _ := r.u32()
	emote, _ := r.u32()
	count, _ := r.u32()
	greetingLen, _ := r.bits(11)
	r.align()
	questID, _ := r.u32()
	_, _ = r.u32()
	questType, _ := r.u32()
	questLevel, _ := r.u32()
	_, _ = r.u32()
	questFlags, _ := r.u32()
	_, _ = r.u32()
	repeatable, _ := r.bit()
	important, _ := r.bit()
	titleLen, _ := r.bits(9)
	title, _ := r.stringN(int(titleLen))
	greeting, _ := r.stringN(int(greetingLen))
	if delay != 100 || emote != 66 || count != 1 || questID != 1234 || questType != 4 || questLevel != 20 || questFlags != 8 || repeatable || important || title != "需要交付的任务" || greeting != "有什么可以帮你？" || r.remaining() != 0 {
		t.Fatalf("modern quest list delay=%d emote=%d count=%d quest=%d type=%d level=%d flags=%d repeat=%v important=%v title=%q greeting=%q remaining=%d", delay, emote, count, questID, questType, questLevel, questFlags, repeatable, important, title, greeting, r.remaining())
	}
}
