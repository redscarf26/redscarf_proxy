package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGQuestGiverHello         = uint16(0x3496)
	CMSGQuestGiverQueryQuest    = uint16(0x3497)
	CMSGQuestGiverAcceptQuest   = uint16(0x3498)
	CMSGQuestGiverCompleteQuest = uint16(0x3499)
	CMSGQuestGiverChooseReward  = uint16(0x349A)
	CMSGQuestGiverRequestReward = uint16(0x349B)
	CMSGQuestLogRemoveQuest     = uint16(0x352E)

	SMSGQuestGiverQuestComplete = uint16(0x2A83)
	SMSGQuestGiverQuestDetails  = uint16(0x2A92)
	SMSGQuestGiverRequestItems  = uint16(0x2A93)
	SMSGQuestGiverOfferReward   = uint16(0x2A94)

	maxQuestRewardEmotes = 16
)

type QuestGiverRequest struct {
	Giver      GUID128
	QuestID    uint32
	FromScript bool
}

type QuestGiverQueryRequest struct {
	Giver          GUID128
	QuestID        uint32
	RespondToGiver bool
}

type QuestGiverAcceptRequest struct {
	Giver      GUID128
	QuestID    uint32
	StartCheat bool
}

type QuestChooseRewardRequest struct {
	Giver    GUID128
	QuestID  uint32
	ItemID   uint32
	Quantity uint32
}

type QuestCollectItem struct {
	ItemID   uint32
	Quantity uint32
	Flags    uint32
}

type QuestRewardItem struct {
	ItemID   uint32
	Quantity uint32
}

type QuestRewardEmote struct {
	Type  uint32
	Delay uint32
}

type LegacyQuestRequestItems struct {
	Giver          uint64
	QuestID        uint32
	Title          string
	CompletionText string
	CompEmoteDelay uint32
	CompEmoteType  uint32
	AutoLaunched   bool
	QuestFlags     uint32
	SuggestedParty uint32
	MoneyToGet     int32
	Collect        []QuestCollectItem
	StatusFlags    uint32
}

type LegacyQuestOfferReward struct {
	Giver           uint64
	QuestID         uint32
	Title           string
	RewardText      string
	AutoLaunched    bool
	QuestFlags      uint32
	SuggestedParty  uint32
	Emotes          []QuestRewardEmote
	ChoiceItems     [questRewardChoiceCount]QuestRewardItem
	ChoiceCount     uint32
	RewardItems     [questRewardItemCount]QuestRewardItem
	RewardCount     uint32
	Money           uint32
	XP              uint32
	Honor           uint32
	TitleID         uint32
	SpellID         uint32
	NumSkillUps     uint32
	FactionID       [questRewardFactionCount]uint32
	FactionValue    [questRewardFactionCount]int32
	FactionOverride [questRewardFactionCount]int32
}

type LegacyQuestDetails struct {
	Giver          uint64
	InformUnit     uint64
	QuestID        uint32
	Title          string
	Description    string
	LogDescription string
	AutoLaunched   bool
	QuestFlags     uint32
	SuggestedParty uint32
	Rewards        LegacyQuestOfferReward
	DescEmotes     [4]QuestRewardEmote
}

type LegacyQuestComplete struct {
	QuestID    uint32
	XP         uint32
	Money      int32
	Honor      int32
	BonusHonor int32
}

