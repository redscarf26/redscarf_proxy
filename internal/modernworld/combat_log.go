package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGLogXPGain              = uint16(9957)
	SMSGSpellPeriodicAuraLog   = uint16(11288)
	SMSGSpellEnergizeLog       = uint16(11289)
	SMSGSpellHealLog           = uint16(11290)
	SMSGSpellNonMeleeDamageLog = uint16(11311)
	SMSGSpellDelayed           = uint16(11324)
	SMSGSpellExecuteLog        = uint16(0x2C3D)
	SMSGSpellInterruptLog      = uint16(0x2C1D)
)

type SpellExecutePowerDrain struct {
	Victim    uint64
	Points    uint32
	PowerType uint32
	Amplitude float32
}

type SpellExecuteExtraAttacks struct {
	Victim     uint64
	NumAttacks uint32
}

type SpellExecuteDurabilityDamage struct {
	Victim uint64
	ItemID int32
	Amount int32
}

type SpellExecuteEffect struct {
	Effect           uint32
	PowerDrains      []SpellExecutePowerDrain
	ExtraAttacks     []SpellExecuteExtraAttacks
	DurabilityDamage []SpellExecuteDurabilityDamage
	GenericVictims   []uint64
	TradeSkillItems  []int32
	FeedPetItems     []int32
	Interrupts       []SpellExecuteInterrupt
}

type SpellExecuteInterrupt struct {
	Victim  uint64
	SpellID uint32
}

type LegacySpellExecuteLog struct {
	Caster  uint64
	SpellID uint32
	Effects []SpellExecuteEffect
}

type SpellNonMeleeDamageLog struct {
	Target   uint64
	Caster   uint64
	SpellID  uint32
	Damage   int32
	Overkill int32
	School   byte
	Absorbed int32
	Resisted int32
	Periodic bool
	Blocked  int32
	HitFlags uint32
}

type SpellHealLog struct {
	Target         uint64
	Caster         uint64
	SpellID        uint32
	Amount         int32
	Overheal       uint32
	Absorbed       uint32
	Crit           bool
	HasCritRoll    bool
	CritRollMade   float32
	CritRollNeeded float32
}

type SpellPeriodicAuraEffect struct {
	Effect              uint32
	Amount              int32
	OriginalDamage      int32
	OverHealOrKill      uint32
	SchoolMaskOrPower   uint32
	AbsorbedOrAmplitude uint32
	Resisted            uint32
	Crit                bool
}

type SpellPeriodicAuraLog struct {
	Target  uint64
	Caster  uint64
	SpellID uint32
	Effects []SpellPeriodicAuraEffect
}

type SpellDelayed struct {
	Caster uint64
	Delay  int32
}

type SpellEnergizeLog struct {
	Target     uint64
	Caster     uint64
	SpellID    uint32
	PowerType  uint32
	Amount     int32
	OverAmount int32
}

type LogXPGain struct {
	Victim     uint64
	Original   int32
	Reason     byte
	Amount     int32
	GroupBonus float32
	RAFBonus   byte
}

func ParseLegacySpellNonMeleeDamageLog(body []byte) (SpellNonMeleeDamageLog, error) {
	var log SpellNonMeleeDamageLog
	r := movementReader{data: body}
	var err error
	if log.Target, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read non-melee target: %w", err)
	}
	if log.Caster, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read non-melee caster: %w", err)
	}
	if log.SpellID, err = r.u32(); err != nil {
		return log, fmt.Errorf("read non-melee spell: %w", err)
	}
	damage, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read non-melee damage: %w", err)
	}
	log.Damage = int32(damage)
	if log.Overkill, err = r.i32(); err != nil {
		return log, fmt.Errorf("read non-melee overkill: %w", err)
	}
	if log.School, err = r.u8(); err != nil {
		return log, fmt.Errorf("read non-melee school: %w", err)
	}
	absorbed, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read non-melee absorb: %w", err)
	}
	log.Absorbed = int32(absorbed)
	if log.Resisted, err = r.i32(); err != nil {
		return log, fmt.Errorf("read non-melee resist: %w", err)
	}
	periodic, err := r.u8()
	if err != nil {
		return log, fmt.Errorf("read non-melee periodic: %w", err)
	}
	log.Periodic = periodic != 0
	if _, err = r.u8(); err != nil {
		return log, fmt.Errorf("read non-melee unused: %w", err)
	}
	blocked, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read non-melee block: %w", err)
	}
	log.Blocked = int32(blocked)
	if log.HitFlags, err = r.u32(); err != nil {
		return log, fmt.Errorf("read non-melee hit-flags: %w", err)
	}
	// WotLK sends extend_flag plus an optional uint32. AC also appends
	// extra debug/content-tuning bytes. Skip leftovers instead of dropping
	// the damage event — that is why thrown-weapon numbers never appeared.
	if _, err = r.take(r.remaining()); err != nil {
		return log, fmt.Errorf("skip non-melee trailing: %w", err)
	}
	return log, nil
}

