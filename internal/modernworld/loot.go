package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGLootUnit                = uint16(12815)
	CMSGLootMoney               = uint16(12816)
	CMSGLootItem                = uint16(12817)
	CMSGLootRelease             = uint16(12819)
	CMSGLootRoll                = uint16(0x3214)
	CMSGMasterLootItem          = uint16(0x3212)
	SMSGLootResponse            = uint16(9748)
	SMSGLootRemoved             = uint16(9749)
	SMSGCoinRemoved             = uint16(9751)
	SMSGLootRelease             = uint16(9755)
	SMSGLootMoneyNotify         = uint16(9756)
	SMSGStartLootRoll           = uint16(0x261d)
	SMSGLootRoll                = uint16(0x261e)
	SMSGLootRollsComplete       = uint16(0x2620)
	SMSGLootAllPassed           = uint16(0x2621)
	SMSGLootRollWon             = uint16(0x2622)
	SMSGMasterLootCandidateList = uint16(0x261f)
	SMSGItemPushResult          = uint16(9763)

	maxLootItems = 32

	lootAcquireCorpse     = uint32(1)
	lootMethodFreeForAll  = uint32(0)
	lootThresholdUncommon = uint32(2)
)

type LootRollKey struct {
	Slot     uint8
	ItemID   uint32
	Suffix   uint32
	Property uint32
}

type LootRollRequest struct {
	LootObj  GUID128
	Slot     uint8
	RollType uint8
}

type MasterLootRequest struct {
	Target GUID128
	Items  []MasterLootItem
}

type MasterLootItem struct {
	LootObj GUID128
	Slot    uint8
}

type LegacyLootStartRoll struct {
	GUID       uint64
	MapID      uint32
	Item       LegacyLootItem
	RollTime   uint32
	ValidRolls uint8
}

type LegacyLootRollBroadcast struct {
	GUID       uint64
	Item       LegacyLootItem
	Player     uint64
	Roll       uint8
	RollType   uint8
	Autopassed bool
}

type LegacyLootRollWon struct {
	GUID     uint64
	Item     LegacyLootItem
	Winner   uint64
	Roll     uint8
	RollType uint8
}

type LegacyLootAllPassed struct {
	GUID uint64
	Item LegacyLootItem
}

type LegacyLootItem struct {
	Slot     uint8
	ItemID   uint32
	Quantity uint32
	Suffix   uint32
	Property uint32
	UIType   uint8
}

type LegacyLootResponse struct {
	GUID          uint64
	FailureReason uint32
	AcquireReason uint32
	Coins         uint32
	Items         []LegacyLootItem
}

func (item LegacyLootItem) RollKey() LootRollKey {
	return LootRollKey{Slot: item.Slot, ItemID: item.ItemID, Suffix: item.Suffix, Property: item.Property}
}

func ParseLootRoll(body []byte) (LootRollRequest, error) {
	var request LootRollRequest
	r := movementReader{data: body}
	var err error
	if request.LootObj, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read loot-roll GUID: %w", err)
	}
	if request.Slot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read loot-roll slot: %w", err)
	}
	if request.RollType, err = r.u8(); err != nil {
		return request, fmt.Errorf("read loot-roll type: %w", err)
	}
	if request.RollType > 3 {
		return request, fmt.Errorf("loot-roll type %d is invalid", request.RollType)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("loot-roll has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyLootRoll(request LootRollRequest, legacyGUID uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, legacyGUID)
	body = binary.LittleEndian.AppendUint32(body, uint32(request.Slot))
	return append(body, request.RollType)
}

func ParseLegacyLootStartRoll(body []byte) (LegacyLootStartRoll, error) {
	var roll LegacyLootStartRoll
	r := movementReader{data: body}
	var err error
	if roll.GUID, err = r.u64(); err != nil {
		return roll, fmt.Errorf("read loot-start GUID: %w", err)
	}
	if roll.MapID, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-start map: %w", err)
	}
	slot, err := r.u32()
	if err != nil {
		return roll, fmt.Errorf("read loot-start slot: %w", err)
	}
	if slot > 255 {
		return roll, fmt.Errorf("loot-start slot %d exceeds uint8", slot)
	}
	roll.Item.Slot = uint8(slot)
	if roll.Item.ItemID, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-start item: %w", err)
	}
	if roll.Item.Suffix, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-start suffix: %w", err)
	}
	if roll.Item.Property, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-start property: %w", err)
	}
	if roll.Item.Quantity, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-start quantity: %w", err)
	}
	if roll.RollTime, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-start time: %w", err)
	}
	if roll.ValidRolls, err = r.u8(); err != nil {
		return roll, fmt.Errorf("read loot-start valid rolls: %w", err)
	}
	if r.remaining() != 0 {
		return roll, fmt.Errorf("loot-start has %d trailing bytes", r.remaining())
	}
	return roll, nil
}

