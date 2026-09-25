package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGAttackSwing         = uint16(12885)
	CMSGAttackStop          = uint16(12886)
	CMSGSetSheathed         = uint16(13449)
	CMSGSetSelection        = uint16(13608)
	CMSGStandStateChange    = uint16(12684)
	SMSGAIReaction          = uint16(9909)
	SMSGAttackStart         = uint16(10557)
	SMSGAttackStop          = uint16(10558)
	SMSGCancelCombat        = uint16(10571)
	SMSGAttackSwingError    = uint16(10572)
	SMSGAttackerStateUpdate = uint16(10578)
	SMSGStandStateUpdate    = uint16(10012)
	SMSGHighestThreatUpdate = uint16(9945)
	SMSGThreatUpdate        = uint16(9946)
	SMSGThreatRemove        = uint16(9947)
	SMSGThreatClear         = uint16(9948)
	SMSGPartyKillLog        = uint16(10074)

	maxThreatTargets = 64

	hitInfoUnk1          uint32 = 0x00000001
	hitInfoFullAbsorb    uint32 = 0x00000020
	hitInfoPartialAbsorb uint32 = 0x00000040
	hitInfoFullResist    uint32 = 0x00000080
	hitInfoPartialResist uint32 = 0x00000100
	hitInfoUnk12         uint32 = 0x00001000
	hitInfoBlock         uint32 = 0x00002000
	hitInfoRageGain      uint32 = 0x00800000
)

const (
	// legacy proxy SMSG_ATTACKSWING_* writes a 3-bit reason: NOTINRANGE xorl %ebx,
	// BADFACING movl $1, CANT_ATTACK movl $2, DEADTARGET movl $3.
	AttackSwingNotInRange byte = iota
	AttackSwingBadFacing
	AttackSwingCantAttack
	AttackSwingDeadTarget
)

func ParsePackedGUID128Exact(body []byte) (GUID128, error) {
	if len(body) == 0 {
		return GUID128{}, nil
	}
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return GUID128{}, err
	}
	if consumed != len(body) {
		return GUID128{}, fmt.Errorf("packed GUID has %d trailing bytes", len(body)-consumed)
	}
	return GUID128{Low: low, High: high}, nil
}

