package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"
)

const (
	CMSGUseItem                 = uint16(12952)
	CMSGCastSpell               = uint16(12956)
	CMSGCancelCast              = uint16(12959)
	CMSGUpdateMissileTrajectory = uint16(14915) // 0x3A43; Hermes 54261 Opcode.cs
	CMSGCancelAura              = uint16(12719)
	CMSGCancelMountAura         = uint16(0x327F)
	CMSGResurrectResponse       = uint16(13957)
	SMSGSpellChannelStart       = uint16(11313)
	SMSGSpellChannelUpdate      = uint16(11314)
	SMSGSetFlatSpellModifier    = uint16(11315)
	SMSGSetPctSpellModifier     = uint16(11316)
	SMSGSpellPrepare            = uint16(11317)
	SMSGSpellGo                 = uint16(11318)
	SMSGSpellStart              = uint16(11319)
	SMSGResurrectRequest        = uint16(9598)
	SMSGSpellFailure            = uint16(11344)
	SMSGSpellFailedOther        = uint16(11346)
	SMSGCastFailed              = uint16(11348)

	maxSpellTargets       = 64
	maxSpellOptionalItems = 8
	// legacy proxy/Hermes create ordinary matched client Cast GUIDs with
	// SpellCastSource.Normal. Unmatched server casts deliberately keep an empty
	// Cast GUID in the proxy instead of calling ModernCastGUID.
	modernSpellCastSource = 3
	modernGUIDCounterMask = uint64(0xffffffffff)

	targetFlagUnit         uint32 = 0x00000002
	targetFlagItem         uint32 = 0x00000010
	targetFlagSourceLoc    uint32 = 0x00000020
	targetFlagDestLoc      uint32 = 0x00000040
	targetFlagCorpseEnemy  uint32 = 0x00000200
	targetFlagGameObject   uint32 = 0x00000800
	targetFlagTradeItem    uint32 = 0x00001000
	targetFlagString       uint32 = 0x00002000
	targetFlagCorpseAlly   uint32 = 0x00008000
	targetFlagUnitMinipet  uint32 = 0x00010000
	targetFlagExtraTargets uint32 = 0x00080000

	targetFlagUnitLike = targetFlagUnit | targetFlagCorpseEnemy | targetFlagGameObject | targetFlagCorpseAlly | targetFlagUnitMinipet
	targetFlagItemLike = targetFlagItem | targetFlagTradeItem

	castFlagHasTrajectory  uint32 = 0x00000002
	castFlagProjectile     uint32 = 0x00000200
	castFlagPredictedPower uint32 = 0x00000800
	castFlagAdjustMissile  uint32 = 0x00020000
	castFlagVisualChain    uint32 = 0x00080000
	castFlagRuneList       uint32 = 0x00200000
	castFlagImmunity       uint32 = 0x04000000
	castFlagHealPrediction uint32 = 0x40000000
)

type Vec3 struct {
	X, Y, Z float32
}

type SpellTargetLocation struct {
	// Transport is the modern 128-bit GUID of the object the coordinates are
	// relative to.  MO_TRANSPORT hulls (legacy HighGuid 0x1fc0) store their
	// identity entirely in High, so dropping that half made dest-location
	// casts resolve as world coordinates and fail SPELL_FAILED_OUT_OF_RANGE.
	Transport GUID128
	Location  Vec3
}

type SpellCastTargets struct {
	Flags       uint32
	Unit        GUID128
	Item        GUID128
	Src         *SpellTargetLocation
	Dst         *SpellTargetLocation
	Orientation *float32
	MapID       *int32
	Name        string
}

type SpellCastRequest struct {
	CastID              GUID128
	SpellID             uint32
	SpellXSpellVisualID uint32
	SendCastFlags       byte
	MissilePitch        float32
	MissileSpeed        float32
	Target              SpellCastTargets
	Misc                [2]uint32
	Move                *PlayerMovement
}

type UseItemRequest struct {
	PackSlot uint8
	Slot     uint8
	CastItem GUID128
	Cast     SpellCastRequest
}

// ParseCancelMountAura validates build 54261's empty dismount request.
func ParseCancelMountAura(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("cancel-mount-aura has %d bytes, want 0", len(body))
	}
	return nil
}

func ParseUpdateMissileTrajectory(body []byte) (UpdateMissileTrajectory, error) {
	var request UpdateMissileTrajectory
	r := movementReader{data: body}
	var err error
	if request.Guid, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read missile guid: %w", err)
	}
	if request.CastID, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read missile cast GUID: %w", err)
	}
	if request.MoveMsgID, err = r.u16(); err != nil {
		return request, fmt.Errorf("read missile move-msg: %w", err)
	}
	if request.SpellID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read missile spell: %w", err)
	}
	if request.Pitch, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile pitch: %w", err)
	}
	if request.Speed, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile speed: %w", err)
	}
	if request.FirePos.X, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile fire x: %w", err)
	}
	if request.FirePos.Y, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile fire y: %w", err)
	}
	if request.FirePos.Z, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile fire z: %w", err)
	}
	if request.ImpactPos.X, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile impact x: %w", err)
	}
	if request.ImpactPos.Y, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile impact y: %w", err)
	}
	if request.ImpactPos.Z, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile impact z: %w", err)
	}
	return request, nil
}

func EncodeLegacyUpdateMissileTrajectory(guid uint64, request UpdateMissileTrajectory) []byte {
	body := binary.LittleEndian.AppendUint64(nil, guid)
	body = binary.LittleEndian.AppendUint32(body, request.SpellID)
	body = appendFloat32(body, request.Pitch)
	body = appendFloat32(body, request.Speed)
	body = appendFloat32(body, request.FirePos.X)
	body = appendFloat32(body, request.FirePos.Y)
	body = appendFloat32(body, request.FirePos.Z)
	body = appendFloat32(body, request.ImpactPos.X)
	body = appendFloat32(body, request.ImpactPos.Y)
	body = appendFloat32(body, request.ImpactPos.Z)
	return append(body, 0) // moveStop
}

// InjectGatheringGameObjectTarget restores the target omitted by the 3.4.3
// client after GAME_OBJ_REPORT_USE. Mining/herbalism proficiency spells and
// Opening (used by chests and gathering nodes) require a GameObject target in
// the 3.3.5a cast packet.
func InjectGatheringGameObjectTarget(request *SpellCastRequest, target GUID128) bool {
	if request == nil || (target.Low == 0 && target.High == 0) ||
		(request.Target.Unit.Low != 0 || request.Target.Unit.High != 0) {
		return false
	}
	switch request.SpellID {
	case 6478, // Opening
		2575, 2576, 3564, 10248, 29354, 50310, // Mining ranks
		2366, 2368, 3570, 11993, 28695, 50300: // Herbalism ranks
		request.Target.Unit = target
		request.Target.Flags |= targetFlagGameObject
		return true
	default:
		return false
	}
}