func EncodeSpellNonMeleeDamageLog(target, caster, castID GUID128, log SpellNonMeleeDamageLog) []byte {
	body := appendPackedGUID128(nil, target.Low, target.High)
	body = appendPackedGUID128(body, caster.Low, caster.High)
	body = appendPackedGUID128(body, castID.Low, castID.High)
	body = binary.LittleEndian.AppendUint32(body, log.SpellID)
	body = binary.LittleEndian.AppendUint32(body, 0) // SpellXSpellVisualID
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Damage))
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Damage))
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Overkill))
	body = append(body, log.School)
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Absorbed))
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Resisted))
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Blocked))
	bits := newBitWriter(body)
	bits.writeBit(log.Periodic)
	bits.writeBits(log.HitFlags, 7)
	bits.writeBit(false) // debug
	bits.writeBit(false) // log data
	bits.writeBit(false) // content tuning
	return bits.flush()
}

func ParseLegacySpellHealLog(body []byte) (SpellHealLog, error) {
	var log SpellHealLog
	r := movementReader{data: body}
	var err error
	if log.Target, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read heal-log target: %w", err)
	}
	if log.Caster, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read heal-log caster: %w", err)
	}
	if log.SpellID, err = r.u32(); err != nil {
		return log, fmt.Errorf("read heal-log spell: %w", err)
	}
	amount, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read heal-log amount: %w", err)
	}
	log.Amount = int32(amount)
	if log.Overheal, err = r.u32(); err != nil {
		return log, fmt.Errorf("read heal-log overheal: %w", err)
	}
	if log.Absorbed, err = r.u32(); err != nil {
		return log, fmt.Errorf("read heal-log absorb: %w", err)
	}
	crit, err := r.u8()
	if err != nil {
		return log, fmt.Errorf("read heal-log crit: %w", err)
	}
	log.Crit = crit != 0
	if r.remaining() > 0 {
		debug, readErr := r.u8()
		if readErr != nil {
			return log, fmt.Errorf("read heal-log debug flag: %w", readErr)
		}
		if debug > 1 {
			return log, fmt.Errorf("heal-log debug flag is %d, want 0 or 1", debug)
		}
		if debug != 0 {
			if log.CritRollMade, readErr = r.f32(); readErr != nil {
				return log, fmt.Errorf("read heal-log crit roll: %w", readErr)
			}
			if log.CritRollNeeded, readErr = r.f32(); readErr != nil {
				return log, fmt.Errorf("read heal-log needed roll: %w", readErr)
			}
			log.HasCritRoll = true
		}
	}
	if r.remaining() != 0 {
		return log, fmt.Errorf("heal-log has %d trailing bytes", r.remaining())
	}
	return log, nil
}

func EncodeSpellHealLog(target, caster GUID128, log SpellHealLog) []byte {
	body := appendPackedGUID128(nil, target.Low, target.High)
	body = appendPackedGUID128(body, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, log.SpellID)
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Amount))
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Amount))
	body = binary.LittleEndian.AppendUint32(body, log.Overheal)
	body = binary.LittleEndian.AppendUint32(body, log.Absorbed)
	bits := newBitWriter(body)
	bits.writeBit(log.Crit)
	bits.writeBit(log.HasCritRoll)
	bits.writeBit(log.HasCritRoll)
	bits.writeBit(false) // log data
	bits.writeBit(false) // content tuning
	body = bits.flush()
	if log.HasCritRoll {
		body = appendFloat32(body, log.CritRollMade)
		body = appendFloat32(body, log.CritRollNeeded)
	}
	return body
}

