package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGQueryQuestInfo                = uint16(12915)
	CMSGQueryQuestCompletionNPCs      = uint16(0x3177)
	CMSGQuestGiverStatusQuery         = uint16(13468)
	CMSGQuestGiverStatusMultipleQuery = uint16(13469)
	CMSGQuestConfirmAccept            = uint16(0x349E)
	CMSGQuestPushResult               = uint16(0x34A0)
	CMSGQuestPOIQuery                 = uint16(14002)
	SMSGQueryQuestInfoResponse        = uint16(10902)
	SMSGQuestGiverStatus              = uint16(10907)
	SMSGQuestGiverStatusMultiple      = uint16(10897)
	SMSGQuestGiverInvalidQuest        = uint16(10885)
	SMSGQuestGiverQuestFailed         = uint16(10886)
	SMSGQuestConfirmAccept            = uint16(10895)
	SMSGQuestPushResult               = uint16(10896)
	SMSGQuestLogFull                  = uint16(10887)
	SMSGQuestUpdateComplete           = uint16(10889)
	SMSGQuestUpdateFailed             = uint16(10890)
	SMSGQuestUpdateFailedTimer        = uint16(10891)
	SMSGQuestUpdateAddCredit          = uint16(10892)
	SMSGQuestPOIQueryResponse         = uint16(10909)

	legacyQuestUpdateGOFlag    = uint32(0x80000000)
	legacyQuestFlagExploration = uint32(0x00000004)

	legacyQuestQueryNotFound     = uint32(0x80000000)
	questRewardDisplaySpellCount = 3
	questRewardItemCount         = 4
	questRewardChoiceCount       = 6
	questRewardFactionCount      = 5
	questRewardCurrencyCount     = 4
	questCreatureObjectives      = 4
	questItemObjectives          = 6
	questObjectiveMonster        = byte(0)
	questObjectiveItem           = byte(1)
	questObjectiveGameObject     = byte(2)
	questObjectiveAreaTrigger    = byte(10)
	questObjectiveFlagHidden     = uint32(8) // Hermes QuestObjectiveFlags.Hidden
	wotlkExpansion               = int32(2)

	maxQuestConfirmAcceptTitle = 1<<10 - 1

	// AzerothCore QuestShareMessages.
	questShareSharing    = uint8(0)
	questShareInvalid    = uint8(1)
	questShareAccepted   = uint8(2)
	questShareDeclined   = uint8(3)
	questShareBusy       = uint8(4)
	questShareLogFull    = uint8(5)
	questShareHaveQuest  = uint8(6)
	questShareFinish     = uint8(7)
	questShareNotDaily   = uint8(8)
	questShareTimer      = uint8(9)
	questShareNotInParty = uint8(10)

	// Build 54261 QuestPushReason. WowClassic.exe registers ERR_QUEST_PUSH_* in
	// this order, matching TrinityCore wotlk_classic. TooFar is 4 and Dead is 6.
	questPushSuccess              = uint8(0)  // ERR_QUEST_PUSH_SUCCESS_S
	questPushInvalid              = uint8(1)  // ERR_QUEST_PUSH_INVALID_S
	questPushAccepted             = uint8(2)  // ERR_QUEST_PUSH_ACCEPTED_S
	questPushDeclined             = uint8(3)  // ERR_QUEST_PUSH_DECLINED_S
	questPushTooFar               = uint8(4)  // ERR_QUEST_PUSH_TOO_FAR_S
	questPushBusy                 = uint8(5)  // ERR_QUEST_PUSH_BUSY_S
	questPushDead                 = uint8(6)  // ERR_QUEST_PUSH_DEAD_S
	questPushLogFull              = uint8(7)  // ERR_QUEST_PUSH_LOG_FULL_S
	questPushOnQuest              = uint8(8)  // ERR_QUEST_PUSH_ONQUEST_S
	questPushAlreadyDone          = uint8(9)  // ERR_QUEST_PUSH_ALREADY_DONE_S
	questPushNotDaily             = uint8(10) // ERR_QUEST_PUSH_NOT_DAILY_S
	questPushTimerExpired         = uint8(11) // ERR_QUEST_PUSH_TIMER_EXPIRED_S
	questPushNotInParty           = uint8(12) // ERR_QUEST_PUSH_NOT_IN_PARTY_S
	questPushDifferentServerDaily = uint8(13) // ERR_QUEST_PUSH_DIFFERENT_SERVER_DAILY_S
	questPushNotAllowed           = uint8(14) // ERR_QUEST_PUSH_NOT_ALLOWED_S
)

// ConvertQuestGiverStatus343 is legacy proxy's expansion-80 map of 3.3.5a DialogStatus 0..10.
var convertQuestGiverStatus343 = [11]uint64{
	0,    // None
	2,    // Unavailable → Future
	4,    // LowLevelAvailable
	4096, // LowLevelRewardRep
	28,   // LowLevelAvailableRep
	32,   // Incomplete
	608,  // RewardRep
	1536, // AvailableRep
	1024, // Available
	4096, // Reward2
	4096, // Reward
}

type questObjective struct {
	ID           uint32
	Type         byte
	StorageIndex int8
	ObjectID     int32
	Amount       int32
	Flags        uint32
	Description  string
}

