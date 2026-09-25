package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGMoveRoot          = uint16(0x2DF9)
	SMSGMoveUnroot        = uint16(0x2DFA)
	SMSGMoveSetWaterWalk  = uint16(0x2DFB)
	SMSGMoveSetLandWalk   = uint16(0x2DFE)
	SMSGMoveSetFeatherFall = uint16(0x2DFF)
	SMSGMoveSetNormalFall  = uint16(0x2E00)
	SMSGMoveSetHovering   = uint16(0x2E01)
	SMSGMoveUnsetHovering = uint16(0x2E02)
	// Possessed flyers (Eye of Acherus) become the active mover. Without these
	// force-flag packets the 3.4.3 client keeps walking physics and falls back
	// to the death knight. legacy proxy sends 11781 / 11782; Hermes 54261 matches.
	SMSGMoveSetCanFly      = uint16(0x2E05)
	SMSGMoveUnsetCanFly    = uint16(0x2E06)
	SMSGMoveDisableGravity = uint16(0x2E0D)
	SMSGMoveEnableGravity  = uint16(0x2E0E)

	// Build 54261 keeps the TBC/Wrath Classic spline flag opcodes. legacy proxy's
	// expansion-80 opcode table maps the four WotLK creature state messages
	// below to these values.
	SMSGMoveSplineStartSwim      = uint16(0x2E25)
	SMSGMoveSplineStopSwim       = uint16(0x2E26)
	SMSGMoveSplineSetRunMode     = uint16(0x2E27)
	SMSGMoveSplineSetWalkMode    = uint16(0x2E28)
	SMSGMoveSplineRoot           = uint16(0x2E19)
	SMSGMoveSplineUnroot         = uint16(0x2E1A)
	SMSGMoveSplineDisableGravity = uint16(0x2E1B)
	SMSGMoveSplineEnableGravity  = uint16(0x2E1C)
	SMSGMoveSplineSetFeatherFall = uint16(0x2E1F)
	SMSGMoveSplineSetNormalFall  = uint16(0x2E20)
	SMSGMoveSplineSetHover       = uint16(0x2E21)
	SMSGMoveSplineUnsetHover     = uint16(0x2E22)
	SMSGMoveSplineSetWaterWalk   = uint16(0x2E23)
	SMSGMoveSplineSetLandWalk    = uint16(0x2E24)
	SMSGMoveSplineSetFlying      = uint16(0x2E29)
	SMSGMoveSplineUnsetFlying    = uint16(0x2E2A)
	// WowPacketParser V3_4_3_51666; 54261 matches the rest of this table.
	SMSGSetPlayHoverAnim = uint16(0x25BA)

	CMSGMoveForceRootAck      = uint16(0x3A0E)
	CMSGMoveForceUnrootAck    = uint16(0x3A0F)
	CMSGMoveFeatherFallAck    = uint16(0x3A1C)
	CMSGMoveWaterWalkAck      = uint16(0x3A1D)
	CMSGMoveHoverAck          = uint16(0x3A13)
	CMSGMoveSetCanFlyAck      = uint16(0x3A27)
	CMSGMoveGravityDisableAck = uint16(0x3A35)
	CMSGMoveGravityEnableAck  = uint16(0x3A36)
)