func ParseLegacyLootRollBroadcast(body []byte) (LegacyLootRollBroadcast, error) {
	var roll LegacyLootRollBroadcast
	r := movementReader{data: body}
	var err error
	if roll.GUID, err = r.u64(); err != nil {
		return roll, fmt.Errorf("read loot-roll GUID: %w", err)
	}
	slot, err := r.u32()
	if err != nil {
		return roll, fmt.Errorf("read loot-roll slot: %w", err)
	}
	if slot > 255 {
		return roll, fmt.Errorf("loot-roll slot %d exceeds uint8", slot)
	}
	roll.Item.Slot = uint8(slot)
	if roll.Player, err = r.u64(); err != nil {
		return roll, fmt.Errorf("read loot-roll player: %w", err)
	}
	if roll.Item.ItemID, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-roll item: %w", err)
	}
	if roll.Item.Suffix, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-roll suffix: %w", err)
	}
	if roll.Item.Property, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-roll property: %w", err)
	}
	roll.Item.Quantity = 1
	if roll.Roll, err = r.u8(); err != nil {
		return roll, fmt.Errorf("read loot-roll number: %w", err)
	}
	if roll.RollType, err = r.u8(); err != nil {
		return roll, fmt.Errorf("read loot-roll type: %w", err)
	}
	autopassed, err := r.u8()
	if err != nil {
		return roll, fmt.Errorf("read loot-roll autopass: %w", err)
	}
	roll.Autopassed = autopassed != 0
	if r.remaining() != 0 {
		return roll, fmt.Errorf("loot-roll broadcast has %d trailing bytes", r.remaining())
	}
	return roll, nil
}

func ParseLegacyLootRollWon(body []byte) (LegacyLootRollWon, error) {
	var roll LegacyLootRollWon
	r := movementReader{data: body}
	var err error
	if roll.GUID, err = r.u64(); err != nil {
		return roll, fmt.Errorf("read loot-won GUID: %w", err)
	}
	slot, err := r.u32()
	if err != nil {
		return roll, fmt.Errorf("read loot-won slot: %w", err)
	}
	if slot > 255 {
		return roll, fmt.Errorf("loot-won slot %d exceeds uint8", slot)
	}
	roll.Item.Slot = uint8(slot)
	if roll.Item.ItemID, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-won item: %w", err)
	}
	if roll.Item.Suffix, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-won suffix: %w", err)
	}
	if roll.Item.Property, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-won property: %w", err)
	}
	roll.Item.Quantity = 1
	if roll.Winner, err = r.u64(); err != nil {
		return roll, fmt.Errorf("read loot-won winner: %w", err)
	}
	if roll.Roll, err = r.u8(); err != nil {
		return roll, fmt.Errorf("read loot-won number: %w", err)
	}
	if roll.RollType, err = r.u8(); err != nil {
		return roll, fmt.Errorf("read loot-won type: %w", err)
	}
	if r.remaining() != 0 {
		return roll, fmt.Errorf("loot-won has %d trailing bytes", r.remaining())
	}
	return roll, nil
}