type QuestItemObjectiveInfo struct {
	QuestID  uint32
	ItemID   uint32
	Required uint16
}

func questObjectiveWireID(questID uint32, storageIndex int8) uint32 {
	// Legacy quests do not carry the DB-backed objective IDs required by the
	// modern client. Derive a stable non-zero ID from the quest and its
	// template objective slot so multiple objectives no longer collide at
	// ID 0. QuestPOI blobs reference the same slot, so the two ID spaces stay
	// aligned.
	return questID*32 + uint32(uint8(storageIndex)) + 1
}

type questQueryInfo struct {
	QuestID               uint32
	QuestType             int32
	QuestLevel            int32
	MinLevel              int32
	QuestSortID           int32
	QuestInfoID           uint32
	SuggestedGroupNum     uint32
	RewardNextQuest       uint32
	RewardXPDifficulty    uint32
	RewardMoney           int32
	RewardBonusMoney      uint32
	RewardSpell           uint32
	RewardHonor           int32
	RewardKillHonor       float32
	StartItem             uint32
	Flags                 uint32
	RewardItems           [questRewardItemCount]uint32
	RewardAmounts         [questRewardItemCount]uint32
	RewardChoiceItems     [questRewardChoiceCount]uint32
	RewardChoiceAmounts   [questRewardChoiceCount]uint32
	RewardFactionID       [questRewardFactionCount]uint32
	RewardFactionValue    [questRewardFactionCount]int32
	RewardFactionOverride [questRewardFactionCount]int32
	POIContinent          uint32
	POIx                  float32
	POIy                  float32
	POIPriority           uint32
	RewardTitle           uint32
	RewardArenaPoints     int32
	RewardSkillLineID     uint32
	RewardNumSkillUps     uint32
	LogTitle              string
	LogDescription        string
	QuestDescription      string
	AreaDescription       string
	QuestCompletionLog    string
	Objectives            []questObjective
}

func ParseQueryQuestInfo(body []byte) (uint32, error) {
	if len(body) < 4 {
		return 0, fmt.Errorf("query-quest-info has %d bytes, want at least 4", len(body))
	}
	entry := binary.LittleEndian.Uint32(body[:4])
	if len(body) == 4 {
		return entry, nil
	}
	if _, err := ParsePackedGUID128Exact(body[4:]); err != nil {
		return 0, fmt.Errorf("query-quest-info GUID: %w", err)
	}
	return entry, nil
}

func ParseQueryQuestCompletionNPCs(body []byte) ([]uint32, error) {
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read quest-completion count: %w", err)
	}
	if count > 64 {
		return nil, fmt.Errorf("quest-completion count %d exceeds 64", count)
	}
	quests := make([]uint32, count)
	for index := range quests {
		if quests[index], err = r.u32(); err != nil {
			return nil, fmt.Errorf("read quest-completion quest %d: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("quest-completion query has %d trailing bytes", r.remaining())
	}
	return quests, nil
}

// EncodeLegacyQueryQuestsCompleted preserves legacy proxy's forwarding layout. The
// 3.3.5 server ignores the request body, but retaining the bounded quest list
// keeps the conversion byte-for-byte compatible with the modern request.
func EncodeLegacyQueryQuestsCompleted(quests []uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(quests)))
	for _, questID := range quests {
		body = binary.LittleEndian.AppendUint32(body, questID)
	}
	return body
}

func EncodeLegacyQuestQuery(entry uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, entry)
}

func ParseQuestGiverStatusQuery(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ConvertQuestGiverStatus343(legacy uint32) uint64 {
	if int(legacy) < len(convertQuestGiverStatus343) {
		return convertQuestGiverStatus343[legacy]
	}
	return 0
}

func EncodeQuestGiverStatus(guid GUID128, status uint64) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	return binary.LittleEndian.AppendUint64(body, status)
}

func TranslateQuestGiverStatus(legacy []byte) (uint64, uint64, error) {
	r := movementReader{data: legacy}
	guid, err := r.u64()
	if err != nil {
		return 0, 0, fmt.Errorf("read quest-giver GUID: %w", err)
	}
	status, err := r.u8()
	if err != nil {
		return 0, 0, fmt.Errorf("read quest-giver status: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("quest-giver status has %d trailing bytes", r.remaining())
	}
	return guid, ConvertQuestGiverStatus343(uint32(status)), nil
}

func TranslateQuestGiverStatusMultiple(legacy []byte) ([]uint64, []uint64, error) {
	r := movementReader{data: legacy}
	count, err := r.u32()
	if err != nil {
		return nil, nil, fmt.Errorf("read quest-giver status count: %w", err)
	}
	guids := make([]uint64, 0, count)
	statuses := make([]uint64, 0, count)
	for index := uint32(0); index < count; index++ {
		guid, readErr := r.u64()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read quest-giver %d GUID: %w", index, readErr)
		}
		status, readErr := r.u8()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read quest-giver %d status: %w", index, readErr)
		}
		guids = append(guids, guid)
		statuses = append(statuses, ConvertQuestGiverStatus343(uint32(status)))
	}
	if r.remaining() != 0 {
		return nil, nil, fmt.Errorf("quest-giver status multiple has %d trailing bytes", r.remaining())
	}
	return guids, statuses, nil
}

