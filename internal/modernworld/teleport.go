package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	CMSGLoadingScreenNotify  = uint16(13817)
	CMSGWorldPortResponse    = uint16(13818)
	CMSGSuspendTokenResponse = uint16(0x376A)
	CMSGMoveTeleportAck      = uint16(14842)
	SMSGNewWorld             = uint16(9620)
	SMSGResumeToken          = uint16(9641)
	SMSGSuspendToken         = uint16(9640)
	SMSGWorldServerInfo      = uint16(9645)
	SMSGTransferPending      = uint16(9677)
	SMSGUpdateLastInstance   = uint16(9864)
	SMSGTransferAborted      = uint16(9987)
	SMSGMoveTeleport         = uint16(11780)

	// HermesProxy 3.4.3 hardcodes this NewWorld reason for every legacy map change.
	newWorldReasonLegacy          = uint32(4)
	suspendTokenSequence          = uint32(3)
	suspendTokenReason            = uint32(1)
	transferAbortDefaultCondition = int32(-6)
)

type TransferPendingShip struct {
	ID          uint32
	OriginMapID int32
}

type TransferPending struct {
	MapID           uint32
	OldMapPosition  [3]float32
	Ship            *TransferPendingShip
	TransferSpellID *int32
}

type TransferAborted struct {
	MapID                     uint32
	Arg                       byte
	MapDifficultyXConditionID int32
	Reason                    byte
}

type NewWorld struct {
	MapID          uint32
	Position       [3]float32
	Orientation    float32
	Reason         uint32
	MovementOffset [3]float32
}

type SuspendToken struct {
	SequenceIndex uint32
	Reason        uint32
}

type WorldServerInfo struct {
	DifficultyID      uint32
	IsTournamentRealm byte
	InstanceGroupSize *uint32
}

type MoveTeleport struct {
	Mover        GUID128
	Transport    GUID128
	MoveCounter  uint32
	Position     [3]float32
	Orientation  float32
	PreloadWorld byte
	VehicleSeat  *int8
}

type MoveTeleportAck struct {
	Mover       GUID128
	MoveCounter uint32
	MoveTime    uint32
}

type LoadingScreenNotify struct {
	MapID   uint32
	Showing bool
}

func ParseLegacyTransferPending(body []byte) (TransferPending, error) {
	var pending TransferPending
	switch len(body) {
	case 4:
	case 12:
	default:
		return pending, fmt.Errorf("legacy transfer-pending has %d bytes, want 4 or 12", len(body))
	}
	pending.MapID = binary.LittleEndian.Uint32(body[:4])
	if len(body) == 12 {
		pending.Ship = &TransferPendingShip{
			ID:          binary.LittleEndian.Uint32(body[4:8]),
			OriginMapID: int32(binary.LittleEndian.Uint32(body[8:12])),
		}
	}
	return pending, nil
}

func EncodeTransferPending(pending TransferPending) []byte {
	body := binary.LittleEndian.AppendUint32(nil, pending.MapID)
	for _, value := range pending.OldMapPosition {
		body = appendFloat32(body, value)
	}
	bits := newBitWriter(body)
	bits.writeBit(pending.Ship != nil)
	bits.writeBit(pending.TransferSpellID != nil)
	body = bits.flush()
	if pending.Ship != nil {
		body = binary.LittleEndian.AppendUint32(body, pending.Ship.ID)
		body = appendInt32(body, int64(pending.Ship.OriginMapID))
	}
	if pending.TransferSpellID != nil {
		body = appendInt32(body, int64(*pending.TransferSpellID))
	}
	return body
}

func ParseLegacyTransferAborted(body []byte) (TransferAborted, error) {
	var aborted TransferAborted
	if len(body) != 5 && len(body) != 6 {
		return aborted, fmt.Errorf("legacy transfer-aborted has %d bytes, want 5 or 6", len(body))
	}
	aborted.MapID = binary.LittleEndian.Uint32(body[:4])
	aborted.Reason = body[4]
	if len(body) == 6 {
		aborted.Arg = body[5]
	}
	aborted.MapDifficultyXConditionID = transferAbortDefaultCondition
	return aborted, nil
}

func EncodeTransferAborted(aborted TransferAborted) []byte {
	body := binary.LittleEndian.AppendUint32(nil, aborted.MapID)
	body = append(body, aborted.Arg)
	body = appendInt32(body, int64(aborted.MapDifficultyXConditionID))
	bits := newBitWriter(body)
	bits.writeBits(uint32(aborted.Reason), 6)
	return bits.flush()
}

func ParseLegacyNewWorld(body []byte) (NewWorld, error) {
	var world NewWorld
	if len(body) != 20 {
		return world, fmt.Errorf("legacy new-world has %d bytes, want 20", len(body))
	}
	world.MapID = binary.LittleEndian.Uint32(body[:4])
	world.Position[0] = math.Float32frombits(binary.LittleEndian.Uint32(body[4:8]))
	world.Position[1] = math.Float32frombits(binary.LittleEndian.Uint32(body[8:12]))
	world.Position[2] = math.Float32frombits(binary.LittleEndian.Uint32(body[12:16]))
	world.Orientation = clampOrientation(math.Float32frombits(binary.LittleEndian.Uint32(body[16:20])))
	world.Reason = newWorldReasonLegacy
	return world, nil
}

func EncodeNewWorld(world NewWorld) []byte {
	body := binary.LittleEndian.AppendUint32(nil, world.MapID)
	for _, value := range world.Position {
		body = appendFloat32(body, value)
	}
	body = appendFloat32(body, clampOrientation(world.Orientation))
	body = binary.LittleEndian.AppendUint32(body, world.Reason)
	for _, value := range world.MovementOffset {
		body = appendFloat32(body, value)
	}
	return body
}

