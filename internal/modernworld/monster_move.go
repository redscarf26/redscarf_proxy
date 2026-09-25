package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	SMSGOnMonsterMove = uint16(11732)

	legacySplineTypeNormal       = uint8(0)
	legacySplineTypeStop         = uint8(1)
	legacySplineTypeFacingSpot   = uint8(2)
	legacySplineTypeFacingTarget = uint8(3)
	legacySplineTypeFacingAngle  = uint8(4)

	legacySplineTrajectory    = uint32(0x00000800)
	legacySplineFlying        = uint32(0x00002000)
	legacySplineCatmullRom    = uint32(0x00040000)
	legacySplineAnimationTier = uint32(0x00200000)

	modernSplineCatmullRom       = uint32(0x00000800)
	modernSplineUncompressedPath = uint32(0x00400000)
	modernSplineFlying           = uint32(0x00000200)
	modernSplineParabolic        = uint32(0x04000000)
	// AzerothCore renamed Trinity's Walkmode bit to CanSwim at the same value.
	// Walking vs running is a separate SMSG_SPLINE_MOVE_SET_WALK/RUN_MODE
	// message; 0x1000 on an AC monster spline means the creature can swim.
	legacySplineCanSwim      = uint32(0x00001000)
	modernSplineCanSwim           = uint32(0x00200000)
	modernSplineAnimTierSwim      = uint32(0x00000001)
	modernSplineAnimTierHover     = uint32(0x00000002)
	modernSplineAnimTierFly       = uint32(0x00000004)
	modernSplineAnimTierSubmerged = uint32(0x00000008)
	legacyUnitFlagSwimming        = uint32(0x00008000)
	modernTaxiSplineFlags    = uint32(0x91600a00) // flying, catmull-rom, can-swim, uncompressed, Unknown5, steering, Unknown10
	maxMonsterMovePoints     = uint32(0xffff)
)

// LegacyMonsterMove is the server-controlled 3.3.5a movement spline carried by
// SMSG_ON_MONSTER_MOVE or SMSG_MONSTER_MOVE_TRANSPORT.
type LegacyMonsterMove struct {
	MoverGUID     uint64
	TransportGUID uint64
	TransportSeat int8

	StartX float32
	StartY float32
	StartZ float32

	SplineID     uint32
	SplineType   uint8
	Flags        uint32
	Duration     uint32
	Elapsed      uint32
	Uncompressed bool
	Mode         uint8

	FacingTarget uint64
	FacingAngle  float32
	FacingX      float32
	FacingY      float32
	FacingZ      float32

	Points       [][3]float32
	End          [3]float32
	PackedDeltas []uint32
	HasEnd       bool

	// JumpGravity and JumpStartTime are the WotLK SPLINEFLAG_TRAJECTORY
	// payload (vertical acceleration + effect start time). Build 54261 reads
	// them as MonsterMove jump extra data; dropping them interpolates the
	// spline on the ground, so ICC rocket-pack jumps look like a run.
	JumpGravity   float32
	JumpStartTime uint32

	// TaxiStart is proxy-side state rather than a WotLK wire field. Only the
	// first taxi spline needs the build-54261 start decoration; decorating every
	// later route segment makes the client repeatedly restart camera control.
	TaxiStart bool

	// Swimming is proxy-side state from UNIT_FLAG_SWIMMING. AzerothCore sends
	// SMSG_SPLINE_MOVE_START_SWIM separately, but 3.4.3 ON_MONSTER_MOVE without
	// CanSwim/AnimTierSwim overrides that and plays the run animation in water.
	Swimming bool
}

