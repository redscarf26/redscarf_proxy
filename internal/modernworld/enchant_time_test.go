package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestItemEnchantTimeTranslation(t *testing.T) {
	legacy := make([]byte, 24)
	binary.LittleEndian.PutUint64(legacy[0:8], 0x4000000000000681) // item
	binary.LittleEndian.PutUint32(legacy[8:12], 3)                 // slot
	binary.LittleEndian.PutUint32(legacy[12:16], 1800)             // duration left
	binary.LittleEndian.PutUint64(legacy[16:24], 0x1)              // owner

	update, err := ParseLegacyItemEnchantTime(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if update.ItemGUID != 0x4000000000000681 || update.Slot != 3 || update.DurationLeft != 1800 || update.OwnerGUID != 0x1 {
		t.Fatalf("update=%+v", update)
	}

	body := EncodeItemEnchantTime(update, GUID128{Low: 0x681, High: 1}, GUID128{Low: 1})
	// Re-read to verify the modern order is item, duration, slot, owner.
	r := movementReader{data: body}
	item, err := r.guid128()
	if err != nil {
		t.Fatal(err)
	}
	duration, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	slot, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := r.guid128()
	if err != nil {
		t.Fatal(err)
	}
	if r.remaining() != 0 {
		t.Fatalf("modern body has %d trailing bytes", r.remaining())
	}
	if item != (GUID128{Low: 0x681, High: 1}) || duration != 1800 || slot != 3 || owner != (GUID128{Low: 1}) {
		t.Fatalf("modern item=%#v duration=%d slot=%d owner=%#v", item, duration, slot, owner)
	}

	if _, err := ParseLegacyItemEnchantTime(legacy[:23]); err == nil {
		t.Fatal("expected truncated legacy body to error")
	}
}

func TestEnchantmentLogTranslation(t *testing.T) {
	legacy := EncodeLegacyPackedGUID(0x42)
	legacy = append(legacy, EncodeLegacyPackedGUID(0)...)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2862) // item ID
	legacy = binary.LittleEndian.AppendUint32(legacy, 2830) // enchant

	log, err := ParseLegacyEnchantmentLog(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if log.OwnerGUID != 0x42 || log.CasterGUID != 0 || log.ItemID != 2862 || log.Enchantment != 2830 {
		t.Fatalf("log=%+v", log)
	}

	item := GUID128{Low: 0x681, High: 1}
	body := EncodeEnchantmentLog(log, GUID128{Low: 0x42}, GUID128{}, item)
	r := movementReader{data: body}
	owner, err := r.guid128()
	if err != nil {
		t.Fatal(err)
	}
	caster, err := r.guid128()
	if err != nil {
		t.Fatal(err)
	}
	gotItem, err := r.guid128()
	if err != nil {
		t.Fatal(err)
	}
	itemID, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	enchant, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	slot, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	if r.remaining() != 0 {
		t.Fatalf("modern body has %d trailing bytes", r.remaining())
	}
	if owner != (GUID128{Low: 0x42}) || caster != (GUID128{}) || gotItem != item || itemID != 2862 || enchant != 2830 || slot != 1 {
		t.Fatalf("owner=%#v caster=%#v item=%#v itemID=%d enchant=%d slot=%d", owner, caster, gotItem, itemID, enchant, slot)
	}
	if _, err := ParseLegacyEnchantmentLog(legacy[:len(legacy)-1]); err == nil {
		t.Fatal("expected truncated enchantment-log body to error")
	}
}

func TestItemCooldownAndDurabilityDeath(t *testing.T) {
	legacy := make([]byte, 12)
	binary.LittleEndian.PutUint64(legacy[0:8], 0x4000000000000681)
	binary.LittleEndian.PutUint32(legacy[8:12], 2828)
	cooldown, err := ParseLegacyItemCooldown(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if cooldown.ItemGUID != 0x4000000000000681 || cooldown.SpellID != 2828 {
		t.Fatalf("cooldown=%+v", cooldown)
	}
	body := EncodeItemCooldown(GUID128{Low: 0x681, High: 1}, cooldown.SpellID, DefaultItemCooldownMS)
	r := movementReader{data: body}
	item, err := r.guid128()
	if err != nil {
		t.Fatal(err)
	}
	spellID, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := r.u32()
	if err != nil {
		t.Fatal(err)
	}
	if r.remaining() != 0 || item != (GUID128{Low: 0x681, High: 1}) || spellID != 2828 || remaining != DefaultItemCooldownMS {
		t.Fatalf("item=%#v spell=%d remaining=%d leftover=%d", item, spellID, remaining, r.remaining())
	}
	if _, err := ParseLegacyItemCooldown(legacy[:11]); err == nil {
		t.Fatal("expected truncated item-cooldown body to error")
	}

	if err := ParseLegacyDurabilityDamageDeath(nil); err != nil {
		t.Fatal(err)
	}
	death := EncodeDurabilityDamageDeath()
	if len(death) != 4 || binary.LittleEndian.Uint32(death) != 10 {
		t.Fatalf("durability-death=%x", death)
	}
	if err := ParseLegacyDurabilityDamageDeath([]byte{1}); err == nil {
		t.Fatal("expected non-empty durability-death body to error")
	}
}

func TestFindLegacyItemGUIDByEntry(t *testing.T) {
	const (
		itemGUID = uint64(0x4000000000000681)
		itemID   = uint32(2862)
	)
	player := map[int]uint32{
		legacyPlayerInvSlotHead:     uint32(itemGUID & 0xffffffff),
		legacyPlayerInvSlotHead + 1: uint32(itemGUID >> 32),
	}
	objects := map[uint64]map[int]uint32{
		itemGUID: {legacyObjectEntry: itemID, legacyItemStackCount: 1},
	}
	if got := FindLegacyItemGUIDByEntry(player, objects, itemID); got != itemGUID {
		t.Fatalf("equipment lookup got 0x%x", got)
	}
	if got := FindLegacyItemGUIDByEntry(nil, objects, itemID); got != itemGUID {
		t.Fatalf("object fallback got 0x%x", got)
	}
	if got := FindLegacyItemGUIDByEntry(player, objects, 1); got != 0 {
		t.Fatalf("missing item returned 0x%x", got)
	}
}
