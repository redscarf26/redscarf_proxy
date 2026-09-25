package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const (
	legacyItemOwner         = 6
	legacyItemContainedIn   = 8
	legacyItemCreator       = 10
	legacyItemGiftCreator   = 12
	legacyItemStackCount    = 14
	legacyItemDuration      = 15
	legacyItemSpellCharges  = 16
	legacyItemFlags         = 21
	legacyItemEnchantment   = 22
	legacyItemPropertySeed  = 58
	legacyItemRandom        = 59
	legacyItemDurability    = 60
	legacyItemMaxDurability = 61
	legacyItemCreatePlayed  = 62

	legacyContainerNumSlots = 64
	legacyContainerSlot1    = 66

	legacyGameObjectCreatedBy = 6
	legacyGameObjectDisplayID = 8
	legacyGameObjectFlags     = 9
	legacyGameObjectRotation  = 10
	legacyGameObjectDynamic   = 14
	legacyGameObjectFaction   = 15
	legacyGameObjectLevel     = 16
	legacyGameObjectBytes1    = 17

	// legacy proxy's build-54261 configuration selects protocol version 80. Its
	// ObjectUpdate.InitializePlaceholders path supplies 1772 for
	// GameObjectData.StateAnimID in that version (1672 is the fallback used by
	// other modern client versions). Without this spawn-tracking animation
	// state, an already-open door created with State=0 stays in its closed
	// visual even though the client accepts its state and collision changes.
	modernGameObjectStateAnimID = 1772

	legacyDynamicObjectCaster   = 6
	legacyDynamicObjectSpellID  = 9
	legacyDynamicObjectRadius   = 10
	legacyDynamicObjectCastTime = 11

	legacyCorpseOwner        = 6
	legacyCorpseParty        = 8
	legacyCorpseDisplayID    = 10
	legacyCorpseItem1        = 11
	legacyCorpseBytes1       = 30
	legacyCorpseGuild        = 32
	legacyCorpseFlags        = 33
	legacyCorpseDynamicFlags = 34
)

func EncodeItemCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions) ([]byte, error) {
	return encodeNonUnitCreate(update, options, 1, 1)
}

func EncodeContainerCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions) ([]byte, error) {
	return encodeNonUnitCreate(update, options, 2, 2)
}

func EncodeGameObjectCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions) ([]byte, error) {
	return encodeNonUnitCreate(update, options, 5, 8)
}

// IsLegacyTransportGameObject reports GameObject creates that participate in
// the transport hierarchy.  Transport roots and passengers use the same
// modern GameObject create packet, with a few transport-specific defaults.
func IsLegacyTransportGameObject(update LegacyObjectUpdate) bool {
	if update.ObjectType != 5 || update.Movement == nil {
		return false
	}
	return isLegacyTransportGUID(update.GUID) || update.Movement.TransportGUID != 0
}

func isLegacyTransportGUID(guid uint64) bool {
	highType := uint16(guid >> 48)
	return highType == 0xf120 || highType == 0x1fc0
}

func isLegacyMOTransportGUID(guid uint64) bool {
	return uint16(guid>>48) == 0x1fc0
}

// ICC gunship hulls. Their MotionTransport paths end on Saurfang's platform.
var iccGunshipEntries = map[uint32]struct{}{
	201580: {}, 201581: {}, 201811: {}, 201812: {},
}

// Dock Z is ~170; combat and Saurfang sit at ~380–585. The realm creates the
// friendly hull at the dock as GO_STATE_ACTIVE (moving, progress 0) and parks
// it a few hundred milliseconds later. 3.4.3 starts the 78 s path on that
// moving create; the later park-at-progress-0 is then read as path-complete,
// so a client that boarded a cannon follows the WMO to Saurfang. A create that
// is already parked (progress 0, state 1) stays at the dock until the
// departure Values update. Combat-height hulls with progress 0 are the enemy
// ship appearing mid-path and must keep moving.
const iccGunshipDockMaxZ = 250

