package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGBindPointUpdate    = uint16(9597)
	SMSGPlayerBound        = uint16(12280)
	SMSGInitializeFactions = uint16(10020)
	SMSGSetForcedReactions = uint16(10013)
	SMSGLoginSetTimeSpeed  = uint16(9997)
)

func EncodeBindPointUpdate(legacyBody []byte) ([]byte, error) {
	if len(legacyBody) != 20 {
		return nil, fmt.Errorf("legacy bind-point update has %d bytes, want 20", len(legacyBody))
	}
	return append([]byte(nil), legacyBody...), nil
}

// EncodePlayerBound converts the WotLK binder GUID and area ID to the
// PackedGuid128 layout used by build 54261.
func EncodePlayerBound(legacyBody []byte, binder GUID128) ([]byte, error) {
	if len(legacyBody) != 12 {
		return nil, fmt.Errorf("legacy player-bound has %d bytes, want 12", len(legacyBody))
	}
	if binder.Low == 0 && binder.High == 0 && binary.LittleEndian.Uint64(legacyBody) != 0 {
		return nil, fmt.Errorf("modern binder GUID is empty")
	}
	body := appendPackedGUID128(nil, binder.Low, binder.High)
	return binary.LittleEndian.AppendUint32(body, binary.LittleEndian.Uint32(legacyBody[8:])), nil
}

func EncodeLoginSetTimeSpeed(legacyBody []byte) ([]byte, error) {
	if len(legacyBody) != 12 {
		return nil, fmt.Errorf("legacy login-set-time-speed has %d bytes, want 12", len(legacyBody))
	}
	serverTime := binary.LittleEndian.Uint32(legacyBody[:4])
	holidayOffset := binary.LittleEndian.Uint32(legacyBody[8:12])
	body := binary.LittleEndian.AppendUint32(nil, serverTime)
	body = binary.LittleEndian.AppendUint32(body, serverTime)
	body = append(body, legacyBody[4:8]...)
	body = binary.LittleEndian.AppendUint32(body, holidayOffset)
	return binary.LittleEndian.AppendUint32(body, holidayOffset), nil
}

func EncodeInitializeFactions(legacyBody []byte) ([]byte, error) {
	if len(legacyBody) < 4 {
		return nil, fmt.Errorf("legacy initialize-factions has %d bytes, want at least 4", len(legacyBody))
	}
	count := int(binary.LittleEndian.Uint32(legacyBody[:4]))
	if count < 0 || count > 1000 || len(legacyBody) != 4+count*5 {
		return nil, fmt.Errorf("legacy initialize-factions count %d does not match %d bytes", count, len(legacyBody))
	}
	body := make([]byte, 0, 6125)
	position := 4
	for index := 0; index < 1000; index++ {
		if index < count {
			body = binary.LittleEndian.AppendUint16(body, uint16(legacyBody[position]))
			body = append(body, legacyBody[position+1:position+5]...)
			position += 5
		} else {
			body = append(body, 0, 0, 0, 0, 0, 0)
		}
	}
	return append(body, make([]byte, 125)...), nil // 1000 FactionHasBonus bits
}

func EncodeSetForcedReactions(legacyBody []byte) ([]byte, error) {
	if len(legacyBody) < 4 {
		return nil, fmt.Errorf("legacy forced-reactions packet has %d bytes", len(legacyBody))
	}
	count := int(int32(binary.LittleEndian.Uint32(legacyBody[:4])))
	if count < 0 || count > 1024 || len(legacyBody) != 4+count*8 {
		return nil, fmt.Errorf("legacy forced-reactions count %d does not match %d bytes", count, len(legacyBody))
	}
	return append([]byte(nil), legacyBody...), nil
}