func ParseLegacyLootAllPassed(body []byte) (LegacyLootAllPassed, error) {
	var roll LegacyLootAllPassed
	r := movementReader{data: body}
	var err error
	if roll.GUID, err = r.u64(); err != nil {
		return roll, fmt.Errorf("read loot-all-passed GUID: %w", err)
	}
	slot, err := r.u32()
	if err != nil {
		return roll, fmt.Errorf("read loot-all-passed slot: %w", err)
	}
	if slot > 255 {
		return roll, fmt.Errorf("loot-all-passed slot %d exceeds uint8", slot)
	}
	roll.Item.Slot = uint8(slot)
	if roll.Item.ItemID, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-all-passed item: %w", err)
	}
	// AzerothCore's 3.3.5 packet writes random property before random suffix
	// for this one message, unlike START_ROLL, LOOT_ROLL and ROLL_WON.
	if roll.Item.Property, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-all-passed property: %w", err)
	}
	if roll.Item.Suffix, err = r.u32(); err != nil {
		return roll, fmt.Errorf("read loot-all-passed suffix: %w", err)
	}
	roll.Item.Quantity = 1
	if r.remaining() != 0 {
		return roll, fmt.Errorf("loot-all-passed has %d trailing bytes", r.remaining())
	}
	return roll, nil
}

func EncodeStartLootRoll(lootObj GUID128, roll LegacyLootStartRoll) []byte {
	body := appendPackedGUID128(nil, lootObj.Low, lootObj.High)
	body = binary.LittleEndian.AppendUint32(body, roll.MapID)
	body = binary.LittleEndian.AppendUint32(body, roll.RollTime)
	body = append(body, roll.ValidRolls)
	// Build 54261 has three uint32 eligibility masks here. legacy proxy writes the
	// all-eligible sentinel followed by two empty masks for a WotLK roll.
	body = binary.LittleEndian.AppendUint32(body, 0x7FA3)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = append(body, 3)                           // LootMethod.GroupLoot
	body = binary.LittleEndian.AppendUint32(body, 0) // DungeonEncounterID
	return appendLootItemData(body, roll.Item)
}

func EncodeLootRollBroadcast(lootObj, player GUID128, roll LegacyLootRollBroadcast) []byte {
	rollNumber := int32(roll.Roll)
	rollType := roll.RollType
	if roll.Roll == 128 && roll.RollType == 128 {
		rollType = 0 // Pass
	} else if roll.Roll == 0 && roll.RollType == 0 {
		rollType = 1 // Need selection notification
	}
	if roll.Roll == 128 {
		rollNumber = 0
	}
	body := appendPackedGUID128(nil, lootObj.Low, lootObj.High)
	body = appendPackedGUID128(body, player.Low, player.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(rollNumber))
	body = append(body, rollType)
	body = binary.LittleEndian.AppendUint32(body, 0) // DungeonEncounterID
	body = appendLootItemData(body, roll.Item)
	bits := newBitWriter(body)
	bits.writeBit(roll.Autopassed)
	bits.writeBit(false) // OffSpec; 3.3.5 has no off-spec rolls
	return bits.flush()
}

func EncodeLootRollWon(lootObj, winner GUID128, roll LegacyLootRollWon) []byte {
	body := appendPackedGUID128(nil, lootObj.Low, lootObj.High)
	body = appendPackedGUID128(body, winner.Low, winner.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(roll.Roll))
	body = append(body, roll.RollType)
	body = binary.LittleEndian.AppendUint32(body, 0) // DungeonEncounterID
	body = appendLootItemData(body, roll.Item)
	// MainSpec is a byte in build 54261. legacy proxy uses the high bit for Need.
	mainSpec := byte(0)
	if roll.RollType == 1 {
		mainSpec = 0x80
	}
	return append(body, mainSpec)
}