func EncodeQuestGiverStatusMultiple(guids []GUID128, statuses []uint64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(guids)))
	for index, guid := range guids {
		status := uint64(0)
		if index < len(statuses) {
			status = statuses[index]
		}
		body = appendPackedGUID128(body, guid.Low, guid.High)
		body = binary.LittleEndian.AppendUint64(body, status)
	}
	return body
}
func TranslateQuestQueryResponse(legacy []byte) ([]byte, error) {
	body, _, _, err := TranslateQuestQueryResponseWithChoices(legacy)
	return body, err
}

// TranslateQuestQueryResponseWithChoices also exposes the reward-choice IDs
// already present in a client-requested query response. The relay uses this
// cache to map 3.4's selected ItemID back to WotLK's reward index without
// issuing its own quest queries.
func TranslateQuestQueryResponseWithChoices(legacy []byte) ([]byte, uint32, [questRewardChoiceCount]uint32, error) {
	r := movementReader{data: legacy}
	masked, err := r.u32()
	if err != nil {
		return nil, 0, [questRewardChoiceCount]uint32{}, fmt.Errorf("read quest entry: %w", err)
	}
	entry := masked &^ legacyQuestQueryNotFound
	if masked&legacyQuestQueryNotFound != 0 {
		return encodeQuestQueryResponse(entry, false, questQueryInfo{}), entry, [questRewardChoiceCount]uint32{}, nil
	}
	info, err := parseLegacyQuestQuery(&r, entry)
	if err != nil {
		return nil, entry, [questRewardChoiceCount]uint32{}, err
	}
	return encodeQuestQueryResponse(entry, true, info), entry, info.RewardChoiceItems, nil
}

// ParseLegacyQuestItemObjectives exposes the item requirements already parsed
// from a WotLK quest template. The relay uses them to rebuild modern quest
// tracker progress when AzerothCore sends its intentionally empty
// SMSG_QUESTUPDATE_ADD_ITEM notification.
func ParseLegacyQuestItemObjectives(legacy []byte) ([]QuestItemObjectiveInfo, error) {
	r := movementReader{data: legacy}
	masked, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read quest entry: %w", err)
	}
	entry := masked &^ legacyQuestQueryNotFound
	if masked&legacyQuestQueryNotFound != 0 {
		return nil, nil
	}
	info, err := parseLegacyQuestQuery(&r, entry)
	if err != nil {
		return nil, err
	}
	items := make([]QuestItemObjectiveInfo, 0, questItemObjectives)
	for _, objective := range info.Objectives {
		if objective.Type != questObjectiveItem || objective.ObjectID <= 0 || objective.Amount <= 0 {
			continue
		}
		required := objective.Amount
		if required > int32(^uint16(0)) {
			required = int32(^uint16(0))
		}
		items = append(items, QuestItemObjectiveInfo{
			QuestID:  entry,
			ItemID:   uint32(objective.ObjectID),
			Required: uint16(required),
		})
	}
	return items, nil
}

// HasLegacyQuestStateObjective identifies synthetic StorageIndex-0 objectives,
// including exploration requirements alongside item or kill objectives.
// Both visible exploration objectives and hidden script/dialogue objectives need
// StateFlags bit 8 set only when the legacy quest slot is COMPLETE.
func HasLegacyQuestStateObjective(legacy []byte) (uint32, bool, error) {
	r := movementReader{data: legacy}
	masked, err := r.u32()
	if err != nil {
		return 0, false, fmt.Errorf("read quest entry: %w", err)
	}
	entry := masked &^ legacyQuestQueryNotFound
	if masked&legacyQuestQueryNotFound != 0 {
		return entry, false, nil
	}
	info, err := parseLegacyQuestQuery(&r, entry)
	if err != nil {
		return entry, false, err
	}
	for _, objective := range info.Objectives {
		if objective.Type == questObjectiveAreaTrigger && objective.StorageIndex == 0 {
			return entry, true, nil
		}
	}
	return entry, false, nil
}

func ActiveQuestIDsFromLegacyFields(fields map[int]uint32) map[uint32]struct{} {
	quests := make(map[uint32]struct{}, 25)
	for slot := 0; slot < 25; slot++ {
		if questID := fields[legacyPlayerQuestLog1+slot*5]; questID != 0 {
			quests[questID] = struct{}{}
		}
	}
	return quests
}

// FindLegacyQuestLogSlot returns the update-field index of the quest-log slot
// (legacyPlayerQuestLog1 + slot*5) that holds questID, or -1 when the quest is
// not in the player's cached quest log.
func FindLegacyQuestLogSlot(fields map[int]uint32, questID uint32) int {
	for slot := 0; slot < 25; slot++ {
		base := legacyPlayerQuestLog1 + slot*5
		if fields[base] == questID {
			return base
		}
	}
	return -1
}

func LegacyItemEntryAndCount(fields map[int]uint32) (uint32, uint32) {
	entry := fields[legacyObjectEntry]
	if entry == 0 {
		return 0, 0
	}
	count := fields[legacyItemStackCount]
	if count == 0 {
		count = 1
	}
	return entry, count
}

func EncodeQuestItemProgress(questID, itemID uint32, count, required uint16) []byte {
	return EncodeQuestUpdateAddCredit(GUID128{}, QuestUpdateAddCredit{
		QuestID: questID, ObjectID: int32(itemID), Count: count,
		Required: required, ObjectiveType: questObjectiveItem,
	})
}

