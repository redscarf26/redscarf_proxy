package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	SMSGSendKnownSpells   = uint16(11303)
	SMSGSendSpellHistory  = uint16(11304)
	SMSGSendSpellCharges  = uint16(11306)
	SMSGSendUnlearnSpells = uint16(11307)
	SMSGSpellCooldown     = uint16(11285)
	SMSGSupercededSpells  = uint16(11337)
	SMSGLearnedSpells     = uint16(11338)
	SMSGUnlearnedSpells   = uint16(11339)
	SMSGCooldownEvent     = uint16(9913)
	SMSGClearCooldown     = uint16(9914)

	maxKnownSpells    = 2048
	maxSpellHistory   = 256
	maxUnlearnSpells  = 1024
	maxSpellCooldowns = 256
	legacyPetHighGuid = uint16(0xF140)
	spellCooldownRate = float32(1)
)

type SpellHistoryEntry struct {
	SpellID              uint32
	ItemID               uint32
	Category             uint32
	RecoveryTime         int32
	CategoryRecoveryTime int32
}

type KnownSpells struct {
	InitialLogin bool
	Spells       []uint32
	History      []SpellHistoryEntry
}

type SpellCooldown struct {
	SpellID        uint32
	ForcedCooldown uint32
}

func ParseLegacyKnownSpells(body []byte) (KnownSpells, error) {
	var result KnownSpells
	r := movementReader{data: body}
	flag, err := r.u8()
	if err != nil {
		return result, fmt.Errorf("read known-spells initial flag: %w", err)
	}
	result.InitialLogin = flag != 0
	count, err := r.u16()
	if err != nil {
		return result, fmt.Errorf("read known-spells count: %w", err)
	}
	if int(count) > maxKnownSpells {
		return result, fmt.Errorf("known-spells count %d exceeds %d", count, maxKnownSpells)
	}
	result.Spells = make([]uint32, 0, count)
	for index := 0; index < int(count); index++ {
		spellID, err := r.u32()
		if err != nil {
			return result, fmt.Errorf("read known spell %d: %w", index, err)
		}
		if _, err := r.u16(); err != nil {
			return result, fmt.Errorf("read known spell %d slot: %w", index, err)
		}
		result.Spells = append(result.Spells, spellID)
	}
	historyCount, err := r.u16()
	if err != nil {
		return result, fmt.Errorf("read spell-history count: %w", err)
	}
	if int(historyCount) > maxSpellHistory {
		return result, fmt.Errorf("spell-history count %d exceeds %d", historyCount, maxSpellHistory)
	}
	result.History = make([]SpellHistoryEntry, 0, historyCount)
	const cooldownSize = 16
	for index := 0; index < int(historyCount); index++ {
		// AzerothCore writes m_spellCooldowns.size() first, then skips entries
		// with needSendToClient=false or a missing SpellInfo, so the count can
		// be larger than the bytes that follow. Keep the spell list anyway.
		if r.remaining() < cooldownSize {
			break
		}
		entry, err := parseLegacySpellHistoryEntry(&r)
		if err != nil {
			return result, fmt.Errorf("read spell history %d: %w", index, err)
		}
		result.History = append(result.History, entry)
	}
	if r.remaining() >= cooldownSize {
		return result, fmt.Errorf("known-spells has %d trailing bytes", r.remaining())
	}
	return result, nil
}

func parseLegacySpellHistoryEntry(r *movementReader) (SpellHistoryEntry, error) {
	var entry SpellHistoryEntry
	spellID, err := r.u32()
	if err != nil {
		return entry, err
	}
	itemID, err := r.u16()
	if err != nil {
		return entry, err
	}
	category, err := r.u16()
	if err != nil {
		return entry, err
	}
	recovery, err := r.i32()
	if err != nil {
		return entry, err
	}
	categoryRecovery, err := r.i32()
	if err != nil {
		return entry, err
	}
	entry.SpellID = spellID
	entry.ItemID = uint32(itemID)
	entry.Category = uint32(category)
	entry.RecoveryTime = recovery
	entry.CategoryRecoveryTime = categoryRecovery
	return entry, nil
}

