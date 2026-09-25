package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGWeather        = uint16(9894)
	SMSGSetProficiency = uint16(10037)
)

func TranslateWeather(legacy []byte) ([]byte, error) {
	r := movementReader{data: legacy}
	weatherID, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read weather type: %w", err)
	}
	intensity, err := r.f32()
	if err != nil {
		return nil, fmt.Errorf("read weather intensity: %w", err)
	}
	abrupt := false
	switch r.remaining() {
	case 0:
	case 1:
		flag, readErr := r.u8()
		if readErr != nil {
			return nil, fmt.Errorf("read weather abrupt: %w", readErr)
		}
		abrupt = flag != 0
	case 5:
		if _, err = r.u32(); err != nil {
			return nil, fmt.Errorf("read weather sound: %w", err)
		}
		flag, readErr := r.u8()
		if readErr != nil {
			return nil, fmt.Errorf("read weather abrupt: %w", readErr)
		}
		abrupt = flag != 0
	default:
		return nil, fmt.Errorf("legacy weather has %d trailing bytes", r.remaining())
	}
	body := binary.LittleEndian.AppendUint32(nil, weatherID)
	body = appendFloat32(body, intensity)
	bits := newBitWriter(body)
	bits.writeBit(abrupt)
	return bits.flush(), nil
}

func TranslateSetProficiency(legacy []byte) ([]byte, error) {
	if len(legacy) != 5 {
		return nil, fmt.Errorf("legacy set-proficiency has %d bytes, want 5", len(legacy))
	}
	itemClass := legacy[0]
	mask := binary.LittleEndian.Uint32(legacy[1:])
	body := binary.LittleEndian.AppendUint32(nil, mask)
	return append(body, itemClass), nil
}
