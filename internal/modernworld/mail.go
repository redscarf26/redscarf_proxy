package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGQueryNextMailTime       = uint16(0x3539)
	SMSGMailQueryNextTimeResult = uint16(0x2757)
)

type NextMailEntry struct {
	SenderGUID   uint64
	AltSenderID  int32
	SenderType   int8
	StationeryID int32
	TimeLeft     float32
}

type NextMailTimeResult struct {
	NextMailTime float32
	Entries      []NextMailEntry
}

func ParseQueryNextMailTime(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("query-next-mail-time has %d bytes, want 0", len(body))
	}
	return nil
}

func ParseLegacyNextMailTime(body []byte) (NextMailTimeResult, error) {
	var result NextMailTimeResult
	r := movementReader{data: body}
	var err error
	if result.NextMailTime, err = r.f32(); err != nil {
		return result, fmt.Errorf("read next-mail time: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return result, fmt.Errorf("read next-mail count: %w", err)
	}
	if count > 64 {
		return result, fmt.Errorf("next-mail-time has %d entries, maximum is 64", count)
	}
	result.Entries = make([]NextMailEntry, count)
	for index := range result.Entries {
		entry := &result.Entries[index]
		if entry.SenderGUID, err = r.u64(); err != nil {
			return result, fmt.Errorf("read next-mail sender %d: %w", index, err)
		}
		altSenderID, readErr := r.u32()
		if readErr != nil {
			return result, fmt.Errorf("read next-mail alternate sender %d: %w", index, readErr)
		}
		entry.AltSenderID = int32(altSenderID)
		senderType, readErr := r.u32()
		if readErr != nil || senderType > 255 {
			return result, fmt.Errorf("read next-mail sender type %d: value=%d error=%v", index, senderType, readErr)
		}
		entry.SenderType = int8(senderType)
		stationeryID, readErr := r.u32()
		if readErr != nil {
			return result, fmt.Errorf("read next-mail stationery %d: %w", index, readErr)
		}
		entry.StationeryID = int32(stationeryID)
		if entry.TimeLeft, err = r.f32(); err != nil {
			return result, fmt.Errorf("read next-mail remaining time %d: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return result, fmt.Errorf("next-mail-time has %d trailing bytes", r.remaining())
	}
	return result, nil
}

func EncodeNextMailTime(result NextMailTimeResult, resolve func(uint64) GUID128) []byte {
	body := appendFloat32(nil, result.NextMailTime)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(result.Entries)))
	for _, entry := range result.Entries {
		sender := resolve(entry.SenderGUID)
		body = appendPackedGUID128(body, sender.Low, sender.High)
		body = appendFloat32(body, entry.TimeLeft)
		body = binary.LittleEndian.AppendUint32(body, uint32(entry.AltSenderID))
		body = append(body, byte(entry.SenderType))
		body = binary.LittleEndian.AppendUint32(body, uint32(entry.StationeryID))
	}
	return body
}