func ParseQuestGiverCompleteQuest(body []byte) (QuestGiverRequest, error) {
	r := movementReader{data: body}
	request, err := parseQuestGiverAndID(&r)
	if err != nil {
		return request, err
	}
	if request.FromScript, err = r.bit(); err != nil {
		return request, fmt.Errorf("read quest-complete FromScript: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return request, fmt.Errorf("quest-complete has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseQuestGiverHello(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseQuestGiverQueryQuest(body []byte) (QuestGiverQueryRequest, error) {
	var request QuestGiverQueryRequest
	r := movementReader{data: body}
	var err error
	if request.Giver, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read query-quest giver: %w", err)
	}
	if request.QuestID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read query-quest ID: %w", err)
	}
	if request.RespondToGiver, err = r.bit(); err != nil {
		return request, fmt.Errorf("read query-quest respond flag: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return request, fmt.Errorf("query-quest has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseQuestGiverAcceptQuest(body []byte) (QuestGiverAcceptRequest, error) {
	var request QuestGiverAcceptRequest
	r := movementReader{data: body}
	var err error
	if request.Giver, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read accept-quest giver: %w", err)
	}
	if request.QuestID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read accept-quest ID: %w", err)
	}
	if request.StartCheat, err = r.bit(); err != nil {
		return request, fmt.Errorf("read accept-quest start-cheat: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return request, fmt.Errorf("accept-quest has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseQuestGiverRequestReward(body []byte) (QuestGiverRequest, error) {
	r := movementReader{data: body}
	request, err := parseQuestGiverAndID(&r)
	if err != nil {
		return request, err
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("quest-request-reward has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func parseQuestGiverAndID(r *movementReader) (QuestGiverRequest, error) {
	var request QuestGiverRequest
	var err error
	if request.Giver, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read quest-giver GUID: %w", err)
	}
	if request.QuestID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read quest ID: %w", err)
	}
	return request, nil
}

func ParseQuestGiverChooseReward(body []byte) (QuestChooseRewardRequest, error) {
	var request QuestChooseRewardRequest
	r := movementReader{data: body}
	var err error
	if request.Giver, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read reward quest-giver GUID: %w", err)
	}
	if request.QuestID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read reward quest ID: %w", err)
	}
	if _, err = r.bits(2); err != nil {
		return request, fmt.Errorf("read reward loot-item type: %w", err)
	}
	r.align()
	if request.ItemID, err = parseItemInstance(&r); err != nil {
		return request, fmt.Errorf("read reward item: %w", err)
	}
	if request.Quantity, err = r.u32(); err != nil {
		return request, fmt.Errorf("read reward quantity: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("quest-choose-reward has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func parseItemInstance(r *movementReader) (uint32, error) {
	itemID, err := r.u32()
	if err != nil {
		return 0, err
	}
	if _, err = r.u32(); err != nil { // random-properties seed
		return 0, err
	}
	if _, err = r.u32(); err != nil { // random-properties ID
		return 0, err
	}
	hasBonus, err := r.bit()
	if err != nil {
		return 0, err
	}
	r.align()
	modCount, err := r.bits(6)
	if err != nil {
		return 0, err
	}
	r.align()
	if _, err = r.take(int(modCount) * 5); err != nil {
		return 0, err
	}
	if hasBonus {
		if _, err = r.u8(); err != nil { // item context
			return 0, err
		}
		bonusCount, readErr := r.u32()
		if readErr != nil {
			return 0, readErr
		}
		if bonusCount > 256 {
			return 0, fmt.Errorf("item bonus count %d is unreasonable", bonusCount)
		}
		if _, err = r.take(int(bonusCount) * 4); err != nil {
			return 0, err
		}
	}
	return itemID, nil
}

func EncodeLegacyQuestGiverRequest(giver uint64, questID uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, giver)
	return binary.LittleEndian.AppendUint32(body, questID)
}

func EncodeLegacyQuestGiverQuery(giver uint64, questID uint32, respondToGiver bool) []byte {
	body := EncodeLegacyQuestGiverRequest(giver, questID)
	if respondToGiver {
		return append(body, 1)
	}
	return append(body, 0)
}

func EncodeLegacyQuestGiverAccept(giver uint64, questID uint32, startCheat bool) []byte {
	body := EncodeLegacyQuestGiverRequest(giver, questID)
	if startCheat {
		return binary.LittleEndian.AppendUint32(body, 1)
	}
	return binary.LittleEndian.AppendUint32(body, 0)
}

func ParseQuestLogRemoveQuest(body []byte) (uint8, error) {
	if len(body) != 1 {
		return 0, fmt.Errorf("quest-log-remove has %d bytes, want 1", len(body))
	}
	return body[0], nil
}

func EncodeLegacyQuestLogRemove(slot uint8) []byte { return []byte{slot} }

func QuestRewardChoiceIndex(itemID uint32, choices [questRewardChoiceCount]uint32) (int32, error) {
	if itemID == 0 {
		return 0, nil
	}
	for index, candidate := range choices {
		if candidate == itemID {
			return int32(index), nil
		}
	}
	return 0, fmt.Errorf("reward item %d is not cached for this quest", itemID)
}

func EncodeLegacyQuestChooseReward(giver uint64, questID uint32, choice int32) []byte {
	body := EncodeLegacyQuestGiverRequest(giver, questID)
	return binary.LittleEndian.AppendUint32(body, uint32(choice))
}

func ParseLegacyQuestGiverQuestDetails(body []byte) (LegacyQuestDetails, error) {
	var quest LegacyQuestDetails
	r := movementReader{data: body}
	var err error
	if quest.Giver, err = r.u64(); err != nil {
		return quest, fmt.Errorf("read quest-details giver: %w", err)
	}
	if quest.InformUnit, err = r.u64(); err != nil {
		return quest, fmt.Errorf("read quest-details inform unit: %w", err)
	}
	if quest.QuestID, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read quest-details quest ID: %w", err)
	}
	if quest.Title, err = r.cstring(); err != nil {
		return quest, fmt.Errorf("read quest-details title: %w", err)
	}
	if quest.Description, err = r.cstring(); err != nil {
		return quest, fmt.Errorf("read quest-details description: %w", err)
	}
	if quest.LogDescription, err = r.cstring(); err != nil {
		return quest, fmt.Errorf("read quest-details log description: %w", err)
	}
	auto, err := r.u8()
	if err != nil {
		return quest, fmt.Errorf("read quest-details auto-launch: %w", err)
	}
	quest.AutoLaunched = auto != 0
	if quest.QuestFlags, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read quest-details flags: %w", err)
	}
	if quest.SuggestedParty, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read quest-details suggested party: %w", err)
	}
	if _, err = r.u8(); err != nil {
		return quest, fmt.Errorf("read quest-details unused byte: %w", err)
	}
	if err = parseLegacyQuestRewards(&r, &quest.Rewards, false); err != nil {
		return quest, err
	}
	emoteCount, err := r.u32()
	if err != nil {
		return quest, fmt.Errorf("read quest-details emote count: %w", err)
	}
	if emoteCount > uint32(len(quest.DescEmotes)) {
		return quest, fmt.Errorf("quest-details emote count %d exceeds %d", emoteCount, len(quest.DescEmotes))
	}
	for index := uint32(0); index < emoteCount; index++ {
		if quest.DescEmotes[index].Type, err = r.u32(); err != nil {
			return quest, fmt.Errorf("read quest-details emote %d type: %w", index, err)
		}
		if quest.DescEmotes[index].Delay, err = r.u32(); err != nil {
			return quest, fmt.Errorf("read quest-details emote %d delay: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return quest, fmt.Errorf("quest-details has %d trailing bytes", r.remaining())
	}
	return quest, nil
}

func EncodeQuestGiverQuestDetails(giver, informUnit GUID128, quest LegacyQuestDetails) []byte {
	body := appendPackedGUID128(nil, giver.Low, giver.High)
	body = appendPackedGUID128(body, informUnit.Low, informUnit.High)
	body = binary.LittleEndian.AppendUint32(body, quest.QuestID)
	body = binary.LittleEndian.AppendUint32(body, 0) // quest package
	for range 4 {                                    // giver portrait/mount/model-scene and turn-in portrait
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, quest.QuestFlags)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, quest.SuggestedParty)
	body = binary.LittleEndian.AppendUint32(body, 0) // learned spell count
	body = binary.LittleEndian.AppendUint32(body, uint32(len(quest.DescEmotes)))
	body = binary.LittleEndian.AppendUint32(body, 0) // objective count
	body = binary.LittleEndian.AppendUint32(body, 0) // quest start item
	body = binary.LittleEndian.AppendUint32(body, 0) // quest-session bonus
	creatureID := uint32((quest.Giver >> 24) & 0x00ffffff)
	body = binary.LittleEndian.AppendUint32(body, creatureID)
	body = binary.LittleEndian.AppendUint32(body, 0)
	for _, emote := range quest.DescEmotes {
		body = binary.LittleEndian.AppendUint32(body, emote.Type)
		body = binary.LittleEndian.AppendUint32(body, emote.Delay)
	}
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(quest.Title)), 9)
	bits.writeBits(uint32(len(quest.Description)), 12)
	bits.writeBits(uint32(len(quest.LogDescription)), 12)
	bits.writeBits(0, 10)
	bits.writeBits(0, 8)
	bits.writeBits(0, 10)
	bits.writeBits(0, 8)
	bits.writeBit(quest.AutoLaunched)
	bits.writeBit(false)
	bits.writeBit(false)
	bits.writeBit(false)
	body = bits.flush()
	body = appendQuestRewardsWithFactionCap(body, quest.Rewards, 7)
	body = append(body, quest.Title...)
	body = append(body, quest.Description...)
	return append(body, quest.LogDescription...)
}

func ParseLegacyQuestGiverRequestItems(body []byte) (LegacyQuestRequestItems, error) {
	var quest LegacyQuestRequestItems
	r := movementReader{data: body}
	var err error
	if quest.Giver, err = r.u64(); err != nil {
		return quest, fmt.Errorf("read request-items giver: %w", err)
	}
	if quest.QuestID, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read request-items quest: %w", err)
	}
	if quest.Title, err = r.cstring(); err != nil {
		return quest, fmt.Errorf("read request-items title: %w", err)
	}
	if quest.CompletionText, err = r.cstring(); err != nil {
		return quest, fmt.Errorf("read request-items completion text: %w", err)
	}
	if quest.CompEmoteDelay, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read request-items emote delay: %w", err)
	}
	if quest.CompEmoteType, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read request-items emote type: %w", err)
	}
	auto, err := r.u32()
	if err != nil {
		return quest, fmt.Errorf("read request-items auto-launch: %w", err)
	}
	quest.AutoLaunched = auto != 0
	if quest.QuestFlags, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read request-items flags: %w", err)
	}
	if quest.SuggestedParty, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read request-items suggested party: %w", err)
	}
	if quest.MoneyToGet, err = r.i32(); err != nil {
		return quest, fmt.Errorf("read request-items money: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return quest, fmt.Errorf("read request-items count: %w", err)
	}
	if count > 64 {
		return quest, fmt.Errorf("request-items count %d is unreasonable", count)
	}
	quest.Collect = make([]QuestCollectItem, 0, count)
	for index := uint32(0); index < count; index++ {
		var item QuestCollectItem
		if item.ItemID, err = r.u32(); err != nil {
			return quest, fmt.Errorf("read request item %d ID: %w", index, err)
		}
		if item.Quantity, err = r.u32(); err != nil {
			return quest, fmt.Errorf("read request item %d quantity: %w", index, err)
		}
		if item.Flags, err = r.u32(); err != nil { // legacy display ID; safe as modern flags=0
			return quest, fmt.Errorf("read request item %d display: %w", index, err)
		}
		item.Flags = 0
		quest.Collect = append(quest.Collect, item)
	}
	status, err := r.u32()
	if err != nil {
		return quest, fmt.Errorf("read request-items status: %w", err)
	}
	if status&3 != 0 {
		quest.StatusFlags = 223
	} else {
		quest.StatusFlags = 219
	}
	// Close-on-cancel, required-money and required-spell fields are not
	// represented by 3.4.3's RequestItems message.
	if _, err = r.take(r.remaining()); err != nil {
		return quest, err
	}
	return quest, nil
}