func EncodeLootAllPassed(lootObj GUID128, roll LegacyLootAllPassed) []byte {
	body := appendPackedGUID128(nil, lootObj.Low, lootObj.High)
	body = binary.LittleEndian.AppendUint32(body, 0) // DungeonEncounterID
	return appendLootItemData(body, roll.Item)
}

func EncodeLootRollsComplete(lootObj GUID128, slot uint8) []byte {
	body := appendPackedGUID128(nil, lootObj.Low, lootObj.High)
	return append(body, slot)
}

func ParseLootUnit(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseLootRelease(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseLootMoney(body []byte) error {
	// 3.4.3 sends one client-only auto-loot flag byte. Hermes ignores it and
	// WotLK's CMSG_LOOT_MONEY has an empty body.
	if len(body) > 1 {
		return fmt.Errorf("loot-money has %d bytes, want at most 1", len(body))
	}
	return nil
}

func ParseLootItemSlots(body []byte) ([]uint8, error) {
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read loot-item count: %w", err)
	}
	if count > maxLootItems {
		return nil, fmt.Errorf("loot-item count %d exceeds %d", count, maxLootItems)
	}
	slots := make([]uint8, 0, count)
	for index := uint32(0); index < count; index++ {
		if _, err := r.guid128(); err != nil {
			return nil, fmt.Errorf("read loot-item %d GUID: %w", index, err)
		}
		slot, readErr := r.u8()
		if readErr != nil {
			return nil, fmt.Errorf("read loot-item %d slot: %w", index, readErr)
		}
		slots = append(slots, slot)
	}
	// 3.4 writes an extra LootItemType byte after the slot list. legacy proxy
	// ignores it; rejecting the leftover blocked CMSG_AUTOSTORE_LOOT_ITEM.
	if _, err := r.take(r.remaining()); err != nil {
		return nil, fmt.Errorf("skip loot-item trailing: %w", err)
	}
	return slots, nil
}

func EncodeLegacyAutostoreLootItem(slot uint8) []byte {
	return []byte{slot}
}

func ParseLegacyLootResponse(body []byte) (LegacyLootResponse, error) {
	var loot LegacyLootResponse
	if len(body) < 9 {
		return loot, fmt.Errorf("loot response has %d bytes, want at least 9", len(body))
	}
	loot.GUID = binary.LittleEndian.Uint64(body[:8])
	r := movementReader{data: body[8:]}
	lootType, err := r.u8()
	if err != nil {
		return loot, fmt.Errorf("read loot type: %w", err)
	}
	if lootType == 0 {
		reason, readErr := r.u8()
		if readErr != nil {
			return loot, fmt.Errorf("read loot error: %w", readErr)
		}
		if r.remaining() != 0 {
			return loot, fmt.Errorf("loot error has %d trailing bytes", r.remaining())
		}
		loot.FailureReason = uint32(reason)
		return loot, nil
	}
	loot.AcquireReason = uint32(lootType)
	if loot.Coins, err = r.u32(); err != nil {
		return loot, fmt.Errorf("read loot coins: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return loot, fmt.Errorf("read loot item count: %w", err)
	}
	if int(count) > maxLootItems {
		return loot, fmt.Errorf("loot item count %d exceeds %d", count, maxLootItems)
	}
	loot.Items = make([]LegacyLootItem, 0, count)
	for index := 0; index < int(count); index++ {
		item, readErr := parseLegacyLootItem(&r)
		if readErr != nil {
			return loot, fmt.Errorf("read loot item %d: %w", index, readErr)
		}
		loot.Items = append(loot.Items, item)
	}
	if r.remaining() != 0 {
		return loot, fmt.Errorf("loot response has %d trailing bytes", r.remaining())
	}
	return loot, nil
}

func parseLegacyLootItem(r *movementReader) (LegacyLootItem, error) {
	var item LegacyLootItem
	var err error
	if item.Slot, err = r.u8(); err != nil {
		return item, fmt.Errorf("slot: %w", err)
	}
	if item.ItemID, err = r.u32(); err != nil {
		return item, fmt.Errorf("item id: %w", err)
	}
	if item.Quantity, err = r.u32(); err != nil {
		return item, fmt.Errorf("quantity: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return item, fmt.Errorf("display id: %w", err)
	}
	if item.Suffix, err = r.u32(); err != nil {
		return item, fmt.Errorf("suffix: %w", err)
	}
	if item.Property, err = r.u32(); err != nil {
		return item, fmt.Errorf("property: %w", err)
	}
	legacyUIType, err := r.u8()
	if err != nil {
		return item, fmt.Errorf("slot type: %w", err)
	}
	item.UIType = modernLootUIType(legacyUIType)
	return item, nil
}

// Wrath and the modern client assign different numeric values to MASTER and
// LOCKED. Forwarding the legacy value unchanged makes master-loot items appear
// locked, causing the client to send CMSG_LOOT_ITEM instead of
// CMSG_LOOT_MASTER_GIVE.
func modernLootUIType(legacy uint8) uint8 {
	switch legacy {
	case 2: // legacy MASTER -> modern MASTER
		return 3
	case 3: // legacy LOCKED -> modern LOCKED
		return 2
	case 0, 1, 4:
		return legacy
	default:
		return 0
	}
}

const modernHighGuidLootObject = 15

// ModernLootGUID is legacy proxy (*WowGuid64).ToLootGuid: same WorldObject layout as
// the creature/GO, with HighGuid type 15 (LootObject) so the 3.4 loot window binds.
func ModernLootGUID(legacy uint64, mapID uint16) GUID128 {
	guid := ModernGUIDForLegacy(legacy, mapID)
	if guid.Low == 0 && guid.High == 0 {
		return guid
	}
	guid.High = guid.High&^(uint64(0x3f)<<58) | uint64(modernHighGuidLootObject)<<58
	return guid
}

func EncodeLootResponse(owner, lootObj GUID128, loot LegacyLootResponse) []byte {
	return EncodeLootResponseWithSettings(owner, lootObj, loot, byte(lootMethodFreeForAll), byte(lootThresholdUncommon))
}

func EncodeLootResponseWithSettings(owner, lootObj GUID128, loot LegacyLootResponse, lootMethod, lootThreshold byte) []byte {
	body := appendPackedGUID128(nil, owner.Low, owner.High)
	body = appendPackedGUID128(body, lootObj.Low, lootObj.High)
	acquire := loot.AcquireReason
	if loot.FailureReason == 0 && acquire == 0 {
		acquire = lootAcquireCorpse
	}
	// legacy proxy LootResponse.Write uses Buffer.WriteByte for these four fields.
	body = append(body, byte(loot.FailureReason), byte(acquire), lootMethod, lootThreshold)
	body = binary.LittleEndian.AppendUint32(body, loot.Coins)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(loot.Items)))
	body = binary.LittleEndian.AppendUint32(body, 0) // currency count
	bits := newBitWriter(body)
	bits.writeBit(loot.FailureReason == 0)
	bits.writeBit(false) // AELooting
	body = bits.flush()
	for _, item := range loot.Items {
		body = appendLootItemData(body, item)
	}
	return body
}

func ParseLegacyMasterLootCandidates(body []byte) ([]uint64, error) {
	r := movementReader{data: body}
	count, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read master-loot candidate count: %w", err)
	}
	if count > 40 {
		return nil, fmt.Errorf("master-loot candidate count %d exceeds 40", count)
	}
	candidates := make([]uint64, count)
	for index := range candidates {
		if candidates[index], err = r.u64(); err != nil {
			return nil, fmt.Errorf("read master-loot candidate %d: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("master-loot candidate list has %d trailing bytes", r.remaining())
	}
	return candidates, nil
}

func EncodeMasterLootList(owner, lootObj, master GUID128) []byte {
	body := appendPackedGUID128(nil, owner.Low, owner.High)
	body = appendPackedGUID128(body, lootObj.Low, lootObj.High)
	bits := newBitWriter(body)
	bits.writeBit(master.Low != 0 || master.High != 0)
	bits.writeBit(false) // RoundRobinWinner
	body = bits.flush()
	if master.Low != 0 || master.High != 0 {
		body = appendPackedGUID128(body, master.Low, master.High)
	}
	return body
}

func EncodeMasterLootCandidateList(lootObj GUID128, candidates []GUID128) []byte {
	body := appendPackedGUID128(nil, lootObj.Low, lootObj.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(candidates)))
	for _, candidate := range candidates {
		body = appendPackedGUID128(body, candidate.Low, candidate.High)
	}
	return body
}

func ParseMasterLootRequest(body []byte) (MasterLootRequest, error) {
	var request MasterLootRequest
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return request, fmt.Errorf("read master-loot item count: %w", err)
	}
	if count == 0 || count > maxLootItems {
		return request, fmt.Errorf("master-loot item count %d is invalid", count)
	}
	if request.Target, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read master-loot target: %w", err)
	}
	request.Items = make([]MasterLootItem, count)
	for index := range request.Items {
		if request.Items[index].LootObj, err = r.guid128(); err != nil {
			return request, fmt.Errorf("read master-loot item %d object: %w", index, err)
		}
		if request.Items[index].Slot, err = r.u8(); err != nil {
			return request, fmt.Errorf("read master-loot item %d slot: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("master-loot request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyMasterLootGive(lootGUID uint64, slot uint8, targetGUID uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, lootGUID)
	body = append(body, slot)
	return binary.LittleEndian.AppendUint64(body, targetGUID)
}

func appendLootItemData(dst []byte, item LegacyLootItem) []byte {
	bits := newBitWriter(dst)
	bits.writeBits(0, 2) // Type = item
	bits.writeBits(uint32(item.UIType), 3)
	bits.writeBit(false) // CanTradeToTapList
	dst = bits.flush()
	dst = appendItemInstance(dst, item.ItemID, item.Suffix, item.Property)
	dst = binary.LittleEndian.AppendUint32(dst, item.Quantity)
	return append(dst, 0, item.Slot) // LootItemType byte, LootListID byte (legacy proxy WriteByte)
}

func appendItemInstance(dst []byte, itemID, seed, property uint32) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, itemID)
	dst = binary.LittleEndian.AppendUint32(dst, seed)
	dst = binary.LittleEndian.AppendUint32(dst, property)
	hasBonus := newBitWriter(dst)
	hasBonus.writeBit(false)
	dst = hasBonus.flush()
	bonusCount := newBitWriter(dst)
	bonusCount.writeBits(0, 6)
	return bonusCount.flush()
}