type PendingCast struct {
	SpellID uint32
	// ServerSpellID is bound by START when the server selects a lower rank.
	ServerSpellID uint32
	PetGUID       uint64 // zero for player and item requests
	CastItemGUID  uint64
	TargetGUID    uint64
	ClientCastID  GUID128
	ServerCastID  GUID128
	VisualID      uint32
	Started       bool
}

type SpellMiss struct {
	Target  uint64
	Reason  byte
	Reflect byte
}

type SpellCastData struct {
	// PetLoadCooldown marks AzerothCore's header-only, server-originated GO.
	// It must never complete a pending client cast, even of the same spell.
	PetLoadCooldown bool
	CasterGUID      uint64
	CasterUnit      uint64
	SpellID         uint32
	VisualID        uint32
	CastFlags       uint32
	CastTime        uint32
	HitTargets      []uint64
	MissTargets     []SpellMiss
	Target          SpellCastTargets
	AmmoDisplayID   *int32
	AmmoInventory   *int32
	TravelTime      uint32
	Pitch           float32
	ImmunitySchool  uint32
	ImmunityValue   uint32
	RemainingPower  []SpellPowerData
	RemainingRunes  *RuneData
}

const (
	// rotfaceUnstableOozeExplosion is the dest missile Rotface's big ooze
	// fires at explosion stalkers. 3.3.5 SpellDifficulty keeps this ID for
	// all ICC modes; 69839 is the preceding channel and 69833 is the landing
	// damage, neither of which is a falling projectile.
	rotfaceUnstableOozeExplosion = uint32(69832)
	// sindragosaFrostBombTrigger is the dest missile Sindragosa drops during
	// the air phase. AzerothCore zeroes Spell.dbc Speed so the ground stalker
	// summons instantly; 3.3.5 then omits ADJUST_MISSILE and the 3.4 kit
	// glues the orb to the dragon. 69845 is the later explosion, 70022 the
	// ground swirl — neither is a falling projectile.
	sindragosaFrostBombTrigger = uint32(69846)
	// defaultTriggeredMissileSpeed is Spell.dbc Speed for 69832.
	defaultTriggeredMissileSpeed = float32(8)
	// defaultMissileFallHeight drops 69832 from above the impact when 3.3.5
	// omitted a src location. Ground-to-ground src made the kit skate along
	// the floor instead of falling.
	defaultMissileFallHeight = float32(30)
)

// NeedsFallingMissileTrajectory reports dest missiles whose 3.3.5 SpellGo
// omitted ADJUST_MISSILE and must fall onto the impact in build 54261.
// Instant dest AOEs (Putricide Slime Puddle 70346) must not match: filling
// TravelTime would spawn a projectile that vanishes when it "lands".
func NeedsFallingMissileTrajectory(spellID uint32) bool {
	return spellID == rotfaceUnstableOozeExplosion || spellID == sindragosaFrostBombTrigger
}

// ApplyTriggeredMissileTrajectory fills dest/src/travel for a server-triggered
// SpellGo that 3.3.5 sent without ADJUST_MISSILE. Build 54261 treats
// HasTrajectory plus TravelTime 0 as an already-landed projectile.
func ApplyTriggeredMissileTrajectory(cast *SpellCastData, casterPos, impactPos *[3]float32, fallFromAbove bool) {
	if cast == nil || cast.TravelTime != 0 {
		return
	}
	if cast.Target.Dst == nil && impactPos != nil {
		cast.Target.Dst = &SpellTargetLocation{Location: Vec3{X: impactPos[0], Y: impactPos[1], Z: impactPos[2]}}
		cast.Target.Flags |= targetFlagDestLoc
	}
	if cast.Target.Dst == nil {
		return
	}
	dest := cast.Target.Dst.Location
	if cast.Target.Src == nil {
		if fallFromAbove {
			srcZ := dest.Z + defaultMissileFallHeight
			if casterPos != nil && casterPos[2] > srcZ {
				srcZ = casterPos[2]
			}
			cast.Target.Src = &SpellTargetLocation{Location: Vec3{X: dest.X, Y: dest.Y, Z: srcZ}}
		} else if casterPos != nil {
			cast.Target.Src = &SpellTargetLocation{Location: Vec3{X: casterPos[0], Y: casterPos[1], Z: casterPos[2]}}
		}
		if cast.Target.Src != nil {
			cast.Target.Flags |= targetFlagSourceLoc
		}
	}
	if cast.Target.Src == nil {
		return
	}
	src := cast.Target.Src.Location
	dx := dest.X - src.X
	dy := dest.Y - src.Y
	dz := dest.Z - src.Z
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
	if dist < 2 {
		return
	}
	travel := uint32(dist/defaultTriggeredMissileSpeed*1000 + 0.5)
	if travel == 0 {
		travel = 1
	}
	cast.TravelTime = travel
}

type SpellChannelStartInfo struct {
	Caster   uint64
	SpellID  uint32
	Duration uint32
}

type SpellChannelUpdateInfo struct {
	Caster        uint64
	TimeRemaining uint32
}

// UpdateMissileTrajectory is CMSG_UPDATE_MISSILE_TRAJECTORY (54261). The
// client sends it while a projectile is in flight so the server can retarget
// the impact point. Optional trailing MovementInfo is ignored, matching Hermes.
type UpdateMissileTrajectory struct {
	Guid      GUID128
	CastID    GUID128
	MoveMsgID uint16
	SpellID   uint32
	Pitch     float32
	Speed     float32
	FirePos   Vec3
	ImpactPos Vec3
}

type LegacySpellModifier struct {
	ClassIndex uint8
	ModIndex   uint8
	Value      int32
}

type ResurrectRequest struct {
	Caster   uint64
	Name     string
	Sickness bool
	UseTimer bool
}

func ModernCastGUID(mapID uint16, spellID uint32, counter uint64) GUID128 {
	high := uint64(modernHighGuidCast)<<58 | uint64(1)<<42 | uint64(mapID&0x1fff)<<29 | (uint64(spellID)&0x7fffff)<<6 | modernSpellCastSource
	return GUID128{Low: counter & modernGUIDCounterMask, High: high}
}

