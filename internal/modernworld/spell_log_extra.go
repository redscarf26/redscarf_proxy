package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Extra legacy spell combat-log / spell-state packets. Layouts mirror
// HermesProxy-WOTLK World/Server/Packets {CancelAutoRepeat,
// EnvironmentalDamageLog,PetCastFailed,PlaySpellVisualKit,SpellDamageShield,
// SpellDispellLog,SpellInstakillLog,TotemCreated}.cs and the matching
// World/Client/WorldClient.cs handlers. Note the two order traps: legacy
// SMSG_SPELL_INSTAKILL_LOG is caster,target while the modern packet is
// target,caster; and AzerothCore's refactored environmental-damage log writes
// Resisted before Absorbed.

const (
	SMSGCancelAutoRepeat       = uint16(9950)
	SMSGEnvironmentalDamageLog = uint16(11294)
	SMSGPetCastFailed          = uint16(11349)
	SMSGPlaySpellVisualKit     = uint16(11334) // legacy SMSG_PLAY_SPELL_VISUAL maps here
	SMSGSpellDamageShield      = uint16(11310)
	SMSGSpellDispellLog        = uint16(11287)
	SMSGSpellInstakillLog      = uint16(11312)
	SMSGTotemCreated           = uint16(9928)
)

// ParseLegacyCancelAutoRepeat reads the packed target GUID of the legacy cancel.
func ParseLegacyCancelAutoRepeat(body []byte) (uint64, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, fmt.Errorf("read cancel-auto-repeat target: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("cancel-auto-repeat has %d trailing bytes", r.remaining())
	}
	return guid, nil
}

func EncodeCancelAutoRepeat(target GUID128) []byte {
	return appendPackedGUID128(nil, target.Low, target.High)
}

// LegacyEnvironmentalDamage mirrors the legacy SMSG_ENVIRONMENTAL_DAMAGE_LOG.
type LegacyEnvironmentalDamage struct {
	Victim   uint64
	Type     uint8
	Amount   uint32
	Resisted uint32
	Absorbed uint32
}

func ParseLegacyEnvironmentalDamage(body []byte) (LegacyEnvironmentalDamage, error) {
	var damage LegacyEnvironmentalDamage
	r := movementReader{data: body}
	var err error
	if damage.Victim, err = r.u64(); err != nil {
		return damage, fmt.Errorf("read environmental victim: %w", err)
	}
	if damage.Type, err = r.u8(); err != nil {
		return damage, fmt.Errorf("read environmental type: %w", err)
	}
	if damage.Amount, err = r.u32(); err != nil {
		return damage, fmt.Errorf("read environmental amount: %w", err)
	}
	// AzerothCore's current packet order writes Resisted then Absorbed.
	if damage.Resisted, err = r.u32(); err != nil {
		return damage, fmt.Errorf("read environmental resisted: %w", err)
	}
	if damage.Absorbed, err = r.u32(); err != nil {
		return damage, fmt.Errorf("read environmental absorbed: %w", err)
	}
	if r.remaining() != 0 {
		return damage, fmt.Errorf("environmental damage has %d trailing bytes", r.remaining())
	}
	return damage, nil
}

func EncodeEnvironmentalDamage(damage LegacyEnvironmentalDamage, victim GUID128) []byte {
	body := appendPackedGUID128(nil, victim.Low, victim.High)
	body = append(body, damage.Type)
	body = binary.LittleEndian.AppendUint32(body, damage.Amount)
	body = binary.LittleEndian.AppendUint32(body, damage.Resisted)
	body = binary.LittleEndian.AppendUint32(body, damage.Absorbed)
	bits := newBitWriter(body)
	bits.writeBit(false) // LogData
	return bits.flush()
}

// LegacyPetCastFailed mirrors the legacy SMSG_PET_CAST_FAILED (no pet guid: the
// packet is cast count + spell id + result plus up to two trailing failure args).
type LegacyPetCastFailed struct {
	SpellID uint32
	Reason  uint8
	Arg1    int32
	Arg2    int32
}

func ParseLegacyPetCastFailed(body []byte) (LegacyPetCastFailed, error) {
	var failure LegacyPetCastFailed
	r := movementReader{data: body}
	var err error
	if _, err = r.u8(); err != nil { // cast count
		return failure, fmt.Errorf("read pet-cast-failed count: %w", err)
	}
	if failure.SpellID, err = r.u32(); err != nil {
		return failure, fmt.Errorf("read pet-cast-failed spell: %w", err)
	}
	if failure.Reason, err = r.u8(); err != nil {
		return failure, fmt.Errorf("read pet-cast-failed reason: %w", err)
	}
	failure.Arg1, failure.Arg2 = -1, -1
	switch r.remaining() {
	case 0:
	case 4:
		if failure.Arg1, err = r.i32(); err != nil {
			return failure, fmt.Errorf("read pet-cast-failed arg1: %w", err)
		}
	case 8:
		if failure.Arg1, err = r.i32(); err != nil {
			return failure, fmt.Errorf("read pet-cast-failed arg1: %w", err)
		}
		if failure.Arg2, err = r.i32(); err != nil {
			return failure, fmt.Errorf("read pet-cast-failed arg2: %w", err)
		}
	default:
		return failure, fmt.Errorf("pet-cast-failed has %d trailing bytes", r.remaining())
	}
	return failure, nil
}

