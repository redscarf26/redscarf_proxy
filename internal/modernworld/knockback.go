package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGMoveKnockBack          = uint16(11779)
	SMSGMoveUpdateKnockBack    = uint16(11746)
	CMSGMoveKnockBackAck       = uint16(14866)
	LegacySMSGMoveKnockBack    = uint16(0xEF)
	LegacyMSGMoveKnockBack     = uint16(0xF1)
	LegacyCMSGMoveKnockBackAck = uint32(0xF0)
)

type KnockBack struct {
	GUID                                                   uint64
	Counter                                                uint32
	DirectionX, DirectionY, HorizontalSpeed, VerticalSpeed float32
}

func ParseLegacyKnockBack(body []byte) (KnockBack, error) {
	r := movementReader{data: body}
	var k KnockBack
	var err error
	if k.GUID, err = r.guid64(); err != nil {
		return k, err
	}
	if k.Counter, err = r.u32(); err != nil {
		return k, err
	}
	v, err := r.float32s(4)
	if err != nil {
		return k, err
	}
	k.DirectionX, k.DirectionY, k.HorizontalSpeed, k.VerticalSpeed = v[0], v[1], v[2], v[3]
	if r.remaining() != 0 {
		return k, fmt.Errorf("knockback has %d trailing bytes", r.remaining())
	}
	return k, nil
}

func EncodeModernKnockBack(k KnockBack, guid GUID128) []byte {
	b := appendPackedGUID128(nil, guid.Low, guid.High)
	b = binary.LittleEndian.AppendUint32(b, k.Counter)
	for _, v := range []float32{k.DirectionX, k.DirectionY, k.HorizontalSpeed, k.VerticalSpeed} {
		b = appendFloat32(b, v)
	}
	return b
}

func ParseLegacyKnockBackUpdate(body []byte) (uint64, LegacyMovement, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, LegacyMovement{}, err
	}
	m, err := r.movementInfo()
	if err != nil {
		return 0, m, err
	}
	v, err := r.float32s(4)
	if err != nil {
		return 0, m, err
	}
	m.JumpSinAngle, m.JumpCosAngle, m.JumpXYSpeed, m.JumpVelocity = v[0], v[1], v[2], v[3]
	m.MoveFlags |= legacyMoveFalling
	if r.remaining() != 0 {
		return 0, m, fmt.Errorf("knockback update has %d trailing bytes", r.remaining())
	}
	return guid, m, nil
}
