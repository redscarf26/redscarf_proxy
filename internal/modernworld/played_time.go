package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGRequestPlayedTime = uint16(12922)
	SMSGPlayedTime        = uint16(9941)
)

func ParseRequestPlayedTime(body []byte) (bool, error) {
	if len(body) != 1 {
		return false, fmt.Errorf("request-played-time has %d bytes, want 1", len(body))
	}
	return body[0]&0x80 != 0, nil
}

func EncodePlayedTime(legacy []byte) ([]byte, error) {
	if len(legacy) != 9 {
		return nil, fmt.Errorf("legacy played-time has %d bytes, want 9", len(legacy))
	}
	body := make([]byte, 0, 9)
	body = binary.LittleEndian.AppendUint32(body, binary.LittleEndian.Uint32(legacy))
	body = binary.LittleEndian.AppendUint32(body, binary.LittleEndian.Uint32(legacy[4:]))
	bits := newBitWriter(body)
	bits.writeBit(legacy[8] != 0)
	return bits.flush(), nil
}