func ParseCastSpell(body []byte) (SpellCastRequest, error) {
	var request SpellCastRequest
	r := movementReader{data: body}
	var err error
	if request.CastID, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read cast GUID: %w", err)
	}
	if request.Misc[0], err = r.u32(); err != nil {
		return request, fmt.Errorf("read cast misc0: %w", err)
	}
	if request.Misc[1], err = r.u32(); err != nil {
		return request, fmt.Errorf("read cast misc1: %w", err)
	}
	if request.SpellID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read spell id: %w", err)
	}
	if request.SpellXSpellVisualID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read spell visual: %w", err)
	}
	if request.MissilePitch, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile pitch: %w", err)
	}
	if request.MissileSpeed, err = r.f32(); err != nil {
		return request, fmt.Errorf("read missile speed: %w", err)
	}
	if _, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read crafting NPC: %w", err)
	}
	reagentCount, err := r.u32()
	if err != nil {
		return request, fmt.Errorf("read optional-reagent count: %w", err)
	}
	currencyCount, err := r.u32()
	if err != nil {
		return request, fmt.Errorf("read optional-currency count: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return request, fmt.Errorf("read removed-modification count: %w", err)
	}
	if reagentCount > maxSpellOptionalItems || currencyCount > maxSpellOptionalItems {
		return request, fmt.Errorf("optional reagent/currency count %d/%d exceeds %d", reagentCount, currencyCount, maxSpellOptionalItems)
	}
	for index := uint32(0); index < reagentCount; index++ {
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read reagent %d item: %w", index, err)
		}
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read reagent %d slot: %w", index, err)
		}
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read reagent %d count: %w", index, err)
		}
	}
	for index := uint32(0); index < currencyCount; index++ {
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read currency %d id: %w", index, err)
		}
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read currency %d slot: %w", index, err)
		}
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read currency %d count: %w", index, err)
		}
	}
	flags, err := r.bits(5)
	if err != nil {
		return request, fmt.Errorf("read send-cast flags: %w", err)
	}
	request.SendCastFlags = byte(flags)
	hasMove, err := r.bit()
	if err != nil {
		return request, fmt.Errorf("read move-update bit: %w", err)
	}
	weightCount, err := r.bits(2)
	if err != nil {
		return request, fmt.Errorf("read weight count: %w", err)
	}
	if _, err = r.bit(); err != nil {
		return request, fmt.Errorf("read crafting-order bit: %w", err)
	}
	if request.Target, err = parseSpellTargetData(&r); err != nil {
		return request, err
	}
	if hasMove {
		move, moveErr := r.modernMovementStats()
		if moveErr != nil {
			return request, fmt.Errorf("read cast movement: %w", moveErr)
		}
		request.Move = &move
	}
	for index := uint32(0); index < weightCount; index++ {
		r.align()
		if _, err = r.bits(2); err != nil {
			return request, fmt.Errorf("read weight %d type: %w", index, err)
		}
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read weight %d id: %w", index, err)
		}
		if _, err = r.u32(); err != nil {
			return request, fmt.Errorf("read weight %d quantity: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("cast-spell has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseUseItem(body []byte) (UseItemRequest, error) {
	var request UseItemRequest
	r := movementReader{data: body}
	var err error
	if request.PackSlot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read use-item pack slot: %w", err)
	}
	if request.Slot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read use-item slot: %w", err)
	}
	if request.CastItem, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read use-item GUID: %w", err)
	}
	request.Cast, err = ParseCastSpell(body[r.offset:])
	if err != nil {
		return request, fmt.Errorf("read use-item cast: %w", err)
	}
	if request.PackSlot != 0xff {
		request.PackSlot = AdjustInventorySlot(request.PackSlot)
	} else {
		request.Slot = AdjustInventorySlot(request.Slot)
	}
	return request, nil
}

func parseSpellTargetData(r *movementReader) (SpellCastTargets, error) {
	var target SpellCastTargets
	r.align()
	flags, err := r.bits(28)
	if err != nil {
		return target, fmt.Errorf("read target flags: %w", err)
	}
	target.Flags = flags
	hasSrc, err := r.bit()
	if err != nil {
		return target, fmt.Errorf("read src-location bit: %w", err)
	}
	hasDst, err := r.bit()
	if err != nil {
		return target, fmt.Errorf("read dst-location bit: %w", err)
	}
	hasOrientation, err := r.bit()
	if err != nil {
		return target, fmt.Errorf("read orientation bit: %w", err)
	}
	hasMap, err := r.bit()
	if err != nil {
		return target, fmt.Errorf("read map bit: %w", err)
	}
	nameLen, err := r.bits(7)
	if err != nil {
		return target, fmt.Errorf("read target name length: %w", err)
	}
	if target.Unit, err = r.guid128(); err != nil {
		return target, fmt.Errorf("read target unit: %w", err)
	}
	if target.Item, err = r.guid128(); err != nil {
		return target, fmt.Errorf("read target item: %w", err)
	}
	if hasSrc {
		loc, locErr := parseSpellTargetLocation(r)
		if locErr != nil {
			return target, fmt.Errorf("read src location: %w", locErr)
		}
		target.Src = &loc
	}
	if hasDst {
		loc, locErr := parseSpellTargetLocation(r)
		if locErr != nil {
			return target, fmt.Errorf("read dst location: %w", locErr)
		}
		target.Dst = &loc
	}
	if hasOrientation {
		value, orientErr := r.f32()
		if orientErr != nil {
			return target, fmt.Errorf("read orientation: %w", orientErr)
		}
		target.Orientation = &value
	}
	if hasMap {
		value, mapErr := r.i32()
		if mapErr != nil {
			return target, fmt.Errorf("read map id: %w", mapErr)
		}
		target.MapID = &value
	}
	if target.Name, err = r.stringN(int(nameLen)); err != nil {
		return target, fmt.Errorf("read target name: %w", err)
	}
	return target, nil
}

func parseSpellTargetLocation(r *movementReader) (SpellTargetLocation, error) {
	var loc SpellTargetLocation
	var err error
	if loc.Transport, err = r.guid128(); err != nil {
		return loc, err
	}
	if loc.Location.X, err = r.f32(); err != nil {
		return loc, err
	}
	if loc.Location.Y, err = r.f32(); err != nil {
		return loc, err
	}
	if loc.Location.Z, err = r.f32(); err != nil {
		return loc, err
	}
	return loc, nil
}