// StabilizeICCGunshipCreate parks a docked ICC hull that the realm sent as
// moving at progress 0. The Fields map is updated in place so later cache and
// Values merges see the same state the client got. Pose, path timer, and
// mid-flight creates are left alone.
func StabilizeICCGunshipCreate(update *LegacyObjectUpdate) {
	if update == nil || update.Movement == nil || !isLegacyMOTransportGUID(update.GUID) {
		return
	}
	fields := update.Values.Fields
	if fields == nil {
		return
	}
	if _, ok := iccGunshipEntries[fields[legacyObjectEntry]]; !ok {
		return
	}
	if fields[legacyGameObjectDynamic]>>16 != 0 {
		return
	}
	packed := fields[legacyGameObjectBytes1]
	if byte(packed) != 0 {
		return
	}
	if update.Movement.Z >= iccGunshipDockMaxZ {
		return
	}
	fields[legacyGameObjectBytes1] = packed&^0xff | 1
}

func EncodeDynamicObjectCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions) ([]byte, error) {
	return encodeNonUnitCreate(update, options, 6, 9)
}

func EncodeCorpseCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions) ([]byte, error) {
	return encodeNonUnitCreate(update, options, 7, 10)
}

func encodeNonUnitCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions, wantLegacyType, modernType byte) ([]byte, error) {
	if update.Type != LegacyUpdateCreateObject1 && update.Type != LegacyUpdateCreateObject2 {
		return nil, fmt.Errorf("object update type is %d, want legacy CreateObject", update.Type)
	}
	if update.ObjectType != wantLegacyType {
		return nil, fmt.Errorf("legacy object type is %d, want %d", update.ObjectType, wantLegacyType)
	}
	if update.GUID == 0 {
		return nil, fmt.Errorf("object GUID is empty")
	}
	if wantLegacyType == 5 {
		StabilizeICCGunshipCreate(&update)
	}
	if update.Movement == nil {
		return nil, fmt.Errorf("object create has no movement block")
	}
	low, high := modernLegacyCreateGUID(update, options.MapID)
	if low == 0 && high == 0 {
		return nil, fmt.Errorf("legacy GUID type 0x%04x is not mapped", uint16(update.GUID>>48))
	}

	objectData := make([]byte, 0, 2<<10)
	objectData = append(objectData, byte(update.Type-1))
	objectData = appendPackedGUID128(objectData, low, high)
	objectData = append(objectData, modernType)
	objectData = appendNonUnitCreateMovement(objectData, update, wantLegacyType == 5, options)

	values := legacyActivePlayerValues{fields: update.Values.Fields}
	ownerGUID := values.legacyGUID(legacyItemOwner)
	owner := (wantLegacyType == 1 || wantLegacyType == 2) && ownerGUID != 0 && ownerGUID == options.OwnerGUID
	valuesData := make([]byte, 0, 2<<10)
	if owner {
		valuesData = append(valuesData, 0x03)
	} else {
		valuesData = append(valuesData, 0)
	}
	dynamicFlags := uint32(0)
	if wantLegacyType == 5 {
		// Transport state flags are separate from ordinary GameObject flags.
		legacyDynamic, hasLegacyDynamic := values.fields[legacyGameObjectDynamic]
		dynamicFlags = modernGameObjectCreateDynamicFlags(
			legacyDynamic,
			hasLegacyDynamic,
			isLegacyTransportGUID(update.GUID),
			update.Movement.TransportPathTime,
			transportPeriod(values.field(legacyObjectEntry)),
		)
		if isLegacyTransportGUID(update.GUID) {
			dynamicFlags = transportStateDynamicFlags(dynamicFlags, values.fields)
		}
	}
	valuesData = values.appendObjectWithDynamicFlags(valuesData, dynamicFlags)
	switch wantLegacyType {
	case 1, 2:
		valuesData = values.appendItem(valuesData, options.MapID, owner)
		if wantLegacyType == 2 {
			valuesData = values.appendContainer(valuesData, options.MapID)
		}
	case 5:
		valuesData = values.appendGameObjectCreate(valuesData, options.MapID,
			isLegacyTransportGUID(update.GUID))
	case 6:
		valuesData = values.appendDynamicObject(valuesData, options.MapID)
	case 7:
		valuesData = values.appendCorpse(valuesData, options.MapID)
	}
	objectData = binary.LittleEndian.AppendUint32(objectData, uint32(len(valuesData)))
	objectData = append(objectData, valuesData...)
	return encodeUpdateObjects(options.MapID, objectData), nil
}

