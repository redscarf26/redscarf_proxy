package modernworld

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const (
	legacyCreatureQueryNotFound = uint32(0x80000000)
	modernGameObjectDataFields  = 35
	legacyGameObjectDataFields  = 24
	legacyCreatureQuestItems    = 6
	maxCreatureNames            = 4
)

type CreatureDisplay struct {
	ID          uint32
	Scale       float32
	Probability float32
}

type BroadcastTextRecord struct {
	ID          uint32
	MaleText    string
	FemaleText  string
	Language    uint32
	Emotes      [3]uint16
	EmoteDelays [3]uint16
}

type CreatureQueryStats struct {
	Names          [maxCreatureNames]string
	Title          string
	CursorName     string
	Flags          uint32
	Type           int32
	Family         int32
	Classification int32
	KillCredit     [2]uint32
	Displays       []CreatureDisplay
	HPMulti        float32
	EnergyMulti    float32
	Leader         bool
	QuestItems     []uint32
	MovementInfoID uint32
}

type GameObjectQueryStats struct {
	Type           uint32
	DisplayID      uint32
	Names          [4]string
	IconName       string
	CastBarCaption string
	UnkString      string
	Data           [legacyGameObjectDataFields]int32
	Size           float32
	QuestItems     []uint32
}

func ParseQueryCreature(body []byte) (uint32, error) {
	if len(body) < 4 {
		return 0, fmt.Errorf("query-creature has %d bytes, want at least 4", len(body))
	}
	return binary.LittleEndian.Uint32(body[:4]), nil
}

func CreatureEntryFromLegacy(guid uint64, fields map[int]uint32) uint32 {
	if fields != nil {
		if entry := fields[legacyObjectEntry]; entry != 0 {
			return entry
		}
	}
	switch uint16(guid >> 48) {
	case 0xf130, 0xf140, 0xf150:
		return uint32((guid >> 24) & 0xffffff)
	}
	return 0
}

func ParseQueryNpcText(body []byte) (uint32, GUID128, error) {
	if len(body) < 4 {
		return 0, GUID128{}, fmt.Errorf("query-npc-text has %d bytes, want at least 4", len(body))
	}
	textID := binary.LittleEndian.Uint32(body[:4])
	guid, err := ParsePackedGUID128Exact(body[4:])
	if err != nil {
		return 0, GUID128{}, fmt.Errorf("query-npc-text GUID: %w", err)
	}
	return textID, guid, nil
}

func EncodeLegacyNpcTextQuery(textID uint32, guid uint64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, textID)
	return binary.LittleEndian.AppendUint64(body, guid)
}

func TranslateNpcTextResponse(legacy []byte) ([]byte, error) {
	body, _, err := TranslateNpcTextResponseWithBroadcasts(legacy)
	return body, err
}

func TranslateNpcTextResponseWithBroadcasts(legacy []byte) ([]byte, []BroadcastTextRecord, error) {
	r := movementReader{data: legacy}
	masked, err := r.u32()
	if err != nil {
		return nil, nil, fmt.Errorf("read npc-text id: %w", err)
	}
	textID := masked &^ legacyCreatureQueryNotFound
	allow := masked&legacyCreatureQueryNotFound == 0
	body := binary.LittleEndian.AppendUint32(nil, textID)
	bits := newBitWriter(body)
	bits.writeBit(allow)
	body = bits.flush()
	if !allow {
		if r.remaining() != 0 {
			return nil, nil, fmt.Errorf("missing npc-text has %d trailing bytes", r.remaining())
		}
		return binary.LittleEndian.AppendUint32(body, 0), nil, nil
	}
	probabilities := make([]float32, npcTextSlots)
	broadcastIDs := make([]uint32, npcTextSlots)
	records := make([]BroadcastTextRecord, 0, npcTextSlots)
	for index := 0; index < npcTextSlots; index++ {
		if probabilities[index], err = r.f32(); err != nil {
			return nil, nil, fmt.Errorf("read npc-text probability %d: %w", index, err)
		}
		maleText, readErr := r.cstring()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read npc-text 0 slot %d: %w", index, readErr)
		}
		femaleText, readErr := r.cstring()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read npc-text 1 slot %d: %w", index, readErr)
		}
		language, readErr := r.u32()
		if readErr != nil {
			return nil, nil, fmt.Errorf("read npc-text language %d: %w", index, readErr)
		}
		var emoteDelays [3]uint16
		var emotes [3]uint16
		for emote := 0; emote < 3; emote++ {
			delay, readErr := r.u32()
			if readErr != nil {
				return nil, nil, fmt.Errorf("read npc-text delay %d/%d: %w", index, emote, readErr)
			}
			emoteID, readErr := r.u32()
			if readErr != nil {
				return nil, nil, fmt.Errorf("read npc-text emote %d/%d: %w", index, emote, readErr)
			}
			emoteDelays[emote] = uint16(delay)
			emotes[emote] = uint16(emoteID)
		}
		maleText = strings.TrimRight(maleText, " \t\r\n\x00")
		femaleText = strings.TrimRight(femaleText, " \t\r\n\x00")
		if (maleText == "" && femaleText == "") || (index != 0 && maleText == "Greetings $N" && femaleText == "Greetings $N") {
			continue
		}
		id, idErr := dynamicBroadcastTextID(textID, index)
		if idErr != nil {
			return nil, nil, idErr
		}
		record := BroadcastTextRecord{ID: id, MaleText: maleText, FemaleText: femaleText, Language: language, Emotes: emotes, EmoteDelays: emoteDelays}
		broadcastIDs[index] = id
		records = append(records, record)
	}
	if r.remaining() != 0 {
		return nil, nil, fmt.Errorf("npc-text has %d trailing bytes", r.remaining())
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(npcTextSlots*(4+4)))
	for _, probability := range probabilities {
		body = appendFloat32(body, probability)
	}
	for _, id := range broadcastIDs {
		body = binary.LittleEndian.AppendUint32(body, id)
	}
	return body, records, nil
}

