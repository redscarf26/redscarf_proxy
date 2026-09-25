package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Layout verified against WPP CombatLogHandler (legacy and V3_4_0_45166)
// and legacy proxy handler_legacy/spell.SMSG_SPELL_MISS_LOG. The 3.4.3 opcode
// is 0x2C3E; this is a combat log, independent of SPELL_FAILED_OTHER.
const SMSGSpellMissLog = uint16(0x2C3E)

type LegacySpellMissEntry struct {
	Target                 uint64
	Reason                 uint8
	HitRoll, HitRollNeeded uint32 // raw float32 bits, preserved exactly
}
type LegacySpellMissLog struct {
	SpellID uint32
	Caster  uint64
	Debug   bool
	Entries []LegacySpellMissEntry
}

func ParseLegacySpellMissLog(body []byte) (LegacySpellMissLog, error) {
	var log LegacySpellMissLog
	if len(body) < 17 {
		return log, fmt.Errorf("spell-miss-log header truncated: %d bytes", len(body))
	}
	log.SpellID = binary.LittleEndian.Uint32(body)
	log.Caster = binary.LittleEndian.Uint64(body[4:])
	log.Debug = body[12] != 0
	count := binary.LittleEndian.Uint32(body[13:])
	size := 9
	if log.Debug {
		size = 17
	}
	body = body[17:]
	if uint64(count)*uint64(size) != uint64(len(body)) {
		return log, fmt.Errorf("spell-miss-log count %d does not match %d bytes", count, len(body))
	}
	log.Entries = make([]LegacySpellMissEntry, int(count))
	for i := range log.Entries {
		e := &log.Entries[i]
		e.Target = binary.LittleEndian.Uint64(body)
		e.Reason = body[8]
		if log.Debug {
			e.HitRoll = binary.LittleEndian.Uint32(body[9:])
			e.HitRollNeeded = binary.LittleEndian.Uint32(body[13:])
		}
		body = body[size:]
	}
	return log, nil
}

func EncodeSpellMissLog(log LegacySpellMissLog, guid func(uint64) GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, log.SpellID)
	caster := guid(log.Caster)
	body = appendPackedGUID128(body, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(log.Entries)))
	for _, e := range log.Entries {
		target := guid(e.Target)
		body = appendPackedGUID128(body, target.Low, target.High)
		body = append(body, e.Reason)
		bits := newBitWriter(body)
		bits.writeBit(log.Debug)
		body = bits.flush()
		if log.Debug {
			body = binary.LittleEndian.AppendUint32(body, e.HitRoll)
			body = binary.LittleEndian.AppendUint32(body, e.HitRollNeeded)
		}
	}
	return body
}