func appendNonUnitCreateMovement(dst []byte, update LegacyObjectUpdate, gameObject bool, options ActivePlayerCreateOptions) []byte {
	move := update.Movement
	highType := uint16(update.GUID >> 48)
	isTransport := gameObject && (highType == 0xf120 || highType == 0x1fc0)
	// Item and Container create records in legacy proxy do not advertise the
	// Stationary flag and therefore do not contain Position/Orientation here.
	// Their location is inherited from the owning inventory/container. Writing
	// the four floats shifts ItemData by 16 bytes and corrupts every following
	// object in a merged login update.
	hasStationaryPosition := gameObject || (update.ObjectType != 1 && update.ObjectType != 2)
	bits := newBitWriter(dst)
	for index := 0; index < 18; index++ {
		set := index == 5 && hasStationaryPosition ||
			gameObject && move.TransportGUID != 0 && index == 4 ||
			move.AttackTarget != 0 && index == 6 ||
			isTransport && index == 7 ||
			move.VehicleID != 0 && index == 8 ||
			gameObject && index == 10
		bits.writeBit(set)
	}
	dst = bits.flush()
	dst = binary.LittleEndian.AppendUint32(dst, 0) // pause-time count
	if hasStationaryPosition {
		for _, value := range []float32{move.X, move.Y, move.Z, clampOrientation(move.Orientation)} {
			dst = appendFloat32(dst, value)
		}
	}
	if move.AttackTarget != 0 {
		low, high := modernLegacyGUID(move.AttackTarget, 0)
		dst = appendPackedGUID128(dst, low, high)
	}
	if isTransport {
		dst = binary.LittleEndian.AppendUint32(dst, transportPathTime(move, time.Now()))
	}
	if move.VehicleID != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, move.VehicleID)
		dst = appendFloat32(dst, move.VehicleOrientation)
	}
	if gameObject {
		// Rotation is the packed local GameObject quaternion supplied by the
		// legacy create block.  Stationary/world orientation is a separate
		// field; synthesizing a quaternion from it rotates transport WMOs twice
		// and differs from legacy proxy's GetPackedRotation result for identity (zero).
		dst = binary.LittleEndian.AppendUint64(dst, gameObjectPackedRotation(update))
	}
	if gameObject && move.TransportGUID != 0 {
		transportLow, transportHigh := modernLegacyGUID(move.TransportGUID, options.MapID)
		dst = appendPackedGUID128(dst, transportLow, transportHigh)
		for _, value := range []float32{move.TransportX, move.TransportY, move.TransportZ, move.TransportOrientation} {
			dst = appendFloat32(dst, value)
		}
		dst = append(dst, byte(move.TransportSeat))
		dst = binary.LittleEndian.AppendUint32(dst, move.TransportTime)
		transportBits := newBitWriter(dst)
		// MovementInfo.WriteTransportInfoModern writes HasPrevTime first,
		// followed by HasVehicleID. Hermes' 3.4.3 writer always clears
		// HasPrevTime and does not forward the legacy TransportTime2 field.
		// Advertising that extra uint32 shifts the following create fields and
		// makes the client parse an invalid transport record.
		transportBits.writeBit(false)
		transportBits.writeBit(move.VehicleID != 0)
		dst = transportBits.flush()
		if move.VehicleID != 0 {
			dst = binary.LittleEndian.AppendUint32(dst, move.VehicleID)
		}
	}
	return dst
}

