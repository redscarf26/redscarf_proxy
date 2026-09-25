package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	SMSGMoveUpdate         = uint16(11744)
	SMSGMoveSetActiveMover = uint16(11733)
	SMSGControlUpdate      = uint16(9799)
	CMSGSetActiveMover     = uint16(14908)
	CMSGMoveTimeSkipped    = uint16(0x3A1B)

	CMSGMoveChangeTransport    = uint16(14895)
	CMSGMoveDismissVehicle     = uint16(0x3A33)
	CMSGMoveFallLand           = uint16(14843)
	CMSGMoveFallReset          = uint16(14873)
	CMSGMoveHeartbeat          = uint16(14864)
	CMSGMoveJump               = uint16(14826)
	CMSGMoveRemoveForces       = uint16(14871)
	CMSGMoveSetFacing          = uint16(14857)
	CMSGMoveSetFacingHeartbeat = uint16(14943)
	CMSGMoveSetFly             = uint16(14888)
	CMSGMoveSetPitch           = uint16(14858)
	CMSGMoveSetRunMode         = uint16(14834)
	CMSGMoveSetWalkMode        = uint16(14835)
	CMSGMoveStartAscend        = uint16(14889)
	CMSGMoveStartBackward      = uint16(14821)
	CMSGMoveStartDescend       = uint16(14896)
	CMSGMoveStartForward       = uint16(14820)
	CMSGMoveStartPitchDown     = uint16(14832)
	CMSGMoveStartPitchUp       = uint16(14831)
	CMSGMoveStartSwim          = uint16(14844)
	CMSGMoveStartTurnLeft      = uint16(14828)
	CMSGMoveStartTurnRight     = uint16(14829)
	CMSGMoveStartStrafeLeft    = uint16(14823)
	CMSGMoveStartStrafeRight   = uint16(14824)
	CMSGMoveStop               = uint16(14822)
	CMSGMoveStopAscend         = uint16(14890)
	CMSGMoveStopPitch          = uint16(14833)
	CMSGMoveStopStrafe         = uint16(14825)
	CMSGMoveStopSwim           = uint16(14845)
	CMSGMoveStopTurn           = uint16(14830)
	CMSGMoveDoubleJump         = uint16(14827)

	legacyMoveDisableGravity = uint32(0x00000400)
	legacyMoveRoot           = uint32(0x00000800)
	legacyMoveFallingFar     = uint32(0x00002000)

	modernMoveFalling    = uint32(0x00000800)
	modernMoveFallingFar = uint32(0x00001000)

	maxRemovedMovementForces = uint32(1024)
)

var modernToLegacyPlayerMoveOpcode = map[uint16]uint32{
	// AzerothCore HandleDismissControlledVehicle reads packed mover + MovementInfo.
	// This is CMSG_DISMISS_CONTROLLED_VEHICLE, not MSG_MOVE_DISMISS_VEHICLE.
	CMSGMoveDismissVehicle:     0x046d,
	CMSGMoveChangeTransport:    0x00da, // no WotLK equivalent; Hermes fallback
	CMSGMoveFallLand:           0x00c9,
	CMSGMoveFallReset:          0x00da,
	CMSGMoveHeartbeat:          0x00ee,
	CMSGMoveJump:               0x00bb,
	CMSGMoveRemoveForces:       0x00da,
	CMSGMoveSetFacing:          0x00da,
	CMSGMoveSetFacingHeartbeat: 0x00da,
	CMSGMoveSetFly:             0x00da,
	CMSGMoveSetPitch:           0x00db,
	CMSGMoveSetRunMode:         0x00c2,
	CMSGMoveSetWalkMode:        0x00c3,
	CMSGMoveStartAscend:        0x0359,
	CMSGMoveStartBackward:      0x00b6,
	CMSGMoveStartDescend:       0x03a7,
	CMSGMoveStartForward:       0x00b5,
	CMSGMoveStartPitchDown:     0x00c0,
	CMSGMoveStartPitchUp:       0x00bf,
	CMSGMoveStartSwim:          0x00ca,
	CMSGMoveStartTurnLeft:      0x00bc,
	CMSGMoveStartTurnRight:     0x00bd,
	CMSGMoveStartStrafeLeft:    0x00b8,
	CMSGMoveStartStrafeRight:   0x00b9,
	CMSGMoveStop:               0x00b7,
	CMSGMoveStopAscend:         0x035a,
	CMSGMoveStopPitch:          0x00c1,
	CMSGMoveStopStrafe:         0x00ba,
	CMSGMoveStopSwim:           0x00cb,
	CMSGMoveStopTurn:           0x00be,
	CMSGMoveDoubleJump:         0x00da,
}

