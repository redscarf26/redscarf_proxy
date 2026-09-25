package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGCloseInteraction    = uint16(0x3493)
	CMSGGossipSelectOption  = uint16(0x3494)
	SMSGGossipComplete      = uint16(0x2A97)
	SMSGGossipMessage       = uint16(0x2A98)
	SMSGQuestGiverQuestList = uint16(0x2A9A)

	maxGossipOptions = 128
	maxGossipQuests  = 128
)

type GossipSelectRequest struct {
	Giver         GUID128
	GossipID      uint32
	OptionIndex   uint32
	PromotionCode string
}

type LegacyGossipOption struct {
	Index   uint32
	Icon    uint8
	Flags   uint8
	Cost    uint32
	Text    string
	Confirm string
}

type LegacyGossipQuest struct {
	QuestID    uint32
	QuestType  int32
	QuestLevel int32
	QuestFlags uint32
	Repeatable bool
	Title      string
}

type LegacyGossipMessage struct {
	Giver    uint64
	GossipID uint32
	TextID   uint32
	Options  []LegacyGossipOption
	Quests   []LegacyGossipQuest
}

type LegacyQuestGiverQuestList struct {
	Giver      uint64
	Greeting   string
	EmoteDelay uint32
	EmoteType  uint32
	Quests     []LegacyGossipQuest
}

func ParseGossipSelectOption(body []byte) (GossipSelectRequest, error) {
	var request GossipSelectRequest
	r := movementReader{data: body}
	var err error
	if request.Giver, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read gossip-select GUID: %w", err)
	}
	if request.GossipID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read gossip-select menu ID: %w", err)
	}
	if request.OptionIndex, err = r.u32(); err != nil {
		return request, fmt.Errorf("read gossip-select option: %w", err)
	}
	length, err := r.bits(8)
	if err != nil {
		return request, fmt.Errorf("read gossip-select code length: %w", err)
	}
	if request.PromotionCode, err = r.stringN(int(length)); err != nil {
		return request, fmt.Errorf("read gossip-select code: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("gossip-select has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyGossipSelectOption(giver uint64, gossipID, optionIndex uint32, promotionCode string) []byte {
	body := binary.LittleEndian.AppendUint64(nil, giver)
	body = binary.LittleEndian.AppendUint32(body, gossipID)
	body = binary.LittleEndian.AppendUint32(body, optionIndex)
	if promotionCode != "" {
		body = append(body, promotionCode...)
		body = append(body, 0)
	}
	return body
}

func ParseLegacyGossipMessage(body []byte) (LegacyGossipMessage, error) {
	var message LegacyGossipMessage
	r := movementReader{data: body}
	var err error
	if message.Giver, err = r.u64(); err != nil {
		return message, fmt.Errorf("read gossip giver: %w", err)
	}
	if message.GossipID, err = r.u32(); err != nil {
		return message, fmt.Errorf("read gossip menu ID: %w", err)
	}
	if message.TextID, err = r.u32(); err != nil {
		return message, fmt.Errorf("read gossip text ID: %w", err)
	}
	optionCount, err := r.u32()
	if err != nil {
		return message, fmt.Errorf("read gossip option count: %w", err)
	}
	if optionCount > maxGossipOptions {
		return message, fmt.Errorf("gossip option count %d exceeds %d", optionCount, maxGossipOptions)
	}
	message.Options = make([]LegacyGossipOption, 0, optionCount)
	for index := uint32(0); index < optionCount; index++ {
		var option LegacyGossipOption
		if option.Index, err = r.u32(); err != nil {
			return message, fmt.Errorf("read gossip option %d index: %w", index, err)
		}
		if option.Icon, err = r.u8(); err != nil {
			return message, fmt.Errorf("read gossip option %d icon: %w", index, err)
		}
		if option.Flags, err = r.u8(); err != nil {
			return message, fmt.Errorf("read gossip option %d flags: %w", index, err)
		}
		if option.Cost, err = r.u32(); err != nil {
			return message, fmt.Errorf("read gossip option %d cost: %w", index, err)
		}
		if option.Text, err = r.cstring(); err != nil {
			return message, fmt.Errorf("read gossip option %d text: %w", index, err)
		}
		if option.Confirm, err = r.cstring(); err != nil {
			return message, fmt.Errorf("read gossip option %d confirmation: %w", index, err)
		}
		message.Options = append(message.Options, option)
	}
	questCount, err := r.u32()
	if err != nil {
		return message, fmt.Errorf("read gossip quest count: %w", err)
	}
	if questCount > maxGossipQuests {
		return message, fmt.Errorf("gossip quest count %d exceeds %d", questCount, maxGossipQuests)
	}
	message.Quests = make([]LegacyGossipQuest, 0, questCount)
	for index := uint32(0); index < questCount; index++ {
		quest, readErr := parseLegacyGossipQuest(&r)
		if readErr != nil {
			return message, fmt.Errorf("read gossip quest %d: %w", index, readErr)
		}
		message.Quests = append(message.Quests, quest)
	}
	if r.remaining() != 0 {
		return message, fmt.Errorf("gossip message has %d trailing bytes", r.remaining())
	}
	return message, nil
}

func parseLegacyGossipQuest(r *movementReader) (LegacyGossipQuest, error) {
	var quest LegacyGossipQuest
	var err error
	if quest.QuestID, err = r.u32(); err != nil {
		return quest, err
	}
	questType, err := r.u32()
	if err != nil {
		return quest, err
	}
	quest.QuestType = int32(questType)
	questLevel, err := r.u32()
	if err != nil {
		return quest, err
	}
	quest.QuestLevel = int32(questLevel)
	if quest.QuestFlags, err = r.u32(); err != nil {
		return quest, err
	}
	repeatable, err := r.u8()
	if err != nil {
		return quest, err
	}
	quest.Repeatable = repeatable != 0
	if quest.Title, err = r.cstring(); err != nil {
		return quest, err
	}
	return quest, nil
}

func EncodeGossipMessage(giver GUID128, message LegacyGossipMessage) []byte {
	body := appendPackedGUID128(nil, giver.Low, giver.High)
	body = binary.LittleEndian.AppendUint32(body, message.GossipID)
	body = binary.LittleEndian.AppendUint32(body, 0) // friendship faction
	body = binary.LittleEndian.AppendUint32(body, uint32(len(message.Options)))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(message.Quests)))
	bits := newBitWriter(body)
	bits.writeBit(true)
	bits.writeBit(false)
	body = bits.flush()
	for _, option := range message.Options {
		body = binary.LittleEndian.AppendUint32(body, option.Index)
		body = append(body, option.Icon, option.Flags)
		body = binary.LittleEndian.AppendUint32(body, option.Cost)
		body = binary.LittleEndian.AppendUint32(body, 0) // language
		body = binary.LittleEndian.AppendUint32(body, 0)
		body = binary.LittleEndian.AppendUint32(body, option.Index)
		bits = newBitWriter(body)
		bits.writeBits(uint32(len(option.Text)), 12)
		bits.writeBits(uint32(len(option.Confirm)), 12)
		bits.writeBits(0, 2) // available
		bits.writeBit(false) // spell ID
		bits.writeBit(false)
		body = bits.flush()
		body = binary.LittleEndian.AppendUint32(body, 0) // treasure item count
		body = append(body, option.Text...)
		body = append(body, option.Confirm...)
	}
	body = binary.LittleEndian.AppendUint32(body, message.TextID)
	for _, quest := range message.Quests {
		body = appendModernWotLKGossipQuest(body, quest)
	}
	return body
}

