package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	SMSGMoveUpdateRunSpeed        = uint16(0x2DD6)
	SMSGMoveUpdateRunBackSpeed    = uint16(0x2DD7)
	SMSGMoveUpdateWalkSpeed       = uint16(0x2DD8)
	SMSGMoveUpdateSwimSpeed       = uint16(0x2DD9)
	SMSGMoveUpdateSwimBackSpeed   = uint16(0x2DDA)
	SMSGMoveUpdateFlightSpeed     = uint16(0x2DDB)
	SMSGMoveUpdateFlightBackSpeed = uint16(0x2DDC)
	SMSGMoveUpdateTurnRate        = uint16(0x2DDD)
	SMSGMoveUpdatePitchRate       = uint16(0x2DDE)

	// Build 54261 SMSG_MOVE_SET_* / SPLINE_SET_* = Ashamane 8.x values + 50,
	// matching the already-landed SMSG_MOVE_UPDATE / SET_ACTIVE_MOVER / TELEPORT trio.
	SMSGMoveSetRunSpeed        = uint16(11760)
	SMSGMoveSetRunBackSpeed    = uint16(11761)
	SMSGMoveSetSwimSpeed       = uint16(11762)
	SMSGMoveSetSwimBackSpeed   = uint16(11763)
	SMSGMoveSetFlightSpeed     = uint16(11764)
	SMSGMoveSetFlightBackSpeed = uint16(11765)
	SMSGMoveSetWalkSpeed       = uint16(11766)
	SMSGMoveSetTurnRate        = uint16(11767)
	SMSGMoveSetPitchRate       = uint16(11768)

	SMSGMoveSplineSetRunSpeed        = uint16(11751)
	SMSGMoveSplineSetRunBackSpeed    = uint16(11752)
	SMSGMoveSplineSetSwimSpeed       = uint16(11753)
	SMSGMoveSplineSetSwimBackSpeed   = uint16(11754)
	SMSGMoveSplineSetFlightSpeed     = uint16(11755)
	SMSGMoveSplineSetFlightBackSpeed = uint16(11756)
	SMSGMoveSplineSetWalkSpeed       = uint16(11757)
	SMSGMoveSplineSetTurnRate        = uint16(11758)
	SMSGMoveSplineSetPitchRate       = uint16(11759)

	// CMSG_MOVE_FORCE_*_ACK = Ashamane 8.x values + 3, matching
	// CMSG_MOVE_FORCE_PITCH_RATE_CHANGE_ACK = 0x3A32 in generated.go.
	CMSGMoveForceRunSpeedAck        = uint16(14859)
	CMSGMoveForceRunBackSpeedAck    = uint16(14860)
	CMSGMoveForceSwimSpeedAck       = uint16(14861)
	CMSGMoveForceWalkSpeedAck       = uint16(14881)
	CMSGMoveForceSwimBackSpeedAck   = uint16(14882)
	CMSGMoveForceTurnRateAck        = uint16(14883)
	CMSGMoveForceFlightSpeedAck     = uint16(14893)
	CMSGMoveForceFlightBackSpeedAck = uint16(14894)
	CMSGMoveForcePitchRateAck       = uint16(14898)
)