func parseLegacyQuestQuery(r *movementReader, entry uint32) (questQueryInfo, error) {
	var info questQueryInfo
	info.QuestID = entry
	var err error
	if info.QuestType, err = r.i32(); err != nil {
		return info, fmt.Errorf("read quest method: %w", err)
	}
	if info.QuestLevel, err = r.i32(); err != nil {
		return info, fmt.Errorf("read quest level: %w", err)
	}
	if info.MinLevel, err = r.i32(); err != nil {
		return info, fmt.Errorf("read quest min level: %w", err)
	}
	if info.QuestSortID, err = r.i32(); err != nil {
		return info, fmt.Errorf("read quest sort: %w", err)
	}
	if info.QuestInfoID, err = r.u32(); err != nil {
		return info, fmt.Errorf("read quest type: %w", err)
	}
	if info.SuggestedGroupNum, err = r.u32(); err != nil {
		return info, fmt.Errorf("read suggested players: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return info, fmt.Errorf("read rep objective faction: %w", err)
	}
	if _, err = r.i32(); err != nil {
		return info, fmt.Errorf("read rep objective value: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return info, fmt.Errorf("read opposite rep faction: %w", err)
	}
	if _, err = r.i32(); err != nil {
		return info, fmt.Errorf("read opposite rep value: %w", err)
	}
	if info.RewardNextQuest, err = r.u32(); err != nil {
		return info, fmt.Errorf("read next quest: %w", err)
	}
	if info.RewardXPDifficulty, err = r.u32(); err != nil {
		return info, fmt.Errorf("read reward xp id: %w", err)
	}
	if info.RewardMoney, err = r.i32(); err != nil {
		return info, fmt.Errorf("read reward money: %w", err)
	}
	if info.RewardBonusMoney, err = r.u32(); err != nil {
		return info, fmt.Errorf("read reward money max level: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return info, fmt.Errorf("read reward spell: %w", err)
	}
	if info.RewardSpell, err = r.u32(); err != nil {
		return info, fmt.Errorf("read reward spell cast: %w", err)
	}
	if info.RewardHonor, err = r.i32(); err != nil {
		return info, fmt.Errorf("read reward honor: %w", err)
	}
	if info.RewardKillHonor, err = r.f32(); err != nil {
		return info, fmt.Errorf("read reward honor multiplier: %w", err)
	}
	if info.StartItem, err = r.u32(); err != nil {
		return info, fmt.Errorf("read source item: %w", err)
	}
	if info.Flags, err = r.u32(); err != nil {
		return info, fmt.Errorf("read quest flags: %w", err)
	}
	if info.RewardTitle, err = r.u32(); err != nil {
		return info, fmt.Errorf("read reward title: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return info, fmt.Errorf("read players slain: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return info, fmt.Errorf("read bonus talents: %w", err)
	}
	if info.RewardArenaPoints, err = r.i32(); err != nil {
		return info, fmt.Errorf("read reward arena points: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return info, fmt.Errorf("read reward reputation mask: %w", err)
	}
	for index := range info.RewardItems {
		if info.RewardItems[index], err = r.u32(); err != nil {
			return info, fmt.Errorf("read reward item %d: %w", index, err)
		}
		if info.RewardAmounts[index], err = r.u32(); err != nil {
			return info, fmt.Errorf("read reward item count %d: %w", index, err)
		}
	}
	for index := range info.RewardChoiceItems {
		if info.RewardChoiceItems[index], err = r.u32(); err != nil {
			return info, fmt.Errorf("read reward choice item %d: %w", index, err)
		}
		if info.RewardChoiceAmounts[index], err = r.u32(); err != nil {
			return info, fmt.Errorf("read reward choice count %d: %w", index, err)
		}
	}
	for index := range info.RewardFactionID {
		if info.RewardFactionID[index], err = r.u32(); err != nil {
			return info, fmt.Errorf("read reward faction %d: %w", index, err)
		}
	}
	for index := range info.RewardFactionValue {
		if info.RewardFactionValue[index], err = r.i32(); err != nil {
			return info, fmt.Errorf("read reward faction value %d: %w", index, err)
		}
	}
	for index := range info.RewardFactionOverride {
		if info.RewardFactionOverride[index], err = r.i32(); err != nil {
			return info, fmt.Errorf("read reward faction override %d: %w", index, err)
		}
	}
	if info.POIContinent, err = r.u32(); err != nil {
		return info, fmt.Errorf("read point map: %w", err)
	}
	if info.POIx, err = r.f32(); err != nil {
		return info, fmt.Errorf("read point x: %w", err)
	}
	if info.POIy, err = r.f32(); err != nil {
		return info, fmt.Errorf("read point y: %w", err)
	}
	if info.POIPriority, err = r.u32(); err != nil {
		return info, fmt.Errorf("read point opt: %w", err)
	}
	if info.LogTitle, err = r.cstring(); err != nil {
		return info, fmt.Errorf("read quest title: %w", err)
	}
	if info.LogDescription, err = r.cstring(); err != nil {
		return info, fmt.Errorf("read quest objectives: %w", err)
	}
	if info.QuestDescription, err = r.cstring(); err != nil {
		return info, fmt.Errorf("read quest details: %w", err)
	}
	if info.AreaDescription, err = r.cstring(); err != nil {
		return info, fmt.Errorf("read quest area description: %w", err)
	}
	if info.QuestCompletionLog, err = r.cstring(); err != nil {
		return info, fmt.Errorf("read quest completed text: %w", err)
	}
	type npcOrGO struct {
		id    int32
		count uint32
	}
	npcs := make([]npcOrGO, questCreatureObjectives)
	for index := range npcs {
		if npcs[index].id, err = r.i32(); err != nil {
			return info, fmt.Errorf("read req npc/go %d: %w", index, err)
		}
		if npcs[index].count, err = r.u32(); err != nil {
			return info, fmt.Errorf("read req npc/go count %d: %w", index, err)
		}
		if _, err = r.u32(); err != nil {
			return info, fmt.Errorf("read req source %d: %w", index, err)
		}
		if _, err = r.u32(); err != nil {
			return info, fmt.Errorf("read req source count %d: %w", index, err)
		}
	}
	type itemReq struct {
		id    uint32
		count uint32
	}
	items := make([]itemReq, questItemObjectives)
	for index := range items {
		if items[index].id, err = r.u32(); err != nil {
			return info, fmt.Errorf("read req item %d: %w", index, err)
		}
		if items[index].count, err = r.u32(); err != nil {
			return info, fmt.Errorf("read req item count %d: %w", index, err)
		}
	}
	texts := make([]string, questCreatureObjectives)
	for index := range texts {
		if texts[index], err = r.cstring(); err != nil {
			return info, fmt.Errorf("read objective text %d: %w", index, err)
		}
	}
	// 3.4.3 reads objective progress as ObjectiveProgress[StorageIndex]
	// (TrinityCore wotlk_classic GetQuestSlotObjectiveData), and AzerothCore
	// writes kill credit at the raw RequiredNpcOrGo slot: KilledMonsterCredit
	// stores CreatureOrGOCount[j], and the quest log carries that field pair
	// positionally into QuestLog.ObjectiveProgress[j]. StorageIndex must
	// therefore be the template slot itself — compacting over non-empty
	// objectives shifts progress off by the number of leading empty slots
	// (quest 6681 The Manor, Ravenholdt: RequiredNpcOrGo1=0, credit in slot 1
	// granted by areatrigger 3066, so a compacted index 0 reads progress 0/1
	// forever). Item requirements never occupy legacy counter slots and the
	// client counts them from the bag, so they use a disjoint index space
	// after the four NPC/GO slots.
	for index, npc := range npcs {
		if npc.id == 0 || npc.count == 0 {
			continue
		}
		storage := int8(index)
		objective := questObjective{ID: questObjectiveWireID(entry, storage), StorageIndex: storage, Amount: int32(npc.count), Description: texts[index]}
		if npc.id < 0 {
			objective.Type = questObjectiveGameObject
			objective.ObjectID = -npc.id
		} else {
			objective.Type = questObjectiveMonster
			objective.ObjectID = npc.id
		}
		info.Objectives = append(info.Objectives, objective)
	}
	for index, item := range items {
		if item.id == 0 || item.count == 0 {
			continue
		}
		storage := int8(questCreatureObjectives + index)
		info.Objectives = append(info.Objectives, questObjective{
			ID:           questObjectiveWireID(entry, storage),
			Type:         questObjectiveItem,
			StorageIndex: storage,
			ObjectID:     int32(item.id),
			Amount:       int32(item.count),
		})
	}
	// Empty legacy templates still need a completion predicate for modern
	// gossip (12700 is complete on the server but appears incomplete without
	// one). Unlike Hermes/legacy proxy's zero-objective conversion, retain a synthetic
	// flag objective. Hide non-exploration objectives: a visible synthetic row
	// has no matching Questie ObjectiveData (12687). Hidden=8 is defined in
	// Hermes QuestObjectiveFlags and means never displayed in the quest log.
	if len(info.Objectives) == 0 || info.Flags&legacyQuestFlagExploration != 0 {
		// AreaTrigger uses a StateFlags bit, independently of the numeric
		// kill counters. Keep bit index 0, but reserve a distinct objective ID
		// for mixed quests so it cannot collide with an item or kill ID.
		idSlot := int8(0)
		if len(info.Objectives) != 0 {
			idSlot = questCreatureObjectives + questItemObjectives
		}
		flags := uint32(0)
		if info.Flags&legacyQuestFlagExploration == 0 {
			flags = questObjectiveFlagHidden
		}
		info.Objectives = append(info.Objectives, questObjective{
			ID:           questObjectiveWireID(entry, idSlot),
			Type:         questObjectiveAreaTrigger,
			StorageIndex: 0,
			Amount:       1,
			Flags:        flags,
		})
	}
	return info, nil
}

func encodeQuestQueryResponse(entry uint32, allow bool, info questQueryInfo) []byte {
	body := binary.LittleEndian.AppendUint32(nil, entry)
	bits := newBitWriter(body)
	bits.writeBit(allow)
	body = bits.flush()
	if !allow {
		return body
	}
	if info.QuestID == 0 {
		info.QuestID = entry
	}
	body = binary.LittleEndian.AppendUint32(body, info.QuestID)
	body = binary.LittleEndian.AppendUint32(body, uint32(info.QuestType))
	body = binary.LittleEndian.AppendUint32(body, uint32(info.QuestLevel))
	body = binary.LittleEndian.AppendUint32(body, 0) // QuestScalingFactionGroup
	body = binary.LittleEndian.AppendUint32(body, 0) // QuestMaxScalingLevel
	body = binary.LittleEndian.AppendUint32(body, 0) // QuestPackageID
	body = binary.LittleEndian.AppendUint32(body, uint32(info.MinLevel))
	body = binary.LittleEndian.AppendUint32(body, uint32(info.QuestSortID))
	body = binary.LittleEndian.AppendUint32(body, info.QuestInfoID)
	body = binary.LittleEndian.AppendUint32(body, info.SuggestedGroupNum)
	body = binary.LittleEndian.AppendUint32(body, info.RewardNextQuest)
	body = binary.LittleEndian.AppendUint32(body, info.RewardXPDifficulty)
	body = appendFloat32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, uint32(info.RewardMoney))
	body = binary.LittleEndian.AppendUint32(body, 0) // RewardMoneyDifficulty
	body = appendFloat32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, info.RewardBonusMoney)
	for range questRewardDisplaySpellCount {
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, info.RewardSpell)
	body = binary.LittleEndian.AppendUint32(body, uint32(info.RewardHonor))
	body = appendFloat32(body, info.RewardKillHonor)
	body = binary.LittleEndian.AppendUint32(body, 0) // RewardArtifactXPDifficulty
	body = appendFloat32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0) // RewardArtifactCategoryID
	body = binary.LittleEndian.AppendUint32(body, info.StartItem)
	body = binary.LittleEndian.AppendUint32(body, info.Flags)
	body = binary.LittleEndian.AppendUint32(body, 0) // FlagsEx
	body = binary.LittleEndian.AppendUint32(body, 0) // FlagsEx2
	for index := 0; index < questRewardItemCount; index++ {
		body = binary.LittleEndian.AppendUint32(body, info.RewardItems[index])
		body = binary.LittleEndian.AppendUint32(body, info.RewardAmounts[index])
		body = binary.LittleEndian.AppendUint32(body, 0)
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	for index := 0; index < questRewardChoiceCount; index++ {
		body = binary.LittleEndian.AppendUint32(body, info.RewardChoiceItems[index])
		body = binary.LittleEndian.AppendUint32(body, info.RewardChoiceAmounts[index])
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, info.POIContinent)
	body = appendFloat32(body, info.POIx)
	body = appendFloat32(body, info.POIy)
	body = binary.LittleEndian.AppendUint32(body, info.POIPriority)
	body = binary.LittleEndian.AppendUint32(body, info.RewardTitle)
	body = binary.LittleEndian.AppendUint32(body, uint32(info.RewardArenaPoints))
	body = binary.LittleEndian.AppendUint32(body, info.RewardSkillLineID)
	body = binary.LittleEndian.AppendUint32(body, info.RewardNumSkillUps)
	body = binary.LittleEndian.AppendUint32(body, 0) // PortraitGiver
	body = binary.LittleEndian.AppendUint32(body, 0) // PortraitGiverMount
	body = binary.LittleEndian.AppendUint32(body, 0) // PortraitGiverModelSceneID
	body = binary.LittleEndian.AppendUint32(body, 0) // PortraitTurnIn
	for index := 0; index < questRewardFactionCount; index++ {
		body = binary.LittleEndian.AppendUint32(body, info.RewardFactionID[index])
		body = binary.LittleEndian.AppendUint32(body, uint32(info.RewardFactionValue[index]))
		body = binary.LittleEndian.AppendUint32(body, uint32(info.RewardFactionOverride[index]))
		body = binary.LittleEndian.AppendUint32(body, 0) // RewardFactionCapIn
	}
	body = binary.LittleEndian.AppendUint32(body, 0) // RewardFactionFlags
	for range questRewardCurrencyCount {
		body = binary.LittleEndian.AppendUint32(body, 0)
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, 0) // AcceptedSoundKitID
	body = binary.LittleEndian.AppendUint32(body, 0) // CompleteSoundKitID
	body = binary.LittleEndian.AppendUint32(body, 0) // AreaGroupID
	body = binary.LittleEndian.AppendUint64(body, 0) // TimeAllowed
	body = binary.LittleEndian.AppendUint32(body, uint32(len(info.Objectives)))
	body = binary.LittleEndian.AppendUint64(body, ^uint64(0)) // AllowableRaces
	body = binary.LittleEndian.AppendUint32(body, 0)          // TreasurePickerID
	body = binary.LittleEndian.AppendUint32(body, uint32(wotlkExpansion))
	body = binary.LittleEndian.AppendUint32(body, 0) // ManagedWorldStateID
	body = binary.LittleEndian.AppendUint32(body, 0) // QuestSessionBonus
	body = binary.LittleEndian.AppendUint32(body, 0) // QuestGiverCreatureID
	bits = newBitWriter(body)
	bits.writeBits(uint32(len(info.LogTitle)), 9)
	bits.writeBits(uint32(len(info.LogDescription)), 12)
	bits.writeBits(uint32(len(info.QuestDescription)), 12)
	bits.writeBits(uint32(len(info.AreaDescription)), 9)
	bits.writeBits(0, 10) // PortraitGiverText
	bits.writeBits(0, 8)  // PortraitGiverName
	bits.writeBits(0, 10) // PortraitTurnInText
	bits.writeBits(0, 8)  // PortraitTurnInName
	bits.writeBits(uint32(len(info.QuestCompletionLog)), 11)
	bits.writeBit(false) // ReadyForTranslation; legacy proxy 54261 and Hermes QueryQuestInfoResponse.Write
	body = bits.flush()
	for _, objective := range info.Objectives {
		body = encodeQuestObjective(body, objective)
	}
	body = append(body, info.LogTitle...)
	body = append(body, info.LogDescription...)
	body = append(body, info.QuestDescription...)
	body = append(body, info.AreaDescription...)
	return append(body, info.QuestCompletionLog...)
}

func ParseLegacyQuestUpdateID(body []byte) (uint32, error) {
	if len(body) < 4 {
		return 0, fmt.Errorf("quest-update has %d bytes, want at least 4", len(body))
	}
	return binary.LittleEndian.Uint32(body[:4]), nil
}

func EncodeQuestUpdateID(questID uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, questID)
}

func ParseLegacyQuestGiverQuestFailed(body []byte) (uint32, uint32, error) {
	if len(body) != 8 {
		return 0, 0, fmt.Errorf("quest-giver-failed has %d bytes, want 8", len(body))
	}
	return binary.LittleEndian.Uint32(body[:4]), binary.LittleEndian.Uint32(body[4:]), nil
}

func EncodeQuestGiverQuestFailed(questID, reason uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, questID)
	modernReason := uint32(ModernInventoryResult(int32(reason)))
	return binary.LittleEndian.AppendUint32(body, modernReason)
}

func ParseLegacyQuestLogFull(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("quest-log-full has %d bytes, want 0", len(body))
	}
	return nil
}