func EncodeLegacyCastSpell(request SpellCastRequest, unit, item uint64, srcTransport, dstTransport uint64) []byte {
	body := []byte{0}
	body = binary.LittleEndian.AppendUint32(body, request.SpellID)
	body = append(body, request.SendCastFlags)
	body = appendLegacySpellTargets(body, request.Target, unit, item, srcTransport, dstTransport)
	if uint32(request.SendCastFlags)&castFlagHasTrajectory != 0 {
		body = appendFloat32(body, request.MissilePitch)
		body = appendFloat32(body, request.MissileSpeed)
		body = append(body, 0)
	}
	return body
}

func EncodeLegacyUseItem(request UseItemRequest, castItem, unit, item, srcTransport, dstTransport uint64) []byte {
	// AzerothCore reads CMSG_USE_ITEM as bag, slot, cast count, spell ID,
	// unpacked item GUID, glyph index and cast flags before SpellCastTargets.
	// The previous encoder omitted the spell/glyph/flags fields, shifting the
	// GUID into spellId and making WorldSession throw ByteBufferException.
	body := []byte{request.PackSlot, request.Slot, 0} // cast count
	body = binary.LittleEndian.AppendUint32(body, request.Cast.SpellID)
	body = binary.LittleEndian.AppendUint64(body, castItem)
	// Modern SpellCastRequest.Misc[0] is the zero-based GlyphSlot for
	// APPLY_GLYPH (TrinityCore wotlk_classic Spell::m_misc). Preserve it:
	// forcing slot 0 sends every minor glyph to a major socket, and the
	// legacy effect can reject it after the cast item has been committed.
	body = binary.LittleEndian.AppendUint32(body, request.Cast.Misc[0]) // glyph index
	body = append(body, request.Cast.SendCastFlags)
	body = appendLegacySpellTargets(body, request.Cast.Target, unit, item, srcTransport, dstTransport)
	if uint32(request.Cast.SendCastFlags)&castFlagHasTrajectory != 0 {
		body = appendFloat32(body, request.Cast.MissilePitch)
		body = appendFloat32(body, request.Cast.MissileSpeed)
		body = append(body, 0) // movement was already forwarded separately
	}
	return body
}

func appendLegacySpellTargets(dst []byte, target SpellCastTargets, unit, item, srcTransport, dstTransport uint64) []byte {
	flags := target.Flags & 0x001fffff
	dst = binary.LittleEndian.AppendUint32(dst, flags)
	if flags&targetFlagUnitLike != 0 {
		dst = appendLegacyPackedGUID(dst, unit)
	}
	if flags&targetFlagItemLike != 0 {
		dst = appendLegacyPackedGUID(dst, item)
	}
	if flags&targetFlagSourceLoc != 0 {
		dst = appendLegacyPackedGUID(dst, srcTransport)
		if target.Src != nil {
			dst = appendFloat32(dst, target.Src.Location.X)
			dst = appendFloat32(dst, target.Src.Location.Y)
			dst = appendFloat32(dst, target.Src.Location.Z)
		} else {
			dst = appendFloat32(dst, 0)
			dst = appendFloat32(dst, 0)
			dst = appendFloat32(dst, 0)
		}
	}
	if flags&targetFlagDestLoc != 0 {
		dst = appendLegacyPackedGUID(dst, dstTransport)
		if target.Dst != nil {
			dst = appendFloat32(dst, target.Dst.Location.X)
			dst = appendFloat32(dst, target.Dst.Location.Y)
			dst = appendFloat32(dst, target.Dst.Location.Z)
		} else {
			dst = appendFloat32(dst, 0)
			dst = appendFloat32(dst, 0)
			dst = appendFloat32(dst, 0)
		}
	}
	if flags&targetFlagString != 0 {
		dst = append(dst, []byte(target.Name)...)
		dst = append(dst, 0)
	}
	return dst
}

