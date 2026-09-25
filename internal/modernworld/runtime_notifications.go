package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Opcodes from the 3.4.3 table; GUID payloads also match legacy proxy's writers.
const (
	CMSGOpeningCinematic         = uint16(13635)
	CMSGNextCinematicCamera      = uint16(13636)
	CMSGCompleteCinematic        = uint16(13637)
	SMSGBreakTarget              = uint16(0x293c)
	SMSGMoveSkipTime             = uint16(0x2e18)
	SMSGTriggerCinematic         = uint16(0x27ca)
	SMSGCalendarRaidLockoutAdded = uint16(0x2699)
)

func IsLegacyRuntimeNotification(opcode uint16) bool {
	switch opcode {
	case 0x152, 0x319, 0xfa, 0x43e, 0x498:
		return true
	}
	return false
}

// TranslateLegacyRuntimeNotification validates the complete legacy packet
// before converting it. mapGUID must use the session's current map context.
func TranslateLegacyRuntimeNotification(opcode uint16, body []byte, mapGUID func(uint64) GUID128) (Packet, error) {
	r := movementReader{data: body}
	var out Packet
	switch opcode {
	case 0x152, 0x319:
		guid, err := r.guid64()
		if err != nil {
			return out, err
		}
		var skipped uint32
		if opcode == 0x319 {
			if skipped, err = r.u32(); err != nil {
				return out, err
			}
		}
		g := mapGUID(guid)
		out = Packet{Opcode: SMSGBreakTarget, Body: appendPackedGUID128(nil, g.Low, g.High)}
		if opcode == 0x319 {
			out.Opcode = SMSGMoveSkipTime
			out.Body = binary.LittleEndian.AppendUint32(out.Body, skipped)
		}
	case 0xfa:
		id, err := r.u32()
		if err != nil {
			return out, err
		}
		out = Packet{Opcode: SMSGTriggerCinematic, Body: appendPackedGUID128(binary.LittleEndian.AppendUint32(nil, id), 0, 0)}
	case 0x43e:
		// AC: packed server time, map, difficulty, time left, instance GUID.
		// Classic: instance ID first, followed by the four uint32 fields.
		if len(body) != 24 {
			return out, fmt.Errorf("raid lockout length %d, want 24", len(body))
		}
		out = Packet{Opcode: SMSGCalendarRaidLockoutAdded, Body: append(append([]byte(nil), body[16:24]...), body[:16]...)}
		r.offset = len(body)
	case 0x498:
		name, err := r.cstring()
		if err != nil {
			return out, err
		}
		if _, err = r.u64(); err != nil {
			return out, err
		}
		id, err := r.u32()
		if err != nil {
			return out, err
		}
		if _, err = r.u32(); err != nil {
			return out, err
		}
		if len(name) > 255 {
			return out, fmt.Errorf("realm-first name too long")
		}
		// The modern plural opcode is an achievement-state list, not this
		// legacy announcement. Preserve the announcement as system chat,
		// as legacy proxy does, rather than inventing earned achievement state.
		text := fmt.Sprintf("服务器首杀/首位成就：%s 获得成就 #%d。", name, id)
		out = Packet{Opcode: SMSGChat, Body: EncodeChatMessage(GUID128{}, GUID128{}, LegacyChatMessage{Type: legacyChatSystem, Text: text}, 0)}
	default:
		return out, fmt.Errorf("unsupported runtime notification %#x", opcode)
	}
	if r.remaining() != 0 {
		return Packet{}, fmt.Errorf("runtime notification %#x has %d trailing bytes", opcode, r.remaining())
	}
	return out, nil
}