const (
	legacySMSGMoveWaterWalk         = uint16(0x00DE)
	legacySMSGMoveLandWalk          = uint16(0x00DF)
	legacySMSGForceMoveRoot         = uint16(0x00E8)
	legacySMSGForceMoveUnroot       = uint16(0x00EA)
	legacySMSGMoveSetFeatherFall    = uint16(0x00F2)
	legacySMSGMoveSetNormalFall     = uint16(0x00F3)
	legacySMSGMoveSetHover          = uint16(0x00F4)
	legacySMSGMoveUnsetHover        = uint16(0x00F5)
	legacySMSGMoveSetCanFly         = uint16(0x0343)
	legacySMSGMoveUnsetCanFly       = uint16(0x0344)
	legacySMSGMoveDisableGravity    = uint16(0x04CE)
	legacySMSGMoveEnableGravity     = uint16(0x04D0)
	legacySMSGSplineStartSwim       = uint16(0x030B)
	legacySMSGSplineStopSwim        = uint16(0x030C)
	legacySMSGSplineSetRunMode      = uint16(0x030D)
	legacySMSGSplineSetWalkMode     = uint16(0x030E)
	legacySMSGSplineRoot            = uint16(0x031A)
	legacySMSGSplineUnroot          = uint16(0x0304)
	legacySMSGSplineSetFeatherFall  = uint16(0x0305)
	legacySMSGSplineSetNormalFall   = uint16(0x0306)
	legacySMSGSplineSetHover        = uint16(0x0307)
	legacySMSGSplineUnsetHover      = uint16(0x0308)
	legacySMSGSplineSetWaterWalk    = uint16(0x0309)
	legacySMSGSplineSetLandWalk     = uint16(0x030A)
	legacySMSGSplineSetFlying       = uint16(0x0422)
	legacySMSGSplineUnsetFlying     = uint16(0x0423)
	legacySMSGSplineDisableGravity  = uint16(0x04D3)
	legacySMSGSplineEnableGravity   = uint16(0x04D4)
	legacyCMSGForceMoveRootAck      = uint32(0x00E9)
	legacyCMSGForceMoveUnrootAck    = uint32(0x00EB)
	legacyCMSGMoveFeatherFallAck    = uint32(0x02CF)
	legacyCMSGMoveWaterWalkAck      = uint32(0x02D0)
	legacyCMSGMoveHoverAck          = uint32(0x00F6)
	legacyCMSGMoveSetCanFlyAck      = uint32(0x0345)
	legacyCMSGMoveGravityDisableAck = uint32(0x04CF)
	legacyCMSGMoveGravityEnableAck  = uint32(0x04D1)
	legacyMoveFallingSlow           = uint32(0x20000000)
	legacyMoveWaterWalking          = uint32(0x10000000)
	legacyMoveHover                 = uint32(0x40000000)
	legacyMoveCanFly                = uint32(0x01000000)
)

var legacyMovementFlagOpcodes = map[uint16]uint16{
	legacySMSGMoveWaterWalk:        SMSGMoveSetWaterWalk,
	legacySMSGMoveLandWalk:         SMSGMoveSetLandWalk,
	legacySMSGForceMoveRoot:        SMSGMoveRoot,
	legacySMSGForceMoveUnroot:      SMSGMoveUnroot,
	legacySMSGMoveSetFeatherFall:   SMSGMoveSetFeatherFall,
	legacySMSGMoveSetNormalFall:    SMSGMoveSetNormalFall,
	legacySMSGMoveSetHover:         SMSGMoveSetHovering,
	legacySMSGMoveUnsetHover:       SMSGMoveUnsetHovering,
	legacySMSGMoveSetCanFly:        SMSGMoveSetCanFly,
	legacySMSGMoveUnsetCanFly:      SMSGMoveUnsetCanFly,
	legacySMSGMoveDisableGravity:   SMSGMoveDisableGravity,
	legacySMSGMoveEnableGravity:    SMSGMoveEnableGravity,
	legacySMSGSplineStartSwim:      SMSGMoveSplineStartSwim,
	legacySMSGSplineStopSwim:       SMSGMoveSplineStopSwim,
	legacySMSGSplineSetRunMode:     SMSGMoveSplineSetRunMode,
	legacySMSGSplineSetWalkMode:    SMSGMoveSplineSetWalkMode,
	legacySMSGSplineRoot:           SMSGMoveSplineRoot,
	legacySMSGSplineUnroot:         SMSGMoveSplineUnroot,
	legacySMSGSplineDisableGravity: SMSGMoveSplineDisableGravity,
	legacySMSGSplineEnableGravity:  SMSGMoveSplineEnableGravity,
	legacySMSGSplineSetFeatherFall: SMSGMoveSplineSetFeatherFall,
	legacySMSGSplineSetNormalFall:  SMSGMoveSplineSetNormalFall,
	legacySMSGSplineSetHover:       SMSGMoveSplineSetHover,
	legacySMSGSplineUnsetHover:     SMSGMoveSplineUnsetHover,
	legacySMSGSplineSetWaterWalk:   SMSGMoveSplineSetWaterWalk,
	legacySMSGSplineSetLandWalk:    SMSGMoveSplineSetLandWalk,
	legacySMSGSplineSetFlying:      SMSGMoveSplineSetFlying,
	legacySMSGSplineUnsetFlying:    SMSGMoveSplineUnsetFlying,
}