var legacyPlayerMoveOpcodes = map[uint16]struct{}{
	0x00b5: {}, 0x00b6: {}, 0x00b7: {}, 0x00b8: {}, 0x00b9: {}, 0x00ba: {},
	0x00bb: {}, 0x00bc: {}, 0x00bd: {}, 0x00be: {}, 0x00bf: {}, 0x00c0: {},
	0x00c1: {}, 0x00c2: {}, 0x00c3: {}, 0x00c5: {}, 0x00c9: {}, 0x00ca: {},
	0x00cb: {}, 0x00cd: {}, 0x00cf: {}, 0x00d1: {}, 0x00d3: {}, 0x00d5: {},
	0x00d8: {}, 0x00da: {}, 0x00db: {}, 0x00ee: {}, 0x0359: {}, 0x035a: {},
	0x037e: {}, 0x0380: {}, 0x03a7: {}, 0x03ad: {}, 0x045b: {},
	0x00ec: {}, 0x00ed: {}, // root/unroot observer broadcasts
	0x0518: {}, // collision-height observer broadcast
	0x00f7: {}, // hover observer broadcast (MovementInfo, not a force-flag packet)
}

var legacyMovementSpeedOpcodes = map[uint16]uint16{
	0x00cd: SMSGMoveUpdateRunSpeed,
	0x00cf: SMSGMoveUpdateRunBackSpeed,
	0x00d1: SMSGMoveUpdateWalkSpeed,
	0x00d3: SMSGMoveUpdateSwimSpeed,
	0x00d5: SMSGMoveUpdateSwimBackSpeed,
	0x00d8: SMSGMoveUpdateTurnRate,
	0x037e: SMSGMoveUpdateFlightSpeed,
	0x0380: SMSGMoveUpdateFlightBackSpeed,
	0x045b: SMSGMoveUpdatePitchRate,
}

// PlayerMovement contains the common build-54261 movement payload. Flags use
// modern values; LegacyMovement is reused for the numeric position/fall fields.
type PlayerMovement struct {
	Mover     GUID128
	Transport GUID128
	Move      LegacyMovement
}

func LegacyOpcodeForModernMovement(opcode uint16) (uint32, bool) {
	legacy, ok := modernToLegacyPlayerMoveOpcode[opcode]
	return legacy, ok
}

func IsLegacyPlayerMovementOpcode(opcode uint16) bool {
	_, ok := legacyPlayerMoveOpcodes[opcode]
	return ok
}

func ParseMoveTimeSkipped(body []byte) (GUID128, uint32, error) {
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return GUID128{}, 0, fmt.Errorf("read skipped-time mover: %w", err)
	}
	if len(body) != consumed+4 {
		return GUID128{}, 0, fmt.Errorf("move-time-skipped has %d bytes after GUID, want 4", len(body)-consumed)
	}
	return GUID128{Low: low, High: high}, binary.LittleEndian.Uint32(body[consumed:]), nil
}

func EncodeLegacyMoveTimeSkipped(guid uint64, skipped uint32) []byte {
	body := appendLegacyPackedGUID(nil, guid)
	return binary.LittleEndian.AppendUint32(body, skipped)
}

func ParseLegacyControlUpdate(body []byte) (uint64, bool, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, false, fmt.Errorf("read controlled GUID: %w", err)
	}
	control, err := r.u8()
	if err != nil {
		return 0, false, fmt.Errorf("read control flag: %w", err)
	}
	if control > 1 {
		return 0, false, fmt.Errorf("invalid control flag %d", control)
	}
	if r.remaining() != 0 {
		return 0, false, fmt.Errorf("control update has %d trailing bytes", r.remaining())
	}
	return guid, control != 0, nil
}

func EncodeControlUpdate(guid GUID128, hasControl bool) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	bits := newBitWriter(body)
	bits.writeBit(hasControl)
	return bits.flush()
}

