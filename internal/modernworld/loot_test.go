package modernworld

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseLootUnitAndRelease(t *testing.T) {
	body := appendPackedGUID128(nil, 0x43, 1)
	guid, err := ParseLootUnit(body)
	if err != nil || guid.Low != 0x43 || guid.High != 1 {
		t.Fatalf("loot unit %#v err=%v", guid, err)
	}
	guid, err = ParseLootRelease(body)
	if err != nil || guid.Low != 0x43 {
		t.Fatalf("loot release %#v err=%v", guid, err)
	}
	if err := ParseLootMoney(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseLootMoney([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := ParseLootMoney([]byte{1, 0}); err == nil {
		t.Fatal("expected trailing loot-money error")
	}
}

func TestParseLootItemSlots(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 2)
	body = appendPackedGUID128(body, 0x43, 1)
	body = append(body, 3)
	body = appendPackedGUID128(body, 0x43, 1)
	body = append(body, 5)
	slots, err := ParseLootItemSlots(body)
	if err != nil || len(slots) != 2 || slots[0] != 3 || slots[1] != 5 {
		t.Fatalf("slots=%v err=%v", slots, err)
	}
	if got := EncodeLegacyAutostoreLootItem(3); len(got) != 1 || got[0] != 3 {
		t.Fatalf("autostore %x", got)
	}
}

func TestLootResponseGoldAndItemRoundTrip(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	legacy = append(legacy, 1) // corpse
	legacy = binary.LittleEndian.AppendUint32(legacy, 15)
	legacy = append(legacy, 1) // one item
	legacy = append(legacy, 2) // slot
	legacy = binary.LittleEndian.AppendUint32(legacy, 117)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3)
	legacy = binary.LittleEndian.AppendUint32(legacy, 999)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = append(legacy, 0) // ALLOW_LOOT
	loot, err := ParseLegacyLootResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if loot.GUID != 0xf130000001000043 || loot.AcquireReason != 1 || loot.Coins != 15 {
		t.Fatalf("loot=%#v", loot)
	}
	if len(loot.Items) != 1 || loot.Items[0].ItemID != 117 || loot.Items[0].Quantity != 3 || loot.Items[0].Slot != 2 {
		t.Fatalf("items=%#v", loot.Items)
	}
	owner := GUID128{Low: 0x42, High: 1}
	obj := GUID128{Low: 0x43, High: 1}
	body := EncodeLootResponse(owner, obj, loot)
	_, _, ownerBytes, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	_, _, objBytes, err := readPackedGUID128(body[ownerBytes:])
	if err != nil {
		t.Fatal(err)
	}
	rest := body[ownerBytes+objBytes:]
	if rest[0] != 0 {
		t.Fatalf("failure %x", rest[:4])
	}
	if rest[1] != 1 {
		t.Fatalf("acquire %x", rest[:4])
	}
	if rest[2] != 0 || rest[3] != 2 {
		t.Fatalf("method/threshold %x", rest[:4])
	}
	if binary.LittleEndian.Uint32(rest[4:8]) != 15 {
		t.Fatalf("coins %x", rest[4:8])
	}
	if binary.LittleEndian.Uint32(rest[8:12]) != 1 {
		t.Fatalf("item count %x", rest[8:12])
	}
	if rest[16]&0x80 == 0 {
		t.Fatalf("acquired bit missing: %x", rest[16:])
	}
	item := rest[17:]
	if item[0]&0xe0 != 0 {
		t.Fatalf("item type/ui bits %x", item[0])
	}
	if binary.LittleEndian.Uint32(item[1:5]) != 117 {
		t.Fatalf("item id %x", item[1:5])
	}
	if item[len(item)-2] != 0 || item[len(item)-1] != 2 {
		t.Fatalf("loot item type/slot %x", item)
	}
}

func TestLegacyLootSlotTypeConversion(t *testing.T) {
	tests := []struct {
		legacy uint8
		modern uint8
	}{
		{legacy: 0, modern: 0}, // allow loot
		{legacy: 1, modern: 1}, // roll ongoing
		{legacy: 2, modern: 3}, // master loot
		{legacy: 3, modern: 2}, // locked
		{legacy: 4, modern: 4}, // owner
	}
	for _, test := range tests {
		if got := modernLootUIType(test.legacy); got != test.modern {
			t.Fatalf("legacy loot slot type %d converted to %d, want %d", test.legacy, got, test.modern)
		}
	}
}

func TestModernLootGUIDUsesLootObjectType(t *testing.T) {
	creature := ModernGUIDForLegacy(0xf130000001000043, 0)
	loot := ModernLootGUID(0xf130000001000043, 0)
	if creature.Low != loot.Low {
		t.Fatalf("counter creature=%x loot=%x", creature.Low, loot.Low)
	}
	if creature.High>>58 != 8 {
		t.Fatalf("creature type %d", creature.High>>58)
	}
	if loot.High>>58 != 15 {
		t.Fatalf("loot type %d", loot.High>>58)
	}
	if creature.High&((1<<58)-1) != loot.High&((1<<58)-1) {
		t.Fatalf("realm/map/entry changed: creature=%x loot=%x", creature.High, loot.High)
	}
}

func TestLootResponseError(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0x42)
	legacy = append(legacy, 0, 4) // LOOT_NONE + too far
	loot, err := ParseLegacyLootResponse(legacy)
	if err != nil || loot.FailureReason != 4 || len(loot.Items) != 0 {
		t.Fatalf("loot=%#v err=%v", loot, err)
	}
	body := EncodeLootResponse(GUID128{Low: 1, High: 1}, GUID128{Low: 2, High: 1}, loot)
	if body[len(body)-1]&0x80 != 0 {
		t.Fatalf("acquired should be clear on error: %x", body)
	}
}

