package modernworld

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	SMSGUpdateObject = uint16(10187)

	LegacySMSGUpdateObject           = uint16(0x00a9)
	LegacySMSGCompressedUpdateObject = uint16(0x01f6)

	maxLegacyUpdateObjectBody = 16 << 20
	maxLegacyObjectUpdates    = 1 << 16
	maxLegacyObjectList       = 1 << 20
	maxLegacySplinePoints     = 1 << 20
)

type LegacyUpdateType uint8

const (
	LegacyUpdateValues LegacyUpdateType = iota
	LegacyUpdateMovement
	LegacyUpdateCreateObject1
	LegacyUpdateCreateObject2
	LegacyUpdateFarObjects
	LegacyUpdateNearObjects
)

const (
	legacyUpdateSelf            = uint16(0x001)
	legacyUpdateTransport       = uint16(0x002)
	legacyUpdateAttackingTarget = uint16(0x004)
	legacyUpdateLowGUID         = uint16(0x008)
	legacyUpdateHighGUID        = uint16(0x010)
	legacyUpdateLiving          = uint16(0x020)
	legacyUpdateStationary      = uint16(0x040)
	legacyUpdateVehicle         = uint16(0x080)
	legacyUpdateGOPosition      = uint16(0x100)
	legacyUpdateGORotation      = uint16(0x200)
)

const (
	legacyMoveOnTransport     = uint32(0x00000200)
	legacyMoveFalling         = uint32(0x00001000)
	legacyMoveSwimming        = uint32(0x00200000)
	legacyMoveFlying          = uint32(0x02000000)
	legacyMoveSplineElevation = uint32(0x04000000)
	legacyMoveSplineEnabled   = uint32(0x08000000)
)

const (
	legacyMoveAlwaysAllowPitching = uint16(0x0020)
	legacyMoveInterpolate         = uint16(0x0400)
)

const (
	legacySplineFinalPoint       = uint32(0x00008000)
	legacySplineFinalTarget      = uint32(0x00010000)
	legacySplineFinalOrientation = uint32(0x00020000)
)

type LegacyUpdateBatch struct {
	Updates []LegacyObjectUpdate
}

type LegacyObjectUpdate struct {
	Type       LegacyUpdateType
	GUID       uint64
	ObjectType uint8
	Movement   *LegacyMovement
	Values     LegacyUpdateValuesBlock
	GUIDs      []uint64
}

type LegacyUpdateValuesBlock struct {
	MaskWords []uint32
	Fields    map[int]uint32
}

type LegacyMovement struct {
	UpdateFlags uint16
	MoveFlags   uint32
	MoveExtra   uint16
	MoveTime    uint32

	X           float32
	Y           float32
	Z           float32
	Orientation float32

	TransportGUID        uint64
	TransportX           float32
	TransportY           float32
	TransportZ           float32
	TransportOrientation float32
	TransportTime        uint32
	TransportTime2       uint32
	TransportSeat        int8

	Pitch           float32
	FallTime        uint32
	JumpVelocity    float32
	JumpSinAngle    float32
	JumpCosAngle    float32
	JumpXYSpeed     float32
	SplineElevation float32

	WalkSpeed       float32
	RunSpeed        float32
	RunBackSpeed    float32
	SwimSpeed       float32
	SwimBackSpeed   float32
	FlightSpeed     float32
	FlightBackSpeed float32
	TurnRate        float32
	PitchRate       float32
	CollisionHeight float32 // MSG_MOVE_SET_COLLISION_HGT observer update only

	CorpseOrientation  float32
	AttackTarget       uint64
	TransportPathTime  uint32
	VehicleID          uint32
	VehicleOrientation float32
	PackedRotation     uint64
	Spline             *LegacyMovementSpline
}

type LegacyMovementSpline struct {
	Flags            uint32
	FacingTarget     uint64
	FacingAngle      float32
	FacingX          float32
	FacingY          float32
	FacingZ          float32
	Time             uint32
	FullTime         uint32
	ID               uint32
	DurationModifier float32
	NextModifier     float32
	VerticalAccel    int32
	StartTime        int32
	Points           [][3]float32
	Mode             uint8
	End              [3]float32
}

func DecodeLegacyUpdateObject(opcode uint16, body []byte) (LegacyUpdateBatch, error) {
	switch opcode {
	case LegacySMSGUpdateObject:
		return ParseLegacyUpdateObject(body)
	case LegacySMSGCompressedUpdateObject:
		inflated, err := inflateLegacyUpdateObject(body)
		if err != nil {
			return LegacyUpdateBatch{}, err
		}
		return ParseLegacyUpdateObject(inflated)
	default:
		return LegacyUpdateBatch{}, fmt.Errorf("unsupported legacy update-object opcode 0x%04x", opcode)
	}
}