func EncodeMoveSetActiveMover(guid GUID128) []byte {
	return appendPackedGUID128(nil, guid.Low, guid.High)
}

func ParseSetActiveMover(body []byte) (GUID128, error) {
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return GUID128{}, err
	}
	if consumed != len(body) {
		return GUID128{}, fmt.Errorf("set-active-mover has %d trailing bytes", len(body)-consumed)
	}
	return GUID128{Low: low, High: high}, nil
}

// ParseModernPlayerMovement decodes ClientPlayerMovement for build 54261.
func ParseModernPlayerMovement(body []byte) (PlayerMovement, error) {
	r := movementReader{data: body}
	result, err := r.modernMovementStats()
	if err != nil {
		return result, err
	}
	if r.remaining() != 0 {
		return result, fmt.Errorf("modern player movement has %d trailing bytes", r.remaining())
	}
	return result, nil
}

// ParseMoveSplineDone reads the same outer mover GUID and MovementInfo as a
// normal modern movement request, followed by the completed spline ID.
func ParseMoveSplineDone(body []byte) (PlayerMovement, uint32, error) {
	r := movementReader{data: body}
	result, err := r.modernMovementStats()
	if err != nil {
		return result, 0, err
	}
	splineID, err := r.u32()
	if err != nil {
		return result, 0, fmt.Errorf("read completed spline ID: %w", err)
	}
	if r.remaining() != 0 {
		return result, 0, fmt.Errorf("move-spline-done has %d trailing bytes", r.remaining())
	}
	return result, splineID, nil
}

func EncodeLegacyMoveSplineDone(movement PlayerMovement, moverGUID, transportGUID uint64, splineID uint32) ([]byte, error) {
	body, err := EncodeLegacyPlayerMovement(movement, moverGUID, transportGUID)
	if err != nil {
		return nil, err
	}
	return binary.LittleEndian.AppendUint32(body, splineID), nil
}