// LegacyGameObjectRotation returns the legacy GAMEOBJECT_ROTATION quaternion
// (fields 10-13) and whether the realm supplied it. The words are kept as raw
// bits because the modern create must forward them unchanged: the captured
// legacy proxy creates disagree with any re-derivation from the packed local rotation.
func LegacyGameObjectRotation(fields map[int]uint32) ([4]uint32, bool) {
	var rotation [4]uint32
	_, present := fields[legacyGameObjectRotation]
	for index := range rotation {
		rotation[index] = fields[legacyGameObjectRotation+index]
	}
	return rotation, present
}

func gameObjectPackedRotation(update LegacyObjectUpdate) uint64 {
	rotation := update.Movement.PackedRotation
	quaternion, unpacked := unpackGameObjectRotation(rotation)
	overridden := false
	for index := range quaternion {
		if value, present := update.Values.Fields[legacyGameObjectRotation+index]; present {
			quaternion[index] = float64(math.Float32frombits(value))
			overridden = true
		}
	}
	entry := update.Values.Fields[legacyObjectEntry]
	tramCorrection := entry == 176080 || entry == 176084 || entry == 176085
	if !overridden && !tramCorrection {
		return rotation
	}
	if !unpacked {
		quaternion = [4]float64{0, 0, 0, 1}
		for index := range quaternion {
			if value, present := update.Values.Fields[legacyGameObjectRotation+index]; present {
				quaternion[index] = float64(math.Float32frombits(value))
			}
		}
	}
	// Hermes applies the Deeprun tram correction to the local GameObject
	// quaternion after reading GAMEOBJECT_ROTATION.  The client uses this
	// rotation for the cart contents, while ParentRotation controls the path
	// pivot; correcting only the latter leaves some carts travelling through
	// the wall in the opposite direction.
	if tramCorrection {
		quaternion = invertGameObjectYaw(quaternion)
	}
	return packGameObjectRotation(quaternion)
}

func invertGameObjectYaw(quaternion [4]float64) [4]float64 {
	// Match Hermes' Quaternion.AsEulerAngles/ EulerAngles.AsQuaternion pair:
	// convert to ZYX Euler angles, negate yaw (the Z angle), and reconstruct
	// the quaternion.  Keeping this in double precision avoids introducing a
	// visible rounding jump before the packed representation is written.
	x, y, z, w := quaternion[0], quaternion[1], quaternion[2], quaternion[3]
	sinr := 2 * (w*x + y*z)
	cosr := 1 - 2*(x*x+y*y)
	roll := math.Atan2(sinr, cosr)
	sinp := 2 * (w*y - z*x)
	pitch := math.Asin(sinp)
	if sinp >= 1 {
		pitch = math.Pi / 2
	} else if sinp <= -1 {
		pitch = -math.Pi / 2
	}
	siny := 2 * (w*z + x*y)
	cosy := 1 - 2*(y*y+z*z)
	yaw := -math.Atan2(siny, cosy)

	cy, sy := math.Cos(yaw*0.5), math.Sin(yaw*0.5)
	cp, sp := math.Cos(pitch*0.5), math.Sin(pitch*0.5)
	cr, sr := math.Cos(roll*0.5), math.Sin(roll*0.5)
	return [4]float64{
		sr*cp*cy - cr*sp*sy,
		cr*sp*cy + sr*cp*sy,
		cr*cp*sy - sr*sp*cy,
		cr*cp*cy + sr*sp*sy,
	}
}

func unpackGameObjectRotation(packed uint64) ([4]float64, bool) {
	signed := func(value uint64, bits uint) int64 {
		shift := 64 - bits
		return int64(value<<shift) >> shift
	}
	x := float64(signed(packed>>42, 22)) / float64(uint64(1)<<21)
	y := float64(signed(packed>>21, 21)) / float64(uint64(1)<<20)
	z := float64(signed(packed, 21)) / float64(uint64(1)<<20)
	wSquared := 1 - x*x - y*y - z*z
	if wSquared < 0 {
		return [4]float64{}, false
	}
	return [4]float64{x, y, z, math.Sqrt(wSquared)}, true
}