func EncodeQuestGiverRequestItems(giver GUID128, quest LegacyQuestRequestItems) []byte {
	body := appendPackedGUID128(nil, giver.Low, giver.High)
	creatureID := uint32((quest.Giver >> 24) & 0x00ffffff)
	body = binary.LittleEndian.AppendUint32(body, creatureID)
	body = binary.LittleEndian.AppendUint32(body, quest.QuestID)
	body = binary.LittleEndian.AppendUint32(body, quest.CompEmoteDelay)
	body = binary.LittleEndian.AppendUint32(body, quest.CompEmoteType)
	body = binary.LittleEndian.AppendUint32(body, quest.QuestFlags)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, quest.SuggestedParty)
	body = binary.LittleEndian.AppendUint32(body, uint32(quest.MoneyToGet))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(quest.Collect)))
	body = binary.LittleEndian.AppendUint32(body, 0) // currency count
	body = binary.LittleEndian.AppendUint32(body, quest.StatusFlags)
	for _, item := range quest.Collect {
		body = binary.LittleEndian.AppendUint32(body, item.ItemID)
		body = binary.LittleEndian.AppendUint32(body, item.Quantity)
		body = binary.LittleEndian.AppendUint32(body, item.Flags)
	}
	bits := newBitWriter(body)
	bits.writeBit(quest.AutoLaunched)
	body = bits.flush()
	body = binary.LittleEndian.AppendUint32(body, creatureID)
	body = binary.LittleEndian.AppendUint32(body, 0) // conditional text count
	bits = newBitWriter(body)
	bits.writeBits(uint32(len(quest.Title)), 9)
	bits.writeBits(uint32(len(quest.CompletionText)), 12)
	body = bits.flush()
	body = append(body, quest.Title...)
	return append(body, quest.CompletionText...)
}