func ParseLegacySpellPeriodicAuraLog(body []byte) (SpellPeriodicAuraLog, error) {
	var log SpellPeriodicAuraLog
	r := movementReader{data: body}
	var err error
	if log.Target, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read periodic-aura target: %w", err)
	}
	if log.Caster, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read periodic-aura caster: %w", err)
	}
	if log.SpellID, err = r.u32(); err != nil {
		return log, fmt.Errorf("read periodic-aura spell: %w", err)
	}
	count, err := r.i32()
	if err != nil {
		return log, fmt.Errorf("read periodic-aura effect count: %w", err)
	}
	if count < 0 || count > 32 {
		return log, fmt.Errorf("periodic-aura effect count %d is invalid", count)
	}
	log.Effects = make([]SpellPeriodicAuraEffect, 0, count)
	for index := int32(0); index < count; index++ {
		var effect SpellPeriodicAuraEffect
		if effect.Effect, err = r.u32(); err != nil {
			return log, fmt.Errorf("read periodic-aura effect %d type: %w", index, err)
		}
		switch effect.Effect {
		case 3, 89: // PeriodicDamage, PeriodicDamagePercent
			if effect.Amount, err = r.i32(); err != nil {
				return log, fmt.Errorf("read periodic damage %d amount: %w", index, err)
			}
			effect.OriginalDamage = effect.Amount
			if effect.OverHealOrKill, err = r.u32(); err != nil {
				return log, fmt.Errorf("read periodic damage %d overkill: %w", index, err)
			}
			if effect.SchoolMaskOrPower, err = r.u32(); err != nil {
				return log, fmt.Errorf("read periodic damage %d school: %w", index, err)
			}
			if effect.AbsorbedOrAmplitude, err = r.u32(); err != nil {
				return log, fmt.Errorf("read periodic damage %d absorbed: %w", index, err)
			}
			if effect.Resisted, err = r.u32(); err != nil {
				return log, fmt.Errorf("read periodic damage %d resisted: %w", index, err)
			}
			crit, readErr := r.u8()
			if readErr != nil {
				return log, fmt.Errorf("read periodic damage %d crit: %w", index, readErr)
			}
			effect.Crit = crit != 0
		case 8, 20: // PeriodicHeal, ObsModHealth
			if effect.Amount, err = r.i32(); err != nil {
				return log, fmt.Errorf("read periodic heal %d amount: %w", index, err)
			}
			effect.OriginalDamage = effect.Amount
			if effect.OverHealOrKill, err = r.u32(); err != nil {
				return log, fmt.Errorf("read periodic heal %d overheal: %w", index, err)
			}
			if effect.AbsorbedOrAmplitude, err = r.u32(); err != nil {
				return log, fmt.Errorf("read periodic heal %d absorbed: %w", index, err)
			}
			crit, readErr := r.u8()
			if readErr != nil {
				return log, fmt.Errorf("read periodic heal %d crit: %w", index, readErr)
			}
			effect.Crit = crit != 0
		case 21, 24: // ObsModPower, PeriodicEnergize
			if effect.SchoolMaskOrPower, err = r.u32(); err != nil {
				return log, fmt.Errorf("read periodic power %d type: %w", index, err)
			}
			if effect.Amount, err = r.i32(); err != nil {
				return log, fmt.Errorf("read periodic power %d amount: %w", index, err)
			}
		case 64: // PeriodicManaLeech
			if effect.SchoolMaskOrPower, err = r.u32(); err != nil {
				return log, fmt.Errorf("read periodic leech %d power: %w", index, err)
			}
			if effect.Amount, err = r.i32(); err != nil {
				return log, fmt.Errorf("read periodic leech %d amount: %w", index, err)
			}
			if _, err = r.f32(); err != nil { // gain multiplier has no modern field
				return log, fmt.Errorf("read periodic leech %d multiplier: %w", index, err)
			}
		default:
			return log, fmt.Errorf("periodic-aura effect %d has unsupported aura type %d", index, effect.Effect)
		}
		log.Effects = append(log.Effects, effect)
	}
	if r.remaining() != 0 {
		return log, fmt.Errorf("periodic-aura log has %d trailing bytes", r.remaining())
	}
	return log, nil
}