func ParseLegacyQuestGiverQuestList(body []byte) (LegacyQuestGiverQuestList, error) {
	var list LegacyQuestGiverQuestList
	r := movementReader{data: body}
	var err error
	if list.Giver, err = r.u64(); err != nil {
		return list, fmt.Errorf("read quest-list giver: %w", err)
	}
	if list.Greeting, err = r.cstring(); err != nil {
		return list, fmt.Errorf("read quest-list greeting: %w", err)
	}
	if list.EmoteDelay, err = r.u32(); err != nil {
		return list, fmt.Errorf("read quest-list emote delay: %w", err)
	}
	if list.EmoteType, err = r.u32(); err != nil {
		return list, fmt.Errorf("read quest-list emote type: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return list, fmt.Errorf("read quest-list count: %w", err)
	}
	if count > maxGossipQuests {
		return list, fmt.Errorf("quest-list count %d exceeds %d", count, maxGossipQuests)
	}
	list.Quests = make([]LegacyGossipQuest, 0, count)
	for index := uint8(0); index < count; index++ {
		quest, readErr := parseLegacyGossipQuest(&r)
		if readErr != nil {
			return list, fmt.Errorf("read quest-list entry %d: %w", index, readErr)
		}
		list.Quests = append(list.Quests, quest)
	}
	if r.remaining() != 0 {
		return list, fmt.Errorf("quest-list has %d trailing bytes", r.remaining())
	}
	return list, nil
}

func EncodeQuestGiverQuestList(giver GUID128, list LegacyQuestGiverQuestList) []byte {
	body := appendPackedGUID128(nil, giver.Low, giver.High)
	body = binary.LittleEndian.AppendUint32(body, list.EmoteDelay)
	body = binary.LittleEndian.AppendUint32(body, list.EmoteType)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(list.Quests)))
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(list.Greeting)), 11)
	body = bits.flush()
	for _, quest := range list.Quests {
		body = appendModernGossipQuest(body, quest)
	}
	return append(body, list.Greeting...)
}

func appendModernWotLKGossipQuest(body []byte, quest LegacyGossipQuest) []byte {
	return appendModernGossipQuest(body, quest)
}

func appendModernGossipQuest(body []byte, quest LegacyGossipQuest) []byte {
	body = binary.LittleEndian.AppendUint32(body, quest.QuestID)
	body = binary.LittleEndian.AppendUint32(body, 0) // content tuning ID
	body = binary.LittleEndian.AppendUint32(body, uint32(quest.QuestType))
	body = binary.LittleEndian.AppendUint32(body, uint32(quest.QuestLevel))
	body = binary.LittleEndian.AppendUint32(body, 255) // quest max level
	body = binary.LittleEndian.AppendUint32(body, quest.QuestFlags)
	body = binary.LittleEndian.AppendUint32(body, 0) // quest flags ex
	bits := newBitWriter(body)
	bits.writeBit(quest.Repeatable)
	bits.writeBit(false) // Important; present in both GossipMessage and QuestList
	bits.writeBits(uint32(len(quest.Title)), 9)
	body = bits.flush()
	return append(body, quest.Title...)
}

func EncodeGossipComplete() []byte { return []byte{0} }