func TestLootReleaseRemovedMoney(t *testing.T) {
	unpacked := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	unpacked = append(unpacked, 1)
	guid, err := ParseLegacyLootRelease(unpacked)
	if err != nil || guid != 0xf130000001000043 {
		t.Fatalf("release %#x err=%v", guid, err)
	}
	slot, err := ParseLegacyLootRemoved([]byte{7})
	if err != nil || slot != 7 {
		t.Fatalf("removed %d err=%v", slot, err)
	}
	money, sole, err := ParseLegacyLootMoneyNotify(binary.LittleEndian.AppendUint32(nil, 42))
	if err != nil || money != 42 || !sole {
		t.Fatalf("money=%d sole=%v err=%v", money, sole, err)
	}
	owner := GUID128{Low: 0x42, High: 1}
	obj := GUID128{Low: 0x43, High: 1}
	release := EncodeLootRelease(obj, owner)
	removed := EncodeLootRemoved(owner, obj, 7)
	coinRemoved := EncodeCoinRemoved(obj)
	notify := EncodeLootMoneyNotify(42, true)
	if len(release) < 4 || len(removed) < 5 || notify[16]&0x80 == 0 {
		t.Fatalf("release=%x removed=%x notify=%x", release, removed, notify)
	}
	_, _, ownerBytes, err := readPackedGUID128(removed)
	if err != nil {
		t.Fatal(err)
	}
	_, _, objBytes, err := readPackedGUID128(removed[ownerBytes:])
	if err != nil {
		t.Fatal(err)
	}
	if tail := removed[ownerBytes+objBytes:]; len(tail) != 1 || tail[0] != 7 {
		t.Fatalf("loot-removed slot tail=%x, want one byte 07", tail)
	}
	coinGUID, err := ParsePackedGUID128Exact(coinRemoved)
	if err != nil || coinGUID != obj {
		t.Fatalf("coin removed guid=%#v err=%v", coinGUID, err)
	}
	if SMSGCoinRemoved != 0x2617 {
		t.Fatalf("coin-removed opcode = 0x%x, want 0x2617", SMSGCoinRemoved)
	}
}

func TestItemPushResultRoundTrip(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // received
	legacy = binary.LittleEndian.AppendUint32(legacy, 0) // created
	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // chat
	legacy = append(legacy, 255)                         // backpack
	legacy = binary.LittleEndian.AppendUint32(legacy, 23)
	legacy = binary.LittleEndian.AppendUint32(legacy, 117)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	push, err := ParseLegacyItemPushResult(legacy)
	if err != nil || push.ItemID != 117 || push.Quantity != 1 || push.Bag != 255 {
		t.Fatalf("push=%#v err=%v", push, err)
	}
	body := EncodeItemPushResult(GUID128{Low: 0x42, High: 1}, push)
	if len(body) < 20 {
		t.Fatalf("encoded item-push %d", len(body))
	}
	_, _, playerBytes, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	bitOffset := playerBytes + 1 + 9*4 + len(appendPackedGUID128(nil, 0, 0))
	if got := body[bitOffset]; got != 0x88 {
		t.Fatalf("item-push flags = 0x%02x, want pushed + Received display (0x88)", got)
	}
}

func TestLootParsersRejectMalformed(t *testing.T) {
	if _, err := ParseLegacyLootResponse([]byte{1}); err == nil || !strings.Contains(err.Error(), "at least 9") {
		t.Fatalf("short loot error=%v", err)
	}
	if _, err := ParseLootItemSlots([]byte{1, 0, 0, 0}); err == nil {
		t.Fatal("expected truncated loot-item error")
	}
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = appendPackedGUID128(body, 0x43, 1)
	body = append(body, 2, 0) // slot + trailing LootItemType
	slots, err := ParseLootItemSlots(body)
	if err != nil || len(slots) != 1 || slots[0] != 2 {
		t.Fatalf("trailing loot-item slots=%v err=%v", slots, err)
	}
	if _, err := ParseLegacyLootRemoved(nil); err == nil {
		t.Fatal("expected empty loot-removed error")
	}
}