var legacySplineMovementFlagOpcodes = map[uint16]struct{}{
	legacySMSGSplineStartSwim:      {},
	legacySMSGSplineStopSwim:       {},
	legacySMSGSplineSetRunMode:     {},
	legacySMSGSplineSetWalkMode:    {},
	legacySMSGSplineRoot:           {},
	legacySMSGSplineUnroot:         {},
	legacySMSGSplineDisableGravity: {},
	legacySMSGSplineEnableGravity:  {},
	legacySMSGSplineSetFeatherFall: {},
	legacySMSGSplineSetNormalFall:  {},
	legacySMSGSplineSetHover:       {},
	legacySMSGSplineUnsetHover:     {},
	legacySMSGSplineSetWaterWalk:   {},
	legacySMSGSplineSetLandWalk:    {},
	legacySMSGSplineSetFlying:      {},
	legacySMSGSplineUnsetFlying:    {},
}

var modernMovementFlagAckToLegacy = map[uint16]uint32{
	CMSGMoveKnockBackAck:      LegacyCMSGMoveKnockBackAck,
	CMSGMoveForceRootAck:      legacyCMSGForceMoveRootAck,
	CMSGMoveForceUnrootAck:    legacyCMSGForceMoveUnrootAck,
	CMSGMoveFeatherFallAck:    legacyCMSGMoveFeatherFallAck,
	CMSGMoveWaterWalkAck:      legacyCMSGMoveWaterWalkAck,
	CMSGMoveHoverAck:          legacyCMSGMoveHoverAck,
	CMSGMoveSetCanFlyAck:      legacyCMSGMoveSetCanFlyAck,
	CMSGMoveGravityDisableAck: legacyCMSGMoveGravityDisableAck,
	CMSGMoveGravityEnableAck:  legacyCMSGMoveGravityEnableAck,
}

type MovementFlagChange struct {
	Modern     uint16
	GUID       uint64
	Counter    uint32
	HasCounter bool
}

func ParseLegacyMovementFlagChange(opcode uint16, body []byte) (MovementFlagChange, bool, error) {
	modern, handled := legacyMovementFlagOpcodes[opcode]
	if !handled {
		return MovementFlagChange{}, false, nil
	}
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return MovementFlagChange{}, true, fmt.Errorf("read movement-flag GUID: %w", err)
	}
	_, spline := legacySplineMovementFlagOpcodes[opcode]
	if spline {
		if r.remaining() != 0 {
			return MovementFlagChange{}, true, fmt.Errorf("spline movement-flag change has %d trailing bytes", r.remaining())
		}
		return MovementFlagChange{Modern: modern, GUID: guid}, true, nil
	}
	counter, err := r.u32()
	if err != nil {
		return MovementFlagChange{}, true, fmt.Errorf("read movement-flag counter: %w", err)
	}
	if r.remaining() != 0 {
		return MovementFlagChange{}, true, fmt.Errorf("movement-flag change has %d trailing bytes", r.remaining())
	}
	return MovementFlagChange{Modern: modern, GUID: guid, Counter: counter, HasCounter: true}, true, nil
}

func EncodeModernMovementFlagChange(change MovementFlagChange, guid GUID128) ([]byte, error) {
	if guid.Low == 0 && guid.High == 0 {
		return nil, fmt.Errorf("movement-flag GUID is empty")
	}
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	if !change.HasCounter {
		return body, nil
	}
	return binary.LittleEndian.AppendUint32(body, change.Counter), nil
}