func ParseLegacyLootRelease(body []byte) (uint64, error) {
	if len(body) == 8 || len(body) == 9 {
		return binary.LittleEndian.Uint64(body[:8]), nil
	}
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, fmt.Errorf("read loot-release GUID: %w", err)
	}
	if r.remaining() == 1 {
		if _, err := r.u8(); err != nil {
			return 0, fmt.Errorf("read loot-release unk: %w", err)
		}
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("loot-release has %d trailing bytes", r.remaining())
	}
	return guid, nil
}

func EncodeLootRelease(lootObj, owner GUID128) []byte {
	body := appendPackedGUID128(nil, lootObj.Low, lootObj.High)
	return appendPackedGUID128(body, owner.Low, owner.High)
}

func ParseLegacyLootRemoved(body []byte) (uint8, error) {
	if len(body) != 1 {
		return 0, fmt.Errorf("loot-removed has %d bytes, want 1", len(body))
	}
	return body[0], nil
}

func EncodeLootRemoved(owner, lootObj GUID128, slot uint8) []byte {
	body := appendPackedGUID128(nil, owner.Low, owner.High)
	body = appendPackedGUID128(body, lootObj.Low, lootObj.High)
	return append(body, slot)
}

func EncodeCoinRemoved(lootObj GUID128) []byte {
	return appendPackedGUID128(nil, lootObj.Low, lootObj.High)
}