// ParseLegacyMonsterMove strictly decodes the WotLK 12340 wire layout. The
// transport variant has an extra packed transport GUID and seat after MoverGUID.
func ParseLegacyMonsterMove(body []byte, transport bool) (LegacyMonsterMove, error) {
	r := legacyMonsterMoveReader{data: body}
	var move LegacyMonsterMove
	var err error
	if move.MoverGUID, err = r.packedGUID(); err != nil {
		return move, fmt.Errorf("read mover GUID: %w", err)
	}
	if transport {
		if move.TransportGUID, err = r.packedGUID(); err != nil {
			return move, fmt.Errorf("read transport GUID: %w", err)
		}
		seat, readErr := r.u8()
		if readErr != nil {
			return move, fmt.Errorf("read transport seat: %w", readErr)
		}
		move.TransportSeat = int8(seat)
	}
	if _, err = r.u8(); err != nil { // Toggle AnimTierInTrans, present in 3.3.5a.
		return move, fmt.Errorf("read animation-tier toggle: %w", err)
	}
	coordinates, err := r.float32s(3)
	if err != nil {
		return move, fmt.Errorf("read start position: %w", err)
	}
	move.StartX, move.StartY, move.StartZ = coordinates[0], coordinates[1], coordinates[2]
	if move.SplineID, err = r.u32(); err != nil {
		return move, fmt.Errorf("read spline ID: %w", err)
	}
	if move.SplineType, err = r.u8(); err != nil {
		return move, fmt.Errorf("read spline type: %w", err)
	}
	switch move.SplineType {
	case legacySplineTypeNormal:
	case legacySplineTypeStop:
		if r.remaining() != 0 {
			return move, fmt.Errorf("stop spline has %d trailing bytes", r.remaining())
		}
		return move, nil
	case legacySplineTypeFacingSpot:
		facing, readErr := r.float32s(3)
		if readErr != nil {
			return move, fmt.Errorf("read facing spot: %w", readErr)
		}
		move.FacingX, move.FacingY, move.FacingZ = facing[0], facing[1], facing[2]
	case legacySplineTypeFacingTarget:
		if move.FacingTarget, err = r.u64(); err != nil {
			return move, fmt.Errorf("read facing target: %w", err)
		}
	case legacySplineTypeFacingAngle:
		if move.FacingAngle, err = r.f32(); err != nil {
			return move, fmt.Errorf("read facing angle: %w", err)
		}
		move.FacingAngle = clampOrientation(move.FacingAngle)
	default:
		return move, fmt.Errorf("unsupported legacy spline type %d", move.SplineType)
	}
	if move.Flags, err = r.u32(); err != nil {
		return move, fmt.Errorf("read spline flags: %w", err)
	}
	if move.Flags&legacySplineAnimationTier != 0 {
		if _, err = r.u8(); err != nil {
			return move, fmt.Errorf("read animation tier: %w", err)
		}
		if _, err = r.u32(); err != nil {
			return move, fmt.Errorf("read animation start time: %w", err)
		}
	}
	if move.Duration, err = r.u32(); err != nil {
		return move, fmt.Errorf("read spline duration: %w", err)
	}
	if move.Flags&legacySplineTrajectory != 0 {
		if move.JumpGravity, err = r.f32(); err != nil {
			return move, fmt.Errorf("read trajectory vertical speed: %w", err)
		}
		if move.JumpStartTime, err = r.u32(); err != nil {
			return move, fmt.Errorf("read trajectory start time: %w", err)
		}
	}
	pointCount, err := r.u32()
	if err != nil {
		return move, fmt.Errorf("read spline point count: %w", err)
	}
	if pointCount > maxMonsterMovePoints {
		return move, fmt.Errorf("spline has %d points (wire limit %d)", pointCount, maxMonsterMovePoints)
	}
	if move.Flags&(legacySplineFlying|legacySplineCatmullRom) != 0 {
		move.Points = make([][3]float32, pointCount)
		for index := range move.Points {
			point, readErr := r.float32s(3)
			if readErr != nil {
				return move, fmt.Errorf("read spline point %d: %w", index, readErr)
			}
			move.Points[index] = [3]float32{point[0], point[1], point[2]}
		}
	} else {
		end, readErr := r.float32s(3)
		if readErr != nil {
			return move, fmt.Errorf("read spline end: %w", readErr)
		}
		move.End = [3]float32{end[0], end[1], end[2]}
		move.HasEnd = true
		if pointCount > 0 {
			move.PackedDeltas = make([]uint32, pointCount-1)
			for index := range move.PackedDeltas {
				if move.PackedDeltas[index], err = r.u32(); err != nil {
					return move, fmt.Errorf("read packed spline delta %d: %w", index, err)
				}
			}
		}
	}
	if r.remaining() != 0 {
		return move, fmt.Errorf("monster move has %d trailing bytes", r.remaining())
	}
	return move, nil
}