func ParseLegacyQuestGiverOfferReward(body []byte) (LegacyQuestOfferReward, error) {
	var quest LegacyQuestOfferReward
	r := movementReader{data: body}
	var err error
	if quest.Giver, err = r.u64(); err != nil {
		return quest, fmt.Errorf("read offer-reward giver: %w", err)
	}
	if quest.QuestID, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read offer-reward quest: %w", err)
	}
	if quest.Title, err = r.cstring(); err != nil {
		return quest, fmt.Errorf("read offer-reward title: %w", err)
	}
	if quest.RewardText, err = r.cstring(); err != nil {
		return quest, fmt.Errorf("read offer-reward text: %w", err)
	}
	auto, err := r.u8()
	if err != nil {
		return quest, fmt.Errorf("read offer-reward auto-launch: %w", err)
	}
	quest.AutoLaunched = auto != 0
	if quest.QuestFlags, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read offer-reward flags: %w", err)
	}
	if quest.SuggestedParty, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read offer-reward suggested party: %w", err)
	}
	emoteCount, err := r.u32()
	if err != nil {
		return quest, fmt.Errorf("read offer-reward emote count: %w", err)
	}
	if emoteCount > maxQuestRewardEmotes {
		return quest, fmt.Errorf("offer-reward emote count %d exceeds %d", emoteCount, maxQuestRewardEmotes)
	}
	for index := uint32(0); index < emoteCount; index++ {
		var emote QuestRewardEmote
		if emote.Delay, err = r.u32(); err != nil {
			return quest, fmt.Errorf("read offer-reward emote %d delay: %w", index, err)
		}
		if emote.Type, err = r.u32(); err != nil {
			return quest, fmt.Errorf("read offer-reward emote %d type: %w", index, err)
		}
		quest.Emotes = append(quest.Emotes, emote)
	}
	if err = parseLegacyQuestRewards(&r, &quest, true); err != nil {
		return quest, err
	}
	if r.remaining() != 0 {
		return quest, fmt.Errorf("offer-reward has %d trailing bytes", r.remaining())
	}
	return quest, nil
}