// modernMovementStats reads the 54261 MovementStatus / MoveUpdate block used by
// ClientPlayerMovement and by CMSG_CAST_SPELL. Callers that embed this block in
// a larger packet must not require the reader to be at EOF.
func (r *movementReader) modernMovementStats() (PlayerMovement, error) {
	var result PlayerMovement
	var err error
	result.Mover, err = r.guid128()
	if err != nil {
		return result, fmt.Errorf("read mover GUID: %w", err)
	}
	move := &result.Move
	move.TransportSeat = -1
	if move.MoveFlags, err = r.u32(); err != nil {
		return result, fmt.Errorf("read movement flags: %w", err)
	}
	extra, err := r.u32()
	if err != nil {
		return result, fmt.Errorf("read movement extra flags: %w", err)
	}
	move.MoveExtra = uint16(extra)
	if _, err = r.u32(); err != nil { // FlagsExtra2 has no WotLK equivalent.
		return result, fmt.Errorf("read movement extra flags 2: %w", err)
	}
	if move.MoveTime, err = r.u32(); err != nil {
		return result, fmt.Errorf("read move time: %w", err)
	}
	values, err := r.float32s(6)
	if err != nil {
		return result, fmt.Errorf("read position: %w", err)
	}
	move.X, move.Y, move.Z, move.Orientation = values[0], values[1], values[2], clampOrientation(values[3])
	move.Pitch, move.SplineElevation = values[4], values[5]
	removedCount, err := r.u32()
	if err != nil {
		return result, fmt.Errorf("read removed-force count: %w", err)
	}
	if removedCount > maxRemovedMovementForces {
		return result, fmt.Errorf("removed-force count %d exceeds limit %d", removedCount, maxRemovedMovementForces)
	}
	if _, err = r.u32(); err != nil { // MoveIndex
		return result, fmt.Errorf("read move index: %w", err)
	}
	for index := uint32(0); index < removedCount; index++ {
		if _, err = r.guid128(); err != nil {
			return result, fmt.Errorf("read removed-force GUID %d: %w", index, err)
		}
	}
	hasStanding, err := r.bit()
	if err != nil {
		return result, fmt.Errorf("read standing bit: %w", err)
	}
	hasTransport, err := r.bit()
	if err != nil {
		return result, fmt.Errorf("read transport bit: %w", err)
	}
	hasFall, err := r.bit()
	if err != nil {
		return result, fmt.Errorf("read fall bit: %w", err)
	}
	if _, err = r.bit(); err != nil { // HasSpline: no extra block in this structure.
		return result, fmt.Errorf("read spline bit: %w", err)
	}
	if _, err = r.bit(); err != nil { // HeightChangeFailed
		return result, err
	}
	if _, err = r.bit(); err != nil { // RemoteTimeValid
		return result, err
	}
	hasInertia, err := r.bit()
	if err != nil {
		return result, fmt.Errorf("read inertia bit: %w", err)
	}
	hasAdvancedFlying, err := r.bit()
	if err != nil {
		return result, fmt.Errorf("read advanced-flying bit: %w", err)
	}
	if hasTransport {
		if result.Transport, err = r.guid128(); err != nil {
			return result, fmt.Errorf("read transport GUID: %w", err)
		}
		transportValues, readErr := r.float32s(4)
		if readErr != nil {
			return result, fmt.Errorf("read transport position: %w", readErr)
		}
		move.TransportX, move.TransportY, move.TransportZ, move.TransportOrientation = transportValues[0], transportValues[1], transportValues[2], clampOrientation(transportValues[3])
		seat, readErr := r.u8()
		if readErr != nil {
			return result, fmt.Errorf("read transport seat: %w", readErr)
		}
		move.TransportSeat = int8(seat)
		if move.TransportTime, err = r.u32(); err != nil {
			return result, fmt.Errorf("read transport time: %w", err)
		}
		hasPreviousTime, readErr := r.bit()
		if readErr != nil {
			return result, fmt.Errorf("read previous transport time bit: %w", readErr)
		}
		hasVehicleID, readErr := r.bit()
		if readErr != nil {
			return result, fmt.Errorf("read vehicle ID bit: %w", readErr)
		}
		if hasPreviousTime {
			if move.TransportTime2, err = r.u32(); err != nil {
				return result, fmt.Errorf("read previous transport time: %w", err)
			}
		}
		if hasVehicleID {
			if move.VehicleID, err = r.u32(); err != nil {
				return result, fmt.Errorf("read vehicle ID: %w", err)
			}
		}
	}
	if hasStanding {
		if _, err = r.guid128(); err != nil {
			return result, fmt.Errorf("read standing game-object GUID: %w", err)
		}
	}
	if hasInertia {
		if _, err = r.guid128(); err != nil {
			return result, fmt.Errorf("read inertia GUID: %w", err)
		}
		if _, err = r.float32s(3); err != nil {
			return result, fmt.Errorf("read inertia force: %w", err)
		}
		if _, err = r.u32(); err != nil {
			return result, fmt.Errorf("read inertia lifetime: %w", err)
		}
	}
	if hasAdvancedFlying {
		if _, err = r.float32s(2); err != nil {
			return result, fmt.Errorf("read advanced-flying velocities: %w", err)
		}
	}
	if hasFall {
		if move.FallTime, err = r.u32(); err != nil {
			return result, fmt.Errorf("read fall time: %w", err)
		}
		if move.JumpVelocity, err = r.f32(); err != nil {
			return result, fmt.Errorf("read jump velocity: %w", err)
		}
		hasFallDirection, readErr := r.bit()
		if readErr != nil {
			return result, fmt.Errorf("read fall-direction bit: %w", readErr)
		}
		if hasFallDirection {
			jump, readErr := r.float32s(3)
			if readErr != nil {
				return result, fmt.Errorf("read fall direction: %w", readErr)
			}
			move.JumpSinAngle, move.JumpCosAngle, move.JumpXYSpeed = jump[0], jump[1], jump[2]
		}
	}
	r.align()
	return result, nil
}