const (
	legacySMSGForceRunSpeedChange           = uint16(0x00E2)
	legacyCMSGForceRunSpeedChangeAck        = uint32(0x00E3)
	legacySMSGForceRunBackSpeedChange       = uint16(0x00E4)
	legacyCMSGForceRunBackSpeedChangeAck    = uint32(0x00E5)
	legacySMSGForceSwimSpeedChange          = uint16(0x00E6)
	legacyCMSGForceSwimSpeedChangeAck       = uint32(0x00E7)
	legacySMSGForceWalkSpeedChange          = uint16(0x02DA)
	legacyCMSGForceWalkSpeedChangeAck       = uint32(0x02DB)
	legacySMSGForceSwimBackSpeedChange      = uint16(0x02DC)
	legacyCMSGForceSwimBackSpeedChangeAck   = uint32(0x02DD)
	legacySMSGForceTurnRateChange           = uint16(0x02DE)
	legacyCMSGForceTurnRateChangeAck        = uint32(0x02DF)
	legacySMSGForceFlightSpeedChange        = uint16(0x0381)
	legacyCMSGForceFlightSpeedChangeAck     = uint32(0x0382)
	legacySMSGForceFlightBackSpeedChange    = uint16(0x0383)
	legacyCMSGForceFlightBackSpeedChangeAck = uint32(0x0384)
	legacyCMSGForcePitchRateChangeAck       = uint32(0x045D)

	// Creature spline speeds. The 3.3.5a values are 766..771/901/902/1118; the
	// earlier 0x02DF..0x02E3 guesses were unrelated client opcodes so the spline
	// speed relays never matched.
	legacySMSGSplineSetRunSpeed        = uint16(0x02FE) // 766
	legacySMSGSplineSetRunBackSpeed    = uint16(0x02FF) // 767
	legacySMSGSplineSetSwimSpeed       = uint16(0x0300) // 768
	legacySMSGSplineSetWalkSpeed       = uint16(0x0301) // 769
	legacySMSGSplineSetSwimBackSpeed   = uint16(0x0302) // 770
	legacySMSGSplineSetTurnRate        = uint16(0x0303) // 771
	legacySMSGSplineSetFlightSpeed     = uint16(0x0385) // 901
	legacySMSGSplineSetFlightBackSpeed = uint16(0x0386) // 902
	legacySMSGSplineSetPitchRate       = uint16(0x045E) // 1118
)

type speedKind uint8

const (
	speedKindForce speedKind = iota
	speedKindSpline
)

type legacySpeedChange struct {
	Kind     speedKind
	Modern   uint16
	GUID     uint64
	Counter  uint32
	Speed    float32
	HasExtra bool
}

var legacyForceSpeedOpcodes = map[uint16]uint16{
	legacySMSGForceRunSpeedChange:        SMSGMoveSetRunSpeed,
	legacySMSGForceRunBackSpeedChange:    SMSGMoveSetRunBackSpeed,
	legacySMSGForceSwimSpeedChange:       SMSGMoveSetSwimSpeed,
	legacySMSGForceWalkSpeedChange:       SMSGMoveSetWalkSpeed,
	legacySMSGForceSwimBackSpeedChange:   SMSGMoveSetSwimBackSpeed,
	legacySMSGForceTurnRateChange:        SMSGMoveSetTurnRate,
	legacySMSGForceFlightSpeedChange:     SMSGMoveSetFlightSpeed,
	legacySMSGForceFlightBackSpeedChange: SMSGMoveSetFlightBackSpeed,
}

var legacySplineSpeedOpcodes = map[uint16]uint16{
	legacySMSGSplineSetRunSpeed:        SMSGMoveSplineSetRunSpeed,
	legacySMSGSplineSetRunBackSpeed:    SMSGMoveSplineSetRunBackSpeed,
	legacySMSGSplineSetSwimSpeed:       SMSGMoveSplineSetSwimSpeed,
	legacySMSGSplineSetSwimBackSpeed:   SMSGMoveSplineSetSwimBackSpeed,
	legacySMSGSplineSetWalkSpeed:       SMSGMoveSplineSetWalkSpeed,
	legacySMSGSplineSetTurnRate:        SMSGMoveSplineSetTurnRate,
	legacySMSGSplineSetFlightSpeed:     SMSGMoveSplineSetFlightSpeed,
	legacySMSGSplineSetFlightBackSpeed: SMSGMoveSplineSetFlightBackSpeed,
	legacySMSGSplineSetPitchRate:       SMSGMoveSplineSetPitchRate,
}

var modernSpeedAckToLegacy = map[uint16]uint32{
	CMSGMoveForceRunSpeedAck:        legacyCMSGForceRunSpeedChangeAck,
	CMSGMoveForceRunBackSpeedAck:    legacyCMSGForceRunBackSpeedChangeAck,
	CMSGMoveForceSwimSpeedAck:       legacyCMSGForceSwimSpeedChangeAck,
	CMSGMoveForceWalkSpeedAck:       legacyCMSGForceWalkSpeedChangeAck,
	CMSGMoveForceSwimBackSpeedAck:   legacyCMSGForceSwimBackSpeedChangeAck,
	CMSGMoveForceTurnRateAck:        legacyCMSGForceTurnRateChangeAck,
	CMSGMoveForceFlightSpeedAck:     legacyCMSGForceFlightSpeedChangeAck,
	CMSGMoveForceFlightBackSpeedAck: legacyCMSGForceFlightBackSpeedChangeAck,
	CMSGMoveForcePitchRateAck:       legacyCMSGForcePitchRateChangeAck,
}