func ParseQuestConfirmAccept(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("quest-confirm-accept has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

type QuestPushResultRequest struct {
	Sender  GUID128
	QuestID uint32
	Result  uint8
}

func ParseQuestPushResult(body []byte) (QuestPushResultRequest, error) {
	var request QuestPushResultRequest
	r := movementReader{data: body}
	var err error
	if request.Sender, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read quest-push sender: %w", err)
	}
	if request.QuestID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read quest-push quest: %w", err)
	}
	if request.Result, err = r.u8(); err != nil {
		return request, fmt.Errorf("read quest-push result: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("quest-push-result has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyQuestPushResult(sender uint64, questID uint32, result uint8) []byte {
	// Hermes WorldSocket.cs:3760-3766 drops QuestID; AzerothCore still reads it.
	body := binary.LittleEndian.AppendUint64(nil, sender)
	body = binary.LittleEndian.AppendUint32(body, questID)
	return append(body, LegacyQuestPushReason(result))
}

func ParseLegacyQuestPushResult(body []byte) (uint64, uint8, error) {
	switch len(body) {
	case 9:
		return binary.LittleEndian.Uint64(body[:8]), body[8], nil
	case 12:
		result := binary.LittleEndian.Uint32(body[8:])
		if result > 0xff {
			return 0, 0, fmt.Errorf("quest-push-result reason %d exceeds uint8", result)
		}
		return binary.LittleEndian.Uint64(body[:8]), uint8(result), nil
	default:
		return 0, 0, fmt.Errorf("quest-push-result has %d bytes, want 9 or 12", len(body))
	}
}

func EncodeQuestPushResult(sender GUID128, result uint8) []byte {
	// 54261 is PackedGuid128 + u8 Result. Do not append the 3.4.4 bits(9) QuestTitle trailer.
	body := appendPackedGUID128(nil, sender.Low, sender.High)
	return append(body, ModernQuestPushReason(result))
}

func ParseLegacyQuestConfirmAccept(body []byte) (uint32, string, uint64, error) {
	r := movementReader{data: body}
	questID, err := r.u32()
	if err != nil {
		return 0, "", 0, fmt.Errorf("read quest-confirm-accept quest: %w", err)
	}
	title, err := r.cstring()
	if err != nil {
		return 0, "", 0, fmt.Errorf("read quest-confirm-accept title: %w", err)
	}
	initiator, err := r.u64()
	if err != nil {
		return 0, "", 0, fmt.Errorf("read quest-confirm-accept initiator: %w", err)
	}
	if r.remaining() != 0 {
		return 0, "", 0, fmt.Errorf("quest-confirm-accept has %d trailing bytes", r.remaining())
	}
	return questID, title, initiator, nil
}

func EncodeQuestConfirmAccept(questID uint32, initiatedBy GUID128, title string) []byte {
	if len(title) > maxQuestConfirmAcceptTitle {
		title = title[:maxQuestConfirmAcceptTitle]
	}
	body := binary.LittleEndian.AppendUint32(nil, questID)
	body = appendPackedGUID128(body, initiatedBy.Low, initiatedBy.High)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(title)), 10)
	body = bits.flush()
	return append(body, title...)
}

func ParseLegacyQuestGiverInvalidQuest(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("quest-giver-invalid has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodeQuestGiverInvalidQuest(reason uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, reason)
	body = binary.LittleEndian.AppendUint32(body, 0) // ContributionRewardID
	bits := newBitWriter(body)
	bits.writeBit(true)  // SendErrorMessage
	bits.writeBits(0, 9) // empty ReasonText
	return bits.flush()
}

// modernQuestPushReason343 maps AzerothCore QuestShareMessages onto 54261
// QuestPushReason. TooFar=4 and Dead=6 sit ahead of the later AC results, so
// Busy becomes 5 and each following AC result shifts by two.
var modernQuestPushReason343 = [...]uint8{
	questPushSuccess,      // Sharing
	questPushInvalid,      // CantTake
	questPushAccepted,     // Accept — keep 2, never shift onto Declined=3
	questPushDeclined,     // Decline
	questPushBusy,         // Busy
	questPushLogFull,      // LogFull
	questPushOnQuest,      // HaveQuest
	questPushAlreadyDone,  // Finish
	questPushNotDaily,     // NotDaily
	questPushTimerExpired, // Timer
	questPushNotInParty,   // NotInParty
}

// ModernQuestPushReason maps AzerothCore QuestShareMessages 0–10 onto 54261
// QuestPushReason. HaveQuest=6 becomes OnQuest=8. Wire 6 is Dead and wire 7 is
// LogFull; wire 10 is the "cannot share today" string.
func ModernQuestPushReason(legacy uint8) uint8 {
	if int(legacy) < len(modernQuestPushReason343) {
		return modernQuestPushReason343[legacy]
	}
	return questPushBusy
}

// LegacyQuestPushReason is the reverse map. 54261 TooFar=4 and Dead=6 have no
// WotLK twin, so both become Busy=4.
func LegacyQuestPushReason(modern uint8) uint8 {
	switch modern {
	case questPushSuccess:
		return questShareSharing
	case questPushInvalid:
		return questShareInvalid
	case questPushAccepted:
		return questShareAccepted
	case questPushDeclined:
		return questShareDeclined
	case questPushBusy, questPushTooFar, questPushDead:
		return questShareBusy
	case questPushLogFull:
		return questShareLogFull
	case questPushOnQuest:
		return questShareHaveQuest
	case questPushAlreadyDone:
		return questShareFinish
	case questPushNotDaily:
		return questShareNotDaily
	case questPushTimerExpired:
		return questShareTimer
	case questPushNotInParty:
		return questShareNotInParty
	default:
		return questShareBusy
	}
}

type QuestUpdateAddCredit struct {
	Victim        uint64
	QuestID       uint32
	ObjectID      int32
	Count         uint16
	Required      uint16
	ObjectiveType byte
}

func ParseLegacyQuestUpdateAddKill(body []byte) (QuestUpdateAddCredit, error) {
	var credit QuestUpdateAddCredit
	r := movementReader{data: body}
	var err error
	if credit.QuestID, err = r.u32(); err != nil {
		return credit, fmt.Errorf("read add-kill quest: %w", err)
	}
	entry, err := r.u32()
	if err != nil {
		return credit, fmt.Errorf("read add-kill entry: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return credit, fmt.Errorf("read add-kill count: %w", err)
	}
	required, err := r.u32()
	if err != nil {
		return credit, fmt.Errorf("read add-kill required: %w", err)
	}
	switch {
	case r.remaining() == 8:
		if credit.Victim, err = r.u64(); err != nil {
			return credit, fmt.Errorf("read add-kill victim: %w", err)
		}
	case r.remaining() > 0:
		if credit.Victim, err = r.guid64(); err != nil {
			return credit, fmt.Errorf("read add-kill victim: %w", err)
		}
	}
	if r.remaining() != 0 {
		return credit, fmt.Errorf("add-kill has %d trailing bytes", r.remaining())
	}
	credit.Count = uint16(count)
	credit.Required = uint16(required)
	if entry&legacyQuestUpdateGOFlag != 0 {
		credit.ObjectID = int32(entry &^ legacyQuestUpdateGOFlag)
		credit.ObjectiveType = questObjectiveGameObject
	} else {
		credit.ObjectID = int32(entry)
		credit.ObjectiveType = questObjectiveMonster
	}
	return credit, nil
}

func EncodeQuestUpdateAddCredit(guid GUID128, credit QuestUpdateAddCredit) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, credit.QuestID)
	body = binary.LittleEndian.AppendUint32(body, uint32(credit.ObjectID))
	body = binary.LittleEndian.AppendUint16(body, credit.Count)
	body = binary.LittleEndian.AppendUint16(body, credit.Required)
	return append(body, credit.ObjectiveType)
}

func encodeQuestObjective(body []byte, objective questObjective) []byte {
	body = binary.LittleEndian.AppendUint32(body, objective.ID)
	// legacy proxy's 54261 writer uses one byte for both fields.
	body = append(body, objective.Type, byte(objective.StorageIndex))
	body = binary.LittleEndian.AppendUint32(body, uint32(objective.ObjectID))
	body = binary.LittleEndian.AppendUint32(body, uint32(objective.Amount))
	body = binary.LittleEndian.AppendUint32(body, objective.Flags)
	body = binary.LittleEndian.AppendUint32(body, 0) // Flags2
	body = appendFloat32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0) // VisualEffects count
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(objective.Description)), 8)
	body = bits.flush()
	return append(body, objective.Description...)
}