func TestLootRollTranslation(t *testing.T) {
	const legacyGUID = uint64(0xf1300006aa00004b)
	legacyStart := binary.LittleEndian.AppendUint64(nil, legacyGUID)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 33)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 2)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 117)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 55)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 66)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 3)
	legacyStart = binary.LittleEndian.AppendUint32(legacyStart, 60000)
	legacyStart = append(legacyStart, 7)
	roll, err := ParseLegacyLootStartRoll(legacyStart)
	if err != nil || roll.GUID != legacyGUID || roll.MapID != 33 || roll.Item.Slot != 2 ||
		roll.Item.ItemID != 117 || roll.Item.Suffix != 55 || roll.Item.Property != 66 ||
		roll.Item.Quantity != 3 || roll.RollTime != 60000 || roll.ValidRolls != 7 {
		t.Fatalf("start=%#v err=%v", roll, err)
	}

	lootObj := ModernLootGUID(legacyGUID, 33)
	start := EncodeStartLootRoll(lootObj, roll)
	r := movementReader{data: start}
	guid, _ := r.guid128()
	mapID, _ := r.u32()
	rollTime, _ := r.u32()
	valid, _ := r.u8()
	eligible, _ := r.u32()
	ineligibleNeed, _ := r.u32()
	ineligibleGreed, _ := r.u32()
	method, _ := r.u8()
	encounterID, _ := r.i32()
	if guid != lootObj || mapID != 33 || rollTime != 60000 || valid != 7 ||
		eligible != 0x7FA3 || ineligibleNeed != 0 || ineligibleGreed != 0 || method != 3 || encounterID != 0 {
		t.Fatalf("modern start guid=%#v map=%d time=%d valid=%d masks=%x/%x/%x method=%d encounter=%d body=%x",
			guid, mapID, rollTime, valid, eligible, ineligibleNeed, ineligibleGreed, method, encounterID, start)
	}

	requestBody := appendPackedGUID128(nil, lootObj.Low, lootObj.High)
	requestBody = append(requestBody, 2, 1)
	request, err := ParseLootRoll(requestBody)
	if err != nil || request.LootObj != lootObj || request.Slot != 2 || request.RollType != 1 {
		t.Fatalf("request=%#v err=%v", request, err)
	}
	legacyRequest := EncodeLegacyLootRoll(request, legacyGUID)
	if len(legacyRequest) != 13 || binary.LittleEndian.Uint64(legacyRequest) != legacyGUID ||
		binary.LittleEndian.Uint32(legacyRequest[8:]) != 2 || legacyRequest[12] != 1 {
		t.Fatalf("legacy request=%x", legacyRequest)
	}

	allPassedBody := binary.LittleEndian.AppendUint64(nil, 0)
	allPassedBody = binary.LittleEndian.AppendUint32(allPassedBody, 2)
	allPassedBody = binary.LittleEndian.AppendUint32(allPassedBody, 117)
	allPassedBody = binary.LittleEndian.AppendUint32(allPassedBody, 66) // property first in AzerothCore
	allPassedBody = binary.LittleEndian.AppendUint32(allPassedBody, 55)
	allPassed, err := ParseLegacyLootAllPassed(allPassedBody)
	if err != nil || allPassed.Item.RollKey() != roll.Item.RollKey() {
		t.Fatalf("all-passed=%#v key=%#v want=%#v err=%v", allPassed, allPassed.Item.RollKey(), roll.Item.RollKey(), err)
	}
	complete := EncodeLootRollsComplete(lootObj, 2)
	completeReader := movementReader{data: complete}
	completedGUID, _ := completeReader.guid128()
	completedSlot, _ := completeReader.u8()
	if completedGUID != lootObj || completedSlot != 2 || completeReader.remaining() != 0 {
		t.Fatalf("complete guid=%#v slot=%d remaining=%d body=%x", completedGUID, completedSlot, completeReader.remaining(), complete)
	}

	won := EncodeLootRollWon(lootObj, GUID128{Low: 0x43, High: 1}, LegacyLootRollWon{
		Roll: 99, RollType: 1, Item: roll.Item,
	})
	if won[len(won)-1] != 0x80 {
		t.Fatalf("need roll main-spec byte=%#x, want 0x80; body=%x", won[len(won)-1], won)
	}
}

func TestMasterLootCandidateListTranslation(t *testing.T) {
	legacy := []byte{2}
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x1)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x43)
	candidates, err := ParseLegacyMasterLootCandidates(legacy)
	if err != nil || len(candidates) != 2 || candidates[0] != 0x1 || candidates[1] != 0x43 {
		t.Fatalf("candidates=%v err=%v", candidates, err)
	}
	lootObj := ModernLootGUID(0xf1300006aa00004b, 33)
	modern := []GUID128{{Low: 1, High: uint64(2) << 58}, {Low: 0x43, High: uint64(2) << 58}}
	body := EncodeMasterLootCandidateList(lootObj, modern)
	r := movementReader{data: body}
	guid, _ := r.guid128()
	count, _ := r.u32()
	first, _ := r.guid128()
	second, _ := r.guid128()
	if guid != lootObj || count != 2 || first != modern[0] || second != modern[1] || r.remaining() != 0 {
		t.Fatalf("master-loot list guid=%#v count=%d first=%#v second=%#v remaining=%d", guid, count, first, second, r.remaining())
	}
}