func ParseLegacySpellStartOrGo(body []byte, isSpellGo bool) (SpellCastData, error) {
	var data SpellCastData
	r := movementReader{data: body}
	var err error
	if data.CasterGUID, err = r.guid64(); err != nil {
		return data, fmt.Errorf("read caster GUID: %w", err)
	}
	if data.CasterUnit, err = r.guid64(); err != nil {
		return data, fmt.Errorf("read caster unit: %w", err)
	}
	castCount, err := r.u8()
	if err != nil {
		return data, fmt.Errorf("read cast count: %w", err)
	}
	if data.SpellID, err = r.u32(); err != nil {
		return data, fmt.Errorf("read spell id: %w", err)
	}
	if data.CastFlags, err = r.u32(); err != nil {
		return data, fmt.Errorf("read cast flags: %w", err)
	}
	if data.CastTime, err = r.u32(); err != nil {
		return data, fmt.Errorf("read cast time: %w", err)
	}
	if isSpellGo {
		// Pet::LoadPetFromDB sends only this header to apply summon cooldown.
		// Packed GUID widths vary: recognize the exact structure, not 17 bytes.
		if r.remaining() == 0 && castCount == 0 && data.SpellID != 0 &&
			data.CastFlags == 0x100 && data.CastTime == 0 &&
			data.CasterGUID != 0 && data.CasterGUID>>48 == 0 && data.CasterGUID == data.CasterUnit {
			data.PetLoadCooldown = true
			return data, nil
		}
		hitCount, hitErr := r.u8()
		if hitErr != nil {
			return data, fmt.Errorf("read hit count: %w", hitErr)
		}
		if int(hitCount) > maxSpellTargets {
			return data, fmt.Errorf("hit count %d exceeds %d", hitCount, maxSpellTargets)
		}
		data.HitTargets = make([]uint64, 0, hitCount)
		for index := 0; index < int(hitCount); index++ {
			guid, guidErr := r.u64()
			if guidErr != nil {
				return data, fmt.Errorf("read hit target %d: %w", index, guidErr)
			}
			data.HitTargets = append(data.HitTargets, guid)
		}
		missCount, missErr := r.u8()
		if missErr != nil {
			return data, fmt.Errorf("read miss count: %w", missErr)
		}
		if int(missCount) > maxSpellTargets {
			return data, fmt.Errorf("miss count %d exceeds %d", missCount, maxSpellTargets)
		}
		data.MissTargets = make([]SpellMiss, 0, missCount)
		for index := 0; index < int(missCount); index++ {
			guid, guidErr := r.u64()
			if guidErr != nil {
				return data, fmt.Errorf("read miss target %d: %w", index, guidErr)
			}
			reason, reasonErr := r.u8()
			if reasonErr != nil {
				return data, fmt.Errorf("read miss reason %d: %w", index, reasonErr)
			}
			miss := SpellMiss{Target: guid, Reason: reason}
			if reason == 7 {
				if miss.Reflect, err = r.u8(); err != nil {
					return data, fmt.Errorf("read miss reflect %d: %w", index, err)
				}
			}
			data.MissTargets = append(data.MissTargets, miss)
		}
	}
	if data.Target, err = parseLegacySpellTargets(&r); err != nil {
		return data, err
	}
	if data.CastFlags&castFlagPredictedPower != 0 {
		if _, err = r.i32(); err != nil {
			return data, fmt.Errorf("read predicted power: %w", err)
		}
	}
	if data.CastFlags&castFlagRuneList != 0 {
		recharging, rechargeErr := r.u8()
		if rechargeErr != nil {
			return data, fmt.Errorf("read rune recharging mask: %w", rechargeErr)
		}
		usable, usableErr := r.u8()
		if usableErr != nil {
			return data, fmt.Errorf("read rune usable mask: %w", usableErr)
		}
		runes := &RuneData{Start: recharging, Count: usable, Cooldowns: make([]byte, runeCount)}
		for index := 0; index < runeCount; index++ {
			bit := byte(1 << index)
			if usable&bit != 0 {
				runes.Cooldowns[index] = 0xff
				continue
			}
			if recharging&bit == 0 {
				continue
			}
			if runes.Cooldowns[index], err = r.u8(); err != nil {
				return data, fmt.Errorf("read rune cooldown %d: %w", index, err)
			}
			if runes.Cooldowns[index] == 0 {
				runes.Cooldowns[index] = 1
			}
		}
		data.RemainingPower = RuneRemainingPower(usable)
		if isSpellGo && recharging != 0 {
			data.RemainingRunes = runes
		}
	}
	if isSpellGo && data.CastFlags&castFlagAdjustMissile != 0 {
		if data.Pitch, err = r.f32(); err != nil {
			return data, fmt.Errorf("read missile pitch: %w", err)
		}
		if data.TravelTime, err = r.u32(); err != nil {
			return data, fmt.Errorf("read missile travel: %w", err)
		}
	}
	if data.CastFlags&castFlagProjectile != 0 {
		display, displayErr := r.i32()
		if displayErr != nil {
			return data, fmt.Errorf("read ammo display: %w", displayErr)
		}
		inventory, inventoryErr := r.i32()
		if inventoryErr != nil {
			return data, fmt.Errorf("read ammo inventory: %w", inventoryErr)
		}
		data.AmmoDisplayID = &display
		data.AmmoInventory = &inventory
	}
	if isSpellGo {
		if data.CastFlags&castFlagVisualChain != 0 {
			if _, err = r.i32(); err != nil {
				return data, fmt.Errorf("read visual chain 1: %w", err)
			}
			if _, err = r.i32(); err != nil {
				return data, fmt.Errorf("read visual chain 2: %w", err)
			}
		}
		if data.Target.Flags&targetFlagDestLoc != 0 {
			if _, err = r.i8(); err != nil {
				return data, fmt.Errorf("read dest-loc index: %w", err)
			}
		}
		if data.Target.Flags&targetFlagExtraTargets != 0 {
			count, countErr := r.i32()
			if countErr != nil {
				return data, fmt.Errorf("read extra-target count: %w", countErr)
			}
			if count < 0 || int(count) > maxSpellTargets {
				return data, fmt.Errorf("extra-target count %d is out of range", count)
			}
			for index := 0; index < int(count); index++ {
				if _, err = r.f32(); err != nil {
					return data, fmt.Errorf("read extra target %d x: %w", index, err)
				}
				if _, err = r.f32(); err != nil {
					return data, fmt.Errorf("read extra target %d y: %w", index, err)
				}
				if _, err = r.f32(); err != nil {
					return data, fmt.Errorf("read extra target %d z: %w", index, err)
				}
				if _, err = r.u64(); err != nil {
					return data, fmt.Errorf("read extra target %d guid: %w", index, err)
				}
			}
		}
	} else {
		if data.CastFlags&castFlagImmunity != 0 {
			if data.ImmunitySchool, err = r.u32(); err != nil {
				return data, fmt.Errorf("read immunity school: %w", err)
			}
			if data.ImmunityValue, err = r.u32(); err != nil {
				return data, fmt.Errorf("read immunity value: %w", err)
			}
		}
		if data.CastFlags&castFlagHealPrediction != 0 {
			if _, err = r.i32(); err != nil {
				return data, fmt.Errorf("read heal-prediction spell: %w", err)
			}
			kind, kindErr := r.u8()
			if kindErr != nil {
				return data, fmt.Errorf("read heal-prediction type: %w", kindErr)
			}
			if kind == 2 {
				if _, err = r.guid64(); err != nil {
					return data, fmt.Errorf("read heal-prediction beacon: %w", err)
				}
			}
		}
	}
	return data, nil
}

func parseLegacySpellTargets(r *movementReader) (SpellCastTargets, error) {
	var target SpellCastTargets
	flags, err := r.u32()
	if err != nil {
		return target, fmt.Errorf("read target flags: %w", err)
	}
	target.Flags = flags
	if flags&targetFlagUnitLike != 0 {
		guid, guidErr := r.guid64()
		if guidErr != nil {
			return target, fmt.Errorf("read target unit: %w", guidErr)
		}
		target.Unit = GUID128{Low: guid}
	}
	if flags&targetFlagItemLike != 0 {
		guid, guidErr := r.guid64()
		if guidErr != nil {
			return target, fmt.Errorf("read target item: %w", guidErr)
		}
		target.Item = GUID128{Low: guid}
	}
	if flags&targetFlagSourceLoc != 0 {
		loc, locErr := parseLegacySpellLocation(r)
		if locErr != nil {
			return target, fmt.Errorf("read src location: %w", locErr)
		}
		target.Src = &loc
	}
	if flags&targetFlagDestLoc != 0 {
		loc, locErr := parseLegacySpellLocation(r)
		if locErr != nil {
			return target, fmt.Errorf("read dst location: %w", locErr)
		}
		target.Dst = &loc
	}
	if flags&targetFlagString != 0 {
		if target.Name, err = r.cstring(); err != nil {
			return target, fmt.Errorf("read target name: %w", err)
		}
	}
	return target, nil
}

func parseLegacySpellLocation(r *movementReader) (SpellTargetLocation, error) {
	var loc SpellTargetLocation
	var err error
	legacyTransport, err := r.guid64()
	if err != nil {
		return loc, err
	}
	loc.Transport = GUID128{Low: legacyTransport}
	if loc.Location.X, err = r.f32(); err != nil {
		return loc, err
	}
	if loc.Location.Y, err = r.f32(); err != nil {
		return loc, err
	}
	if loc.Location.Z, err = r.f32(); err != nil {
		return loc, err
	}
	return loc, nil
}