func packGameObjectRotation(quaternion [4]float64) uint64 {
	sign := int64(1)
	if quaternion[3] < 0 {
		sign = -1
	}
	x := uint64(int64(quaternion[0]*float64(uint64(1)<<21))*sign) & 0x3fffff
	y := uint64(int64(quaternion[1]*float64(uint64(1)<<20))*sign) & 0x1fffff
	z := uint64(int64(quaternion[2]*float64(uint64(1)<<20))*sign) & 0x1fffff
	return x<<42 | y<<21 | z
}

func (v legacyActivePlayerValues) legacyGUID(index int) uint64 {
	return uint64(v.field(index)) | uint64(v.field(index+1))<<32
}

func (v legacyActivePlayerValues) appendLegacyGUID(dst []byte, index int, mapID uint16) []byte {
	low, high := modernLegacyGUID(v.legacyGUID(index), mapID)
	return appendPackedGUID128(dst, low, high)
}

func (v legacyActivePlayerValues) appendItem(dst []byte, mapID uint16, owner bool) []byte {
	for _, index := range []int{legacyItemOwner, legacyItemContainedIn, legacyItemCreator, legacyItemGiftCreator} {
		dst = v.appendLegacyGUID(dst, index, mapID)
	}
	if owner {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemStackCount))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemDuration))
		for index := 0; index < 5; index++ {
			dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemSpellCharges+index))
		}
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemFlags))
	for slot := 0; slot < 13; slot++ {
		// Build 54261 inserts Use at slot 7 before the five random enchants.
		if slot != 7 {
			legacySlot := slot
			if slot > 7 {
				legacySlot--
			}
			base := legacyItemEnchantment + legacySlot*3
			dst = binary.LittleEndian.AppendUint32(dst, v.field(base))
			dst = binary.LittleEndian.AppendUint32(dst, v.field(base+1))
			dst = binary.LittleEndian.AppendUint16(dst, uint16(v.field(base+2)))
		} else {
			dst = binary.LittleEndian.AppendUint32(dst, 0)
			dst = binary.LittleEndian.AppendUint32(dst, 0)
			dst = binary.LittleEndian.AppendUint16(dst, 0)
		}
		dst = binary.LittleEndian.AppendUint16(dst, 0) // inactive
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemPropertySeed))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemRandom))
	if owner {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemDurability))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemMaxDurability))
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyItemCreatePlayed))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint64(dst, 0)
	if owner {
		dst = binary.LittleEndian.AppendUint64(dst, 0)
		dst = append(dst, 0)
	}
	gems := itemSocketedGems(v)
	dst = binary.LittleEndian.AppendUint32(dst, 0) // ArtifactPowers.size
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(gems)))
	// legacy proxy WriteCreateItemData: Gems.size, then remaining ItemData scalars
	// (owner DEBUGItemLevel uint32, two always-present uint32s, owner uint16),
	// THEN SocketedGem bodies, THEN 6-bit ItemModList. Empty items are all
	// zeros so the old "gems before uint16" layout still matched 263/212, but
	// the client reads the uint16 before Gems[i] and would consume the first
	// two ItemID bytes — tooltip sockets stay empty.
	if owner {
		dst = binary.LittleEndian.AppendUint32(dst, 0) // DEBUGItemLevel
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	if owner {
		dst = binary.LittleEndian.AppendUint16(dst, 0)
	}
	dst = appendSocketedGemsCreate(dst, gems)
	return append(dst, 0) // ItemModList: six zero bits, flushed
}

func (v legacyActivePlayerValues) appendContainer(dst []byte, mapID uint16) []byte {
	for slot := 0; slot < 36; slot++ {
		dst = v.appendLegacyGUID(dst, legacyContainerSlot1+slot*2, mapID)
	}
	return binary.LittleEndian.AppendUint32(dst, v.field(legacyContainerNumSlots))
}

func (v legacyActivePlayerValues) appendGameObject(dst []byte, mapID uint16) []byte {
	return v.appendGameObjectCreate(dst, mapID, false)
}