func EncodeSendKnownSpells(spells []uint32, initialLogin bool) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(initialLogin)
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, uint32(len(spells)))
	body = binary.LittleEndian.AppendUint32(body, 0)
	for _, spellID := range spells {
		body = binary.LittleEndian.AppendUint32(body, spellID)
	}
	return body
}

func EncodeSendSpellHistory(entries []SpellHistoryEntry) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(entries)))
	for _, entry := range entries {
		body = binary.LittleEndian.AppendUint32(body, entry.SpellID)
		body = binary.LittleEndian.AppendUint32(body, entry.ItemID)
		body = binary.LittleEndian.AppendUint32(body, entry.Category)
		body = binary.LittleEndian.AppendUint32(body, uint32(entry.RecoveryTime))
		body = binary.LittleEndian.AppendUint32(body, uint32(entry.CategoryRecoveryTime))
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(spellCooldownRate))
		bits := newBitWriter(body)
		bits.writeBit(false)
		bits.writeBit(false)
		bits.writeBit(false)
		body = bits.flush()
	}
	return body
}

func ParseLegacyLearnedSpell(body []byte) (uint32, error) {
	r := movementReader{data: body}
	spellID, err := r.u32()
	if err != nil {
		return 0, fmt.Errorf("read learned spell: %w", err)
	}
	switch r.remaining() {
	case 0:
	case 2:
		if _, err := r.u16(); err != nil {
			return 0, fmt.Errorf("read learned-spell extra: %w", err)
		}
	default:
		return 0, fmt.Errorf("learned-spell has %d trailing bytes", r.remaining())
	}
	return spellID, nil
}

func EncodeLearnedSpells(spells []uint32, suppressMessaging bool) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(spells)))
	body = binary.LittleEndian.AppendUint32(body, 0)
	bits := newBitWriter(body)
	bits.writeBit(suppressMessaging)
	body = bits.flush()
	for _, spellID := range spells {
		body = binary.LittleEndian.AppendUint32(body, spellID)
		entryBits := newBitWriter(body)
		entryBits.writeBit(false)
		entryBits.writeBit(false)
		entryBits.writeBit(false)
		entryBits.writeBit(false)
		body = entryBits.flush()
	}
	return body
}

func ParseLegacyUnlearnedSpell(body []byte) (uint32, error) {
	r := movementReader{data: body}
	spellID, err := r.u32()
	if err != nil {
		return 0, fmt.Errorf("read unlearned spell: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("unlearned-spell has %d trailing bytes", r.remaining())
	}
	return spellID, nil
}

func EncodeUnlearnedSpells(spells []uint32, suppressMessaging bool) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(spells)))
	for _, spellID := range spells {
		body = binary.LittleEndian.AppendUint32(body, spellID)
	}
	bits := newBitWriter(body)
	bits.writeBit(suppressMessaging)
	return bits.flush()
}

func ParseLegacySupercededSpells(body []byte) (uint32, uint32, error) {
	r := movementReader{data: body}
	oldSpell, err := r.u32()
	if err != nil {
		return 0, 0, fmt.Errorf("read superceded old spell: %w", err)
	}
	newSpell, err := r.u32()
	if err != nil {
		return 0, 0, fmt.Errorf("read superceded new spell: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("superceded-spells has %d trailing bytes", r.remaining())
	}
	return oldSpell, newSpell, nil
}

func EncodeSupercededSpells(newSpell, oldSpell uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = binary.LittleEndian.AppendUint32(body, newSpell)
	bits := newBitWriter(body)
	bits.writeBit(false)
	bits.writeBit(false)
	bits.writeBit(true)
	bits.writeBit(false)
	body = bits.flush()
	return binary.LittleEndian.AppendUint32(body, oldSpell)
}

func ParseLegacySendUnlearnSpells(body []byte) ([]uint32, error) {
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read send-unlearn count: %w", err)
	}
	if int(count) > maxUnlearnSpells {
		return nil, fmt.Errorf("send-unlearn count %d exceeds %d", count, maxUnlearnSpells)
	}
	spells := make([]uint32, 0, count)
	for index := 0; index < int(count); index++ {
		spellID, err := r.u32()
		if err != nil {
			return nil, fmt.Errorf("read send-unlearn spell %d: %w", index, err)
		}
		spells = append(spells, spellID)
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("send-unlearn has %d trailing bytes", r.remaining())
	}
	return spells, nil
}

