package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const (
	CMSGTimeSyncResponse        = uint16(14909)
	CMSGServerTimeOffsetRequest = uint16(0x369C)
	CMSGTutorialFlag            = uint16(14052)
	SMSGFeatureSystemStatus     = uint16(9663)
	SMSGTimeSyncRequest         = uint16(11730)
	SMSGServerTimeOffset        = uint16(0x2714)
	SMSGSetDungeonDifficulty    = uint16(0x26A4)
	SMSGTutorialFlags           = uint16(10174)
	SMSGMOTD                    = uint16(11183)
)

const (
	TutorialUpdate byte = iota
	TutorialClear
	TutorialReset
)

type TutorialAction struct {
	Action byte
	Bit    uint32
}

func ParseServerTimeOffsetRequest(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("server-time-offset request has %d bytes, want 0", len(body))
	}
	return nil
}

func EncodeServerTimeOffset(now time.Time) []byte {
	return binary.LittleEndian.AppendUint32(nil, uint32(now.Unix()))
}

func ParseLegacyDungeonDifficulty(body []byte) (uint32, error) {
	if len(body) != 12 {
		return 0, fmt.Errorf("dungeon-difficulty has %d bytes, want 12", len(body))
	}
	difficulty := binary.LittleEndian.Uint32(body[:4])
	if difficulty > 1 {
		return 0, fmt.Errorf("unsupported legacy dungeon difficulty %d", difficulty)
	}
	return difficulty + 1, nil
}

func EncodeDungeonDifficulty(difficulty uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, difficulty)
}

func EncodeFeatureSystemStatus() []byte {
	var body []byte
	body = append(body, 2)                           // complaint status
	body = binary.LittleEndian.AppendUint32(body, 1) // cfg realm ID
	body = binary.LittleEndian.AppendUint32(body, 1) // cfg realm record ID
	for range 5 {                                    // RAF limits + 3.4.3 unknown
		body = binary.LittleEndian.AppendUint32(body, 0)
	}
	body = binary.LittleEndian.AppendUint32(body, 300)   // token poll seconds
	body = binary.LittleEndian.AppendUint32(body, 30)    // kiosk minutes
	body = binary.LittleEndian.AppendUint64(body, 0)     // token balance
	body = binary.LittleEndian.AppendUint32(body, 180)   // store delivery delay
	body = binary.LittleEndian.AppendUint32(body, 0)     // clubs presence timer
	body = binary.LittleEndian.AppendUint32(body, 60000) // hidden clubs timer
	body = binary.LittleEndian.AppendUint32(body, 0)     // active season
	body = binary.LittleEndian.AppendUint32(body, 0)     // game-rule count
	body = binary.LittleEndian.AppendUint16(body, 0)     // max name queries
	body = binary.LittleEndian.AppendUint16(body, 0)     // name-query telemetry
	body = binary.LittleEndian.AppendUint32(body, 10)    // name-query interval

	featureBits := []bool{
		false, false, false, false, false, false, false, false,
		false, false, false, false, false, true, true, false,
		false, false, false, false, false, false, false, false,
		false, false, true, false, false, false, false, false,
		false, false, false, true, false, false, false, true,
		true, true, false, false,
	}
	bits := newBitWriter(body)
	for _, value := range featureBits {
		bits.writeBit(value)
	}
	body = bits.flush()

	bits = newBitWriter(body)
	bits.writeBit(false) // quick-join toasts enabled
	body = bits.flush()
	quickJoin := []float32{
		7, 10, 1, 1, 5, 1, 0, 60, 20, 0, 50,
		1, 10, 50, 100, 50, 1, 1, 100, 1, 850, 80,
	}
	for _, value := range quickJoin {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}

	bits = newBitWriter(body)
	bits.writeBit(false) // not squelched
	body = bits.flush()
	body = appendPackedGUID128(body, 0, 0) // BNet account
	body = appendPackedGUID128(body, 0, 0) // guild
	return body
}

func EncodeMOTD(legacyBody []byte) ([]byte, error) {
	if len(legacyBody) < 4 {
		return nil, fmt.Errorf("legacy MOTD has %d bytes, want at least 4", len(legacyBody))
	}
	count := int(binary.LittleEndian.Uint32(legacyBody[:4]))
	if count > 15 {
		return nil, fmt.Errorf("legacy MOTD has %d lines, maximum is 15", count)
	}
	position := 4
	lines := make([][]byte, 0, count)
	for range count {
		end := position
		for end < len(legacyBody) && legacyBody[end] != 0 {
			end++
		}
		if end == len(legacyBody) {
			return nil, fmt.Errorf("legacy MOTD line is not NUL terminated")
		}
		line := append([]byte(nil), legacyBody[position:end]...)
		if len(line) > 127 {
			return nil, fmt.Errorf("legacy MOTD line has %d bytes, maximum is 127", len(line))
		}
		lines = append(lines, line)
		position = end + 1
	}
	if position != len(legacyBody) {
		return nil, fmt.Errorf("legacy MOTD has %d trailing bytes", len(legacyBody)-position)
	}
	bits := newBitWriter(nil)
	bits.writeBits(uint32(count), 4)
	body := bits.flush()
	for _, line := range lines {
		bits = newBitWriter(body)
		bits.writeBits(uint32(len(line)), 7)
		body = bits.flush()
		body = append(body, line...)
	}
	return body, nil
}

func ParseTutorialAction(body []byte) (TutorialAction, error) {
	var action TutorialAction
	if len(body) < 1 {
		return action, fmt.Errorf("tutorial action is empty")
	}
	action.Action = body[0] >> 6
	switch action.Action {
	case TutorialUpdate:
		if len(body) != 5 {
			return action, fmt.Errorf("tutorial update has %d bytes, want 5", len(body))
		}
		action.Bit = binary.LittleEndian.Uint32(body[1:5])
	case TutorialClear, TutorialReset:
		if len(body) != 1 {
			return action, fmt.Errorf("tutorial action %d has %d bytes, want 1", action.Action, len(body))
		}
	default:
		return action, fmt.Errorf("unknown tutorial action %d", action.Action)
	}
	return action, nil
}

func EncodeTutorialAction(action TutorialAction) []byte {
	body := []byte{action.Action << 6}
	if action.Action == TutorialUpdate {
		body = binary.LittleEndian.AppendUint32(body, action.Bit)
	}
	return body
}