func parseLegacyQuestRewards(r *movementReader, quest *LegacyQuestOfferReward, readFlags bool) error {
	choiceCount, err := r.u32()
	if err != nil {
		return fmt.Errorf("read choice reward count: %w", err)
	}
	if choiceCount > questRewardChoiceCount {
		return fmt.Errorf("choice reward count %d exceeds %d", choiceCount, questRewardChoiceCount)
	}
	quest.ChoiceCount = choiceCount
	for index := uint32(0); index < choiceCount; index++ {
		if quest.ChoiceItems[index].ItemID, err = r.u32(); err != nil {
			return fmt.Errorf("read choice reward %d ID: %w", index, err)
		}
		if quest.ChoiceItems[index].Quantity, err = r.u32(); err != nil {
			return fmt.Errorf("read choice reward %d quantity: %w", index, err)
		}
		if _, err = r.u32(); err != nil {
			return fmt.Errorf("read choice reward %d display: %w", index, err)
		}
	}
	rewardCount, err := r.u32()
	if err != nil {
		return fmt.Errorf("read fixed reward count: %w", err)
	}
	if rewardCount > questRewardItemCount {
		return fmt.Errorf("fixed reward count %d exceeds %d", rewardCount, questRewardItemCount)
	}
	quest.RewardCount = rewardCount
	for index := uint32(0); index < rewardCount; index++ {
		if quest.RewardItems[index].ItemID, err = r.u32(); err != nil {
			return fmt.Errorf("read fixed reward %d ID: %w", index, err)
		}
		if quest.RewardItems[index].Quantity, err = r.u32(); err != nil {
			return fmt.Errorf("read fixed reward %d quantity: %w", index, err)
		}
		if _, err = r.u32(); err != nil {
			return fmt.Errorf("read fixed reward %d display: %w", index, err)
		}
	}
	if quest.Money, err = r.u32(); err != nil {
		return fmt.Errorf("read reward money: %w", err)
	}
	if quest.XP, err = r.u32(); err != nil {
		return fmt.Errorf("read reward XP: %w", err)
	}
	if quest.Honor, err = r.u32(); err != nil {
		return fmt.Errorf("read reward honor: %w", err)
	}
	if _, err = r.f32(); err != nil {
		return fmt.Errorf("read reward honor multiplier: %w", err)
	}
	if readFlags {
		if _, err = r.u32(); err != nil { // reward flags
			return fmt.Errorf("read reward flags: %w", err)
		}
	}
	if quest.SpellID, err = r.u32(); err != nil {
		return fmt.Errorf("read reward completion spell: %w", err)
	}
	if _, err = r.u32(); err != nil { // reward spell to learn
		return fmt.Errorf("read reward learn spell: %w", err)
	}
	if quest.TitleID, err = r.u32(); err != nil {
		return fmt.Errorf("read reward title: %w", err)
	}
	if quest.NumSkillUps, err = r.u32(); err != nil {
		return fmt.Errorf("read reward skill-ups: %w", err)
	}
	if _, err = r.u32(); err != nil { // bonus talents
		return fmt.Errorf("read reward bonus talents: %w", err)
	}
	if _, err = r.u32(); err != nil { // arena points
		return fmt.Errorf("read reward arena points: %w", err)
	}
	for index := range quest.FactionID {
		if quest.FactionID[index], err = r.u32(); err != nil {
			return fmt.Errorf("read reward faction %d: %w", index, err)
		}
	}
	for index := range quest.FactionValue {
		if quest.FactionValue[index], err = r.i32(); err != nil {
			return fmt.Errorf("read reward faction value %d: %w", index, err)
		}
	}
	for index := range quest.FactionOverride {
		if quest.FactionOverride[index], err = r.i32(); err != nil {
			return fmt.Errorf("read reward faction override %d: %w", index, err)
		}
	}
	return nil
}