func EncodeSpellPeriodicAuraLog(target, caster GUID128, log SpellPeriodicAuraLog) []byte {
	body := appendPackedGUID128(nil, target.Low, target.High)
	body = appendPackedGUID128(body, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, log.SpellID)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(log.Effects)))
	bits := newBitWriter(body)
	bits.writeBit(false) // no SpellCastLogData source in 3.3.5a
	body = bits.flush()
	for _, effect := range log.Effects {
		body = binary.LittleEndian.AppendUint32(body, effect.Effect)
		body = binary.LittleEndian.AppendUint32(body, uint32(effect.Amount))
		body = binary.LittleEndian.AppendUint32(body, uint32(effect.OriginalDamage))
		body = binary.LittleEndian.AppendUint32(body, effect.OverHealOrKill)
		body = binary.LittleEndian.AppendUint32(body, effect.SchoolMaskOrPower)
		body = binary.LittleEndian.AppendUint32(body, effect.AbsorbedOrAmplitude)
		body = binary.LittleEndian.AppendUint32(body, effect.Resisted)
		effectBits := newBitWriter(body)
		effectBits.writeBit(effect.Crit)
		effectBits.writeBit(false) // debug info
		effectBits.writeBit(false) // content tuning
		body = effectBits.flush()
	}
	return body
}

func ParseLegacySpellDelayed(body []byte) (SpellDelayed, error) {
	var delayed SpellDelayed
	r := movementReader{data: body}
	var err error
	if delayed.Caster, err = r.guid64(); err != nil {
		return delayed, fmt.Errorf("read spell-delayed caster: %w", err)
	}
	if delayed.Delay, err = r.i32(); err != nil {
		return delayed, fmt.Errorf("read spell-delayed duration: %w", err)
	}
	if r.remaining() != 0 {
		return delayed, fmt.Errorf("spell-delayed has %d trailing bytes", r.remaining())
	}
	return delayed, nil
}

func EncodeSpellDelayed(caster GUID128, delayed SpellDelayed) []byte {
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	return binary.LittleEndian.AppendUint32(body, uint32(delayed.Delay))
}

func ParseLegacySpellEnergizeLog(body []byte) (SpellEnergizeLog, error) {
	var log SpellEnergizeLog
	r := movementReader{data: body}
	var err error
	if log.Target, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read energize-log target: %w", err)
	}
	if log.Caster, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read energize-log caster: %w", err)
	}
	if log.SpellID, err = r.u32(); err != nil {
		return log, fmt.Errorf("read energize-log spell: %w", err)
	}
	if log.PowerType, err = r.u32(); err != nil {
		return log, fmt.Errorf("read energize-log power: %w", err)
	}
	amount, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read energize-log amount: %w", err)
	}
	log.Amount = int32(amount)
	if r.remaining() != 0 {
		return log, fmt.Errorf("energize-log has %d trailing bytes", r.remaining())
	}
	return log, nil
}

func EncodeSpellEnergizeLog(target, caster GUID128, log SpellEnergizeLog) []byte {
	body := appendPackedGUID128(nil, target.Low, target.High)
	body = appendPackedGUID128(body, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, log.SpellID)
	body = binary.LittleEndian.AppendUint32(body, log.PowerType)
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Amount))
	body = binary.LittleEndian.AppendUint32(body, uint32(log.OverAmount))
	bits := newBitWriter(body)
	bits.writeBit(false)
	return bits.flush()
}

func ParseLegacyLogXPGain(body []byte) (LogXPGain, error) {
	var log LogXPGain
	r := movementReader{data: body}
	var err error
	if log.Victim, err = r.u64(); err != nil {
		return log, fmt.Errorf("read xp-gain victim: %w", err)
	}
	original, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read xp-gain original: %w", err)
	}
	log.Original = int32(original)
	if log.Reason, err = r.u8(); err != nil {
		return log, fmt.Errorf("read xp-gain reason: %w", err)
	}
	log.GroupBonus = 1
	if log.Reason == 0 {
		amount, readErr := r.u32()
		if readErr != nil {
			return log, fmt.Errorf("read xp-gain rest: %w", readErr)
		}
		log.Amount = int32(amount)
		if log.GroupBonus, err = r.f32(); err != nil {
			return log, fmt.Errorf("read xp-gain group bonus: %w", err)
		}
	}
	if r.remaining() > 0 {
		if log.RAFBonus, err = r.u8(); err != nil {
			return log, fmt.Errorf("read xp-gain RAF: %w", err)
		}
	}
	if r.remaining() != 0 {
		return log, fmt.Errorf("xp-gain has %d trailing bytes", r.remaining())
	}
	return log, nil
}