func ParseLegacyPackedGUIDPair(body []byte) (uint64, uint64, error) {
	r := movementReader{data: body}
	first, err := r.guid64()
	if err != nil {
		return 0, 0, fmt.Errorf("read first GUID: %w", err)
	}
	second, err := r.guid64()
	if err != nil {
		return 0, 0, fmt.Errorf("read second GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("GUID pair has %d trailing bytes", r.remaining())
	}
	return first, second, nil
}

func ParseLegacyUnpackedGUIDPair(body []byte) (uint64, uint64, error) {
	r := movementReader{data: body}
	first, err := r.u64()
	if err != nil {
		return 0, 0, fmt.Errorf("read first GUID: %w", err)
	}
	second, err := r.u64()
	if err != nil {
		return 0, 0, fmt.Errorf("read second GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("GUID pair has %d trailing bytes", r.remaining())
	}
	return first, second, nil
}

func ParseLegacyAttackStop(body []byte) (uint64, uint64, bool, error) {
	r := movementReader{data: body}
	attacker, err := r.guid64()
	if err != nil {
		return 0, 0, false, fmt.Errorf("read attacker GUID: %w", err)
	}
	// AzerothCore normally includes victim and now-dead fields, but some
	// combat-stop paths only send the attacker GUID. Treat that short variant
	// as a valid stop with an empty victim, matching the client's semantics.
	if r.remaining() == 0 {
		return attacker, 0, false, nil
	}
	victim, err := r.guid64()
	if err != nil {
		return 0, 0, false, fmt.Errorf("read victim GUID: %w", err)
	}
	nowDead := false
	if r.remaining() >= 4 {
		value, readErr := r.u32()
		if readErr != nil {
			return 0, 0, false, fmt.Errorf("read attack-stop extra: %w", readErr)
		}
		nowDead = value != 0
	}
	if r.remaining() != 0 {
		return 0, 0, false, fmt.Errorf("attack-stop has %d trailing bytes", r.remaining())
	}
	return attacker, victim, nowDead, nil
}

func ParseLegacyAIReaction(body []byte) (uint64, uint32, error) {
	r := movementReader{data: body}
	guid, err := r.u64()
	if err != nil {
		return 0, 0, fmt.Errorf("read reaction GUID: %w", err)
	}
	reaction, err := r.u32()
	if err != nil {
		return 0, 0, fmt.Errorf("read reaction: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("AI reaction has %d trailing bytes", r.remaining())
	}
	return guid, reaction, nil
}

func EncodeAttackStart(attacker, victim GUID128) []byte {
	body := appendPackedGUID128(nil, attacker.Low, attacker.High)
	return appendPackedGUID128(body, victim.Low, victim.High)
}

func EncodeAttackStop(attacker, victim GUID128, nowDead bool) []byte {
	body := EncodeAttackStart(attacker, victim)
	bits := newBitWriter(body)
	bits.writeBit(nowDead)
	return bits.flush()
}

func EncodeAIReaction(guid GUID128, reaction uint32) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	return binary.LittleEndian.AppendUint32(body, reaction)
}

func EncodeLegacyPackedGUID(guid uint64) []byte {
	return appendLegacyPackedGUID(nil, guid)
}

func EncodeLegacyUnpackedGUID(guid uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, guid)
}

func ParseSetSheathed(body []byte) (uint32, error) {
	r := movementReader{data: body}
	state, err := r.u32()
	if err != nil {
		return 0, fmt.Errorf("read sheath state: %w", err)
	}
	if _, err := r.bit(); err != nil {
		return 0, fmt.Errorf("read sheath animate bit: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return 0, fmt.Errorf("set-sheathed has %d trailing bytes", r.remaining())
	}
	if state > 2 {
		return 0, fmt.Errorf("sheath state %d is out of range", state)
	}
	return state, nil
}

func EncodeLegacySheathed(state uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, state)
}

func EncodeAttackSwingError(reason byte) ([]byte, error) {
	if reason > AttackSwingDeadTarget {
		return nil, fmt.Errorf("attack-swing error %d is out of range", reason)
	}
	bits := newBitWriter(nil)
	bits.writeBits(uint32(reason), 3)
	return bits.flush(), nil
}

func ParseStandStateChange(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("stand-state change has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func TranslateStandStateUpdate(legacy []byte) ([]byte, error) {
	if len(legacy) != 1 {
		return nil, fmt.Errorf("legacy stand-state update has %d bytes, want 1", len(legacy))
	}
	// 54261 SMSG_STAND_STATE_UPDATE is AnimKitID (uint32) then State (uint8);
	// legacy proxy's handler writes a zero uint32 in front of the WotLK byte. Sending
	// the bare byte leaves the client unable to parse the packet, so /sit is
	// never confirmed and the client snaps back to standing.
	body := binary.LittleEndian.AppendUint32(nil, 0)
	return append(body, legacy[0]), nil
}

type SubDamage struct {
	SchoolMask  uint32
	FloatDamage float32
	IntDamage   int32
	Absorbed    int32
	Resisted    int32
}

type AttackerStateUpdate struct {
	HitInfo       uint32
	Attacker      uint64
	Victim        uint64
	Damage        int32
	OverDamage    int32
	SubDamage     []SubDamage
	VictimState   byte
	AttackerState int32
	MeleeSpellID  uint32
	BlockAmount   int32
	RageGained    int32
}

func ParseLegacyAttackerStateUpdate(body []byte) (AttackerStateUpdate, error) {
	var update AttackerStateUpdate
	r := movementReader{data: body}
	var err error
	if update.HitInfo, err = r.u32(); err != nil {
		return update, fmt.Errorf("read hit-info: %w", err)
	}
	if update.Attacker, err = r.guid64(); err != nil {
		return update, fmt.Errorf("read attacker GUID: %w", err)
	}
	if update.Victim, err = r.guid64(); err != nil {
		return update, fmt.Errorf("read victim GUID: %w", err)
	}
	if update.Damage, err = r.i32(); err != nil {
		return update, fmt.Errorf("read damage: %w", err)
	}
	if update.OverDamage, err = r.i32(); err != nil {
		return update, fmt.Errorf("read overkill: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return update, fmt.Errorf("read sub-damage count: %w", err)
	}
	if count == 0 || count > 8 {
		return update, fmt.Errorf("sub-damage count %d is out of range", count)
	}
	update.SubDamage = make([]SubDamage, count)
	absorb := update.HitInfo&(hitInfoFullAbsorb|hitInfoPartialAbsorb) != 0
	resist := update.HitInfo&(hitInfoFullResist|hitInfoPartialResist) != 0
	for index := range update.SubDamage {
		sub := &update.SubDamage[index]
		if sub.SchoolMask, err = r.u32(); err != nil {
			return update, fmt.Errorf("read sub-damage %d school: %w", index, err)
		}
		if sub.FloatDamage, err = r.f32(); err != nil {
			return update, fmt.Errorf("read sub-damage %d float: %w", index, err)
		}
		if sub.IntDamage, err = r.i32(); err != nil {
			return update, fmt.Errorf("read sub-damage %d int: %w", index, err)
		}
		if absorb {
			if sub.Absorbed, err = r.i32(); err != nil {
				return update, fmt.Errorf("read sub-damage %d absorb: %w", index, err)
			}
		}
		if resist {
			if sub.Resisted, err = r.i32(); err != nil {
				return update, fmt.Errorf("read sub-damage %d resist: %w", index, err)
			}
		}
	}
	if update.VictimState, err = r.u8(); err != nil {
		return update, fmt.Errorf("read victim state: %w", err)
	}
	if update.AttackerState, err = r.i32(); err != nil {
		return update, fmt.Errorf("read attacker state: %w", err)
	}
	if update.MeleeSpellID, err = r.u32(); err != nil {
		return update, fmt.Errorf("read melee spell: %w", err)
	}
	if update.HitInfo&hitInfoBlock != 0 {
		if update.BlockAmount, err = r.i32(); err != nil {
			return update, fmt.Errorf("read block amount: %w", err)
		}
	}
	if update.HitInfo&hitInfoRageGain != 0 {
		if update.RageGained, err = r.i32(); err != nil {
			return update, fmt.Errorf("read rage gained: %w", err)
		}
	}
	if update.HitInfo&hitInfoUnk1 != 0 {
		if _, err = r.u32(); err != nil {
			return update, fmt.Errorf("read unk-state 1: %w", err)
		}
		for index := 0; index < 10; index++ {
			if _, err = r.f32(); err != nil {
				return update, fmt.Errorf("read unk-state float %d: %w", index, err)
			}
		}
		if _, err = r.u32(); err != nil {
			return update, fmt.Errorf("read unk-state 12: %w", err)
		}
		if r.remaining() >= 8 {
			if _, err = r.u32(); err != nil {
				return update, err
			}
			if _, err = r.u32(); err != nil {
				return update, err
			}
		}
	}
	return update, nil
}

func EncodeAttackerStateUpdate(attacker, victim GUID128, update AttackerStateUpdate) []byte {
	inner := binary.LittleEndian.AppendUint32(nil, update.HitInfo)
	inner = appendPackedGUID128(inner, attacker.Low, attacker.High)
	inner = appendPackedGUID128(inner, victim.Low, victim.High)
	inner = binary.LittleEndian.AppendUint32(inner, uint32(update.Damage))
	inner = binary.LittleEndian.AppendUint32(inner, uint32(update.Damage))
	inner = binary.LittleEndian.AppendUint32(inner, uint32(update.OverDamage))
	inner = append(inner, byte(len(update.SubDamage)))
	absorb := update.HitInfo&(hitInfoFullAbsorb|hitInfoPartialAbsorb) != 0
	resist := update.HitInfo&(hitInfoFullResist|hitInfoPartialResist) != 0
	for _, sub := range update.SubDamage {
		inner = binary.LittleEndian.AppendUint32(inner, sub.SchoolMask)
		inner = appendFloat32(inner, sub.FloatDamage)
		inner = binary.LittleEndian.AppendUint32(inner, uint32(sub.IntDamage))
		if absorb {
			inner = binary.LittleEndian.AppendUint32(inner, uint32(sub.Absorbed))
		}
		if resist {
			inner = binary.LittleEndian.AppendUint32(inner, uint32(sub.Resisted))
		}
	}
	inner = append(inner, update.VictimState)
	inner = binary.LittleEndian.AppendUint32(inner, uint32(update.AttackerState))
	inner = binary.LittleEndian.AppendUint32(inner, update.MeleeSpellID)
	if update.HitInfo&hitInfoBlock != 0 {
		inner = binary.LittleEndian.AppendUint32(inner, uint32(update.BlockAmount))
	}
	if update.HitInfo&hitInfoRageGain != 0 {
		inner = binary.LittleEndian.AppendUint32(inner, uint32(update.RageGained))
	}
	if update.HitInfo&(hitInfoBlock|hitInfoUnk12) != 0 {
		inner = appendFloat32(inner, 0)
	}
	inner = append(inner, 0, 0, 0) // ContentTuning type/level/expansion
	inner = binary.LittleEndian.AppendUint16(inner, 0)
	inner = appendFloat32(inner, 0)
	inner = appendFloat32(inner, 0)

	bits := newBitWriter(nil)
	bits.writeBit(false)
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, uint32(len(inner)))
	return append(body, inner...)
}

type ThreatEntry struct {
	Unit   uint64
	Threat uint32
}

func ParseLegacyThreatUpdate(body []byte) (uint64, []ThreatEntry, error) {
	r := movementReader{data: body}
	unit, err := r.guid64()
	if err != nil {
		return 0, nil, fmt.Errorf("read threat unit: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return 0, nil, fmt.Errorf("read threat count: %w", err)
	}
	if int(count) > maxThreatTargets {
		return 0, nil, fmt.Errorf("threat count %d exceeds %d", count, maxThreatTargets)
	}
	entries := make([]ThreatEntry, 0, count)
	for index := 0; index < int(count); index++ {
		guid, guidErr := r.guid64()
		if guidErr != nil {
			return 0, nil, fmt.Errorf("read threat target %d: %w", index, guidErr)
		}
		threat, threatErr := r.u32()
		if threatErr != nil {
			return 0, nil, fmt.Errorf("read threat value %d: %w", index, threatErr)
		}
		entries = append(entries, ThreatEntry{Unit: guid, Threat: threat})
	}
	if r.remaining() != 0 {
		return 0, nil, fmt.Errorf("threat-update has %d trailing bytes", r.remaining())
	}
	return unit, entries, nil
}

func EncodeThreatUpdate(unit GUID128, entries []ThreatEntry, targets []GUID128) []byte {
	body := appendPackedGUID128(nil, unit.Low, unit.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(entries)))
	for index, entry := range entries {
		target := GUID128{}
		if index < len(targets) {
			target = targets[index]
		}
		body = appendPackedGUID128(body, target.Low, target.High)
		body = binary.LittleEndian.AppendUint64(body, uint64(entry.Threat))
	}
	return body
}

func ParseLegacyHighestThreatUpdate(body []byte) (uint64, uint64, []ThreatEntry, error) {
	r := movementReader{data: body}
	unit, err := r.guid64()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("read highest-threat unit: %w", err)
	}
	victim, err := r.guid64()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("read highest-threat victim: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("read highest-threat count: %w", err)
	}
	if int(count) > maxThreatTargets {
		return 0, 0, nil, fmt.Errorf("highest-threat count %d exceeds %d", count, maxThreatTargets)
	}
	entries := make([]ThreatEntry, 0, count)
	for index := 0; index < int(count); index++ {
		guid, guidErr := r.guid64()
		if guidErr != nil {
			return 0, 0, nil, fmt.Errorf("read highest-threat target %d: %w", index, guidErr)
		}
		threat, threatErr := r.u32()
		if threatErr != nil {
			return 0, 0, nil, fmt.Errorf("read highest-threat value %d: %w", index, threatErr)
		}
		entries = append(entries, ThreatEntry{Unit: guid, Threat: threat})
	}
	if r.remaining() != 0 {
		return 0, 0, nil, fmt.Errorf("highest-threat-update has %d trailing bytes", r.remaining())
	}
	return unit, victim, entries, nil
}

func EncodeHighestThreatUpdate(unit, victim GUID128, entries []ThreatEntry, targets []GUID128) []byte {
	body := appendPackedGUID128(nil, unit.Low, unit.High)
	body = appendPackedGUID128(body, victim.Low, victim.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(entries)))
	for index, entry := range entries {
		target := GUID128{}
		if index < len(targets) {
			target = targets[index]
		}
		body = appendPackedGUID128(body, target.Low, target.High)
		body = binary.LittleEndian.AppendUint64(body, uint64(entry.Threat))
	}
	return body
}

func EncodeThreatRemove(unit, about GUID128) []byte {
	body := appendPackedGUID128(nil, unit.Low, unit.High)
	return appendPackedGUID128(body, about.Low, about.High)
}

func EncodeThreatClear(unit GUID128) []byte {
	return appendPackedGUID128(nil, unit.Low, unit.High)
}

func EncodePartyKillLog(player, victim GUID128) []byte {
	body := appendPackedGUID128(nil, player.Low, player.High)
	return appendPackedGUID128(body, victim.Low, victim.High)
}