func EncodeSpellPrepare(client, server GUID128) []byte {
	body := appendPackedGUID128(nil, client.Low, client.High)
	return appendPackedGUID128(body, server.Low, server.High)
}

func ParseLegacySpellChannelStart(body []byte) (SpellChannelStartInfo, error) {
	var info SpellChannelStartInfo
	r := movementReader{data: body}
	var err error
	if info.Caster, err = r.guid64(); err != nil {
		return info, fmt.Errorf("read channel-start caster: %w", err)
	}
	if info.SpellID, err = r.u32(); err != nil {
		return info, fmt.Errorf("read channel-start spell: %w", err)
	}
	if info.Duration, err = r.u32(); err != nil {
		return info, fmt.Errorf("read channel-start duration: %w", err)
	}
	if r.remaining() != 0 {
		return info, fmt.Errorf("channel-start has %d trailing bytes", r.remaining())
	}
	return info, nil
}

func EncodeSpellChannelStart(caster GUID128, spellID, visualID, duration uint32) []byte {
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, spellID)
	body = binary.LittleEndian.AppendUint32(body, visualID)
	body = binary.LittleEndian.AppendUint32(body, duration)
	bits := newBitWriter(body)
	bits.writeBit(false) // InterruptImmunities
	bits.writeBit(false) // HealPrediction
	return bits.flush()
}

func ParseLegacySpellChannelUpdate(body []byte) (SpellChannelUpdateInfo, error) {
	var info SpellChannelUpdateInfo
	r := movementReader{data: body}
	var err error
	if info.Caster, err = r.guid64(); err != nil {
		return info, fmt.Errorf("read channel-update caster: %w", err)
	}
	if info.TimeRemaining, err = r.u32(); err != nil {
		return info, fmt.Errorf("read channel-update remaining time: %w", err)
	}
	if r.remaining() != 0 {
		return info, fmt.Errorf("channel-update has %d trailing bytes", r.remaining())
	}
	return info, nil
}

func EncodeSpellChannelUpdate(caster GUID128, timeRemaining uint32) []byte {
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	return binary.LittleEndian.AppendUint32(body, timeRemaining)
}

func ParseLegacySpellModifier(body []byte) (LegacySpellModifier, error) {
	var modifier LegacySpellModifier
	r := movementReader{data: body}
	var err error
	if modifier.ClassIndex, err = r.u8(); err != nil {
		return modifier, fmt.Errorf("read spell-modifier class index: %w", err)
	}
	if modifier.ModIndex, err = r.u8(); err != nil {
		return modifier, fmt.Errorf("read spell-modifier mod index: %w", err)
	}
	if modifier.Value, err = r.i32(); err != nil {
		return modifier, fmt.Errorf("read spell-modifier value: %w", err)
	}
	if r.remaining() != 0 {
		return modifier, fmt.Errorf("spell-modifier has %d trailing bytes", r.remaining())
	}
	return modifier, nil
}

func EncodeSpellModifier(modifier LegacySpellModifier) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 1) // Modifiers
	body = append(body, modifier.ModIndex)
	body = binary.LittleEndian.AppendUint32(body, 1) // ModifierData
	body = binary.LittleEndian.AppendUint32(body, uint32(modifier.Value))
	return append(body, modifier.ClassIndex)
}

func EncodeSpellStart(cast SpellCastData, caster, unit, castID GUID128, hits, misses []GUID128) []byte {
	return encodeSpellCastData(cast, caster, unit, castID, hits, misses, false)
}

func EncodeSpellGo(cast SpellCastData, caster, unit, castID GUID128, hits, misses []GUID128) []byte {
	body := encodeSpellCastData(cast, caster, unit, castID, hits, misses, true)
	bits := newBitWriter(body)
	bits.writeBit(false)
	return bits.flush()
}

func encodeSpellCastData(cast SpellCastData, caster, unit, castID GUID128, hits, misses []GUID128, isSpellGo bool) []byte {
	flags := cast.CastFlags
	if isSpellGo {
		// 3.3.5a SpellGo does not set CAST_FLAG_HAS_TRAJECTORY. Build 54261
		// uses the bit plus TravelTime/Pitch to spawn a projectile. Hermes ORs
		// it onto unit-target ticks (Arcane Missiles 7268); doing that for a
		// dest missile with TravelTime 0 makes the kit land instantly, which
		// hid Rotface's falling ooze. Keep the bit for unit-only GOs and for
		// packets that already carry a trajectory.
		if cast.TravelTime != 0 || cast.Pitch != 0 || (cast.Target.Dst == nil && cast.Target.Src == nil) {
			flags |= castFlagHasTrajectory
		}
	}
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	body = appendPackedGUID128(body, unit.Low, unit.High)
	body = appendPackedGUID128(body, castID.Low, castID.High)
	body = appendPackedGUID128(body, 0, 0) // OriginalCastID
	body = binary.LittleEndian.AppendUint32(body, cast.SpellID)
	body = binary.LittleEndian.AppendUint32(body, cast.VisualID)
	body = binary.LittleEndian.AppendUint32(body, flags)
	body = binary.LittleEndian.AppendUint32(body, 0) // CastFlagsEx
	body = binary.LittleEndian.AppendUint32(body, cast.CastTime)
	body = binary.LittleEndian.AppendUint32(body, cast.TravelTime)
	body = appendFloat32(body, cast.Pitch)
	body = append(body, 0) // DestLocSpellCastIndex
	body = binary.LittleEndian.AppendUint32(body, cast.ImmunitySchool)
	body = binary.LittleEndian.AppendUint32(body, cast.ImmunityValue)
	body = binary.LittleEndian.AppendUint32(body, 0) // Predict.Points
	body = append(body, 0)                           // Predict.Type
	body = appendPackedGUID128(body, 0, 0)           // Predict.BeaconGUID

	ammoDisplay := cast.AmmoDisplayID != nil
	ammoInventory := cast.AmmoInventory != nil
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(hits)), 16)
	bits.writeBits(uint32(len(misses)), 16)
	bits.writeBits(uint32(len(cast.MissTargets)), 16)
	bits.writeBits(uint32(len(cast.RemainingPower)), 9)
	bits.writeBit(cast.RemainingRunes != nil)
	bits.writeBits(0, 16)
	bits.writeBit(ammoDisplay)
	bits.writeBit(ammoInventory)
	body = bits.flush()

	body = appendSpellTargetData(body, cast.Target, hits, misses)
	for _, hit := range hits {
		body = appendPackedGUID128(body, hit.Low, hit.High)
	}
	for _, miss := range misses {
		body = appendPackedGUID128(body, miss.Low, miss.High)
	}
	for _, miss := range cast.MissTargets {
		body = append(body, miss.Reason)
		if miss.Reason == 7 {
			body = append(body, miss.Reflect)
		}
	}
	for _, power := range cast.RemainingPower {
		body = append(body, EncodeSpellPowerData(power)...)
	}
	if cast.RemainingRunes != nil {
		body = append(body, EncodeRuneData(*cast.RemainingRunes)...)
	}
	if ammoDisplay {
		body = binary.LittleEndian.AppendUint32(body, uint32(*cast.AmmoDisplayID))
	}
	if ammoInventory {
		body = binary.LittleEndian.AppendUint32(body, uint32(*cast.AmmoInventory))
	}
	return body
}