// appendGameObjectCreate writes the 3.4.3 GameObjectData descriptor.  Hermes
// applies transport defaults while initializing placeholders; doing that at
// create time is important because the client otherwise receives a platform
// with no path progress or a zero-length transport period.
func (v legacyActivePlayerValues) appendGameObjectCreate(dst []byte, mapID uint16, transport bool) []byte {
	// legacy proxy ObjectUpdateBuilder343.WriteCreateGameObjectData (empty effects):
	// Int32 display, SpellVisualID, StateSpellVisualID, StateAnimID,
	// StateAnimKitID, UInt32 StateWorldEffectIDs count, 2x PackedGuid128,
	// flags, 4x float32, faction, level, State byte, TypeID int8, PercentHealth byte,
	// ArtKit UInt32, then 3 more UInt32.
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyGameObjectDisplayID))
	dst = binary.LittleEndian.AppendUint32(dst, 0) // SpellVisualID
	dst = binary.LittleEndian.AppendUint32(dst, 0) // StateSpellVisualID
	dst = binary.LittleEndian.AppendUint32(dst, modernGameObjectStateAnimID)
	dst = binary.LittleEndian.AppendUint32(dst, 0) // StateAnimKitID
	dst = binary.LittleEndian.AppendUint32(dst, 0) // StateWorldEffectIDs count
	dst = v.appendLegacyGUID(dst, legacyGameObjectCreatedBy, mapID)
	dst = appendPackedGUID128(dst, 0, 0)
	flags := v.field(legacyGameObjectFlags)
	if transport {
		// legacy proxy marks every MO_TRANSPORT root (legacy TypeID 15, e.g. the
		// Purple Princess and the other ship hulls) with the modern transport
		// flag 0x1000000, independent of whether the entry has a DB2 path
		// period.  The 0xf120 platform components are GAMEOBJECT_TYPE_TRANSPORT
		// (TypeID 11) and keep their legacy flags.  Gating on the period table
		// alone left period-0 ships with 0x28, which the 3.4.3 client renders
		// as a plain object and dereferences a missing transport record while
		// attaching the WMO.
		packed := v.field(legacyGameObjectBytes1)
		if period := transportPeriod(v.field(legacyObjectEntry)); period != 0 || byte(packed>>8) == 15 {
			flags = modernTransportGameObjectFlags
		}
	}
	dst = binary.LittleEndian.AppendUint32(dst, flags)
	legacyRotation, hasLegacyRotation := LegacyGameObjectRotation(v.fields)
	for index := 0; index < 4; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, gameObjectCreateParentRotation(
			v.field(legacyObjectEntry),
			legacyRotation,
			hasLegacyRotation,
			transport,
			index,
		))
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyGameObjectFaction))
	// legacy proxy's ObjectUpdate.InitializePlaceholders deliberately does not fill an
	// absent transport Level when protocol version 80 (3.4.3.54261) is active.
	// Level therefore remains the legacy value, normally zero.  Substituting the
	// DB2 path period here changes the GameObject descriptor consumed while the
	// client constructs the transport WMO.
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyGameObjectLevel))
	packed := v.field(legacyGameObjectBytes1)
	percentHealth := byte(0)
	if _, present := v.fields[legacyGameObjectBytes1]; present {
		// legacy proxy copies WotLK GAMEOBJECT_BYTES_1 byte 3 into the modern
		// PercentHealth/animation-progress field.
		percentHealth = byte(packed >> 24)
	}
	dst = append(dst, byte(packed), byte(packed>>8), percentHealth)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(byte(packed>>16)))
	for index := 0; index < 3; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	return dst
}

const modernTransportGameObjectFlags = uint32(1048616) // 0x100028

// TransportPeriods is the Build 54261 (ExpansionVersion=2) table shipped with
// HermesProxy.  Unknown transports still get a deterministic 16-bit fallback,
// which is what Hermes uses when no DB2 period is available.
var modernTransportPeriods = map[uint32]uint32{
	20808:  231237,
	164871: 239337,
	175080: 248994,
	176231: 230164,
	176244: 312321,
	176310: 241780,
	176495: 317278,
	177233: 259753,
	181056: 1208095,
	181646: 238709,
	186238: 302415,
	190549: 566367,
}