func QuestRewardChoices(quest LegacyQuestOfferReward) [questRewardChoiceCount]uint32 {
	var result [questRewardChoiceCount]uint32
	for index := range result {
		result[index] = quest.ChoiceItems[index].ItemID
	}
	return result
}

func EncodeQuestGiverOfferReward(giver GUID128, quest LegacyQuestOfferReward) []byte {
	body := appendPackedGUID128(nil, giver.Low, giver.High)
	creatureID := uint32((quest.Giver >> 24) & 0x00ffffff)
	body = binary.LittleEndian.AppendUint32(body, creatureID)
	body = binary.LittleEndian.AppendUint32(body, quest.QuestID)
	body = binary.LittleEndian.AppendUint32(body, quest.QuestFlags)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, quest.SuggestedParty)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(quest.Emotes)))
	for _, emote := range quest.Emotes {
		body = binary.LittleEndian.AppendUint32(body, emote.Type)
		body = binary.LittleEndian.AppendUint32(body, emote.Delay)
	}
	bits := newBitWriter(body)
	bits.writeBit(quest.AutoLaunched)
	bits.writeBit(false)
	body = bits.flush()
	body = appendQuestRewards(body, quest)
	for range 7 { // package, four portraits, creature ID, conditional text count
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	// Creature ID is the sixth value in the suffix.
	binary.LittleEndian.PutUint32(body[len(body)-8:len(body)-4], creatureID)
	bits = newBitWriter(body)
	bits.writeBits(uint32(len(quest.Title)), 9)
	bits.writeBits(uint32(len(quest.RewardText)), 12)
	bits.writeBits(0, 10)
	bits.writeBits(0, 8)
	bits.writeBits(0, 10)
	bits.writeBits(0, 8)
	body = bits.flush()
	body = append(body, quest.Title...)
	return append(body, quest.RewardText...)
}