// EncodeMonsterMove emits the exact 3.4.3.54261 SMSG_ON_MONSTER_MOVE body.
// mover and facingTarget should reuse the session's corrected create GUIDs.
func EncodeMonsterMove(move LegacyMonsterMove, mover, transport, facingTarget GUID128) ([]byte, error) {
	if mover.Low == 0 && mover.High == 0 {
		return nil, fmt.Errorf("modern mover GUID is empty")
	}
	modernType, err := modernSplineType(move.SplineType)
	if err != nil {
		return nil, err
	}
	modernFlags := modernSplineFlags(move.Flags)
	if move.Swimming {
		modernFlags |= modernSplineCanSwim | modernSplineAnimTierSwim
	}
	points := move.Points
	packedDeltas := move.PackedDeltas
	if move.TaxiStart {
		modernFlags = modernTaxiSplineFlags
	} else if move.Uncompressed || move.Flags&(legacySplineFlying|legacySplineCatmullRom) != 0 {
		modernFlags |= modernSplineUncompressedPath
	} else if move.HasEnd || !zeroVector3(move.End) {
		// legacy proxy keeps linear WotLK splines compressed. The modern packet's
		// Points array contains only the final destination; PackedDeltas carries
		// the intermediate midpoint offsets. In particular, an in-place
		// FacingTarget refresh is one point and zero deltas. Marking this as an
		// uncompressed path prevents build 54261 from applying FaceGUID as the
		// completed spline's persistent facing target.
		points = [][3]float32{move.End}
	} else {
		points = nil
		packedDeltas = nil
	}
	if len(points) > int(maxMonsterMovePoints) || len(packedDeltas) > int(maxMonsterMovePoints) {
		return nil, fmt.Errorf("modern spline exceeds 16-bit point count")
	}

	body := appendPackedGUID128(nil, mover.Low, mover.High)
	for _, value := range []float32{move.StartX, move.StartY, move.StartZ} {
		body = appendFloat32(body, value)
	}
	body = binary.LittleEndian.AppendUint32(body, move.SplineID)
	for range 3 { // Destination is unused by this packet version.
		body = appendFloat32(body, 0)
	}
	tolerance := newBitWriter(body)
	tolerance.writeBit(false) // CrzTeleport
	if len(points) == 0 {
		tolerance.writeBits(2, 3)
	} else {
		tolerance.writeBits(0, 3)
	}
	body = tolerance.flush()
	body = binary.LittleEndian.AppendUint32(body, modernFlags)
	body = binary.LittleEndian.AppendUint32(body, move.Elapsed) // elapsed
	duration, gravity := move.gunshipRocketJump()
	body = binary.LittleEndian.AppendUint32(body, duration)
	body = binary.LittleEndian.AppendUint32(body, 0) // fade-object time
	body = append(body, move.Mode)
	body = appendPackedGUID128(body, transport.Low, transport.High)
	body = append(body, byte(move.TransportSeat))
	bits := newBitWriter(body)
	bits.writeBits(uint32(modernType), 2)
	bits.writeBits(uint32(len(points)), 16)
	bits.writeBit(false) // voluntary vehicle exit
	bits.writeBit(false) // interpolate
	bits.writeBits(uint32(len(packedDeltas)), 16)
	hasJumpExtra := move.HasJumpExtra()
	bits.writeBit(false)        // spline filter
	bits.writeBit(false)        // spell effect extra data
	bits.writeBit(hasJumpExtra) // jump extra data
	body = bits.flush()

	switch modernType {
	case 1:
		for _, value := range []float32{move.FacingX, move.FacingY, move.FacingZ} {
			body = appendFloat32(body, value)
		}
	case 2:
		// legacy proxy leaves FaceDirection zero and lets the client continuously
		// derive the creature's orientation from FaceGUID.
		body = appendFloat32(body, 0)
		body = appendPackedGUID128(body, facingTarget.Low, facingTarget.High)
	case 3:
		body = appendFloat32(body, clampOrientation(move.FacingAngle))
	}
	for _, point := range points {
		for _, value := range point {
			body = appendFloat32(body, value)
		}
	}
	for _, packed := range packedDeltas {
		body = binary.LittleEndian.AppendUint32(body, packed)
	}
	if hasJumpExtra {
		body = appendFloat32(body, gravity)
		body = binary.LittleEndian.AppendUint32(body, move.JumpStartTime)
	}
	return body, nil
}