func transportPeriod(entry uint32) uint32 {
	return modernTransportPeriods[entry]
}

// transportPathTime is the timer the client animates a transport from.
// AzerothCore fills it for the static ICC lifts but leaves it zero for the two
// gunship hulls, where legacy proxy stamps the local clock; both ICC hull creates match
// legacy proxy's capture byte for byte once this field agrees. Deriving the timer from
// the realm's progress word instead - zero for a docked hull - sent both hulls
// to the end of their paths, so a zero timer is not a phase the client accepts.
func transportPathTime(move *LegacyMovement, now time.Time) uint32 {
	if move.TransportPathTime != 0 {
		return move.TransportPathTime
	}
	return uint32(now.Unix())
}

func transportDynamicFlags(pathTime, period uint32) uint32 {
	if period != 0 {
		progress := (uint64(pathTime%period) * uint64(^uint16(0))) / uint64(period)
		return uint32(progress) << 16
	}
	return (pathTime % uint32(^uint16(0))) << 16
}

func modernGameObjectCreateDynamicFlags(legacy uint32, present, transport bool, pathTime, period uint32) uint32 {
	modern := modernGameObjectDynamicFlags(legacy, transport)
	low := modern & 0xffff
	if !transport {
		// legacy proxy initializes every ordinary GameObject create to completed
		// animation progress before OR-ing the remapped low-word flags.
		return 0xffff0000 | low
	}
	if present {
		// legacy proxy preserves the high-word animation/path progress supplied by the
		// legacy realm while remapping only the low-word interaction flags.
		return modern
	}
	return transportDynamicFlags(pathTime, period) | low
}

const (
	goArthasPlatform = uint32(202161)

	// Reference capture update-000116: GameObject 202161
	// ParentRotation is (0x43, 0, 0, 1). Query data[18] is the same 67.
	// Identity (0,0,0,1) and the 3.3.5 integer 5535469 with W=0 both leave
	// the Frozen Throne WMO without collision, so 70860 drops the raid
	// (and Tirion) through the floor.
	arthasPlatformParentRotationX = uint32(0x43)
)

func parentRotationIdentity(index int) uint32 {
	if index == 3 {
		return math.Float32bits(1)
	}
	return 0
}

func arthasPlatformParentRotation(index int) uint32 {
	if index == 0 {
		return arthasPlatformParentRotationX
	}
	return parentRotationIdentity(index)
}

// gameObjectCreateParentRotation is GameObjectData.ParentRotation for one
// create word. Transports keep legacy proxy's copy of GAMEOBJECT_ROTATION plus the
// Hermes pivot table. Ordinary doors stay identity, matching captured legacy proxy
// creates. The Frozen Throne floor is the exception: 3.4.3 walks on it only
// when ParentRotation matches legacy proxy's (0x43, 0, 0, 1).
func gameObjectCreateParentRotation(entry uint32, legacy [4]uint32, hasLegacy, transport bool, index int) uint32 {
	if transport {
		rotation := parentRotationIdentity(index)
		if hasLegacy {
			rotation = legacy[index]
		}
		return transportParentRotation(entry, index, rotation)
	}
	if entry == goArthasPlatform {
		return arthasPlatformParentRotation(index)
	}
	return parentRotationIdentity(index)
}

func translateFrozenThronePlatformStats(entry uint32, stats *GameObjectQueryStats) {
	if stats == nil || entry != goArthasPlatform || stats.Type != 33 {
		return
	}
	if stats.Data[18] == 0 {
		stats.Data[18] = int32(arthasPlatformParentRotationX)
	}
}

func transportParentRotation(entry uint32, index int, fallback uint32) uint32 {
	// These are the same pivot corrections used by HermesProxy for the
	// Deeprun tram and the Zangarmarsh elevator.  They are only applied when a
	// transport object is being created, so ordinary doors retain their legacy
	// quaternion unchanged.
	switch entry {
	case 176081, 176082, 176083, 176085:
		rotation := [...]uint32{
			math.Float32bits(-4.371139e-08),
			math.Float32bits(0),
			math.Float32bits(1),
			math.Float32bits(0),
		}
		return rotation[index]
	case 183177:
		rotation := [...]uint32{
			math.Float32bits(0),
			math.Float32bits(0),
			math.Float32bits(-0.69465846),
			math.Float32bits(0.7193397),
		}
		return rotation[index]
	default:
		return fallback
	}
}