// EncodeLegacyPlayerMovement converts the common modern movement body to the
// WotLK MSG_MOVE_* body. The caller resolves GUID128 values through its object cache.
func EncodeLegacyPlayerMovement(movement PlayerMovement, moverGUID, transportGUID uint64) ([]byte, error) {
	if moverGUID == 0 {
		return nil, fmt.Errorf("legacy mover GUID is empty")
	}
	move := movement.Move
	flags := modernMovementFlagsToLegacy(move.MoveFlags)
	if transportGUID != 0 {
		flags |= legacyMoveOnTransport
	}
	body := appendLegacyPackedGUID(nil, moverGUID)
	body = binary.LittleEndian.AppendUint32(body, flags)
	body = binary.LittleEndian.AppendUint16(body, move.MoveExtra)
	body = binary.LittleEndian.AppendUint32(body, move.MoveTime)
	for _, value := range []float32{move.X, move.Y, move.Z, clampOrientation(move.Orientation)} {
		body = appendFloat32(body, value)
	}
	if flags&legacyMoveOnTransport != 0 {
		body = appendLegacyPackedGUID(body, transportGUID)
		for _, value := range []float32{move.TransportX, move.TransportY, move.TransportZ, clampOrientation(move.TransportOrientation)} {
			body = appendFloat32(body, value)
		}
		body = binary.LittleEndian.AppendUint32(body, move.TransportTime)
		body = append(body, byte(move.TransportSeat))
		if move.MoveExtra&legacyMoveInterpolate != 0 {
			body = binary.LittleEndian.AppendUint32(body, move.TransportTime2)
		}
	}
	if flags&(legacyMoveSwimming|legacyMoveFlying) != 0 || move.MoveExtra&legacyMoveAlwaysAllowPitching != 0 {
		body = appendFloat32(body, move.Pitch)
	}
	body = binary.LittleEndian.AppendUint32(body, move.FallTime)
	if flags&legacyMoveFalling != 0 {
		for _, value := range []float32{move.JumpVelocity, move.JumpSinAngle, move.JumpCosAngle, move.JumpXYSpeed} {
			body = appendFloat32(body, value)
		}
	}
	if flags&legacyMoveSplineElevation != 0 {
		body = appendFloat32(body, move.SplineElevation)
	}
	return body, nil
}

// ParseLegacyPlayerMovement decodes the common 3.3.5a MSG_MOVE_* body.
// It remains strict for callers that do not know the source opcode.
func ParseLegacyPlayerMovement(body []byte) (uint64, LegacyMovement, error) {
	return parseLegacyPlayerMovement(0, body)
}

// ParseLegacyPlayerMovementForOpcode handles opcode-specific fields which are
// appended after the common MovementInfo block. The MSG_MOVE_SET_*_SPEED family
// carries one float32 speed/rate value there on 3.3.5a servers.
func ParseLegacyPlayerMovementForOpcode(opcode uint16, body []byte) (uint64, LegacyMovement, error) {
	return parseLegacyPlayerMovement(opcode, body)
}

func parseLegacyPlayerMovement(opcode uint16, body []byte) (uint64, LegacyMovement, error) {
	r := movementReader{data: body}
	mover, err := r.guid64()
	if err != nil {
		return 0, LegacyMovement{}, fmt.Errorf("read mover GUID: %w", err)
	}
	move, err := r.movementInfo()
	if err != nil {
		return 0, move, err
	}
	if opcode == 0x0518 {
		if move.CollisionHeight, err = r.f32(); err != nil {
			return 0, move, fmt.Errorf("read broadcast collision height: %w", err)
		}
		if math.IsNaN(float64(move.CollisionHeight)) || math.IsInf(float64(move.CollisionHeight), 0) || move.CollisionHeight < 0 {
			return 0, move, fmt.Errorf("invalid broadcast collision height %v", move.CollisionHeight)
		}
	}
	if _, speedUpdate := legacyMovementSpeedOpcodes[opcode]; speedUpdate {
		if r.remaining() != 4 {
			return 0, move, fmt.Errorf("legacy movement-speed update has %d trailing bytes, want 4", r.remaining())
		}
		speed, readErr := r.f32()
		if readErr != nil {
			return 0, move, fmt.Errorf("read legacy movement speed: %w", readErr)
		}
		switch opcode {
		case 0x00cd:
			move.RunSpeed = speed
		case 0x00cf:
			move.RunBackSpeed = speed
		case 0x00d1:
			move.WalkSpeed = speed
		case 0x00d3:
			move.SwimSpeed = speed
		case 0x00d5:
			move.SwimBackSpeed = speed
		case 0x00d8:
			move.TurnRate = speed
		case 0x037e:
			move.FlightSpeed = speed
		case 0x0380:
			move.FlightBackSpeed = speed
		case 0x045b:
			move.PitchRate = speed
		}
	}
	if r.remaining() != 0 {
		return 0, move, fmt.Errorf("legacy player movement has %d trailing bytes", r.remaining())
	}
	return mover, move, nil
}