func EncodeLogXPGain(victim GUID128, log LogXPGain) []byte {
	body := appendPackedGUID128(nil, victim.Low, victim.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Original))
	body = append(body, log.Reason)
	body = binary.LittleEndian.AppendUint32(body, uint32(log.Amount))
	body = appendFloat32(body, log.GroupBonus)
	return append(body, log.RAFBonus)
}

func ParseLegacySpellExecuteLog(body []byte) (LegacySpellExecuteLog, error) {
	var log LegacySpellExecuteLog
	r := movementReader{data: body}
	var err error
	if log.Caster, err = r.guid64(); err != nil {
		return log, fmt.Errorf("read spell-execute caster: %w", err)
	}
	if log.SpellID, err = r.u32(); err != nil {
		return log, fmt.Errorf("read spell-execute spell: %w", err)
	}
	effectCount, err := r.u32()
	if err != nil {
		return log, fmt.Errorf("read spell-execute effect count: %w", err)
	}
	if effectCount > 32 {
		return log, fmt.Errorf("spell-execute has %d effects, maximum is 32", effectCount)
	}
	log.Effects = make([]SpellExecuteEffect, effectCount)
	for effectIndex := range log.Effects {
		effect := &log.Effects[effectIndex]
		if effect.Effect, err = r.u32(); err != nil {
			return log, fmt.Errorf("read spell-execute effect %d: %w", effectIndex, err)
		}
		targetCount, readErr := r.u32()
		if readErr != nil {
			return log, fmt.Errorf("read spell-execute effect %d target count: %w", effectIndex, readErr)
		}
		if targetCount > 128 {
			return log, fmt.Errorf("spell-execute effect %d has %d targets, maximum is 128", effect.Effect, targetCount)
		}
		switch effect.Effect {
		case 68: // SPELL_EFFECT_INTERRUPT_CAST
			effect.Interrupts = make([]SpellExecuteInterrupt, targetCount)
			for i := range effect.Interrupts {
				if effect.Interrupts[i].Victim, err = r.guid64(); err != nil {
					return log, err
				}
				if effect.Interrupts[i].SpellID, err = r.u32(); err != nil {
					return log, err
				}
			}
		case 8, 62: // SPELL_EFFECT_POWER_DRAIN / SPELL_EFFECT_POWER_BURN
			effect.PowerDrains = make([]SpellExecutePowerDrain, targetCount)
			for targetIndex := range effect.PowerDrains {
				target := &effect.PowerDrains[targetIndex]
				if target.Victim, err = r.guid64(); err != nil {
					return log, fmt.Errorf("read power-drain victim: %w", err)
				}
				if target.Points, err = r.u32(); err != nil {
					return log, fmt.Errorf("read power-drain points: %w", err)
				}
				if target.PowerType, err = r.u32(); err != nil {
					return log, fmt.Errorf("read power-drain type: %w", err)
				}
				if target.Amplitude, err = r.f32(); err != nil {
					return log, fmt.Errorf("read power-drain amplitude: %w", err)
				}
			}
		case 19: // SPELL_EFFECT_ADD_EXTRA_ATTACKS
			effect.ExtraAttacks = make([]SpellExecuteExtraAttacks, targetCount)
			for targetIndex := range effect.ExtraAttacks {
				target := &effect.ExtraAttacks[targetIndex]
				if target.Victim, err = r.guid64(); err != nil {
					return log, fmt.Errorf("read extra-attacks victim: %w", err)
				}
				if target.NumAttacks, err = r.u32(); err != nil {
					return log, fmt.Errorf("read extra-attacks count: %w", err)
				}
			}
		case 111, 115: // SPELL_EFFECT_DURABILITY_DAMAGE / _PCT
			effect.DurabilityDamage = make([]SpellExecuteDurabilityDamage, targetCount)
			for targetIndex := range effect.DurabilityDamage {
				target := &effect.DurabilityDamage[targetIndex]
				if target.Victim, err = r.guid64(); err != nil {
					return log, fmt.Errorf("read durability-damage victim: %w", err)
				}
				itemID, itemErr := r.u32()
				if itemErr != nil {
					return log, fmt.Errorf("read durability-damage item: %w", itemErr)
				}
				target.ItemID = int32(itemID)
				amount, amountErr := r.u32()
				if amountErr != nil {
					return log, fmt.Errorf("read durability-damage amount: %w", amountErr)
				}
				target.Amount = int32(amount)
			}
		case 24, 59: // SPELL_EFFECT_CREATE_ITEM / CREATE_RANDOM_ITEM
			effect.TradeSkillItems = make([]int32, targetCount)
			for targetIndex := range effect.TradeSkillItems {
				itemID, itemErr := r.u32()
				if itemErr != nil {
					return log, fmt.Errorf("read trade-skill item: %w", itemErr)
				}
				effect.TradeSkillItems[targetIndex] = int32(itemID)
			}
		case 101: // SPELL_EFFECT_FEED_PET
			effect.FeedPetItems = make([]int32, targetCount)
			for targetIndex := range effect.FeedPetItems {
				itemID, itemErr := r.u32()
				if itemErr != nil {
					return log, fmt.Errorf("read feed-pet item: %w", itemErr)
				}
				effect.FeedPetItems[targetIndex] = int32(itemID)
			}
		default:
			effect.GenericVictims = make([]uint64, targetCount)
			for targetIndex := range effect.GenericVictims {
				if effect.GenericVictims[targetIndex], err = r.guid64(); err != nil {
					return log, fmt.Errorf("read generic effect victim: %w", err)
				}
			}
		}
	}
	if r.remaining() != 0 {
		return log, fmt.Errorf("spell-execute has %d trailing bytes", r.remaining())
	}
	return log, nil
}