func dynamicBroadcastTextID(textID uint32, slot int) (uint32, error) {
	const firstDynamicBroadcastText = uint64(1_000_000_000)
	offset := uint64(textID)*npcTextSlots + uint64(slot)
	if firstDynamicBroadcastText+offset > uint64(^uint32(0)>>1) {
		return 0, fmt.Errorf("npc-text id %d cannot be mapped to a dynamic BroadcastText record", textID)
	}
	return uint32(firstDynamicBroadcastText + offset), nil
}

func EncodeLegacyCreatureQuery(entry uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, entry)
	return binary.LittleEndian.AppendUint64(body, 0)
}

func ParseQueryGameObject(body []byte) (uint32, GUID128, error) {
	if len(body) < 4 {
		return 0, GUID128{}, fmt.Errorf("query-game-object has %d bytes, want at least 4", len(body))
	}
	entry := binary.LittleEndian.Uint32(body[:4])
	guid, err := ParsePackedGUID128Exact(body[4:])
	if err != nil {
		return 0, GUID128{}, fmt.Errorf("query-game-object GUID: %w", err)
	}
	return entry, guid, nil
}

func EncodeLegacyGameObjectQuery(entry uint32, guid uint64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, entry)
	return binary.LittleEndian.AppendUint64(body, guid)
}

func TranslateCreatureQueryResponse(legacy []byte) ([]byte, error) {
	entry, found, stats, err := parseLegacyCreatureQuery(legacy)
	if err != nil {
		return nil, err
	}
	return encodeCreatureQueryResponse(entry, found, stats), nil
}

// LegacyCreatureQueryFirstDisplay returns the first non-zero display ID from a
// WotLK creature-query response. Missing templates and empty display lists
// return display 0 without error so the stable list can still be sent.
func LegacyCreatureQueryFirstDisplay(legacy []byte) (entry, display uint32, err error) {
	entry, found, stats, err := parseLegacyCreatureQuery(legacy)
	if err != nil || !found || len(stats.Displays) == 0 {
		return entry, 0, err
	}
	return entry, stats.Displays[0].ID, nil
}