func EncodeSetPlayHoverAnim(guid GUID128, play bool) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	bits := newBitWriter(body)
	bits.writeBit(play)
	return bits.flush()
}

func PlayHoverAnimForSplineOpcode(legacyOpcode uint16) (play bool, ok bool) {
	switch legacyOpcode {
	case legacySMSGSplineSetHover:
		return true, true
	case legacySMSGSplineUnsetHover:
		return false, true
	default:
		return false, false
	}
}

func LegacyOpcodeForModernMovementFlagAck(opcode uint16) (uint32, bool) {
	legacy, ok := modernMovementFlagAckToLegacy[opcode]
	return legacy, ok
}

type MovementFlagAck struct {
	Move    PlayerMovement
	Counter uint32
}

func ParseModernMovementFlagAck(body []byte) (MovementFlagAck, error) {
	return parseModernMovementFlagAck(body, false)
}

// Knockback ACKs append HasSpeeds and optional horizontal/vertical speeds.
func ParseModernKnockBackAck(body []byte) (MovementFlagAck, error) {
	return parseModernMovementFlagAck(body, true)
}

func parseModernMovementFlagAck(body []byte, knockback bool) (MovementFlagAck, error) {
	r := movementReader{data: body}
	move, err := r.modernMovementStats()
	if err != nil {
		return MovementFlagAck{}, err
	}
	counter, err := r.u32()
	if err != nil {
		return MovementFlagAck{}, fmt.Errorf("read movement-flag ack counter: %w", err)
	}
	if knockback {
		hasSpeeds, err := r.bits(1)
		if err != nil {
			return MovementFlagAck{}, fmt.Errorf("read knockback HasSpeeds: %w", err)
		}
		r.align()
		if hasSpeeds != 0 {
			if move.Move.JumpXYSpeed, err = r.f32(); err != nil {
				return MovementFlagAck{}, err
			}
			if move.Move.JumpVelocity, err = r.f32(); err != nil {
				return MovementFlagAck{}, err
			}
		}
	}
	if r.remaining() != 0 {
		return MovementFlagAck{}, fmt.Errorf("movement-flag ack has %d trailing bytes", r.remaining())
	}
	return MovementFlagAck{Move: move, Counter: counter}, nil
}

func EncodeLegacyMovementFlagAck(opcode uint32, mover, transport uint64, ack MovementFlagAck) ([]byte, error) {
	moveBody, err := EncodeLegacyPlayerMovement(ack.Move, mover, transport)
	if err != nil {
		return nil, err
	}
	r := movementReader{data: moveBody}
	if _, err := r.guid64(); err != nil {
		return nil, fmt.Errorf("skip packed mover GUID: %w", err)
	}
	body := appendLegacyPackedGUID(nil, mover)
	body = binary.LittleEndian.AppendUint32(body, ack.Counter)
	body = append(body, moveBody[r.offset:]...)
	// AzerothCore's HandleMoveFlagChangeOpcode reads a final uint32 isApplied
	// after MovementInfo for water-walk, hover, can-fly, and feather-fall ACKs.
	// Root, unroot, and gravity ACKs use a different handler and omit it.
	if opcode == legacyCMSGMoveWaterWalkAck || opcode == legacyCMSGMoveHoverAck || opcode == legacyCMSGMoveSetCanFlyAck || opcode == legacyCMSGMoveFeatherFallAck {
		var applied uint32
		flag := legacyMoveWaterWalking
		switch opcode {
		case legacyCMSGMoveHoverAck:
			flag = legacyMoveHover
		case legacyCMSGMoveSetCanFlyAck:
			flag = legacyMoveCanFly
		case legacyCMSGMoveFeatherFallAck:
			flag = legacyMoveFallingSlow
		}
		if modernMovementFlagsToLegacy(ack.Move.Move.MoveFlags)&flag != 0 {
			applied = 1
		}
		body = binary.LittleEndian.AppendUint32(body, applied)
	}
	return body, nil
}