func EncodeSpellExecuteLog(log LegacySpellExecuteLog, resolve func(uint64) GUID128) []byte {
	caster := resolve(log.Caster)
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, log.SpellID)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(log.Effects)))
	for _, effect := range log.Effects {
		body = binary.LittleEndian.AppendUint32(body, effect.Effect)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(effect.PowerDrains)))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(effect.ExtraAttacks)))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(effect.DurabilityDamage)))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(effect.GenericVictims)))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(effect.TradeSkillItems)))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(effect.FeedPetItems)))
		for _, target := range effect.PowerDrains {
			victim := resolve(target.Victim)
			body = appendPackedGUID128(body, victim.Low, victim.High)
			body = binary.LittleEndian.AppendUint32(body, target.Points)
			body = binary.LittleEndian.AppendUint32(body, target.PowerType)
			body = appendFloat32(body, target.Amplitude)
		}
		for _, target := range effect.ExtraAttacks {
			victim := resolve(target.Victim)
			body = appendPackedGUID128(body, victim.Low, victim.High)
			body = binary.LittleEndian.AppendUint32(body, target.NumAttacks)
		}
		for _, target := range effect.DurabilityDamage {
			victim := resolve(target.Victim)
			body = appendPackedGUID128(body, victim.Low, victim.High)
			body = binary.LittleEndian.AppendUint32(body, uint32(target.ItemID))
			body = binary.LittleEndian.AppendUint32(body, uint32(target.Amount))
		}
		for _, legacyGUID := range effect.GenericVictims {
			victim := resolve(legacyGUID)
			body = appendPackedGUID128(body, victim.Low, victim.High)
		}
		for _, itemID := range effect.TradeSkillItems {
			body = binary.LittleEndian.AppendUint32(body, uint32(itemID))
		}
		for _, itemID := range effect.FeedPetItems {
			body = binary.LittleEndian.AppendUint32(body, uint32(itemID))
		}
	}
	bits := newBitWriter(body)
	bits.writeBit(false) // basic combat log packet has no advanced log data
	return bits.flush()
}

// Modern clients receive interrupt details in a dedicated combat-log packet.
func EncodeSpellInterruptLogs(log LegacySpellExecuteLog, resolve func(uint64) GUID128) []Packet {
	var packets []Packet
	for _, effect := range log.Effects {
		for _, target := range effect.Interrupts {
			b := guildAppendGUID(nil, resolve(log.Caster))
			b = guildAppendGUID(b, resolve(target.Victim))
			b = binary.LittleEndian.AppendUint32(b, log.SpellID)
			b = binary.LittleEndian.AppendUint32(b, target.SpellID)
			packets = append(packets, Packet{Opcode: SMSGSpellInterruptLog, Body: b})
		}
	}
	return packets
}