func EncodePetCastFailed(castID GUID128, failure LegacyPetCastFailed) []byte {
	body := appendPackedGUID128(nil, castID.Low, castID.High)
	body = binary.LittleEndian.AppendUint32(body, failure.SpellID)
	body = binary.LittleEndian.AppendUint32(body, uint32(ConvertSpellCastResult343(uint16(failure.Reason))))
	body = binary.LittleEndian.AppendUint32(body, uint32(failure.Arg1))
	return binary.LittleEndian.AppendUint32(body, uint32(failure.Arg2))
}

// ParseLegacyPlaySpellVisual reads the caster GUID and the spell-visual kit id.
func ParseLegacyPlaySpellVisual(body []byte) (caster uint64, kitID uint32, err error) {
	r := movementReader{data: body}
	if caster, err = r.u64(); err != nil {
		return 0, 0, fmt.Errorf("read play-spell-visual caster: %w", err)
	}
	if kitID, err = r.u32(); err != nil {
		return 0, 0, fmt.Errorf("read play-spell-visual kit: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("play-spell-visual has %d trailing bytes", r.remaining())
	}
	return caster, kitID, nil
}

func EncodePlaySpellVisualKit(caster GUID128, kitID uint32) []byte {
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, kitID)
	body = binary.LittleEndian.AppendUint32(body, 0) // KitType
	body = binary.LittleEndian.AppendUint32(body, 0) // Duration
	bits := newBitWriter(body)
	bits.writeBit(false) // MountedVisual
	return bits.flush()
}

// LegacySpellDamageShield mirrors the legacy SMSG_SPELL_DAMAGE_SHIELD.
type LegacySpellDamageShield struct {
	Victim   uint64
	Caster   uint64
	SpellID  uint32
	Damage   uint32
	Overkill uint32
	School   uint32
}

func ParseLegacySpellDamageShield(body []byte) (LegacySpellDamageShield, error) {
	var shield LegacySpellDamageShield
	r := movementReader{data: body}
	var err error
	if shield.Victim, err = r.u64(); err != nil {
		return shield, fmt.Errorf("read damage-shield victim: %w", err)
	}
	if shield.Caster, err = r.u64(); err != nil {
		return shield, fmt.Errorf("read damage-shield caster: %w", err)
	}
	if shield.SpellID, err = r.u32(); err != nil {
		return shield, fmt.Errorf("read damage-shield spell: %w", err)
	}
	if shield.Damage, err = r.u32(); err != nil {
		return shield, fmt.Errorf("read damage-shield amount: %w", err)
	}
	if shield.Overkill, err = r.u32(); err != nil {
		return shield, fmt.Errorf("read damage-shield overkill: %w", err)
	}
	if shield.School, err = r.u32(); err != nil {
		return shield, fmt.Errorf("read damage-shield school: %w", err)
	}
	if r.remaining() != 0 {
		return shield, fmt.Errorf("damage-shield has %d trailing bytes", r.remaining())
	}
	return shield, nil
}

func EncodeSpellDamageShield(shield LegacySpellDamageShield, victim, caster GUID128) []byte {
	body := appendPackedGUID128(nil, victim.Low, victim.High)
	body = appendPackedGUID128(body, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, shield.SpellID)
	body = binary.LittleEndian.AppendUint32(body, shield.Damage)
	body = binary.LittleEndian.AppendUint32(body, shield.Damage) // OriginalDamage
	body = binary.LittleEndian.AppendUint32(body, shield.Overkill)
	body = binary.LittleEndian.AppendUint32(body, shield.School)
	body = binary.LittleEndian.AppendUint32(body, 0) // LogAbsorbed
	bits := newBitWriter(body)
	bits.writeBit(false) // LogData
	return bits.flush()
}

// LegacyDispelEntry is one dispelled (or cleansed) buff on the target.
type LegacyDispelEntry struct {
	SpellID uint32
	Harmful bool
}

// LegacySpellDispellLog mirrors the legacy SMSG_SPELL_DISPELL_LOG.
type LegacySpellDispellLog struct {
	Target      uint64
	Caster      uint64
	SpellID     uint32
	DispelledBy []LegacyDispelEntry
}