func (move LegacyMonsterMove) HasJumpExtra() bool {
	return move.Flags&legacySplineTrajectory != 0 || move.JumpGravity != 0 || move.JumpStartTime != 0
}

// AzerothCore sizes rocket-pack jumps as dist/speedXY, so a nearby click lasts
// 46–500ms and looks like a grasshopper hop. Stretching those to 1.2s without
// retuning gravity would send a 46ms/g=863 hop ~100 yards up. Keep Parabolic
// (legacy proxy drops it, which is why its pack is also wrong) and give short gunship
// jumps a 10-yard apex over at least 1.2s.
const (
	gunshipJumpMinDurationMS = uint32(1200)
	gunshipJumpMinApexYards  = float32(10)
)

func (move LegacyMonsterMove) gunshipRocketJump() (duration uint32, gravity float32) {
	duration = move.Duration
	gravity = move.JumpGravity
	if gravity <= 0 || !move.HasJumpExtra() || !isLegacyMOTransportGUID(move.TransportGUID) {
		return duration, gravity
	}
	if duration <= move.JumpStartTime {
		return duration, gravity
	}
	origSeconds := float32(duration-move.JumpStartTime) / 1000
	if origSeconds <= 0 {
		return duration, gravity
	}
	apex := gravity * origSeconds * origSeconds / 8
	if duration < move.JumpStartTime+gunshipJumpMinDurationMS {
		duration = move.JumpStartTime + gunshipJumpMinDurationMS
	}
	seconds := float32(duration-move.JumpStartTime) / 1000
	if apex < gunshipJumpMinApexYards {
		apex = gunshipJumpMinApexYards
	}
	return duration, apex * 8 / (seconds * seconds)
}

// IsLegacyTaxiFlight identifies the WotLK player taxi spline. Build 54261
// needs extra spline decoration and a delayed success reply for this path.
func IsLegacyTaxiFlight(move LegacyMonsterMove) bool {
	return move.Flags == legacySplineCanSwim|legacySplineFlying
}

func modernSplineType(legacy uint8) (uint8, error) {
	switch legacy {
	case legacySplineTypeNormal, legacySplineTypeStop:
		return 0, nil
	case legacySplineTypeFacingSpot:
		return 1, nil
	case legacySplineTypeFacingTarget:
		return 2, nil
	case legacySplineTypeFacingAngle:
		return 3, nil
	default:
		return 0, fmt.Errorf("unsupported legacy spline type %d", legacy)
	}
}