func ParseLegacyLootMoneyNotify(body []byte) (uint32, bool, error) {
	r := movementReader{data: body}
	money, err := r.u32()
	if err != nil {
		return 0, false, fmt.Errorf("read loot money: %w", err)
	}
	sole := true
	if r.remaining() == 1 {
		flag, readErr := r.u8()
		if readErr != nil {
			return 0, false, fmt.Errorf("read loot sole-looter: %w", readErr)
		}
		sole = flag != 0
	} else if r.remaining() != 0 {
		return 0, false, fmt.Errorf("loot money notify has %d trailing bytes", r.remaining())
	}
	return money, sole, nil
}

func EncodeLootMoneyNotify(money uint32, sole bool) []byte {
	body := binary.LittleEndian.AppendUint64(nil, uint64(money))
	body = binary.LittleEndian.AppendUint64(body, 0)
	bits := newBitWriter(body)
	bits.writeBit(sole)
	return bits.flush()
}

type LegacyItemPush struct {
	Player         uint64
	Received       uint32
	Created        uint32
	ShowInChat     uint32
	Bag            uint8
	Slot           uint32
	ItemID         uint32
	Suffix         uint32
	Property       uint32
	Quantity       uint32
	InventoryCount uint32
}

func ParseLegacyItemPushResult(body []byte) (LegacyItemPush, error) {
	var push LegacyItemPush
	r := movementReader{data: body}
	var err error
	if push.Player, err = r.u64(); err != nil {
		return push, fmt.Errorf("read item-push player: %w", err)
	}
	if push.Received, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push received: %w", err)
	}
	if push.Created, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push created: %w", err)
	}
	if push.ShowInChat, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push chat: %w", err)
	}
	if push.Bag, err = r.u8(); err != nil {
		return push, fmt.Errorf("read item-push bag: %w", err)
	}
	if push.Slot, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push slot: %w", err)
	}
	if push.ItemID, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push item: %w", err)
	}
	if push.Suffix, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push suffix: %w", err)
	}
	if push.Property, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push property: %w", err)
	}
	if push.Quantity, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push quantity: %w", err)
	}
	if push.InventoryCount, err = r.u32(); err != nil {
		return push, fmt.Errorf("read item-push inventory: %w", err)
	}
	if _, err = r.take(r.remaining()); err != nil {
		return push, fmt.Errorf("skip item-push trailing: %w", err)
	}
	return push, nil
}