func parseLegacyCreatureQuery(legacy []byte) (uint32, bool, CreatureQueryStats, error) {
	r := movementReader{data: legacy}
	masked, err := r.u32()
	if err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature entry: %w", err)
	}
	entry := masked &^ legacyCreatureQueryNotFound
	if masked&legacyCreatureQueryNotFound != 0 {
		if r.remaining() != 0 {
			return 0, false, CreatureQueryStats{}, fmt.Errorf("missing creature query has %d trailing bytes", r.remaining())
		}
		return entry, false, CreatureQueryStats{}, nil
	}
	var stats CreatureQueryStats
	for index := range stats.Names {
		name, readErr := r.cstring()
		if readErr != nil {
			return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature name %d: %w", index, readErr)
		}
		stats.Names[index] = name
	}
	if stats.Title, err = r.cstring(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature title: %w", err)
	}
	if stats.CursorName, err = r.cstring(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature cursor: %w", err)
	}
	if stats.Flags, err = r.u32(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature flags: %w", err)
	}
	if stats.Type, err = r.i32(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature type: %w", err)
	}
	if stats.Family, err = r.i32(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature family: %w", err)
	}
	if stats.Classification, err = r.i32(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature rank: %w", err)
	}
	for index := range stats.KillCredit {
		if stats.KillCredit[index], err = r.u32(); err != nil {
			return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature kill-credit %d: %w", index, err)
		}
	}
	for index := 0; index < 4; index++ {
		displayID, readErr := r.u32()
		if readErr != nil {
			return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature display %d: %w", index, readErr)
		}
		if displayID == 0 {
			continue
		}
		stats.Displays = append(stats.Displays, CreatureDisplay{ID: displayID, Scale: 1, Probability: 100})
	}
	if stats.HPMulti, err = r.f32(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature health modifier: %w", err)
	}
	if stats.EnergyMulti, err = r.f32(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature mana modifier: %w", err)
	}
	leader, err := r.u8()
	if err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature leader: %w", err)
	}
	stats.Leader = leader != 0
	for index := 0; index < legacyCreatureQuestItems; index++ {
		itemID, readErr := r.u32()
		if readErr != nil {
			return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature quest item %d: %w", index, readErr)
		}
		if itemID != 0 {
			stats.QuestItems = append(stats.QuestItems, itemID)
		}
	}
	if _, err = r.u32(); err != nil {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("read creature movement id: %w", err)
	}
	if r.remaining() != 0 {
		return 0, false, CreatureQueryStats{}, fmt.Errorf("creature query has %d trailing bytes", r.remaining())
	}
	stats.MovementInfoID = 1693
	return entry, true, stats, nil
}

func encodeCreatureQueryResponse(entry uint32, allow bool, stats CreatureQueryStats) []byte {
	body := binary.LittleEndian.AppendUint32(nil, entry)
	allowBits := newBitWriter(body)
	allowBits.writeBit(allow)
	body = allowBits.flush()
	if !allow {
		return body
	}
	bits := newBitWriter(body)
	bits.writeBits(optionalCStringBits(stats.Title), 11)
	bits.writeBits(0, 11) // TitleAlt
	bits.writeBits(optionalCStringBits(stats.CursorName), 6)
	bits.writeBit(false) // Civilian
	bits.writeBit(stats.Leader)
	for index := 0; index < maxCreatureNames; index++ {
		bits.writeBits(uint32(len(stats.Names[index])+1), 11)
		bits.writeBits(1, 11) // empty NameAlt still reports a null
	}
	body = bits.flush()
	for index := 0; index < maxCreatureNames; index++ {
		if stats.Names[index] != "" {
			body = appendCString(body, stats.Names[index])
		}
	}
	body = binary.LittleEndian.AppendUint32(body, stats.Flags)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, uint32(stats.Type))
	body = binary.LittleEndian.AppendUint32(body, uint32(stats.Family))
	body = binary.LittleEndian.AppendUint32(body, uint32(stats.Classification))
	body = binary.LittleEndian.AppendUint32(body, 0) // PetSpellDataId
	body = binary.LittleEndian.AppendUint32(body, stats.KillCredit[0])
	body = binary.LittleEndian.AppendUint32(body, stats.KillCredit[1])
	body = binary.LittleEndian.AppendUint32(body, uint32(len(stats.Displays)))
	var total float32
	for _, display := range stats.Displays {
		total += display.Probability
	}
	body = appendFloat32(body, total)
	for _, display := range stats.Displays {
		body = binary.LittleEndian.AppendUint32(body, display.ID)
		body = appendFloat32(body, display.Scale)
		body = appendFloat32(body, display.Probability)
	}
	body = appendFloat32(body, stats.HPMulti)
	body = appendFloat32(body, stats.EnergyMulti)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(stats.QuestItems)))
	body = binary.LittleEndian.AppendUint32(body, stats.MovementInfoID)
	body = binary.LittleEndian.AppendUint32(body, 0) // HealthScalingExpansion
	body = binary.LittleEndian.AppendUint32(body, 0) // RequiredExpansion
	body = binary.LittleEndian.AppendUint32(body, 0) // VignetteID
	body = binary.LittleEndian.AppendUint32(body, 1) // Class
	body = binary.LittleEndian.AppendUint32(body, 0) // DifficultyID
	body = binary.LittleEndian.AppendUint32(body, 0) // WidgetSetID
	body = binary.LittleEndian.AppendUint32(body, 0) // WidgetSetUnitConditionID
	if stats.Title != "" {
		body = appendCString(body, stats.Title)
	}
	if stats.CursorName != "" {
		body = appendCString(body, stats.CursorName)
	}
	for _, itemID := range stats.QuestItems {
		body = binary.LittleEndian.AppendUint32(body, itemID)
	}
	return body
}