func appendQuestRewards(body []byte, quest LegacyQuestOfferReward) []byte {
	return appendQuestRewardsWithFactionCap(body, quest, 0)
}

func appendQuestRewardsWithFactionCap(body []byte, quest LegacyQuestOfferReward, factionCap uint32) []byte {
	body = binary.LittleEndian.AppendUint32(body, quest.ChoiceCount)
	body = binary.LittleEndian.AppendUint32(body, quest.RewardCount)
	for _, item := range quest.RewardItems {
		body = binary.LittleEndian.AppendUint32(body, item.ItemID)
		body = binary.LittleEndian.AppendUint32(body, item.Quantity)
	}
	body = binary.LittleEndian.AppendUint32(body, quest.Money)
	body = binary.LittleEndian.AppendUint32(body, quest.XP)
	body = binary.LittleEndian.AppendUint64(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, quest.Honor)
	body = binary.LittleEndian.AppendUint32(body, quest.TitleID)
	body = binary.LittleEndian.AppendUint32(body, 0)
	for index := range quest.FactionID {
		body = binary.LittleEndian.AppendUint32(body, quest.FactionID[index])
		body = binary.LittleEndian.AppendUint32(body, uint32(quest.FactionValue[index]))
		body = binary.LittleEndian.AppendUint32(body, uint32(quest.FactionOverride[index]))
		body = binary.LittleEndian.AppendUint32(body, factionCap)
	}
	for range questRewardDisplaySpellCount {
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, quest.SpellID)
	for range questRewardCurrencyCount * 2 {
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, 0) // skill line
	body = binary.LittleEndian.AppendUint32(body, quest.NumSkillUps)
	body = binary.LittleEndian.AppendUint32(body, 0) // treasure picker
	for _, item := range quest.ChoiceItems {
		bits := newBitWriter(body)
		bits.writeBits(0, 2)
		body = bits.flush()
		body = appendItemInstance(body, item.ItemID, 0, 0)
		body = binary.LittleEndian.AppendUint32(body, item.Quantity)
	}
	bits := newBitWriter(body)
	bits.writeBit(false)
	return bits.flush()
}

func ParseLegacyQuestGiverQuestComplete(body []byte) (LegacyQuestComplete, error) {
	var quest LegacyQuestComplete
	r := movementReader{data: body}
	var err error
	if quest.QuestID, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read completed quest ID: %w", err)
	}
	if quest.XP, err = r.u32(); err != nil {
		return quest, fmt.Errorf("read completed quest XP: %w", err)
	}
	if quest.Money, err = r.i32(); err != nil {
		return quest, fmt.Errorf("read completed quest money: %w", err)
	}
	if quest.Honor, err = r.i32(); err != nil {
		return quest, fmt.Errorf("read completed quest honor: %w", err)
	}
	if quest.BonusHonor, err = r.i32(); err != nil {
		return quest, fmt.Errorf("read completed quest bonus honor: %w", err)
	}
	if _, err = r.i32(); err != nil {
		return quest, fmt.Errorf("read completed quest trailing reward: %w", err)
	}
	if r.remaining() != 0 {
		return quest, fmt.Errorf("quest-complete has %d trailing bytes", r.remaining())
	}
	return quest, nil
}

func EncodeQuestGiverQuestComplete(quest LegacyQuestComplete) []byte {
	body := binary.LittleEndian.AppendUint32(nil, quest.QuestID)
	body = binary.LittleEndian.AppendUint32(body, quest.XP)
	body = binary.LittleEndian.AppendUint64(body, uint64(int64(quest.Money)))
	body = binary.LittleEndian.AppendUint32(body, 0) // skill line
	body = binary.LittleEndian.AppendUint32(body, 0) // skill-ups
	bits := newBitWriter(body)
	bits.writeBit(false) // UseQuestReward
	bits.writeBit(false) // LaunchGossip
	// Do not ask the modern client to immediately reopen the quest giver.
	// In practice this races auto-turn-in addons: the legacy server has already
	// committed the reward, but LaunchQuest makes the client send another hello
	// and display a fresh quest frame over the completed interaction.
	bits.writeBit(false) // LaunchQuest
	bits.writeBit(false) // HideChatMessage
	body = bits.flush()
	return appendItemInstance(body, 0, 0, 0)
}