func EncodeSendUnlearnSpells(spells []uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(spells)))
	for _, spellID := range spells {
		body = binary.LittleEndian.AppendUint32(body, spellID)
	}
	return body
}

func EncodeSendSpellCharges(count uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, count)
}

func ParseLegacySpellCooldown(body []byte) (uint64, byte, []SpellCooldown, error) {
	if guid, flags, cooldowns, err := parseLegacySpellCooldown(body, true); err == nil {
		return guid, flags, cooldowns, nil
	}
	return parseLegacySpellCooldown(body, false)
}

func parseLegacySpellCooldown(body []byte, packed bool) (uint64, byte, []SpellCooldown, error) {
	r := movementReader{data: body}
	var (
		guid uint64
		err  error
	)
	if packed {
		guid, err = r.guid64()
	} else {
		guid, err = r.u64()
	}
	if err != nil {
		return 0, 0, nil, fmt.Errorf("read spell-cooldown GUID: %w", err)
	}
	flags, err := r.u8()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("read spell-cooldown flags: %w", err)
	}
	if r.remaining()%8 != 0 {
		return 0, 0, nil, fmt.Errorf("spell-cooldown remainder %d is not a multiple of 8", r.remaining())
	}
	count := r.remaining() / 8
	if count > maxSpellCooldowns {
		return 0, 0, nil, fmt.Errorf("spell-cooldown count %d exceeds %d", count, maxSpellCooldowns)
	}
	cooldowns := make([]SpellCooldown, 0, count)
	for index := 0; index < count; index++ {
		spellID, err := r.u32()
		if err != nil {
			return 0, 0, nil, fmt.Errorf("read spell-cooldown %d id: %w", index, err)
		}
		forced, err := r.u32()
		if err != nil {
			return 0, 0, nil, fmt.Errorf("read spell-cooldown %d duration: %w", index, err)
		}
		cooldowns = append(cooldowns, SpellCooldown{SpellID: spellID, ForcedCooldown: forced})
	}
	return guid, flags, cooldowns, nil
}

func EncodeSpellCooldown(caster GUID128, flags byte, cooldowns []SpellCooldown) []byte {
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	body = append(body, flags)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(cooldowns)))
	for _, cooldown := range cooldowns {
		body = binary.LittleEndian.AppendUint32(body, cooldown.SpellID)
		body = binary.LittleEndian.AppendUint32(body, cooldown.ForcedCooldown)
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(spellCooldownRate))
	}
	return body
}

func ParseLegacyCooldownSpellAndGUID(body []byte) (uint32, uint64, error) {
	r := movementReader{data: body}
	spellID, err := r.u32()
	if err != nil {
		return 0, 0, fmt.Errorf("read cooldown spell: %w", err)
	}
	guid, err := parseLegacyPackedOrUnpackedGUID(&r)
	if err != nil {
		return 0, 0, fmt.Errorf("read cooldown GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("cooldown packet has %d trailing bytes", r.remaining())
	}
	return spellID, guid, nil
}

func EncodeCooldownEvent(spellID uint32, isPet bool) []byte {
	body := binary.LittleEndian.AppendUint32(nil, spellID)
	bits := newBitWriter(body)
	bits.writeBit(isPet)
	return bits.flush()
}

func EncodeClearCooldown(spellID uint32, isPet bool) []byte {
	body := binary.LittleEndian.AppendUint32(nil, spellID)
	bits := newBitWriter(body)
	bits.writeBit(false)
	bits.writeBit(isPet)
	return bits.flush()
}

func LegacyGUIDIsPet(guid uint64) bool {
	return uint16(guid>>48) == legacyPetHighGuid
}

func parseLegacyPackedOrUnpackedGUID(r *movementReader) (uint64, error) {
	if r.remaining() == 8 {
		return r.u64()
	}
	return r.guid64()
}