func (v legacyActivePlayerValues) appendDynamicObject(dst []byte, mapID uint16) []byte {
	dst = v.appendLegacyGUID(dst, legacyDynamicObjectCaster, mapID)
	dst = append(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, KnownSpellVisual(v.field(legacyDynamicObjectSpellID)))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyDynamicObjectSpellID))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyDynamicObjectRadius))
	return binary.LittleEndian.AppendUint32(dst, v.field(legacyDynamicObjectCastTime))
}

func (v legacyActivePlayerValues) appendCorpse(dst []byte, mapID uint16) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyCorpseDynamicFlags))
	dst = v.appendLegacyGUID(dst, legacyCorpseOwner, mapID)
	dst = v.appendLegacyGUID(dst, legacyCorpseParty, mapID)
	guildID := v.field(legacyCorpseGuild)
	if guildID == 0 {
		dst = appendPackedGUID128(dst, 0, 0)
	} else {
		dst = appendPackedGUID128(dst, uint64(guildID), uint64(28)<<58|uint64(1)<<42)
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyCorpseDisplayID))
	flags := v.field(legacyCorpseFlags)
	for index := 0; index < 19; index++ {
		item := v.field(legacyCorpseItem1 + index)
		if index == 0 && flags&0x04 != 0 || index == 14 && flags&0x08 != 0 {
			item = 0
		}
		dst = binary.LittleEndian.AppendUint32(dst, item)
	}
	packed := v.field(legacyCorpseBytes1)
	dst = append(dst, byte(packed>>8), byte(packed>>16), 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0) // customization count
	dst = binary.LittleEndian.AppendUint32(dst, flags&^0x0c)
	return binary.LittleEndian.AppendUint32(dst, 0) // faction template
}

func modernGameObjectDynamicFlags(legacy uint32, transport bool) uint32 {
	// GAMEOBJECT_DYNAMIC packs the client-visible flags into the low word and
	// the animation/path progress into the high word.  CastModern only remaps
	// the flags. legacy proxy preserves the high word and, when an ordinary object's
	// initial value omits it, supplies 0xffff as the completed-path sentinel.
	// Dropping that word leaves doors clickable/collidable but visually stuck
	// in their closed frame.
	modern := legacy & 0xffff0000
	if modern == 0 && !transport {
		modern = 0xffff0000
	}
	legacy &= 0xffff
	if legacy&0x01 != 0 {
		modern |= 0x04
	}
	if legacy&0x02 != 0 {
		modern |= 0x08
	}
	if legacy&0x04 != 0 {
		modern |= 0x80
	}
	if legacy&0x08 != 0 {
		modern |= 0x20
	}
	if legacy&0x10 != 0 {
		modern |= 0x40
	}
	if transport {
		// Default for transport records without cached state. Create and Values
		// encoders replace this with the state-dependent flags when available.
		modern |= 0x40
	}
	return modern
}

// transportStateDynamicFlags matches legacy proxy ReadValuesUpdateBlock at 0xa684b9:
// modern HighGuid 6 (both transport GUID families), state 0 => 0x104,
// state 1 => 0x40. Ordinary GameObjects must not receive these state bits.
func transportStateDynamicFlags(dynamic uint32, fields map[int]uint32) uint32 {
	packed, present := fields[legacyGameObjectBytes1]
	if !present {
		return dynamic
	}
	// Remove the unconditional default, retaining explicitly supplied flags.
	dynamic &^= 0x40
	if fields[legacyGameObjectDynamic]&0x10 != 0 {
		dynamic |= 0x40
	}
	switch byte(packed) {
	case 0:
		dynamic |= 0x104
	case 1:
		dynamic |= 0x40
	}
	return dynamic
}