func inflateLegacyUpdateObject(body []byte) ([]byte, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("compressed update-object is shorter than its size prefix")
	}
	expected := binary.LittleEndian.Uint32(body[:4])
	if expected > maxLegacyUpdateObjectBody {
		return nil, fmt.Errorf("compressed update-object expands to %d bytes (limit %d)", expected, maxLegacyUpdateObjectBody)
	}
	zr, err := zlib.NewReader(bytes.NewReader(body[4:]))
	if err != nil {
		return nil, fmt.Errorf("open compressed update-object: %w", err)
	}
	decompressed, readErr := io.ReadAll(io.LimitReader(zr, int64(maxLegacyUpdateObjectBody)+1))
	closeErr := zr.Close()
	if readErr != nil {
		return nil, fmt.Errorf("inflate update-object: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close update-object inflater: %w", closeErr)
	}
	if len(decompressed) > maxLegacyUpdateObjectBody {
		return nil, fmt.Errorf("inflated update-object exceeds %d bytes", maxLegacyUpdateObjectBody)
	}
	if uint32(len(decompressed)) != expected {
		return nil, fmt.Errorf("inflated update-object size %d does not match prefix %d", len(decompressed), expected)
	}
	return decompressed, nil
}

func ParseLegacyUpdateObject(body []byte) (LegacyUpdateBatch, error) {
	if len(body) > maxLegacyUpdateObjectBody {
		return LegacyUpdateBatch{}, fmt.Errorf("legacy update-object is %d bytes (limit %d)", len(body), maxLegacyUpdateObjectBody)
	}
	r := legacyUpdateReader{data: body}
	count, err := r.u32()
	if err != nil {
		return LegacyUpdateBatch{}, fmt.Errorf("read update count: %w", err)
	}
	if count > maxLegacyObjectUpdates {
		return LegacyUpdateBatch{}, fmt.Errorf("legacy update-object contains %d updates (limit %d)", count, maxLegacyObjectUpdates)
	}
	batch := LegacyUpdateBatch{Updates: make([]LegacyObjectUpdate, 0, count)}
	for index := uint32(0); index < count; index++ {
		update, err := r.objectUpdate()
		if err != nil {
			return LegacyUpdateBatch{}, fmt.Errorf("parse legacy update %d/%d at byte %d: %w", index+1, count, r.offset, err)
		}
		batch.Updates = append(batch.Updates, update)
	}
	if r.remaining() != 0 {
		return LegacyUpdateBatch{}, fmt.Errorf("legacy update-object has %d trailing bytes", r.remaining())
	}
	return batch, nil
}

type legacyUpdateReader struct {
	data   []byte
	offset int
}

func (r *legacyUpdateReader) remaining() int { return len(r.data) - r.offset }

func (r *legacyUpdateReader) take(size int) ([]byte, error) {
	if size < 0 || size > r.remaining() {
		return nil, io.ErrUnexpectedEOF
	}
	value := r.data[r.offset : r.offset+size]
	r.offset += size
	return value, nil
}

func (r *legacyUpdateReader) u8() (uint8, error) {
	b, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *legacyUpdateReader) i8() (int8, error) {
	v, err := r.u8()
	return int8(v), err
}