func ParseLegacySpeedChange(opcode uint16, body []byte) (legacySpeedChange, bool, error) {
	if modern, ok := legacyForceSpeedOpcodes[opcode]; ok {
		change, err := parseLegacyForceSpeed(body, opcode == legacySMSGForceRunSpeedChange)
		change.Kind = speedKindForce
		change.Modern = modern
		return change, true, err
	}
	if modern, ok := legacySplineSpeedOpcodes[opcode]; ok {
		change, err := parseLegacySplineSpeed(body)
		change.Kind = speedKindSpline
		change.Modern = modern
		return change, true, err
	}
	return legacySpeedChange{}, false, nil
}

func parseLegacyForceSpeed(body []byte, extraByte bool) (legacySpeedChange, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return legacySpeedChange{}, fmt.Errorf("read force-speed GUID: %w", err)
	}
	counter, err := r.u32()
	if err != nil {
		return legacySpeedChange{}, fmt.Errorf("read force-speed counter: %w", err)
	}
	if extraByte {
		if _, err = r.u8(); err != nil {
			return legacySpeedChange{}, fmt.Errorf("read force-run extra: %w", err)
		}
	}
	speed, err := r.f32()
	if err != nil {
		return legacySpeedChange{}, fmt.Errorf("read force-speed value: %w", err)
	}
	if r.remaining() != 0 {
		return legacySpeedChange{}, fmt.Errorf("force-speed has %d trailing bytes", r.remaining())
	}
	return legacySpeedChange{GUID: guid, Counter: counter, Speed: speed, HasExtra: extraByte}, nil
}

func parseLegacySplineSpeed(body []byte) (legacySpeedChange, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return legacySpeedChange{}, fmt.Errorf("read spline-speed GUID: %w", err)
	}
	speed, err := r.f32()
	if err != nil {
		return legacySpeedChange{}, fmt.Errorf("read spline-speed value: %w", err)
	}
	if r.remaining() != 0 {
		return legacySpeedChange{}, fmt.Errorf("spline-speed has %d trailing bytes", r.remaining())
	}
	return legacySpeedChange{GUID: guid, Speed: speed}, nil
}

func EncodeModernSpeedChange(change legacySpeedChange, guid GUID128) ([]byte, error) {
	if guid.Low == 0 && guid.High == 0 {
		return nil, fmt.Errorf("speed change GUID is empty")
	}
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	if change.Kind == speedKindForce {
		body = binary.LittleEndian.AppendUint32(body, change.Counter)
	}
	return appendFloat32(body, change.Speed), nil
}

func LegacyOpcodeForModernSpeedAck(opcode uint16) (uint32, bool) {
	legacy, ok := modernSpeedAckToLegacy[opcode]
	return legacy, ok
}

type MovementSpeedAck struct {
	Move    PlayerMovement
	Counter uint32
	Speed   float32
}

func ParseModernSpeedAck(body []byte) (MovementSpeedAck, error) {
	r := movementReader{data: body}
	move, err := r.modernMovementStats()
	if err != nil {
		return MovementSpeedAck{}, err
	}
	counter, err := r.u32()
	if err != nil {
		return MovementSpeedAck{}, fmt.Errorf("read speed-ack counter: %w", err)
	}
	speed, err := r.f32()
	if err != nil {
		return MovementSpeedAck{}, fmt.Errorf("read speed-ack value: %w", err)
	}
	if r.remaining() != 0 {
		return MovementSpeedAck{}, fmt.Errorf("speed-ack has %d trailing bytes", r.remaining())
	}
	return MovementSpeedAck{Move: move, Counter: counter, Speed: speed}, nil
}

func EncodeLegacyForceSpeedAck(mover, transport uint64, ack MovementSpeedAck) ([]byte, error) {
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
	return appendFloat32(body, ack.Speed), nil
}