func (r *movementReader) movementInfo() (LegacyMovement, error) {
	var move LegacyMovement
	move.TransportSeat = -1
	var err error
	if move.MoveFlags, err = r.u32(); err != nil {
		return move, fmt.Errorf("read movement flags: %w", err)
	}
	if move.MoveExtra, err = r.u16(); err != nil {
		return move, fmt.Errorf("read movement extra flags: %w", err)
	}
	if move.MoveTime, err = r.u32(); err != nil {
		return move, fmt.Errorf("read move time: %w", err)
	}
	values, err := r.float32s(4)
	if err != nil {
		return move, fmt.Errorf("read position: %w", err)
	}
	move.X, move.Y, move.Z, move.Orientation = values[0], values[1], values[2], clampOrientation(values[3])
	if move.MoveFlags&legacyMoveOnTransport != 0 {
		if move.TransportGUID, err = r.guid64(); err != nil {
			return move, fmt.Errorf("read transport GUID: %w", err)
		}
		transport, readErr := r.float32s(4)
		if readErr != nil {
			return move, fmt.Errorf("read transport position: %w", readErr)
		}
		move.TransportX, move.TransportY, move.TransportZ, move.TransportOrientation = transport[0], transport[1], transport[2], clampOrientation(transport[3])
		if move.TransportTime, err = r.u32(); err != nil {
			return move, fmt.Errorf("read transport time: %w", err)
		}
		seat, readErr := r.u8()
		if readErr != nil {
			return move, fmt.Errorf("read transport seat: %w", readErr)
		}
		move.TransportSeat = int8(seat)
		if move.MoveExtra&legacyMoveInterpolate != 0 {
			if move.TransportTime2, err = r.u32(); err != nil {
				return move, fmt.Errorf("read previous transport time: %w", err)
			}
		}
	}
	if move.MoveFlags&(legacyMoveSwimming|legacyMoveFlying) != 0 || move.MoveExtra&legacyMoveAlwaysAllowPitching != 0 {
		if move.Pitch, err = r.f32(); err != nil {
			return move, fmt.Errorf("read pitch: %w", err)
		}
	}
	if move.FallTime, err = r.u32(); err != nil {
		return move, fmt.Errorf("read fall time: %w", err)
	}
	if move.MoveFlags&legacyMoveFalling != 0 {
		jump, readErr := r.float32s(4)
		if readErr != nil {
			return move, fmt.Errorf("read jump: %w", readErr)
		}
		move.JumpVelocity, move.JumpSinAngle, move.JumpCosAngle, move.JumpXYSpeed = jump[0], jump[1], jump[2], jump[3]
	}
	if move.MoveFlags&legacyMoveSplineElevation != 0 {
		if move.SplineElevation, err = r.f32(); err != nil {
			return move, fmt.Errorf("read spline elevation: %w", err)
		}
	}
	return move, nil
}

func EncodeMoveUpdate(move LegacyMovement, mover, transport GUID128) []byte {
	return appendModernMovementInfoResolved(nil, mover.Low, mover.High, &move, transport.Low, transport.High)
}

func EncodeMoveUpdateSpeed(opcode uint16, move LegacyMovement, mover, transport GUID128) (uint16, []byte, bool) {
	modern, ok := legacyMovementSpeedOpcodes[opcode]
	if !ok {
		return 0, nil, false
	}
	speed := move.RunSpeed
	switch opcode {
	case 0x00cf:
		speed = move.RunBackSpeed
	case 0x00d1:
		speed = move.WalkSpeed
	case 0x00d3:
		speed = move.SwimSpeed
	case 0x00d5:
		speed = move.SwimBackSpeed
	case 0x00d8:
		speed = move.TurnRate
	case 0x037e:
		speed = move.FlightSpeed
	case 0x0380:
		speed = move.FlightBackSpeed
	case 0x045b:
		speed = move.PitchRate
	}
	body := appendModernMovementInfoResolved(nil, mover.Low, mover.High, &move, transport.Low, transport.High)
	return modern, appendFloat32(body, speed), true
}