func appendSpellTargetData(dst []byte, target SpellCastTargets, _, _ []GUID128) []byte {
	name := target.Name
	if len(name) > 127 {
		name = name[:127]
	}
	bits := newBitWriter(dst)
	bits.writeBits(target.Flags, 28)
	bits.writeBit(target.Src != nil)
	bits.writeBit(target.Dst != nil)
	bits.writeBit(target.Orientation != nil)
	bits.writeBit(target.MapID != nil)
	bits.writeBits(uint32(len(name)), 7)
	dst = bits.flush()
	dst = appendPackedGUID128(dst, target.Unit.Low, target.Unit.High)
	dst = appendPackedGUID128(dst, target.Item.Low, target.Item.High)
	if target.Src != nil {
		dst = appendPackedGUID128(dst, target.Src.Transport.Low, target.Src.Transport.High)
		dst = appendFloat32(dst, target.Src.Location.X)
		dst = appendFloat32(dst, target.Src.Location.Y)
		dst = appendFloat32(dst, target.Src.Location.Z)
	}
	if target.Dst != nil {
		dst = appendPackedGUID128(dst, target.Dst.Transport.Low, target.Dst.Transport.High)
		dst = appendFloat32(dst, target.Dst.Location.X)
		dst = appendFloat32(dst, target.Dst.Location.Y)
		dst = appendFloat32(dst, target.Dst.Location.Z)
	}
	if target.Orientation != nil {
		dst = appendFloat32(dst, *target.Orientation)
	}
	if target.MapID != nil {
		dst = binary.LittleEndian.AppendUint32(dst, uint32(*target.MapID))
	}
	return append(dst, name...)
}

func ParseResurrectResponse(body []byte) (GUID128, uint32, error) {
	r := movementReader{data: body}
	guid, err := r.guid128()
	if err != nil {
		return GUID128{}, 0, fmt.Errorf("read resurrect caster: %w", err)
	}
	response, err := r.u32()
	if err != nil {
		return GUID128{}, 0, fmt.Errorf("read resurrect response: %w", err)
	}
	if r.remaining() != 0 {
		return GUID128{}, 0, fmt.Errorf("resurrect-response has %d trailing bytes", r.remaining())
	}
	return guid, response, nil
}

func EncodeLegacyResurrectResponse(caster uint64, accepted bool) []byte {
	body := EncodeLegacyUnpackedGUID(caster)
	status := byte(1)
	if accepted {
		status = 0
	}
	return append(body, status)
}

func ParseLegacyResurrectRequest(body []byte) (ResurrectRequest, error) {
	var request ResurrectRequest
	r := movementReader{data: body}
	var err error
	if request.Caster, err = r.u64(); err != nil {
		return request, fmt.Errorf("read resurrect caster: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return request, fmt.Errorf("read resurrect name length: %w", err)
	}
	if request.Name, err = r.cstring(); err != nil {
		return request, fmt.Errorf("read resurrect name: %w", err)
	}
	sickness, err := r.u8()
	if err != nil {
		return request, fmt.Errorf("read resurrect sickness: %w", err)
	}
	useTimer, err := r.u8()
	if err != nil {
		return request, fmt.Errorf("read resurrect timer: %w", err)
	}
	request.Sickness = sickness != 0
	request.UseTimer = useTimer != 0
	if r.remaining() != 0 {
		return request, fmt.Errorf("resurrect-request has %d trailing bytes", r.remaining())
	}
	if !utf8.ValidString(request.Name) {
		return request, fmt.Errorf("resurrect name is not valid UTF-8")
	}
	return request, nil
}

func EncodeResurrectRequest(caster GUID128, realmAddress uint32, request ResurrectRequest) []byte {
	name := request.Name
	if utf8.RuneCountInString(name) > 2047 {
		name = string([]rune(name)[:2047])
	}
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = binary.LittleEndian.AppendUint32(body, 0) // PetNumber
	body = binary.LittleEndian.AppendUint32(body, 0) // SpellID
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(name)), 11)
	bits.writeBit(request.UseTimer)
	bits.writeBit(request.Sickness)
	body = bits.flush()
	return append(body, name...)
}

func CountNonZeroActionButtons(buttons []int32) int {
	count := 0
	for _, button := range buttons {
		if button != 0 {
			count++
		}
	}
	return count
}

// ActionButtonSpellIDs returns WotLK packed spell actions (type 0) from the bar.
func ActionButtonSpellIDs(buttons []int32) []uint32 {
	ids := make([]uint32, 0, 12)
	for _, button := range buttons {
		packed := uint32(button)
		if packed == 0 || byte(packed>>24) != 0 {
			continue
		}
		ids = append(ids, packed&0x00ffffff)
	}
	return ids
}

func MissingKnownSpells(spells []uint32, need []uint32) []uint32 {
	have := make(map[uint32]struct{}, len(spells))
	for _, spellID := range spells {
		have[spellID] = struct{}{}
	}
	var missing []uint32
	seen := make(map[uint32]struct{}, len(need))
	for _, spellID := range need {
		if _, dup := seen[spellID]; dup {
			continue
		}
		seen[spellID] = struct{}{}
		if _, ok := have[spellID]; !ok {
			missing = append(missing, spellID)
		}
	}
	return missing
}

type CancelCastRequest struct {
	CastID  GUID128
	SpellID uint32
}

func ParseCancelCast(body []byte) (CancelCastRequest, error) {
	var request CancelCastRequest
	r := movementReader{data: body}
	var err error
	if request.CastID, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read cancel-cast GUID: %w", err)
	}
	if request.SpellID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read cancel-cast spell: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("cancel-cast has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyCancelCast(spellID uint32) []byte {
	return binary.LittleEndian.AppendUint32([]byte{0}, spellID)
}

