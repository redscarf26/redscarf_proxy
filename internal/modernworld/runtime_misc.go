package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGEmote            = uint16(0x3541)
	CMSGMountSpecialAnim = uint16(0x3280)
	SMSGPageText         = uint16(0x2719)
	SMSGPlayObjectSound  = uint16(0x276E)
)

// The modern emote notification is empty; text/animation choices are already
// carried by CMSG_SEND_TEXT_EMOTE. MountSpecial has a count, sequence, and kits.
func ParseRuntimeMiscRequest(op uint16, body []byte) (uint32, error) {
	switch op {
	case CMSGEmote:
		if len(body) != 0 {
			return 0, fmt.Errorf("emote notification must be empty")
		}
		return 0, nil
	case CMSGMountSpecialAnim:
		if len(body) < 8 {
			return 0, fmt.Errorf("mount special header truncated")
		}
		n := binary.LittleEndian.Uint32(body)
		if n > 64 || len(body) != 8+4*int(n) {
			return 0, fmt.Errorf("invalid mount visual count or length")
		}
		return 0x171, nil
	}
	return 0, fmt.Errorf("unknown misc request")
}

// AC prefixes compound movement with the byte size, then byte-sized records
// containing a uint16 opcode and payload. Validate the whole batch atomically.
func ParseLegacyMultipleMoves(body []byte) ([]Packet, error) {
	if len(body) < 4 || binary.LittleEndian.Uint32(body) != uint32(len(body)-4) {
		return nil, fmt.Errorf("multiple moves size mismatch")
	}
	var packets []Packet
	for b := body[4:]; len(b) != 0; {
		n := int(b[0])
		if n < 2 || n+1 > len(b) {
			return nil, fmt.Errorf("multiple moves record truncated")
		}
		op := binary.LittleEndian.Uint16(b[1:3])
		// Reject nested containers to bound replay work and avoid recursive input.
		if op == 0x51E || op == 0x2FB {
			return nil, fmt.Errorf("nested multiple moves")
		}
		packets = append(packets, Packet{Opcode: op, Body: append([]byte(nil), b[3:n+1]...)})
		b = b[n+1:]
	}
	return packets, nil
}

func EncodeObjectSound(sound uint32, source, target GUID128, position [3]float32) []byte {
	b := binary.LittleEndian.AppendUint32(nil, sound)
	b = guildAppendGUID(b, source)
	b = guildAppendGUID(b, target)
	for _, v := range position {
		b = appendFloat32(b, v)
	}
	return binary.LittleEndian.AppendUint32(b, 0) // no broadcast text in legacy
}

func EncodePageTextObject(g GUID128) []byte { return guildAppendGUID(nil, g) }