func (r *legacyUpdateReader) u16() (uint16, error) {
	b, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

func (r *legacyUpdateReader) u32() (uint32, error) {
	b, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (r *legacyUpdateReader) i32() (int32, error) {
	v, err := r.u32()
	return int32(v), err
}

func (r *legacyUpdateReader) u64() (uint64, error) {
	b, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

func (r *legacyUpdateReader) f32() (float32, error) {
	v, err := r.u32()
	return math.Float32frombits(v), err
}

func (r *legacyUpdateReader) packedGUID() (uint64, error) {
	mask, err := r.u8()
	if err != nil {
		return 0, err
	}
	var guid uint64
	for index := uint(0); index < 8; index++ {
		if mask&(1<<index) == 0 {
			continue
		}
		part, err := r.u8()
		if err != nil {
			return 0, err
		}
		guid |= uint64(part) << (index * 8)
	}
	return guid, nil
}

func (r *legacyUpdateReader) objectUpdate() (LegacyObjectUpdate, error) {
	typeByte, err := r.u8()
	if err != nil {
		return LegacyObjectUpdate{}, err
	}
	update := LegacyObjectUpdate{Type: LegacyUpdateType(typeByte)}
	switch update.Type {
	case LegacyUpdateValues:
		update.GUID, err = r.packedGUID()
		if err == nil {
			update.Values, err = r.values()
		}
	case LegacyUpdateMovement:
		update.GUID, err = r.packedGUID()
		if err == nil {
			update.Movement, err = r.movement()
		}
	case LegacyUpdateCreateObject1, LegacyUpdateCreateObject2:
		update.GUID, err = r.packedGUID()
		if err != nil {
			break
		}
		update.ObjectType, err = r.u8()
		if err != nil {
			break
		}
		if update.ObjectType > 7 {
			err = fmt.Errorf("unsupported legacy object type %d", update.ObjectType)
			break
		}
		update.Movement, err = r.movement()
		if err == nil {
			update.Values, err = r.values()
		}
	case LegacyUpdateFarObjects, LegacyUpdateNearObjects:
		update.GUIDs, err = r.guidList()
	default:
		err = fmt.Errorf("unknown legacy update type %d", typeByte)
	}
	return update, err
}

func (r *legacyUpdateReader) guidList() ([]uint64, error) {
	count, err := r.i32()
	if err != nil {
		return nil, err
	}
	if count < 0 || count > maxLegacyObjectList {
		return nil, fmt.Errorf("invalid GUID-list count %d", count)
	}
	guids := make([]uint64, 0, count)
	for index := int32(0); index < count; index++ {
		guid, err := r.packedGUID()
		if err != nil {
			return nil, err
		}
		guids = append(guids, guid)
	}
	return guids, nil
}

func (r *legacyUpdateReader) values() (LegacyUpdateValuesBlock, error) {
	maskSize, err := r.u8()
	if err != nil {
		return LegacyUpdateValuesBlock{}, err
	}
	result := LegacyUpdateValuesBlock{
		MaskWords: make([]uint32, maskSize),
		Fields:    make(map[int]uint32),
	}
	for index := range result.MaskWords {
		result.MaskWords[index], err = r.u32()
		if err != nil {
			return LegacyUpdateValuesBlock{}, err
		}
	}
	for wordIndex, word := range result.MaskWords {
		for bit := 0; bit < 32; bit++ {
			if word&(uint32(1)<<bit) == 0 {
				continue
			}
			value, err := r.u32()
			if err != nil {
				return LegacyUpdateValuesBlock{}, err
			}
			result.Fields[wordIndex*32+bit] = value
		}
	}
	return result, nil
}

func (r *legacyUpdateReader) movement() (*LegacyMovement, error) {
	flags, err := r.u16()
	if err != nil {
		return nil, err
	}
	move := &LegacyMovement{UpdateFlags: flags, TransportSeat: -1}
	if flags&legacyUpdateLiving != 0 {
		if err := r.livingMovement(move); err != nil {
			return nil, err
		}
	} else if flags&legacyUpdateGOPosition != 0 {
		if move.TransportGUID, err = r.packedGUID(); err != nil {
			return nil, err
		}
		values, err := r.float32s(8)
		if err != nil {
			return nil, err
		}
		move.X, move.Y, move.Z = values[0], values[1], values[2]
		move.TransportX, move.TransportY, move.TransportZ = values[3], values[4], values[5]
		// The legacy GO_POSITION payload contains the object's orientation and
		// then its corpse orientation.  Unlike a living object's transport block,
		// it does not carry a second transport-facing value: HermesProxy copies
		// the object orientation into TransportOrientation and keeps the final
		// float as CorpseOrientation.  Treating the last float as the transport
		// orientation makes passengers rotate incorrectly on elevators.
		move.Orientation = values[6]
		move.TransportOrientation = values[6]
		move.CorpseOrientation = values[7]
	} else if flags&legacyUpdateStationary != 0 {
		values, err := r.float32s(4)
		if err != nil {
			return nil, err
		}
		move.X, move.Y, move.Z, move.Orientation = values[0], values[1], values[2], values[3]
	}

	if flags&legacyUpdateLowGUID != 0 {
		if _, err := r.u32(); err != nil {
			return nil, err
		}
	}
	if flags&legacyUpdateHighGUID != 0 {
		if _, err := r.u32(); err != nil {
			return nil, err
		}
	}
	if flags&legacyUpdateAttackingTarget != 0 {
		if move.AttackTarget, err = r.packedGUID(); err != nil {
			return nil, err
		}
	}
	if flags&legacyUpdateTransport != 0 {
		if move.TransportPathTime, err = r.u32(); err != nil {
			return nil, err
		}
	}
	if flags&legacyUpdateVehicle != 0 {
		if move.VehicleID, err = r.u32(); err != nil {
			return nil, err
		}
		if move.VehicleOrientation, err = r.f32(); err != nil {
			return nil, err
		}
	}
	if flags&legacyUpdateGORotation != 0 {
		if move.PackedRotation, err = r.u64(); err != nil {
			return nil, err
		}
	}
	return move, nil
}

func (r *legacyUpdateReader) livingMovement(move *LegacyMovement) error {
	var err error
	if move.MoveFlags, err = r.u32(); err != nil {
		return err
	}
	if move.MoveExtra, err = r.u16(); err != nil {
		return err
	}
	if move.MoveTime, err = r.u32(); err != nil {
		return err
	}
	position, err := r.float32s(4)
	if err != nil {
		return err
	}
	move.X, move.Y, move.Z, move.Orientation = position[0], position[1], position[2], position[3]

	if move.MoveFlags&legacyMoveOnTransport != 0 {
		if move.TransportGUID, err = r.packedGUID(); err != nil {
			return err
		}
		transport, err := r.float32s(4)
		if err != nil {
			return err
		}
		move.TransportX, move.TransportY, move.TransportZ, move.TransportOrientation = transport[0], transport[1], transport[2], transport[3]
		if move.TransportTime, err = r.u32(); err != nil {
			return err
		}
		if move.TransportSeat, err = r.i8(); err != nil {
			return err
		}
		if move.MoveExtra&legacyMoveInterpolate != 0 {
			if move.TransportTime2, err = r.u32(); err != nil {
				return err
			}
		}
	}

	if move.MoveFlags&(legacyMoveSwimming|legacyMoveFlying) != 0 || move.MoveExtra&legacyMoveAlwaysAllowPitching != 0 {
		if move.Pitch, err = r.f32(); err != nil {
			return err
		}
	}
	if move.FallTime, err = r.u32(); err != nil {
		return err
	}
	if move.MoveFlags&legacyMoveFalling != 0 {
		jump, err := r.float32s(4)
		if err != nil {
			return err
		}
		move.JumpVelocity, move.JumpSinAngle, move.JumpCosAngle, move.JumpXYSpeed = jump[0], jump[1], jump[2], jump[3]
	}
	if move.MoveFlags&legacyMoveSplineElevation != 0 {
		if move.SplineElevation, err = r.f32(); err != nil {
			return err
		}
	}

	speeds, err := r.float32s(9)
	if err != nil {
		return err
	}
	move.WalkSpeed, move.RunSpeed, move.RunBackSpeed = speeds[0], speeds[1], speeds[2]
	move.SwimSpeed, move.SwimBackSpeed = speeds[3], speeds[4]
	move.FlightSpeed, move.FlightBackSpeed = speeds[5], speeds[6]
	move.TurnRate, move.PitchRate = speeds[7], speeds[8]
	if move.MoveFlags&legacyMoveSplineEnabled != 0 {
		move.Spline, err = r.movementSpline()
	}
	return err
}

func (r *legacyUpdateReader) movementSpline() (*LegacyMovementSpline, error) {
	spline := &LegacyMovementSpline{}
	var err error
	if spline.Flags, err = r.u32(); err != nil {
		return nil, err
	}
	switch {
	case spline.Flags&legacySplineFinalTarget != 0:
		if spline.FacingTarget, err = r.u64(); err != nil {
			return nil, err
		}
	case spline.Flags&legacySplineFinalOrientation != 0:
		if spline.FacingAngle, err = r.f32(); err != nil {
			return nil, err
		}
	case spline.Flags&legacySplineFinalPoint != 0:
		point, err := r.float32s(3)
		if err != nil {
			return nil, err
		}
		spline.FacingX, spline.FacingY, spline.FacingZ = point[0], point[1], point[2]
	}
	if spline.Time, err = r.u32(); err != nil {
		return nil, err
	}
	if spline.FullTime, err = r.u32(); err != nil {
		return nil, err
	}
	if spline.ID, err = r.u32(); err != nil {
		return nil, err
	}
	if spline.DurationModifier, err = r.f32(); err != nil {
		return nil, err
	}
	if spline.NextModifier, err = r.f32(); err != nil {
		return nil, err
	}
	if spline.VerticalAccel, err = r.i32(); err != nil {
		return nil, err
	}
	if spline.StartTime, err = r.i32(); err != nil {
		return nil, err
	}
	pointCount, err := r.u32()
	if err != nil {
		return nil, err
	}
	if pointCount > maxLegacySplinePoints {
		return nil, fmt.Errorf("spline has %d points (limit %d)", pointCount, maxLegacySplinePoints)
	}
	spline.Points = make([][3]float32, pointCount)
	for index := range spline.Points {
		point, err := r.float32s(3)
		if err != nil {
			return nil, err
		}
		spline.Points[index] = [3]float32{point[0], point[1], point[2]}
	}
	if spline.Mode, err = r.u8(); err != nil {
		return nil, err
	}
	end, err := r.float32s(3)
	if err != nil {
		return nil, err
	}
	spline.End = [3]float32{end[0], end[1], end[2]}
	return spline, nil
}

func (r *legacyUpdateReader) float32s(count int) ([]float32, error) {
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
