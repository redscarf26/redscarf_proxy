package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGResetInstances          = uint16(0x366A)
	CMSGRequestRaidInfo         = uint16(0x36D2)
	SMSGInstanceReset           = uint16(0x2686)
	SMSGInstanceResetFailed     = uint16(0x2687)
	SMSGInstanceInfo            = uint16(0x2634)
	SMSGResetFailedNotify       = uint16(0x26B7)
	SMSGUpdateInstanceOwnership = uint16(0x26A9)
)

type InstanceLock struct {
	MapID         uint32
	DifficultyID  uint32
	InstanceID    uint64
	Locked        bool
	Extended      bool
	TimeRemaining uint32
}

type InstanceResetFailure struct {
	Reason uint32
	MapID  uint32
}

func ParseResetInstances(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("reset-instances has %d bytes, want 0", len(body))
	}
	return nil
}

func ParseLegacyInstanceReset(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("instance-reset has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodeInstanceReset(mapID uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, mapID)
}

func ParseLegacyInstanceResetFailed(body []byte) (InstanceResetFailure, error) {
	if len(body) != 8 {
		return InstanceResetFailure{}, fmt.Errorf("instance-reset-failed has %d bytes, want 8", len(body))
	}
	failure := InstanceResetFailure{
		Reason: binary.LittleEndian.Uint32(body[:4]),
		MapID:  binary.LittleEndian.Uint32(body[4:]),
	}
	if failure.Reason > 3 {
		return InstanceResetFailure{}, fmt.Errorf("instance-reset-failed reason %d does not fit the modern 2-bit field", failure.Reason)
	}
	return failure, nil
}

func EncodeInstanceResetFailed(failure InstanceResetFailure) []byte {
	body := binary.LittleEndian.AppendUint32(nil, failure.MapID)
	bits := newBitWriter(body)
	bits.writeBits(failure.Reason, 2)
	return bits.flush()
}

func ParseLegacyResetFailedNotify(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("reset-failed-notify has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func ParseRequestRaidInfo(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("request-raid-info has %d bytes, want 0", len(body))
	}
	return nil
}

func TranslateInstanceOwnership(body []byte) ([]byte, error) {
	if len(body) != 4 {
		return nil, fmt.Errorf("instance-ownership has %d bytes, want 4", len(body))
	}
	return append([]byte(nil), body...), nil
}

func ParseLegacyInstanceInfo(body []byte) ([]InstanceLock, error) {
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read instance-info count: %w", err)
	}
	if count > 256 {
		return nil, fmt.Errorf("instance-info has %d locks, maximum is 256", count)
	}
	locks := make([]InstanceLock, count)
	for index := range locks {
		lock := &locks[index]
		if lock.MapID, err = r.u32(); err != nil {
			return nil, fmt.Errorf("read instance %d map: %w", index, err)
		}
		if lock.DifficultyID, err = r.u32(); err != nil {
			return nil, fmt.Errorf("read instance %d difficulty: %w", index, err)
		}
		if lock.InstanceID, err = r.u64(); err != nil {
			return nil, fmt.Errorf("read instance %d ID: %w", index, err)
		}
		locked, readErr := r.u8()
		if readErr != nil || locked > 1 {
			return nil, fmt.Errorf("read instance %d locked flag: value=%d error=%v", index, locked, readErr)
		}
		lock.Locked = locked != 0
		extended, readErr := r.u8()
		if readErr != nil || extended > 1 {
			return nil, fmt.Errorf("read instance %d extended flag: value=%d error=%v", index, extended, readErr)
		}
		lock.Extended = extended != 0
		if lock.TimeRemaining, err = r.u32(); err != nil {
			return nil, fmt.Errorf("read instance %d remaining time: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("instance-info has %d trailing bytes", r.remaining())
	}
	return locks, nil
}

func EncodeInstanceInfo(locks []InstanceLock) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(locks)))
	for _, lock := range locks {
		body = binary.LittleEndian.AppendUint32(body, lock.MapID)
		body = binary.LittleEndian.AppendUint32(body, InstanceDifficultyForMap(lock.MapID, lock.DifficultyID))
		body = binary.LittleEndian.AppendUint64(body, lock.InstanceID)
		body = binary.LittleEndian.AppendUint32(body, lock.TimeRemaining)
		body = binary.LittleEndian.AppendUint32(body, 0) // completed encounter mask is unavailable in 3.3.5
		bits := newBitWriter(body)
		bits.writeBit(lock.Locked)
		bits.writeBit(lock.Extended)
		body = bits.flush()
	}
	return body
}