func ParseLegacySpellDispellLog(body []byte) (LegacySpellDispellLog, error) {
	var log LegacySpellDispellLog
	r := movementReader{data: body}
	var err error
	if log.Target, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read dispell target: %w", err)
	}
	if log.Caster, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read dispell caster: %w", err)
	}
	if log.SpellID, err = r.u32(); err != nil {
		return log, fmt.Errorf("read dispell spell: %w", err)
	}
	if _, err = r.u8(); err != nil { // unused debug flag
		return log, fmt.Errorf("read dispell debug flag: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read dispell count: %w", err)
	}
	if count > 64 {
		return log, fmt.Errorf("dispell count %d unreasonable", count)
	}
	log.DispelledBy = make([]LegacyDispelEntry, 0, count)
	for index := uint32(0); index < count; index++ {
		var entry LegacyDispelEntry
		if entry.SpellID, err = r.u32(); err != nil {
			return log, fmt.Errorf("read dispell entry %d spell: %w", index, err)
		}
		harmful, readErr := r.u8()
		if readErr != nil {
			return log, fmt.Errorf("read dispell entry %d harmful: %w", index, readErr)
		}
		entry.Harmful = harmful != 0
		log.DispelledBy = append(log.DispelledBy, entry)
	}
	if r.remaining() != 0 {
		return log, fmt.Errorf("dispell log has %d trailing bytes", r.remaining())
	}
	return log, nil
}

func EncodeSpellDispellLog(log LegacySpellDispellLog, target, caster GUID128) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(false) // IsSteal
	bits.writeBit(false) // IsBreak
	body := bits.flush()
	body = appendPackedGUID128(body, target.Low, target.High)
	body = appendPackedGUID128(body, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, log.SpellID)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(log.DispelledBy)))
	for _, entry := range log.DispelledBy {
		body = binary.LittleEndian.AppendUint32(body, entry.SpellID)
		entryBits := newBitWriter(body)
		entryBits.writeBit(entry.Harmful)
		entryBits.writeBit(false) // Rolled
		entryBits.writeBit(false) // Needed
		body = entryBits.flush()
	}
	return body
}

// LegacySpellInstakill mirrors the legacy SMSG_SPELL_INSTAKILL_LOG. The legacy
// payload is caster,target,spell; the modern packet is target,caster,spell.
type LegacySpellInstakill struct {
	Caster  uint64
	Target  uint64
	SpellID uint32
}

func ParseLegacySpellInstakill(body []byte) (LegacySpellInstakill, error) {
	var kill LegacySpellInstakill
	r := movementReader{data: body}
	var err error
	if kill.Caster, err = r.u64(); err != nil {
		return kill, fmt.Errorf("read instakill caster: %w", err)
	}
	if kill.Target, err = r.u64(); err != nil {
		return kill, fmt.Errorf("read instakill target: %w", err)
	}
	if kill.SpellID, err = r.u32(); err != nil {
		return kill, fmt.Errorf("read instakill spell: %w", err)
	}
	if r.remaining() != 0 {
		return kill, fmt.Errorf("instakill has %d trailing bytes", r.remaining())
	}
	return kill, nil
}

func EncodeSpellInstakill(kill LegacySpellInstakill, caster, target GUID128) []byte {
	body := appendPackedGUID128(nil, target.Low, target.High) // modern order: target first
	body = appendPackedGUID128(body, caster.Low, caster.High)
	return binary.LittleEndian.AppendUint32(body, kill.SpellID)
}

// LegacyTotemCreated mirrors the legacy SMSG_TOTEM_CREATED.
type LegacyTotemCreated struct {
	Slot     uint8
	Totem    uint64
	Duration uint32
	SpellID  uint32
}

func ParseLegacyTotemCreated(body []byte) (LegacyTotemCreated, error) {
	var totem LegacyTotemCreated
	r := movementReader{data: body}
	var err error
	if totem.Slot, err = r.u8(); err != nil {
		return totem, fmt.Errorf("read totem-created slot: %w", err)
	}
	if totem.Totem, err = r.u64(); err != nil {
		return totem, fmt.Errorf("read totem-created totem: %w", err)
	}
	if totem.Duration, err = r.u32(); err != nil {
		return totem, fmt.Errorf("read totem-created duration: %w", err)
	}
	if totem.SpellID, err = r.u32(); err != nil {
		return totem, fmt.Errorf("read totem-created spell: %w", err)
	}
	if r.remaining() != 0 {
		return totem, fmt.Errorf("totem-created has %d trailing bytes", r.remaining())
	}
	return totem, nil
}

func EncodeTotemCreated(totem LegacyTotemCreated, totemGUID GUID128) []byte {
	body := []byte{totem.Slot}
	body = appendPackedGUID128(body, totemGUID.Low, totemGUID.High)
	body = binary.LittleEndian.AppendUint32(body, totem.Duration)
	body = binary.LittleEndian.AppendUint32(body, totem.SpellID)
	body = binary.LittleEndian.AppendUint32(body, math.Float32bits(1.0)) // TimeMod
	bits := newBitWriter(body)
	bits.writeBit(false) // CannotDismiss
	return bits.flush()
}
