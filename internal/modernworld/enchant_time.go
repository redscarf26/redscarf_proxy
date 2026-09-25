package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Temporary weapon enchant countdown (3.3.5a SMSG_ITEM_ENCHANT_TIME_UPDATE ->
// build 54261). AzerothCore announces a temporary enchant's remaining duration
// right after a sharpening stone / wizard oil is applied. Build 54261 reorders
// the legacy slot/duration pair and widens the two GUIDs to PackedGuid128.
// Mirrors HermesProxy-WOTLK World/Client/WorldClient.cs
// HandleItemEnchantTimeUpdate and Packets/ItemEnchantTimeUpdate.cs.

const (
	SMSGItemEnchantTimeUpdate = uint16(10069) // 0x2755
	SMSGEnchantmentLog        = uint16(10003) // 0x2713
	SMSGItemCooldown          = uint16(10184) // 0x27C8
	SMSGDurabilityDamageDeath = uint16(10053) // 0x2745
	DefaultItemCooldownMS     = uint32(30000)
	durabilityDamageDeathPct  = uint32(10)
	legacyTempEnchantmentSlot = uint32(1)
	legacyEquipmentSlotCount  = 23
)

type LegacyItemEnchantTime struct {
	ItemGUID     uint64
	Slot         uint32
	DurationLeft uint32
	OwnerGUID    uint64
}

type LegacyEnchantmentLog struct {
	OwnerGUID   uint64
	CasterGUID  uint64
	ItemID      uint32
	Enchantment uint32
}

type LegacyItemCooldown struct {
	ItemGUID uint64
	SpellID  uint32
}

func ParseLegacyItemEnchantTime(body []byte) (LegacyItemEnchantTime, error) {
	var update LegacyItemEnchantTime
	if len(body) != 24 {
		return update, fmt.Errorf("item-enchant-time has %d bytes, want 24", len(body))
	}
	update.ItemGUID = binary.LittleEndian.Uint64(body[0:8])
	update.Slot = binary.LittleEndian.Uint32(body[8:12])
	update.DurationLeft = binary.LittleEndian.Uint32(body[12:16])
	update.OwnerGUID = binary.LittleEndian.Uint64(body[16:24])
	return update, nil
}

func EncodeItemEnchantTime(update LegacyItemEnchantTime, item, owner GUID128) []byte {
	body := appendPackedGUID128(nil, item.Low, item.High)
	body = binary.LittleEndian.AppendUint32(body, update.DurationLeft)
	body = binary.LittleEndian.AppendUint32(body, update.Slot)
	return appendPackedGUID128(body, owner.Low, owner.High)
}

func ParseLegacyEnchantmentLog(body []byte) (LegacyEnchantmentLog, error) {
	var log LegacyEnchantmentLog
	r := movementReader{data: body}
	owner, err := r.guid64()
	if err != nil {
		return log, fmt.Errorf("read enchantment owner GUID: %w", err)
	}
	caster, err := r.guid64()
	if err != nil {
		return log, fmt.Errorf("read enchantment caster GUID: %w", err)
	}
	itemID, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read enchantment item ID: %w", err)
	}
	enchantment, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read enchantment ID: %w", err)
	}
	if r.remaining() != 0 {
		return log, fmt.Errorf("enchantment-log has %d trailing bytes", r.remaining())
	}
	log.OwnerGUID = owner
	log.CasterGUID = caster
	log.ItemID = itemID
	log.Enchantment = enchantment
	return log, nil
}

func EncodeEnchantmentLog(log LegacyEnchantmentLog, owner, caster, item GUID128) []byte {
	body := appendPackedGUID128(nil, owner.Low, owner.High)
	body = appendPackedGUID128(body, caster.Low, caster.High)
	body = appendPackedGUID128(body, item.Low, item.High)
	body = binary.LittleEndian.AppendUint32(body, log.ItemID)
	body = binary.LittleEndian.AppendUint32(body, log.Enchantment)
	return binary.LittleEndian.AppendUint32(body, legacyTempEnchantmentSlot)
}

func ParseLegacyItemCooldown(body []byte) (LegacyItemCooldown, error) {
	var cooldown LegacyItemCooldown
	if len(body) != 12 {
		return cooldown, fmt.Errorf("item-cooldown has %d bytes, want 12", len(body))
	}
	cooldown.ItemGUID = binary.LittleEndian.Uint64(body[0:8])
	cooldown.SpellID = binary.LittleEndian.Uint32(body[8:12])
	return cooldown, nil
}

func EncodeItemCooldown(item GUID128, spellID, cooldownMS uint32) []byte {
	body := appendPackedGUID128(nil, item.Low, item.High)
	body = binary.LittleEndian.AppendUint32(body, spellID)
	return binary.LittleEndian.AppendUint32(body, cooldownMS)
}

func ParseLegacyDurabilityDamageDeath(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("durability-damage-death has %d bytes, want 0", len(body))
	}
	return nil
}

func EncodeDurabilityDamageDeath() []byte {
	return binary.LittleEndian.AppendUint32(nil, durabilityDamageDeathPct)
}

// FindLegacyItemGUIDByEntry looks up a cached item GUID by template ID. Hermes
// only walks the 23 equipment/bag slots; this also falls back to every cached
// item object so a newly created weapon still matches the following
// enchantment-log packet.
func FindLegacyItemGUIDByEntry(playerFields map[int]uint32, objects map[uint64]map[int]uint32, itemID uint32) uint64 {
	if itemID == 0 {
		return 0
	}
	if playerFields != nil {
		for slot := 0; slot < legacyEquipmentSlotCount; slot++ {
			base := legacyPlayerInvSlotHead + slot*2
			guid := uint64(playerFields[base]) | uint64(playerFields[base+1])<<32
			if guid == 0 {
				continue
			}
			if entry, _ := LegacyItemEntryAndCount(objects[guid]); entry == itemID {
				return guid
			}
		}
	}
	for guid, fields := range objects {
		if entry, _ := LegacyItemEntryAndCount(fields); entry == itemID {
			return guid
		}
	}
	return 0
}
