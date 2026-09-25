package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Build 54261 shares WPP's V3_4_3_51666 opcode table. Hermes' table
// omits raid difficulty; absence there does not mean absence on the client.
const (
	CMSGSetDungeonDifficulty = uint16(0x3684)
	CMSGSetRaidDifficulty    = uint16(0x36E3)
	CMSGInstanceLockResponse = uint16(0x350B)
	SMSGRaidDifficultySet    = uint16(0x27AD)
	SMSGInstanceSaveCreated  = uint16(0x2780)
	SMSGRaidInstanceMessage  = uint16(0x2BB4)
	SMSGPendingRaidLock      = uint16(0x26F8)
)

func TranslateSetDungeonDifficulty(body []byte) ([]byte, error) {
	if len(body) != 4 {
		return nil, fmt.Errorf("set dungeon difficulty has %d bytes, want 4", len(body))
	}
	id := binary.LittleEndian.Uint32(body)
	if id < 1 || id > 2 {
		return nil, fmt.Errorf("unsupported dungeon difficulty %d", id)
	}
	return binary.LittleEndian.AppendUint32(nil, id-1), nil
}

func TranslateSetRaidDifficulty(body []byte) ([]byte, error) {
	if len(body) != 5 {
		return nil, fmt.Errorf("set raid difficulty has %d bytes, want 5", len(body))
	}
	id := binary.LittleEndian.Uint32(body)
	if id < 3 || id > 6 || body[4] > 1 {
		return nil, fmt.Errorf("unsupported raid difficulty %d legacy=%d", id, body[4])
	}
	return binary.LittleEndian.AppendUint32(nil, id-3), nil
}

func TranslateLegacyRaidDifficulty(body []byte) ([]byte, error) {
	if len(body) != 12 {
		return nil, fmt.Errorf("raid difficulty has %d bytes, want 12", len(body))
	}
	id := binary.LittleEndian.Uint32(body)
	if id > 3 {
		return nil, fmt.Errorf("unsupported legacy raid difficulty %d", id)
	}
	return append(binary.LittleEndian.AppendUint32(nil, id+3), 0), nil
}

func TranslateInstanceLockResponse(body []byte) ([]byte, error) {
	if len(body) != 1 {
		return nil, fmt.Errorf("instance lock response has %d bytes, want 1", len(body))
	}
	return []byte{body[0] >> 7}, nil
}

func TranslateInstanceSaveCreated(body []byte) ([]byte, error) {
	if len(body) != 4 {
		return nil, fmt.Errorf("instance save created has %d bytes, want 4", len(body))
	}
	if binary.LittleEndian.Uint32(body) != 0 {
		return []byte{0x80}, nil
	}
	return []byte{0}, nil
}

// The pre-3.4.4 modern message has no time-left field. Preserve its
// warning category and lock flags; do not fabricate a countdown.
func TranslateRaidInstanceMessage(body []byte) ([]byte, error) {
	if len(body) < 16 {
		return nil, fmt.Errorf("raid instance message has %d bytes, want at least 16", len(body))
	}
	kind := binary.LittleEndian.Uint32(body)
	size := 16
	if kind == 4 {
		size = 18
	}
	if kind < 1 || kind > 5 || len(body) != size {
		return nil, fmt.Errorf("invalid raid instance message type=%d bytes=%d", kind, len(body))
	}
	out := append([]byte{byte(kind)}, body[4:8]...)
	out = binary.LittleEndian.AppendUint32(out, InstanceDifficultyForMap(binary.LittleEndian.Uint32(body[4:8]), binary.LittleEndian.Uint32(body[8:12])))
	flags := byte(0)
	if kind == 4 {
		if body[16] != 0 {
			flags |= 0x80
		}
		if body[17] != 0 {
			flags |= 0x40
		}
	}
	return append(out, flags), nil
}

func TranslatePendingRaidLock(body []byte) ([]byte, error) {
	if len(body) != 9 {
		return nil, fmt.Errorf("pending raid lock has %d bytes, want 9", len(body))
	}
	out := append([]byte(nil), body[:8]...)
	flags := byte(0)
	if body[8] != 0 {
		flags = 0x80
	}
	return append(out, flags), nil // WarningOnly=false: AC starts a real pending bind.
}

// WotLK uses overlapping dungeon/raid spawn-mode enums. Map context is
// essential for lock lists and welcome messages (unlike selection packets).
func InstanceDifficultyForMap(mapID, legacy uint32) uint32 {
	switch mapID {
	case 409, 469, 531:
		return 9 // fixed 40-player raids
	case 309, 509:
		return 148 // fixed 20-player raids
	case 532, 568:
		return 3 // fixed 10-player raids
	case 534, 544, 548, 550, 564, 565, 580:
		return 4 // fixed 25-player raids
	case 249, 533, 603, 615, 616, 624, 631, 649, 724:
		if legacy <= 3 {
			return legacy + 3
		}
	default:
		if legacy <= 1 {
			return legacy + 1
		}
	}
	return 0 // unsupported custom map/mode; never wrap/truncate the ID
}

// This is the current map's spawn mode, not the selected dungeon setting.
func ValidateLegacyInstanceDifficulty(body []byte) error {
	_, err := ParseLegacyInstanceDifficulty(body)
	return err
}