func ParseCancelAura(body []byte) (uint32, GUID128, error) {
	r := movementReader{data: body}
	spellID, err := r.u32()
	if err != nil {
		return 0, GUID128{}, fmt.Errorf("read cancel-aura spell: %w", err)
	}
	caster, err := r.guid128()
	if err != nil {
		return 0, GUID128{}, fmt.Errorf("read cancel-aura caster: %w", err)
	}
	if r.remaining() != 0 {
		return 0, GUID128{}, fmt.Errorf("cancel-aura has %d trailing bytes", r.remaining())
	}
	return spellID, caster, nil
}

func EncodeLegacyCancelAura(spellID uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, spellID)
}

type SpellFailureInfo struct {
	Caster  uint64
	SpellID uint32
	Reason  uint16
	Arg1    int32
	Arg2    int32
}

func ParseLegacySpellFailure(body []byte, other bool) (SpellFailureInfo, error) {
	var info SpellFailureInfo
	r := movementReader{data: body}
	var err error
	if info.Caster, err = r.guid64(); err != nil {
		return info, fmt.Errorf("read spell-failure caster: %w", err)
	}
	switch r.remaining() {
	case 6:
		if _, err = r.u8(); err != nil {
			return info, fmt.Errorf("read spell-failure count: %w", err)
		}
		fallthrough
	case 5:
		if info.SpellID, err = r.u32(); err != nil {
			return info, fmt.Errorf("read spell-failure spell: %w", err)
		}
		reason, readErr := r.u8()
		if readErr != nil {
			return info, fmt.Errorf("read spell-failure reason: %w", readErr)
		}
		info.Reason = uint16(reason)
	default:
		return info, fmt.Errorf("spell-failure has %d trailing bytes", r.remaining())
	}
	_ = other
	return info, nil
}

func EncodeSpellFailure(caster, castID GUID128, info SpellFailureInfo, visualID uint32) []byte {
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	body = appendPackedGUID128(body, castID.Low, castID.High)
	body = binary.LittleEndian.AppendUint32(body, info.SpellID)
	body = binary.LittleEndian.AppendUint32(body, visualID)
	return binary.LittleEndian.AppendUint16(body, ConvertSpellCastResult343(info.Reason))
}

func EncodeSpellFailedOther(caster, castID GUID128, info SpellFailureInfo, visualID uint32) []byte {
	body := appendPackedGUID128(nil, caster.Low, caster.High)
	body = appendPackedGUID128(body, castID.Low, castID.High)
	body = binary.LittleEndian.AppendUint32(body, info.SpellID)
	body = binary.LittleEndian.AppendUint32(body, visualID)
	// Hermes SpellFailedOther.Write uses uint8 Reason. SpellFailure keeps uint16.
	// Writing uint16 here left the 3.4.3 caster pose raised after an interrupt.
	return append(body, byte(ConvertSpellCastResult343(info.Reason)))
}

func ParseLegacyCastFailed(body []byte) (SpellFailureInfo, error) {
	var info SpellFailureInfo
	r := movementReader{data: body}
	if _, err := r.u8(); err != nil {
		return info, fmt.Errorf("read cast-failed count: %w", err)
	}
	var err error
	if info.SpellID, err = r.u32(); err != nil {
		return info, fmt.Errorf("read cast-failed spell: %w", err)
	}
	reason, err := r.u8()
	if err != nil {
		return info, fmt.Errorf("read cast-failed reason: %w", err)
	}
	info.Reason = uint16(reason)
	info.Arg1, info.Arg2 = -1, -1
	switch r.remaining() {
	case 0:
	case 4:
		arg, readErr := r.i32()
		if readErr != nil {
			return info, fmt.Errorf("read cast-failed arg1: %w", readErr)
		}
		info.Arg1 = arg
	case 8:
		if info.Arg1, err = r.i32(); err != nil {
			return info, fmt.Errorf("read cast-failed arg1: %w", err)
		}
		if info.Arg2, err = r.i32(); err != nil {
			return info, fmt.Errorf("read cast-failed arg2: %w", err)
		}
	default:
		return info, fmt.Errorf("cast-failed has %d trailing bytes", r.remaining())
	}
	return info, nil
}

func EncodeCastFailed(castID GUID128, info SpellFailureInfo, visualID uint32) []byte {
	body := appendPackedGUID128(nil, castID.Low, castID.High)
	body = binary.LittleEndian.AppendUint32(body, info.SpellID)
	body = binary.LittleEndian.AppendUint32(body, visualID)
	body = binary.LittleEndian.AppendUint32(body, uint32(ConvertSpellCastResult343(info.Reason)))
	body = binary.LittleEndian.AppendUint32(body, uint32(info.Arg1))
	return binary.LittleEndian.AppendUint32(body, uint32(info.Arg2))
}

// ConvertSpellCastResult343 is legacy proxy's SpellCastResultWLK.convert table
// for expansion 80. 3.4.3 inserted extra enum values, so a raw 3.3.5a reason
// shows the wrong client message (stealth cooldown became "cannot enchant").
func ConvertSpellCastResult343(legacy uint16) uint16 {
	if int(legacy) >= len(spellCastResultWLKTo343) {
		return 0
	}
	return spellCastResultWLKTo343[legacy]
}

// spellCastResultWLKTo343 maps WotLK reason 0..187. Unmapped entries stay 0.
var spellCastResultWLKTo343 = [...]uint16{
	1, 1, 2, 3, 4, 5, 6, 7, 9, 10, 11, 12, 13, 15, 0, 0,
	19, 20, 21, 22, 23, 24, 26, 27, 28, 29, 30, 32, 33, 34, 35, 36,
	37, 39, 40, 0, 0, 60, 61, 62, 63, 64, 65, 66, 67, 68, 70, 71,
	0, 0, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 0, 86, 87,
	88, 89, 90, 91, 92, 93, 94, 95, 96, 97, 98, 99, 100, 0, 101, 102,
	103, 104, 105, 106, 107, 108, 109, 110, 0, 0, 112, 113, 0, 114, 115, 116,
	117, 118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132,
	133, 134, 135, 136, 137, 139, 140, 141, 142, 143, 144, 145, 146, 147, 148, 149,
	150, 151, 152, 153, 155, 156, 157, 159, 160, 161, 162, 163, 0, 165, 166, 167,
	169, 170, 171, 172, 173, 174, 175, 243, 177, 178, 179, 180, 181, 182, 183, 0,
	191, 192, 204, 205, 206, 207, 208, 209, 210, 211, 212, 213, 214, 215, 216, 217,
	218, 219, 224, 225, 226, 227, 228, 229, 230, 231, 232, 321,
}