func EncodeItemPushResult(player GUID128, push LegacyItemPush) []byte {
	body := appendPackedGUID128(nil, player.Low, player.High)
	body = append(body, push.Bag)
	body = binary.LittleEndian.AppendUint32(body, push.Slot)
	body = binary.LittleEndian.AppendUint32(body, 0) // QuestLogItemID
	body = binary.LittleEndian.AppendUint32(body, push.Quantity)
	body = binary.LittleEndian.AppendUint32(body, push.InventoryCount)
	body = binary.LittleEndian.AppendUint32(body, 0) // EncounterClosingItemID
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0) // BattlePet*
	body = appendPackedGUID128(body, 0, 0)           // ItemGUID
	created := push.Created != 0
	display := uint32(0) // Hidden
	pushed := false
	if push.Received != 0 && !created {
		display = 1 // Received from NPC/loot
		pushed = true
	} else if push.ShowInChat != 0 {
		display = 3
	}
	bits := newBitWriter(body)
	bits.writeBit(pushed)
	bits.writeBit(created)
	bits.writeBits(display, 3)
	bits.writeBit(false) // IsBonusRoll
	bits.writeBit(false) // IsEncounterLoot
	body = bits.flush()
	return appendItemInstance(body, push.ItemID, push.Suffix, push.Property)
}
