package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGMoveUpdateCollisionHeight = uint16(0x2DDF)
	SMSGMoveSetCollisionHeight    = uint16(0x2E13)
	CMSGMoveSetCollisionHeightAck = uint16(0x3A3B)
)

// Observer updates carry MovementStatus, height and scale, without an ACK counter.
func EncodeMoveUpdateCollisionHeight(move LegacyMovement, mover, transport GUID128) []byte {
	body := EncodeMoveUpdate(move, mover, transport)
	body = appendFloat32(body, move.CollisionHeight)
	return appendFloat32(body, 1) // WotLK height already includes scale
}

type CollisionHeightChange struct {
	GUID    uint64
	Counter uint32
	Height  float32
}

type CollisionHeightAck struct {
	Move    PlayerMovement
	Counter uint32
	Height  float32
}

func ParseLegacyCollisionHeightChange(body []byte) (CollisionHeightChange, error) {
	var change CollisionHeightChange
	r := movementReader{data: body}
	var err error
	if change.GUID, err = r.guid64(); err != nil {
		return change, fmt.Errorf("read collision-height GUID: %w", err)
	}
	if change.Counter, err = r.u32(); err != nil {
		return change, fmt.Errorf("read collision-height counter: %w", err)
	}
	if change.Height, err = r.f32(); err != nil {
		return change, fmt.Errorf("read collision height: %w", err)
	}
	if r.remaining() != 0 {
		return change, fmt.Errorf("collision-height change has %d trailing bytes", r.remaining())
	}
	return change, nil
}

func EncodeModernCollisionHeightChange(change CollisionHeightChange, mover GUID128) []byte {
	body := appendPackedGUID128(nil, mover.Low, mover.High)
	body = binary.LittleEndian.AppendUint32(body, change.Counter)
	body = appendFloat32(body, change.Height)
	body = appendFloat32(body, 1)                       // legacy already supplies the final scaled height
	body = append(body, 2)                              // UpdateCollisionHeightReason.Force
	body = binary.LittleEndian.AppendUint32(body, 0)    // MountDisplayID
	return binary.LittleEndian.AppendUint32(body, 2000) // ScaleDuration
}

func ParseModernCollisionHeightAck(body []byte) (CollisionHeightAck, error) {
	var ack CollisionHeightAck
	r := movementReader{data: body}
	var err error
	if ack.Move, err = r.modernMovementStats(); err != nil {
		return ack, err
	}
	if ack.Counter, err = r.u32(); err != nil {
		return ack, fmt.Errorf("read collision-height ack counter: %w", err)
	}
	if ack.Height, err = r.f32(); err != nil {
		return ack, fmt.Errorf("read collision-height ack height: %w", err)
	}
	if _, err = r.u32(); err != nil { // MountDisplayID has no legacy equivalent.
		return ack, fmt.Errorf("read collision-height ack mount display: %w", err)
	}
	if _, err = r.u8(); err != nil { // Reason has no legacy equivalent.
		return ack, fmt.Errorf("read collision-height ack reason: %w", err)
	}
	if r.remaining() != 0 {
		return ack, fmt.Errorf("collision-height ack has %d trailing bytes", r.remaining())
	}
	return ack, nil
}

func EncodeLegacyCollisionHeightAck(mover, transport uint64, ack CollisionHeightAck) ([]byte, error) {
	return EncodeLegacyForceSpeedAck(mover, transport, MovementSpeedAck{
		Move: ack.Move, Counter: ack.Counter, Speed: ack.Height,
	})
}