func TranslateGameObjectQueryResponse(legacy []byte, guid GUID128) ([]byte, error) {
	r := movementReader{data: legacy}
	masked, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read game-object entry: %w", err)
	}
	entry := masked &^ legacyCreatureQueryNotFound
	body := binary.LittleEndian.AppendUint32(nil, entry)
	body = appendPackedGUID128(body, guid.Low, guid.High)
	bits := newBitWriter(body)
	if masked&legacyCreatureQueryNotFound != 0 {
		if r.remaining() != 0 {
			return nil, fmt.Errorf("missing game-object query has %d trailing bytes", r.remaining())
		}
		bits.writeBit(false)
		body = bits.flush()
		return binary.LittleEndian.AppendUint32(body, 0), nil
	}
	var stats GameObjectQueryStats
	if stats.Type, err = r.u32(); err != nil {
		return nil, fmt.Errorf("read game-object type: %w", err)
	}
	if stats.DisplayID, err = r.u32(); err != nil {
		return nil, fmt.Errorf("read game-object display: %w", err)
	}
	for index := range stats.Names {
		name, readErr := r.cstring()
		if readErr != nil {
			return nil, fmt.Errorf("read game-object name %d: %w", index, readErr)
		}
		stats.Names[index] = name
	}
	if stats.IconName, err = r.cstring(); err != nil {
		return nil, fmt.Errorf("read game-object icon: %w", err)
	}
	if stats.CastBarCaption, err = r.cstring(); err != nil {
		return nil, fmt.Errorf("read game-object cast-bar: %w", err)
	}
	if stats.UnkString, err = r.cstring(); err != nil {
		return nil, fmt.Errorf("read game-object unknown string: %w", err)
	}
	for index := range stats.Data {
		value, readErr := r.i32()
		if readErr != nil {
			return nil, fmt.Errorf("read game-object data %d: %w", index, readErr)
		}
		stats.Data[index] = value
	}
	if stats.Size, err = r.f32(); err != nil {
		return nil, fmt.Errorf("read game-object size: %w", err)
	}
	for index := 0; index < legacyCreatureQuestItems; index++ {
		itemID, readErr := r.u32()
		if readErr != nil {
			return nil, fmt.Errorf("read game-object quest item %d: %w", index, readErr)
		}
		if itemID != 0 {
			stats.QuestItems = append(stats.QuestItems, itemID)
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("game-object query has %d trailing bytes", r.remaining())
	}
	bits.writeBit(true)
	body = bits.flush()
	translateInstancePortalStats(entry, &stats)
	translateFrozenThronePlatformStats(entry, &stats)
	statsBody := encodeGameObjectStats(stats)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(statsBody)))
	return append(body, statsBody...), nil
}

func encodeGameObjectStats(stats GameObjectQueryStats) []byte {
	body := binary.LittleEndian.AppendUint32(nil, stats.Type)
	body = binary.LittleEndian.AppendUint32(body, stats.DisplayID)
	for _, name := range stats.Names {
		body = appendCString(body, name)
	}
	body = appendCString(body, stats.IconName)
	body = appendCString(body, stats.CastBarCaption)
	body = appendCString(body, stats.UnkString)
	for index := 0; index < modernGameObjectDataFields; index++ {
		value := int32(0)
		if index < len(stats.Data) {
			value = stats.Data[index]
		}
		body = binary.LittleEndian.AppendUint32(body, uint32(value))
	}
	body = appendFloat32(body, stats.Size)
	body = append(body, byte(len(stats.QuestItems)))
	for _, itemID := range stats.QuestItems {
		body = binary.LittleEndian.AppendUint32(body, itemID)
	}
	return binary.LittleEndian.AppendUint32(body, 0) // ContentTuningId
}

func optionalCStringBits(value string) uint32 {
	if value == "" {
		return 0
	}
	return uint32(len(value) + 1)
}

func appendCString(dst []byte, value string) []byte {
	dst = append(dst, value...)
	return append(dst, 0)
}