func EncodeSuspendToken(token SuspendToken) []byte {
	body := binary.LittleEndian.AppendUint32(nil, token.SequenceIndex)
	bits := newBitWriter(body)
	bits.writeBits(token.Reason, 2)
	return bits.flush()
}

func DefaultSuspendToken() SuspendToken {
	return SuspendToken{SequenceIndex: suspendTokenSequence, Reason: suspendTokenReason}
}

func ParseSuspendTokenResponse(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("suspend-token-response has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodeUpdateLastInstance(mapID uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, mapID)
}

func EncodeWorldServerInfo(info WorldServerInfo) []byte {
	body := binary.LittleEndian.AppendUint32(nil, info.DifficultyID)
	body = append(body, info.IsTournamentRealm)
	bits := newBitWriter(body)
	bits.writeBit(false) // XRealmPvpAlert
	bits.writeBit(false) // RestrictedAccountMaxLevel
	bits.writeBit(false) // RestrictedAccountMaxMoney
	bits.writeBit(info.InstanceGroupSize != nil)
	body = bits.flush()
	if info.InstanceGroupSize != nil {
		body = binary.LittleEndian.AppendUint32(body, *info.InstanceGroupSize)
	}
	return body
}

func WorldServerInfoForMap(mapID uint32) WorldServerInfo {
	return WorldServerInfoForDifficulty(mapID, MapDifficulty{})
}

func ParseLegacyMoveTeleportAck(body []byte) (uint64, uint32, LegacyMovement, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, 0, LegacyMovement{}, fmt.Errorf("read teleport GUID: %w", err)
	}
	counter, err := r.u32()
	if err != nil {
		return 0, 0, LegacyMovement{}, fmt.Errorf("read teleport counter: %w", err)
	}
	move, err := r.movementInfo()
	if err != nil {
		return 0, 0, move, err
	}
	if r.remaining() != 0 {
		return 0, 0, move, fmt.Errorf("legacy teleport-ack has %d trailing bytes", r.remaining())
	}
	return guid, counter, move, nil
}

func EncodeMoveTeleport(teleport MoveTeleport) []byte {
	body := appendPackedGUID128(nil, teleport.Mover.Low, teleport.Mover.High)
	body = binary.LittleEndian.AppendUint32(body, teleport.MoveCounter)
	for _, value := range teleport.Position {
		body = appendFloat32(body, value)
	}
	body = appendFloat32(body, clampOrientation(teleport.Orientation))
	body = append(body, teleport.PreloadWorld)
	bits := newBitWriter(body)
	bits.writeBit(teleport.Transport.Low != 0 || teleport.Transport.High != 0)
	bits.writeBit(teleport.VehicleSeat != nil)
	body = bits.flush()
	if teleport.VehicleSeat != nil {
		body = append(body, byte(*teleport.VehicleSeat))
		vehicleBits := newBitWriter(body)
		vehicleBits.writeBit(false) // VehicleExitVoluntary
		vehicleBits.writeBit(false) // VehicleExitTeleport
		body = vehicleBits.flush()
	}
	if teleport.Transport.Low != 0 || teleport.Transport.High != 0 {
		body = appendPackedGUID128(body, teleport.Transport.Low, teleport.Transport.High)
	}
	return body
}

func MoveTeleportFromLegacy(guid GUID128, counter uint32, move LegacyMovement, transport GUID128) MoveTeleport {
	teleport := MoveTeleport{
		Mover:       guid,
		Transport:   transport,
		MoveCounter: counter,
		Position:    [3]float32{move.X, move.Y, move.Z},
		Orientation: move.Orientation,
	}
	if move.TransportSeat > 0 {
		seat := move.TransportSeat
		teleport.VehicleSeat = &seat
	}
	return teleport
}

func ParseMoveTeleportAck(body []byte) (MoveTeleportAck, error) {
	var ack MoveTeleportAck
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return ack, err
	}
	if consumed+8 != len(body) {
		return ack, fmt.Errorf("move-teleport-ack has %d trailing/missing bytes", len(body)-consumed-8)
	}
	ack.Mover = GUID128{Low: low, High: high}
	ack.MoveCounter = binary.LittleEndian.Uint32(body[consumed : consumed+4])
	ack.MoveTime = binary.LittleEndian.Uint32(body[consumed+4:])
	return ack, nil
}

func EncodeLegacyMoveTeleportAck(legacyGUID uint64, ack MoveTeleportAck) []byte {
	body := appendLegacyPackedGUID(nil, legacyGUID)
	body = binary.LittleEndian.AppendUint32(body, ack.MoveCounter)
	return binary.LittleEndian.AppendUint32(body, ack.MoveTime)
}

func ParseWorldPortResponse(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("world-port-response has %d bytes, want 0", len(body))
	}
	return nil
}

func ParseLoadingScreenNotify(body []byte) (LoadingScreenNotify, error) {
	var notify LoadingScreenNotify
	if len(body) != 5 {
		return notify, fmt.Errorf("loading-screen-notify has %d bytes, want 5", len(body))
	}
	notify.MapID = binary.LittleEndian.Uint32(body[:4])
	r := movementReader{data: body[4:]}
	showing, err := r.bit()
	if err != nil {
		return notify, fmt.Errorf("read loading-screen showing bit: %w", err)
	}
	notify.Showing = showing
	r.align()
	if r.remaining() != 0 {
		return notify, fmt.Errorf("loading-screen-notify has %d trailing bytes", r.remaining())
	}
	return notify, nil
}

func EncodeLoadingScreenNotify(notify LoadingScreenNotify) []byte {
	body := binary.LittleEndian.AppendUint32(nil, notify.MapID)
	bits := newBitWriter(body)
	bits.writeBit(notify.Showing)
	return bits.flush()
}