func modernMovementFlagsToLegacy(modern uint32) uint32 {
	return modern&0x000001ff |
		(modern&0x03fffe00)<<1 |
		(modern&0x1c000000)<<2
}

func appendLegacyPackedGUID(dst []byte, guid uint64) []byte {
	maskIndex := len(dst)
	dst = append(dst, 0)
	var mask byte
	for index := uint(0); index < 8; index++ {
		value := byte(guid >> (8 * index))
		if value == 0 {
			continue
		}
		mask |= 1 << index
		dst = append(dst, value)
	}
	dst[maskIndex] = mask
	return dst
}

type movementReader struct {
	data      []byte
	offset    int
	bitOffset uint8
}

func (r *movementReader) align() {
	if r.bitOffset != 0 {
		r.offset++
		r.bitOffset = 0
	}
}

func (r *movementReader) remaining() int {
	return len(r.data) - r.offset
}

func (r *movementReader) take(size int) ([]byte, error) {
	r.align()
	if size < 0 || size > r.remaining() {
		return nil, fmt.Errorf("need %d bytes at offset %d, body has %d", size, r.offset, len(r.data))
	}
	value := r.data[r.offset : r.offset+size]
	r.offset += size
	return value, nil
}

func (r *movementReader) u8() (uint8, error) {
	value, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

func (r *movementReader) u16() (uint16, error) {
	value, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(value), nil
}

func (r *movementReader) u32() (uint32, error) {
	value, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(value), nil
}

func (r *movementReader) u64() (uint64, error) {
	value, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(value), nil
}

func (r *movementReader) i32() (int32, error) {
	value, err := r.u32()
	return int32(value), err
}

func (r *movementReader) cstring() (string, error) {
	r.align()
	start := r.offset
	for r.offset < len(r.data) && r.data[r.offset] != 0 {
		r.offset++
	}
	if r.offset == len(r.data) {
		return "", fmt.Errorf("unterminated string at offset %d", start)
	}
	value := string(r.data[start:r.offset])
	r.offset++
	return value, nil
}

func (r *movementReader) f32() (float32, error) {
	value, err := r.u32()
	return math.Float32frombits(value), err
}

func (r *movementReader) float32s(count int) ([]float32, error) {
	values := make([]float32, count)
	for index := range values {
		value, err := r.f32()
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func (r *movementReader) i8() (int8, error) {
	value, err := r.u8()
	return int8(value), err
}

func (r *movementReader) bit() (bool, error) {
	if r.offset >= len(r.data) {
		return false, fmt.Errorf("need bit at byte %d, body has %d", r.offset, len(r.data))
	}
	value := r.data[r.offset]&(1<<(7-r.bitOffset)) != 0
	r.bitOffset++
	if r.bitOffset == 8 {
		r.offset++
		r.bitOffset = 0
	}
	return value, nil
}

func (r *movementReader) bits(count int) (uint32, error) {
	var value uint32
	for index := 0; index < count; index++ {
		bit, err := r.bit()
		if err != nil {
			return 0, err
		}
		value <<= 1
		if bit {
			value |= 1
		}
	}
	return value, nil
}

func (r *movementReader) guid64() (uint64, error) {
	mask, err := r.u8()
	if err != nil {
		return 0, err
	}
	var guid uint64
	for index := uint(0); index < 8; index++ {
		if mask&(1<<index) == 0 {
			continue
		}
		part, readErr := r.u8()
		if readErr != nil {
			return 0, readErr
		}
		guid |= uint64(part) << (8 * index)
	}
	return guid, nil
}

func (r *movementReader) stringN(n int) (string, error) {
	r.align()
	if n < 0 || n > r.remaining() {
		return "", fmt.Errorf("need %d string bytes at offset %d, body has %d", n, r.offset, len(r.data))
	}
	value := string(r.data[r.offset : r.offset+n])
	r.offset += n
	return value, nil
}

func (r *movementReader) guid128() (GUID128, error) {
	r.align()
	low, high, consumed, err := readPackedGUID128(r.data[r.offset:])
	if err != nil {
		return GUID128{}, err
	}
	r.offset += consumed
	return GUID128{Low: low, High: high}, nil
}