// Hermes CastFlags maps enum members by name instead of retaining their old
// numeric values. Keep that mapping explicit so WotLK-only bits cannot leak.
func modernSplineFlags(legacy uint32) uint32 {
	var modern uint32
	// WotLK low 3 bits are an AnimTier enum (Ground/Swim/Hover/Fly/Submerged).
	// 3.4.3 uses one-hot bits. legacy proxy's CastModern collapses 1/2/3 onto
	// Swim|Hover, so a Fly(3) NPC stands; do not copy that.
	switch legacy & 0x7 {
	case 1:
		modern |= modernSplineAnimTierSwim
	case 2:
		modern |= modernSplineAnimTierHover
	case 3:
		modern |= modernSplineAnimTierFly
	case 4:
		modern |= modernSplineAnimTierSubmerged
	}
	if legacy&legacySplineCanSwim != 0 {
		modern |= modernSplineCanSwim
	}
	if legacy&0x00000100 != 0 {
		modern |= 0x00000020
	} // Done
	if legacy&0x00000200 != 0 {
		modern |= 0x00000040
	} // Falling
	if legacy&0x00000400 != 0 {
		modern |= 0x00000080
	} // NoSpline
	if legacy&legacySplineTrajectory != 0 {
		// WotLK Trajectory (0x800) is modern Parabolic. Without this bit the
		// 3.4.3 client ignores jump extra data and interpolates the spline on
		// the ground. Do not also set Flying: that bit is the fly locomotion
		// pose, which is why rocket-pack jumps put the arms out.
		modern |= modernSplineParabolic
	}
	if legacy&0x00002000 != 0 {
		// WotLK's Flying flag selects the Catmull-Rom path layout as well as
		// the flying animation. Modern Classic split those meanings into two
		// flags; omitting CatmullRom makes mounts snap their facing/animation
		// at every decoded path node. Keep this in the shared mapper so resumed
		// create-time flight splines get the same smooth interpolation.
		modern |= modernSplineFlying | modernSplineCatmullRom
	} // Flying
	if legacy&0x00040000 != 0 {
		modern |= modernSplineCatmullRom
	} // CatmullRom
	if legacy&0x00080000 != 0 {
		modern |= 0x00001000
	} // Cyclic
	if legacy&0x00100000 != 0 {
		modern |= 0x00002000
	} // EnterCycle
	if legacy&0x00400000 != 0 {
		modern |= 0x00004000
	} // Frozen
	if legacy&0x01000000 != 0 {
		modern |= 0x00010000
	} // TransportExit
	if legacy&0x04000000 != 0 {
		modern |= 0x20000000
	} // Unknown8
	if legacy&0x20000000 != 0 {
		modern |= 0x02000000
	} // Animation
	if legacy&0x40000000 != 0 {
		modern |= 0x00400000
	} // UncompressedPath
	if legacy&0x80000000 != 0 {
		modern |= 0x80000000
	} // Unknown10
	return modern
}

// LegacyUnitIsSwimming reports AzerothCore UNIT_FLAG_SWIMMING on a cached
// values snapshot so monster-move encoding can keep the swim animation.
func LegacyUnitIsSwimming(fields map[int]uint32) bool {
	if len(fields) == 0 {
		return false
	}
	return fields[legacyUnitFlags]&legacyUnitFlagSwimming != 0
}

func clampOrientation(value float32) float32 {
	const fullCircle = float32(2 * math.Pi)
	for value < 0 {
		value += fullCircle
	}
	for value >= fullCircle {
		value -= fullCircle
	}
	return value
}

func zeroVector3(value [3]float32) bool {
	return value[0] == 0 && value[1] == 0 && value[2] == 0
}

type legacyMonsterMoveReader struct {
	data   []byte
	offset int
}

func (r *legacyMonsterMoveReader) remaining() int { return len(r.data) - r.offset }

func (r *legacyMonsterMoveReader) take(size int) ([]byte, error) {
	if size < 0 || size > r.remaining() {
		return nil, fmt.Errorf("need %d bytes at offset %d, body has %d", size, r.offset, len(r.data))
	}
	value := r.data[r.offset : r.offset+size]
	r.offset += size
	return value, nil
}

func (r *legacyMonsterMoveReader) u8() (uint8, error) {
	value, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

func (r *legacyMonsterMoveReader) u32() (uint32, error) {
	value, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(value), nil
}

func (r *legacyMonsterMoveReader) u64() (uint64, error) {
	value, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(value), nil
}

func (r *legacyMonsterMoveReader) f32() (float32, error) {
	value, err := r.u32()
	return math.Float32frombits(value), err
}

func (r *legacyMonsterMoveReader) float32s(count int) ([]float32, error) {
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

func (r *legacyMonsterMoveReader) packedGUID() (uint64, error) {
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
