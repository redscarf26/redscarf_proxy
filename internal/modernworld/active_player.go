package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const (
	CMSGSetActionBarToggles = uint16(13618)
	CMSGSetActionButton     = uint16(13661)
	SMSGUpdateActionButtons = uint16(9696)

	legacyActionButtonCount = 144
	modernActionButtonCount = 180
)

// ActivePlayerCreateOptions contains the small amount of session state which is
// not present in AzerothCore's legacy CreateObject block.
type ActivePlayerCreateOptions struct {
	MapID                uint16
	VirtualRealm         uint32
	GameAccountID        uint64
	ActionButtons        []int32
	Runes                *LegacyRuneState
	CompletedQuestBlocks []uint64
	StateOnlyQuests      map[uint32]struct{}
	Now                  time.Time
	OwnerGUID            uint64
}

// ParseLegacyActionButtons reads AzerothCore's WotLK 3.3.5a initial action-bar
// sync: one reason byte followed by 144 packed uint32 buttons.
func ParseLegacyActionButtons(body []byte) ([]int32, byte, error) {
	const expectedSize = 1 + legacyActionButtonCount*4
	if len(body) != expectedSize {
		return nil, 0, fmt.Errorf("legacy action-buttons body has %d bytes, want %d", len(body), expectedSize)
	}
	reason := body[0]
	buttons := make([]int32, legacyActionButtonCount)
	for index := range buttons {
		buttons[index] = int32(binary.LittleEndian.Uint32(body[1+index*4:]))
	}
	return buttons, reason, nil
}

// EncodeUpdateActionButtons writes 180 int64 slots plus a reason byte.
// legacy proxy's 3.4.3 capture zero-extends each WotLK packed uint32, leaving the
// type in bits 24..31, and keeps the legacy reason. Login sends reason 1
// before the ActivePlayer create.
func EncodeUpdateActionButtons(buttons []int32, reason byte) []byte {
	body := make([]byte, 0, modernActionButtonCount*8+1)
	for index := 0; index < modernActionButtonCount; index++ {
		var legacy uint32
		if index < len(buttons) {
			legacy = uint32(buttons[index])
		}
		body = binary.LittleEndian.AppendUint64(body, uint64(legacy))
	}
	return append(body, reason)
}

// EncodeCreateActionButtons writes the 180 Int32 values legacy proxy's
// ObjectUpdateBuilder343.buildMovementUpdate emits when HasActionButtons is set.
func EncodeCreateActionButtons(buttons []int32) []byte {
	body := make([]byte, 0, modernActionButtonCount*4)
	for index := 0; index < modernActionButtonCount; index++ {
		var packed uint32
		if index < len(buttons) {
			packed = uint32(buttons[index])
		}
		body = binary.LittleEndian.AppendUint32(body, packed)
	}
	return body
}

// ActionButtonSlotPreview is a short hex dump for logs. n is capped at len(buttons).
func ActionButtonSlotPreview(buttons []int32, n int) string {
	if n > len(buttons) {
		n = len(buttons)
	}
	if n <= 0 {
		return ""
	}
	buf := make([]byte, 0, n*13)
	for index := 0; index < n; index++ {
		if index > 0 {
			buf = append(buf, ' ')
		}
		buf = fmt.Appendf(buf, "%d:0x%08x", index, uint32(buttons[index]))
	}
	return string(buf)
}

func ParseSetActionBarToggles(body []byte) (byte, error) {
	if len(body) != 1 {
		return 0, fmt.Errorf("set-action-bar-toggles has %d bytes, want 1", len(body))
	}
	return body[0], nil
}

type ActionButtonChange struct {
	Index  byte
	Packed uint32
}

func ParseSetActionButton(body []byte) (ActionButtonChange, error) {
	var change ActionButtonChange
	switch len(body) {
	case 5:
		// Build 54261 writes one packed uint32, but Hermes' packet reader
		// exposes it as two uint16 values. The low byte of the second word
		// is action bits 16..23; its high byte is the action type.
		actionLow := binary.LittleEndian.Uint16(body[:2])
		actionHighAndType := binary.LittleEndian.Uint16(body[2:4])
		change.Index = body[4]
		change.Packed = uint32(actionLow) |
			uint32(actionHighAndType&0x00ff)<<16 |
			uint32(actionHighAndType>>8)<<24
	case 9:
		packed := binary.LittleEndian.Uint64(body[:8])
		change.Index = body[8]
		buttonType := byte(packed >> 56)
		if buttonType == 0 {
			// Retain compatibility with the earlier observed variant that put
			// the WotLK-style type byte in bits 24..31 of the uint64.
			buttonType = byte(packed >> 24)
		}
		change.Packed = uint32(packed&0x00ffffff) | uint32(buttonType)<<24
	default:
		return change, fmt.Errorf("set-action-button has %d bytes, want 5 or 9", len(body))
	}
	if int(change.Index) >= modernActionButtonCount {
		return change, fmt.Errorf("action-button index %d exceeds %d", change.Index, modernActionButtonCount)
	}
	return change, nil
}

func EncodeLegacySetActionButton(change ActionButtonChange) []byte {
	return append([]byte{change.Index}, binary.LittleEndian.AppendUint32(nil, change.Packed)...)
}

const (
	legacyObjectEntry = 3
	legacyObjectScale = 4

	legacyUnitCharm              = 6
	legacyUnitSummon             = 8
	legacyUnitCritter            = 10
	legacyUnitCharmedBy          = 12
	legacyUnitSummonedBy         = 14
	legacyUnitCreatedBy          = 16
	legacyUnitTarget             = 18
	legacyUnitChannelObject      = 20
	legacyUnitChannelSpell       = 22
	legacyUnitBytes0             = 23
	legacyUnitHealth             = 24
	legacyUnitPower1             = 25
	legacyUnitMaxHealth          = 32
	legacyUnitMaxPower1          = 33
	legacyUnitLevel              = 54
	legacyUnitFaction            = 55
	legacyUnitVirtualItem1       = 56
	legacyUnitFlags              = 59
	legacyUnitFlags2             = 60
	legacyUnitAuraState          = 61
	legacyUnitBaseAttackTime     = 62
	legacyUnitRangedAttackTime   = 64
	legacyUnitBoundingRadius     = 65
	legacyUnitCombatReach        = 66
	legacyUnitDisplayID          = 67
	legacyUnitNativeDisplayID    = 68
	legacyUnitMountDisplayID     = 69
	legacyUnitMinDamage          = 70
	legacyUnitBytes1             = 74
	legacyUnitPetNumber          = 75
	legacyUnitDynamicFlags       = 79
	legacyUnitModCastSpeed       = 80
	legacyUnitCreatedBySpell     = 81
	legacyUnitNPCFlags           = 82
	legacyUnitEmoteState         = 83
	legacyUnitStat0              = 84
	legacyUnitPositiveStat0      = 89
	legacyUnitNegativeStat0      = 94
	legacyUnitResistance0        = 99
	legacyUnitPositiveResist0    = 106
	legacyUnitNegativeResist0    = 113
	legacyUnitBaseMana           = 120
	legacyUnitBaseHealth         = 121
	legacyUnitBytes2             = 122
	legacyUnitAttackPower        = 123
	legacyUnitAttackPowerMods    = 124
	legacyUnitAttackPowerMult    = 125
	legacyUnitRangedAttackPower  = 126
	legacyUnitRangedAPMods       = 127
	legacyUnitRangedAPMult       = 128
	legacyUnitMinRangedDamage    = 129
	legacyUnitPowerCostModifier0 = 131
	legacyUnitPowerCostMult0     = 138
	legacyUnitMaxHealthModifier  = 145
	legacyUnitHoverHeight        = 146

	legacyPlayerFlags            = 150
	legacyPlayerGuildID          = 151
	legacyPlayerGuildRank        = 152
	legacyPlayerBytes            = 153
	legacyPlayerBytes2           = 154
	legacyPlayerBytes3           = 155
	legacyPlayerDuelTeam         = 156
	legacyPlayerGuildTimestamp   = 157
	legacyPlayerQuestLog1        = 158
	legacyPlayerVisibleItem1     = 283
	legacyPlayerChosenTitle      = 321
	legacyPlayerInvSlotHead      = 324
	legacyPlayerPackSlot1        = 370
	legacyPlayerBankSlot1        = 402
	legacyPlayerBankBagSlot1     = 458
	legacyPlayerBuybackSlot1     = 472
	legacyPlayerKeyringSlot1     = 496
	legacyPlayerFarsight         = 624
	legacyPlayerKnownTitles      = 626
	legacyPlayerXP               = 634
	legacyPlayerNextLevelXP      = 635
	legacyPlayerSkill1           = 636
	legacyPlayerCharacterPts1    = 1020
	legacyPlayerCharacterPts2    = 1021
	legacyPlayerTrackCreatures   = 1022
	legacyPlayerTrackResources   = 1023
	legacyPlayerBlockPercent     = 1024
	legacyPlayerDodgePercent     = 1025
	legacyPlayerParryPercent     = 1026
	legacyPlayerExpertise        = 1027
	legacyPlayerOffExpertise     = 1028
	legacyPlayerCritPercent      = 1029
	legacyPlayerRangedCrit       = 1030
	legacyPlayerOffhandCrit      = 1031
	legacyPlayerSpellCrit1       = 1032
	legacyPlayerShieldBlock      = 1039
	legacyPlayerExplored1        = 1041
	legacyPlayerRestXP           = 1169
	legacyPlayerCoinage          = 1170
	legacyPlayerDamagePos1       = 1171
	legacyPlayerDamageNeg1       = 1178
	legacyPlayerDamagePct1       = 1185
	legacyPlayerHealingPos       = 1192
	legacyPlayerHealingPct       = 1193
	legacyPlayerHealingDonePct   = 1194
	legacyPlayerTargetResist     = 1195
	legacyPlayerTargetPhysical   = 1196
	legacyPlayerFieldBytes       = 1197
	legacyPlayerAmmoID           = 1198
	legacyPlayerPVPMedals        = 1200
	legacyPlayerBuybackPrice1    = 1201
	legacyPlayerBuybackTime1     = 1213
	legacyPlayerKills            = 1225
	legacyPlayerTodayContrib     = 1226
	legacyPlayerYesterdayContrib = 1227
	legacyPlayerLifetimeKills    = 1228
	legacyPlayerFieldBytes2      = 1229
	legacyPlayerWatchedFaction   = 1230
	legacyPlayerCombatRating1    = 1231
	legacyPlayerHonorCurrency    = 1277
	legacyPlayerArenaCurrency    = 1278
	legacyPlayerMaxLevel         = 1279
	legacyPlayerNoReagentCost1   = 1309
	legacyPlayerGlyphSlot1       = 1312
	legacyPlayerGlyph1           = 1318
	legacyPlayerGlyphsEnabled    = 1324
	legacyPlayerPetSpellPower    = 1325

	// 3.3.5 UNIT_FLAG_GHOST. On 3.4.3 the same bit is UNIT_FLAG_NON_ATTACKABLE_2.
	legacyUnitFlagGhost   = uint32(0x00010000)
	legacyUnitFlagStunned = uint32(0x00040000)
	legacyPlayerFlagGhost = uint32(0x00000010)
	modernUnitAnimStun    = uint32(14)

	// AzerothCore / Trinity 3.3.5 PLAYER_FIELD_BYTES[0] uses the same bits as
	// 3.4.3 PlayerLocalFlags for these three values.
	legacyPlayerByteTrackStealthed  = byte(0x02)
	legacyPlayerByteReleaseTimer    = byte(0x08)
	legacyPlayerByteNoReleaseWindow = byte(0x10)
	modernLocalFlagTrackStealthed   = uint32(0x02)
	modernLocalFlagReleaseTimer     = uint32(0x08)
	modernLocalFlagNoReleaseWindow  = uint32(0x10)

	// PLAYER_FIELD_BYTES2 byte 3 is AURA_VISION. Stealth sets 0x20 here; that
	// Values blob must not open an ActivePlayer section.
	legacyPlayerFieldBytes2Stealth = uint32(0x20000000)
)

// legacyOverrideSpellsID is the uint16 AzerothCore writes at
// PLAYER_FIELD_BYTES_2_OFFSET_OVERRIDE_SPELLS_ID (bytes 0-1).
func legacyOverrideSpellsID(bytes2 uint32) uint32 {
	return bytes2 & 0xFFFF
}

// emitOverrideSpellsIDDelta reports whether a PLAYER_FIELD_BYTES2 Values word
// should update ActivePlayerData.OverrideSpellsID. Real override sets put the
// DBC id in byte 0 (241 → 0x00F1). Byte 1 is legacy proxy's AuraVision source and is
// 0 for those ids; stealth writes 0x20 in byte 3 and must stay suppressed.
func emitOverrideSpellsIDDelta(bytes2 uint32) bool {
	if byte(bytes2) != 0 {
		return true
	}
	return bytes2&legacyPlayerFieldBytes2Stealth == 0
}

// EncodeActivePlayerCreate translates the local player's WotLK 3.3.5a
// CreateObject into the exact 3.4.3.54261 ActivePlayer create layout. It emits a
// complete SMSG_UPDATE_OBJECT body containing one CreateObject entry.
func EncodeActivePlayerCreate(update LegacyObjectUpdate, options ActivePlayerCreateOptions) ([]byte, error) {
	if update.Type != LegacyUpdateCreateObject1 && update.Type != LegacyUpdateCreateObject2 {
		return nil, fmt.Errorf("active-player update type is %d, want legacy CreateObject", update.Type)
	}
	if update.ObjectType != 4 {
		return nil, fmt.Errorf("active-player legacy object type is %d, want 4", update.ObjectType)
	}
	if update.GUID == 0 {
		return nil, fmt.Errorf("active-player GUID is empty")
	}
	if update.Movement == nil || update.Movement.UpdateFlags&legacyUpdateLiving == 0 {
		return nil, fmt.Errorf("active-player create has no living movement block")
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	if options.VirtualRealm == 0 {
		options.VirtualRealm = 1
	}

	low, high := modernPlayerGUID(update.GUID)
	objectData := make([]byte, 0, 16<<10)
	if update.Type == LegacyUpdateCreateObject1 {
		objectData = append(objectData, 1)
	} else {
		objectData = append(objectData, 2)
	}
	objectData = appendPackedGUID128(objectData, low, high)
	objectData = append(objectData, 7) // ActivePlayer
	var err error
	runes := RuneStateForCreate(PlayerClassFromFields(update.Values.Fields), options.Runes)
	objectData, err = appendCreateMovement(objectData, update, low, high, options.MapID, true, false, options.ActionButtons, runes, options.Now)
	if err != nil {
		return nil, err
	}

	values := legacyActivePlayerValues{fields: update.Values.Fields}
	if update.Movement != nil {
		values.moveFlags = update.Movement.MoveFlags
		values.vehicleID = update.Movement.VehicleID
	}
	valuesData := make([]byte, 0, 15<<10)
	valuesData = append(valuesData, 0x03) // owner + party-member visibility
	valuesData = values.appendObject(valuesData)
	valuesData = values.appendUnit(valuesData, true, false, options.MapID)
	valuesData = values.appendPlayer(valuesData, update.GUID, options, true)
	valuesData = values.appendActivePlayer(valuesData, options.MapID, options.CompletedQuestBlocks)
	objectData = binary.LittleEndian.AppendUint32(objectData, uint32(len(valuesData)))
	objectData = append(objectData, valuesData...)

	return encodeUpdateObjects(options.MapID, objectData), nil
}

func encodeUpdateObjects(mapID uint16, objects ...[]byte) []byte {
	totalSize := 11
	for _, object := range objects {
		totalSize += len(object)
	}
	body := make([]byte, 0, totalSize)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(objects)))
	body = binary.LittleEndian.AppendUint16(body, mapID)
	removals := newBitWriter(body)
	removals.writeBit(false)
	body = removals.flush()
	dataSize := 0
	for _, object := range objects {
		dataSize += len(object)
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(dataSize))
	for _, object := range objects {
		body = append(body, object...)
	}
	return body
}

func appendCreateMovement(dst []byte, update LegacyObjectUpdate, objectLow, objectHigh uint64, mapID uint16, activePlayer, otherPlayer bool, actionButtons []int32, runes *LegacyRuneState, now time.Time) ([]byte, error) {
	move := update.Movement
	// A living create that carries both a transport parent and a create-time
	// spline is the one shape build 54261 will not apply: the ICC gunship
	// boarding wave (Kor'kron bolted to Orgrim's Hammer, spline already 5 s
	// in) is the only place AzerothCore sends it, and both clients dropped
	// with Disconnect 7 within 40 ms of those creates. Create the unit
	// standing instead; it stays put until the realm's next monster move.
	if move.Spline != nil && move.TransportGUID != 0 {
		standing := *move
		standing.Spline = nil
		standing.MoveFlags &^= legacyMoveSplineEnabled
		move = &standing
	}
	// legacy proxy ObjectUpdateBuilder343.buildMovementUpdate writes create-time
	// splines when MovementInfo.Spline != nil. Omitting them diverges from the
	// binary that stays connected on this realm.
	hasSpline := move.Spline != nil
	bits := newBitWriter(dst)
	for index := 0; index < 18; index++ {
		// legacy proxy setCreateObjectBits: +19 hover, +20 living, +23 attack,
		// +24 other Player, +25 vehicle, +31/+33 ActivePlayer.
		// Battle-mages ride the hull with DisableGravity; that is not Hover.
		// Live ICC cannons are vehicle 554 with MOVEFLAG_ONTRANSPORT only.
		// legacy proxy's boarded create is living+vehicle (10 80 00) without Hover;
		// adding PlayHoverAnim made 3.4 show a gear that never sent a click.
		set := index == 3 || activePlayer && (index == 14 || index == 16)
		if index == 2 && createMovementPlaysHoverAnim(move) {
			set = true
		}
		if index == 6 && move.AttackTarget != 0 {
			set = true
		}
		if index == 7 && otherPlayer {
			set = true
		}
		if index == 8 && move.VehicleID != 0 {
			set = true
		}
		bits.writeBit(set)
	}
	dst = bits.flush()
	transportLow, transportHigh := modernLegacyGUID(move.TransportGUID, mapID)
	dst = appendModernMovementInfoDetails(dst, objectLow, objectHigh, move, transportLow, transportHigh, hasSpline)

	for _, speed := range []float32{
		move.WalkSpeed, move.RunSpeed, move.RunBackSpeed,
		move.SwimSpeed, move.SwimBackSpeed, move.FlightSpeed,
		move.FlightBackSpeed, move.TurnRate, move.PitchRate,
	} {
		dst = appendFloat32(dst, speed)
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for _, value := range []float32{1, 2, 65, 1, 3, 10, 100, 90, 140, 180, 360, 90, 270, 30, 80, 2.75, 7, 0.4} {
		dst = appendFloat32(dst, value)
	}
	splineBits := newBitWriter(dst)
	splineBits.writeBit(hasSpline)
	dst = splineBits.flush()
	if hasSpline {
		var err error
		dst, err = appendCreateSplineData(dst, move.Spline, mapID)
		if err != nil {
			return nil, err
		}
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0) // pause-time count
	if move.AttackTarget != 0 {
		low, high := modernLegacyGUID(move.AttackTarget, mapID)
		dst = appendPackedGUID128(dst, low, high)
	}
	if otherPlayer {
		if move.VehicleID != 0 {
			dst = binary.LittleEndian.AppendUint32(dst, move.VehicleID)
		} else {
			dst = binary.LittleEndian.AppendUint32(dst, uint32(now.Unix()))
		}
	}
	if move.VehicleID != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, move.VehicleID)
		dst = appendFloat32(dst, move.Orientation)
	}

	if activePlayer {
		// legacy proxy ObjectUpdateBuilder343: 3 bits then optional 180 Int32s when
		// the cached action-button slice is non-empty. AzerothCore sends the
		// bar before Create; creating without it leaves the local bar empty,
		// and a later reason-1 SMSG_UPDATE_ACTION_BUTTONS overlays that empty
		// bar (the 144 CMSG zeros).
		hasButtons := len(actionButtons) != 0
		hasRunes := runes != nil
		activeBits := newBitWriter(dst)
		activeBits.writeBit(false) // scene instance IDs
		activeBits.writeBit(hasRunes)
		activeBits.writeBit(hasButtons)
		dst = activeBits.flush()
		if hasRunes {
			dst = append(dst, EncodeRuneResync(*runes)...)
		}
		if hasButtons {
			dst = append(dst, EncodeCreateActionButtons(actionButtons)...)
		}
	}
	return dst, nil
}

func appendModernMovementInfo(dst []byte, objectLow, objectHigh uint64, move *LegacyMovement) []byte {
	transportLow, transportHigh := modernLegacyGUID(move.TransportGUID, 0)
	return appendModernMovementInfoResolved(dst, objectLow, objectHigh, move, transportLow, transportHigh)
}

func appendModernMovementInfoResolved(dst []byte, objectLow, objectHigh uint64, move *LegacyMovement, transportLow, transportHigh uint64) []byte {
	return appendModernMovementInfoDetails(dst, objectLow, objectHigh, move, transportLow, transportHigh, false)
}

func appendModernMovementInfoDetails(dst []byte, objectLow, objectHigh uint64, move *LegacyMovement, transportLow, transportHigh uint64, hasSplineData bool) []byte {
	dst = appendPackedGUID128(dst, objectLow, objectHigh)
	modernFlags := modernMovementFlags(move.MoveFlags)
	dst = binary.LittleEndian.AppendUint32(dst, modernFlags)
	// legacy proxy sets the first 3.4.3 movement-extra word to 0x200 on living
	// creates. It is part of the fixed MovementInfo shape even though WotLK
	// has no corresponding legacy field.
	dst = binary.LittleEndian.AppendUint32(dst, 0x200) // modern flags-extra
	dst = binary.LittleEndian.AppendUint32(dst, 0)     // flags-extra2
	dst = binary.LittleEndian.AppendUint32(dst, move.MoveTime)
	for _, value := range []float32{move.X, move.Y, move.Z, clampOrientation(move.Orientation), clampOrientation(move.Pitch), move.SplineElevation} {
		dst = appendFloat32(dst, value)
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0) // removed forces
	dst = binary.LittleEndian.AppendUint32(dst, 0) // move index

	hasTransport := transportLow != 0 || transportHigh != 0
	hasFallDirection := move.MoveFlags&(0x00001000|0x00002000) != 0
	hasFall := hasFallDirection || move.FallTime != 0
	flags := newBitWriter(dst)
	flags.writeBit(false) // standing on game object
	flags.writeBit(hasTransport)
	flags.writeBit(hasFall)
	flags.writeBit(hasSplineData)
	flags.writeBit(false) // height change failed
	flags.writeBit(false) // remote time valid
	flags.writeBit(false) // inertia
	flags.writeBit(false) // advanced flying
	dst = flags.flush()
	if hasTransport {
		dst = appendPackedGUID128(dst, transportLow, transportHigh)
		for _, value := range []float32{move.TransportX, move.TransportY, move.TransportZ, move.TransportOrientation} {
			dst = appendFloat32(dst, value)
		}
		dst = append(dst, byte(move.TransportSeat))
		dst = binary.LittleEndian.AppendUint32(dst, move.TransportTime)
		transportBits := newBitWriter(dst)
		transportBits.writeBit(false) // V3_4_3 writer does not forward legacy prev-time
		transportBits.writeBit(move.VehicleID != 0)
		dst = transportBits.flush()
		if move.VehicleID != 0 {
			dst = binary.LittleEndian.AppendUint32(dst, move.VehicleID)
		}
	}
	if hasFall {
		dst = binary.LittleEndian.AppendUint32(dst, move.FallTime)
		dst = appendFloat32(dst, move.JumpVelocity)
		fallBits := newBitWriter(dst)
		fallBits.writeBit(hasFallDirection)
		dst = fallBits.flush()
		if hasFallDirection {
			dst = appendFloat32(dst, move.JumpSinAngle)
			dst = appendFloat32(dst, move.JumpCosAngle)
			dst = appendFloat32(dst, move.JumpXYSpeed)
		}
	}
	return dst
}

func appendCreateSplineData(dst []byte, spline *LegacyMovementSpline, mapID uint16) ([]byte, error) {
	if spline == nil {
		return dst, nil
	}
	if len(spline.Points) > int(maxMonsterMovePoints) {
		return nil, fmt.Errorf("create spline has %d points (wire limit %d)", len(spline.Points), maxMonsterMovePoints)
	}
	modernFlags := modernSplineFlags(spline.Flags)
	modernType := uint8(0)
	switch {
	case spline.Flags&legacySplineFinalTarget != 0:
		modernType = 2
	case spline.Flags&legacySplineFinalOrientation != 0:
		modernType = 3
	case spline.Flags&legacySplineFinalPoint != 0:
		modernType = 1
	}
	dst = binary.LittleEndian.AppendUint32(dst, spline.ID)
	end := spline.End
	if modernFlags&0x00001000 != 0 { // Cyclic
		end = [3]float32{}
	} else if !finiteVec3(end) {
		end = [3]float32{}
	}
	for _, value := range end {
		dst = appendFloat32(dst, value)
	}
	// 3.4.3 apply rejects finalized / zero-length / non-finite create splines
	// (Disconnect 7, no jam). legacy proxy still writes count!=0; the live 832-byte
	// Walkmode creates are the ones that hit this path after Active Player
	// Created. Keep the spline header so HasSpline matches WotLK, but drop
	// the move block when the client cannot interpolate it.
	points := finiteVec3s(spline.Points)
	hasMove := len(points) != 0 && spline.FullTime > spline.Time && splinePathLength(points, end) >= createSplineMinLength
	hasMoveBits := newBitWriter(dst)
	hasMoveBits.writeBit(hasMove)
	dst = hasMoveBits.flush()
	if !hasMove {
		return dst, nil
	}
	elapsed := spline.Time
	dst = binary.LittleEndian.AppendUint32(dst, modernFlags)
	dst = binary.LittleEndian.AppendUint32(dst, elapsed)
	dst = binary.LittleEndian.AppendUint32(dst, spline.FullTime)
	dst = appendFloat32(dst, 1)
	dst = appendFloat32(dst, 1)
	bits := newBitWriter(dst)
	bits.writeBits(uint32(modernType), 2)
	bits.writeBit(false) // fade-object time
	bits.writeBits(uint32(len(spline.Points)), 16)
	// legacy proxy writeCreateObjectSplineDataBlock writes 5 extra false bits after the
	// 16-bit point count (filter / spell extra / jump extra / anim tier / unused).
	for range 5 {
		bits.writeBit(false)
	}
	dst = bits.flush()
	switch modernType {
	case 1:
		for _, value := range []float32{spline.FacingX, spline.FacingY, spline.FacingZ} {
			dst = appendFloat32(dst, value)
		}
	case 2:
		low, high := modernLegacyGUID(spline.FacingTarget, mapID)
		dst = appendPackedGUID128(dst, low, high)
	case 3:
		dst = appendFloat32(dst, clampOrientation(spline.FacingAngle))
	}
	for _, point := range points {
		for _, value := range point {
			dst = appendFloat32(dst, value)
		}
	}
	return dst, nil
}

const createSplineMinLength = float32(0.01)

func finiteVec3(value [3]float32) bool {
	for _, component := range value {
		if math.IsNaN(float64(component)) || math.IsInf(float64(component), 0) {
			return false
		}
	}
	return true
}

func finiteVec3s(points [][3]float32) [][3]float32 {
	out := make([][3]float32, 0, len(points))
	for _, point := range points {
		if finiteVec3(point) {
			out = append(out, point)
		}
	}
	return out
}

func splinePathLength(points [][3]float32, end [3]float32) float32 {
	if len(points) == 0 {
		return 0
	}
	var length float32
	prev := points[0]
	for _, point := range points[1:] {
		length += vec3Distance(prev, point)
		prev = point
	}
	if finiteVec3(end) && !zeroVector3(end) {
		length += vec3Distance(prev, end)
	}
	return length
}

func vec3Distance(a, b [3]float32) float32 {
	dx := float64(a[0] - b[0])
	dy := float64(a[1] - b[1])
	dz := float64(a[2] - b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

func modernMovementFlags(legacy uint32) uint32 {
	return legacy&0x000001ff |
		(legacy&0x07fffc00)>>1 |
		(legacy&0x70000000)>>2
}

type legacyActivePlayerValues struct {
	fields    map[int]uint32
	moveFlags uint32
	vehicleID uint32
}

func (v legacyActivePlayerValues) field(index int) uint32 { return v.fields[index] }

func (v legacyActivePlayerValues) fieldOr(index int, fallback uint32) uint32 {
	if value, ok := v.fields[index]; ok {
		return value
	}
	return fallback
}

func (v legacyActivePlayerValues) appendObject(dst []byte) []byte {
	return v.appendObjectWithDynamicFlags(dst, modernUnitDynamicFlags(v.field(legacyUnitDynamicFlags)))
}

func (v legacyActivePlayerValues) appendObjectWithDynamicFlags(dst []byte, dynamicFlags uint32) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyObjectEntry))
	dst = binary.LittleEndian.AppendUint32(dst, dynamicFlags)
	return binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyObjectScale, math.Float32bits(1)))
}

func (v legacyActivePlayerValues) appendUnit(dst []byte, owner, creature bool, mapID uint16) []byte {
	bytes0 := v.field(legacyUnitBytes0)
	race, class, sex, displayPower := byte(bytes0), byte(bytes0>>8), byte(bytes0>>16), byte(bytes0>>24)
	// UNIT_FLAG2_MIRROR_IMAGE. legacy proxy's StoreObjectUpdateInternal zeros the
	// clone's race bytes (object+0x90) so the create does not apply the
	// player's local ChrCustomization. Build 54261 only sends
	// CMSG_GET_MIRROR_IMAGE_DATA while Flags2 bit 0x10 is still set on that
	// create; unsolicited 0x2C14 is ignored and the mesh stays white.
	if creature && v.field(legacyUnitFlags2)&0x10 != 0 {
		race, class, sex = 0, 0, 0
	}
	dst = binary.LittleEndian.AppendUint64(dst, uint64(modernUnitHealth(v)))
	dst = binary.LittleEndian.AppendUint64(dst, uint64(v.field(legacyUnitMaxHealth)))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitDisplayID))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitNPCFlags))
	for index := 0; index < 5; index++ {
		value := uint32(0)
		if index == 2 { // UnitData.StateAnimID
			value = modernUnitStateAnimID(v.field(legacyUnitFlags))
		}
		dst = binary.LittleEndian.AppendUint32(dst, value)
	}
	// UnitData GUID create order is Charm, Summon, optional owner-only Critter,
	// CharmedBy, SummonedBy, CreatedBy, DemonCreator, LookAtControllerTarget,
	// Target and BattlePetCompanion. These control GUIDs are gameplay state,
	// not cosmetic metadata: omitting Charm prevents the 3.4.3 client from
	// exposing the temporary quest pet and therefore from abandoning it before
	// the next taming-rod step.
	dst = v.appendLegacyGUID(dst, legacyUnitCharm, mapID)
	dst = v.appendLegacyGUID(dst, legacyUnitSummon, mapID)
	if owner {
		dst = v.appendLegacyGUID(dst, legacyUnitCritter, mapID)
	}
	dst = v.appendLegacyGUID(dst, legacyUnitCharmedBy, mapID)
	dst = v.appendLegacyGUID(dst, legacyUnitSummonedBy, mapID)
	dst = v.appendLegacyGUID(dst, legacyUnitCreatedBy, mapID)
	dst = appendPackedGUID128(dst, 0, 0) // DemonCreator
	dst = appendPackedGUID128(dst, 0, 0) // LookAtControllerTarget
	dst = v.appendLegacyGUID(dst, legacyUnitTarget, mapID)
	dst = appendPackedGUID128(dst, 0, 0) // BattlePetCompanionGUID
	dst = binary.LittleEndian.AppendUint64(dst, 0)
	channelSpell := v.field(legacyUnitChannelSpell)
	dst = binary.LittleEndian.AppendUint32(dst, channelSpell)
	dst = binary.LittleEndian.AppendUint32(dst, KnownSpellVisual(channelSpell))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = append(dst, race, class, 0, sex, displayPower)
	dst = binary.LittleEndian.AppendUint32(dst, 0) // OverrideDisplayPowerID

	// UnitData::WriteCreate order: owner-only PowerRegenFlatModifier and
	// PowerRegenInterruptedFlatModifier interleaved, then Power, MaxPower and
	// ModPowerRegen interleaved per index. Writing the three power arrays
	// sequentially instead lands MaxPower[0] where the client reads Power[1],
	// so every MaxPower stays 0 and the client refuses to spend energy.
	power, maxPower := modernClassPowers(v, class)
	if creature && displayPower == 2 {
		// A creature whose DisplayPower is Focus is a hunter pet. Its legacy
		// powers live at UNIT_FIELD_POWER type 2 (focus) and type 4 (happiness);
		// the modern client reads those from Power[0]/MaxPower[0] and
		// Power[3]/MaxPower[3] (legacy proxy's create and per-tick Values updates write
		// happiness to Power[3] = descriptor bit 140). Leaving them zero left the
		// pet frame's happiness stuck at "unhappy" no matter what the server fed.
		power = [7]uint32{}
		maxPower = [7]uint32{}
		power[0] = v.field(legacyUnitPower1 + 2)
		maxPower[0] = v.field(legacyUnitMaxPower1 + 2)
		power[3] = v.field(legacyUnitPower1 + 4)
		maxPower[3] = v.field(legacyUnitMaxPower1 + 4)
	}
	if owner {
		var regen, interrupted [10]uint32
		for legacyType := 0; legacyType < 7; legacyType++ {
			slot := unitPowerSlot(updateObjectTypeForUnit(creature), v, class, legacyType)
			if slot >= 0 {
				regen[slot] = v.field(40 + legacyType)
				interrupted[slot] = v.field(47 + legacyType)
			}
		}
		for index := 0; index < 10; index++ {
			dst = binary.LittleEndian.AppendUint32(dst, regen[index])
			dst = binary.LittleEndian.AppendUint32(dst, interrupted[index])
		}
	}
	for index := 0; index < 10; index++ {
		var current, maximum uint32
		if index < len(power) {
			current, maximum = power[index], maxPower[index]
		}
		dst = binary.LittleEndian.AppendUint32(dst, current)
		dst = binary.LittleEndian.AppendUint32(dst, maximum)
		// legacy proxy's UnitData placeholders initialize ModPowerRegen to 1.0.
		dst = appendFloat32(dst, 1)
	}
	level := v.field(legacyUnitLevel)
	for _, value := range []uint32{level, level, 0, 0, 0, 0, 0, 0, 0, v.field(legacyUnitFaction)} {
		dst = binary.LittleEndian.AppendUint32(dst, value)
	}
	for index := 0; index < 3; index++ {
		itemID := v.field(legacyUnitVirtualItem1 + index)
		if itemID == 0 {
			itemID = v.field(legacyPlayerVisibleItem1 + (15+index)*2)
		}
		dst = binary.LittleEndian.AppendUint32(dst, itemID)
		dst = binary.LittleEndian.AppendUint16(dst, 0)
		dst = binary.LittleEndian.AppendUint16(dst, 0)
	}
	flags2 := v.fieldOr(legacyUnitFlags2, 0x800)
	unitFlags := []uint32{
		modernUnitFlags(v.field(legacyUnitFlags)), flags2, 0,
		v.field(legacyUnitAuraState), v.field(legacyUnitBaseAttackTime),
		v.field(legacyUnitBaseAttackTime + 1),
	}
	if owner {
		unitFlags = append(unitFlags, v.field(legacyUnitRangedAttackTime))
	}
	for _, value := range unitFlags {
		dst = binary.LittleEndian.AppendUint32(dst, value)
	}
	dst = appendRawFloatOr(dst, v, legacyUnitBoundingRadius, 0.389)
	dst = appendRawFloatOr(dst, v, legacyUnitCombatReach, 1.5)
	dst = appendFloat32(dst, 1)
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitNativeDisplayID))
	dst = appendFloat32(dst, 1)
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitMountDisplayID))
	if owner {
		for index := 0; index < 4; index++ {
			dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitMinDamage+index))
		}
	}
	stand, pet, vis, anim := modernUnitBytes1(v.field(legacyUnitBytes1))
	if anim == 0 && createValuesPlayHoverAnim(v) {
		anim = 2
	}
	dst = append(dst, stand, pet, vis, anim)
	for index := 0; index < 4; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitPetNumber+index))
	}
	dst = appendRawFloatOr(dst, v, legacyUnitModCastSpeed, 1)
	for index := 0; index < 5; index++ {
		dst = appendFloat32(dst, 1)
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitCreatedBySpell))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitEmoteState))
	dst = binary.LittleEndian.AppendUint16(dst, 0)
	dst = binary.LittleEndian.AppendUint16(dst, 0)
	if owner {
		for index := 0; index < 5; index++ {
			for _, base := range []int{legacyUnitStat0, legacyUnitPositiveStat0, legacyUnitNegativeStat0} {
				dst = binary.LittleEndian.AppendUint32(dst, v.field(base+index))
			}
		}
		for index := 0; index < 7; index++ {
			dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitResistance0+index))
		}
		for index := 0; index < 7; index++ {
			dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitPowerCostModifier0+index))
			dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitPowerCostMult0+index))
		}
	}
	for index := 0; index < 7; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitPositiveResist0+index))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitNegativeResist0+index))
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitBaseMana))
	if owner {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitBaseHealth))
	}
	bytes2 := v.field(legacyUnitBytes2)
	dst = append(dst, byte(bytes2), byte(bytes2>>8), byte(bytes2>>16), byte(bytes2>>24))
	if owner {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitAttackPower))
		dst = appendSignedInt16Pair(dst, v.field(legacyUnitAttackPowerMods))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitAttackPowerMult))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitRangedAttackPower))
		dst = appendSignedInt16Pair(dst, v.field(legacyUnitRangedAPMods))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitRangedAPMult))
		dst = binary.LittleEndian.AppendUint32(dst, 0)
		dst = appendFloat32(dst, 0)
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitMinRangedDamage))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyUnitMinRangedDamage+1))
		dst = binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyUnitMaxHealthModifier, math.Float32bits(1)))
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyUnitHoverHeight, math.Float32bits(1)))
	for index := 0; index < 4; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, ^uint32(0))
	dst = binary.LittleEndian.AppendUint32(dst, 0)

	guildID := v.field(legacyPlayerGuildID)
	if guildID == 0 {
		dst = appendPackedGUID128(dst, 0, 0)
	} else {
		dst = appendPackedGUID128(dst, uint64(guildID), uint64(28)<<58|uint64(1)<<42)
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	channelObject := v.legacyGUID(legacyUnitChannelObject)
	channelObjectCount := uint32(0)
	if owner || channelObject != 0 {
		// legacy proxy allocates UnitData.ChannelObjects with one slot for the local
		// player. The slot is serialized as an empty packed GUID when the legacy
		// channel target is zero; omitting it shortens ActivePlayer UnitData by
		// two bytes. Public units only carry the slot when a target exists.
		channelObjectCount = 1
	}
	dst = binary.LittleEndian.AppendUint32(dst, channelObjectCount)
	dst = appendPackedGUID128(dst, 0, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = appendFloat32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	if owner {
		dst = appendPackedGUID128(dst, 0, 0) // ComboTarget
	}
	if channelObjectCount != 0 {
		dst = v.appendLegacyGUID(dst, legacyUnitChannelObject, mapID)
	}
	return dst
}

func (v legacyActivePlayerValues) appendPlayer(dst []byte, guid uint64, options ActivePlayerCreateOptions, owner bool) []byte {
	bytes0 := v.field(legacyUnitBytes0)
	race, class, sex := byte(bytes0), byte(bytes0>>8), byte(bytes0>>16)
	appearance := v.field(legacyPlayerBytes)
	appearance2 := v.field(legacyPlayerBytes2)
	appearance3 := v.field(legacyPlayerBytes3)
	customizations := modernCustomizations(LegacyCharacter{
		Race: race, Class: class, Sex: sex,
		Skin: byte(appearance), Face: byte(appearance >> 8),
		HairStyle: byte(appearance >> 16), HairColor: byte(appearance >> 24),
		FacialHair: byte(appearance2),
	})

	dst = appendPackedGUID128(dst, 0, 0) // duel arbiter
	accountID := uint64(uint32(guid))
	if owner && options.GameAccountID != 0 {
		accountID = options.GameAccountID
	}
	dst = appendPackedGUID128(dst, accountID, uint64(29)<<58) // WoW account
	dst = appendPackedGUID128(dst, 0, 0)                      // loot target
	legacyFlags := v.field(legacyPlayerFlags)
	dst = binary.LittleEndian.AppendUint32(dst, modernPlayerFlags(legacyFlags))
	var flagsEx uint32
	if legacyFlags&0x00000400 != 0 {
		flagsEx |= 0x80
	}
	if legacyFlags&0x00000800 != 0 {
		flagsEx |= 0x100
	}
	dst = binary.LittleEndian.AppendUint32(dst, flagsEx)
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerGuildRank))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	if v.field(legacyPlayerGuildRank) != 0 || v.field(legacyPlayerGuildID) != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, 25)
	} else {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	// Build 54261's PlayerData create descriptor has a fixed 36-entry
	// customization array. legacy proxy fills the five WotLK choices and serializes
	// the remaining entries as zero pairs.
	const createCustomizationCount = 36
	dst = binary.LittleEndian.AppendUint32(dst, createCustomizationCount)
	dst = append(dst, 0, 0, byte(appearance2>>16), byte(appearance3)&1, byte(uint16(appearance3)&0xfffe), byte(appearance3>>16), factionForRace(race), byte(appearance3>>24))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerDuelTeam))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerGuildTimestamp))
	if owner {
		for slot := 0; slot < 25; slot++ {
			base := legacyPlayerQuestLog1 + slot*5
			dst = appendQuestLogSlot(dst, v.field(base), v.field(base+1), v.field(base+2), v.field(base+3), v.field(base+4), options.StateOnlyQuests)
		}
	}
	for slot := 0; slot < 19; slot++ {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerVisibleItem1+slot*2))
		dst = binary.LittleEndian.AppendUint16(dst, 0)
		dst = binary.LittleEndian.AppendUint16(dst, visibleItemEnchantVisual(v.field(legacyPlayerVisibleItem1+slot*2+1)))
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerChosenTitle))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, options.VirtualRealm)
	dst = binary.LittleEndian.AppendUint32(dst, 0) // current specialization
	for index := 0; index < 7; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	dst = append(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 1) // honor level
	var logoutTime uint64
	if owner {
		logoutTime = uint64(options.Now.Unix())
	}
	dst = binary.LittleEndian.AppendUint64(dst, logoutTime)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = appendPackedGUID128(dst, 0, 0) // BNet account GUID
	for index := 0; index < 20; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	for _, customization := range customizations {
		dst = binary.LittleEndian.AppendUint32(dst, customization[0])
		dst = binary.LittleEndian.AppendUint32(dst, customization[1])
	}
	for index := len(customizations); index < createCustomizationCount; index++ {
		dst = binary.LittleEndian.AppendUint64(dst, 0)
	}
	dst = appendFloat32(dst, 0)
	dst = appendFloat32(dst, 0)
	return binary.LittleEndian.AppendUint32(dst, 0)
}

func (v legacyActivePlayerValues) appendActivePlayer(dst []byte, mapID uint16, completedQuestBlocks []uint64) []byte {
	for index := 0; index < 141; index++ {
		legacyField := modernInventoryLegacyField(index)
		if legacyField < 0 {
			dst = appendPackedGUID128(dst, 0, 0)
		} else {
			dst = v.appendLegacyGUID(dst, legacyField, mapID)
		}
	}
	dst = v.appendLegacyGUID(dst, legacyPlayerFarsight, mapID)
	dst = appendPackedGUID128(dst, 0, 0) // summoned battle pet
	// WotLK exposes PLAYER_FIELD_KNOWN_TITLES as three uint64 blocks. legacy proxy
	// preserves that fixed dynamic-field length even when every block is zero.
	dst = binary.LittleEndian.AppendUint32(dst, 3) // known-title count
	dst = binary.LittleEndian.AppendUint64(dst, uint64(v.field(legacyPlayerCoinage)))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerXP))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerNextLevelXP))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for slot := 0; slot < 256; slot++ {
		var line, step, rank, maxRank, temp, perm uint16
		if slot < 128 {
			word0 := v.field(legacyPlayerSkill1 + slot*3)
			word1 := v.field(legacyPlayerSkill1 + slot*3 + 1)
			word2 := v.field(legacyPlayerSkill1 + slot*3 + 2)
			line, step = uint16(word0), uint16(word0>>16)
			rank, maxRank = uint16(word1), uint16(word1>>16)
			temp, perm = uint16(word2), uint16(word2>>16)
		}
		for _, value := range []uint16{line, step, rank, 0, maxRank, temp, perm} {
			dst = binary.LittleEndian.AppendUint16(dst, value)
		}
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerCharacterPts1))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerCharacterPts2))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerTrackCreatures))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerTrackResources))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for _, index := range []int{
		legacyPlayerExpertise, legacyPlayerOffExpertise, -1, -1,
		legacyPlayerBlockPercent, legacyPlayerDodgePercent, -1,
		legacyPlayerParryPercent, -1, legacyPlayerCritPercent,
		legacyPlayerRangedCrit, legacyPlayerOffhandCrit,
	} {
		if index < 0 {
			dst = appendFloat32(dst, 0)
		} else if index == legacyPlayerExpertise || index == legacyPlayerOffExpertise {
			dst = appendFloat32(dst, float32(int32(v.field(index))))
		} else {
			dst = binary.LittleEndian.AppendUint32(dst, v.field(index))
		}
	}
	for school := 0; school < 7; school++ {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerSpellCrit1+school))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerDamagePos1+school))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerDamageNeg1+school))
		dst = binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyPlayerDamagePct1+school, math.Float32bits(1)))
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerShieldBlock))
	dst = appendFloat32(dst, 0)
	for index := 0; index < 4; index++ {
		dst = appendFloat32(dst, 0)
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for index := 0; index < 3; index++ {
		dst = appendFloat32(dst, 0)
	}
	for index := 0; index < 240; index++ {
		var explored uint64
		if index < 64 {
			explored = uint64(v.field(legacyPlayerExplored1+index*2)) |
				uint64(v.field(legacyPlayerExplored1+index*2+1))<<32
		}
		dst = binary.LittleEndian.AppendUint64(dst, explored)
	}
	restState := byte(2)
	if raw, ok := v.fields[legacyPlayerBytes2]; ok {
		restState = byte(raw >> 24)
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyPlayerRestXP, 1))
	dst = append(dst, restState)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = append(dst, 1)
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerHealingPos))
	dst = binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyPlayerHealingPct, math.Float32bits(1)))
	dst = binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyPlayerHealingDonePct, math.Float32bits(1)))
	dst = appendFloat32(dst, 1)
	for index := 0; index < 6; index++ {
		dst = appendFloat32(dst, 1)
	}
	dst = appendFloat32(dst, 1)
	dst = appendFloat32(dst, 0)
	dst = appendFloat32(dst, 0)
	dst = appendFloat32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerTargetResist))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerTargetPhysical))
	fieldBytes := v.field(legacyPlayerFieldBytes)
	dst = binary.LittleEndian.AppendUint32(dst, modernPlayerLocalFlags(fieldBytes, v.isLegacyGhost()))
	multiActionBars := byte(fieldBytes >> 16)
	if _, present := v.fields[legacyPlayerFieldBytes]; !present {
		multiActionBars = 7
	}
	dst = append(dst, byte(fieldBytes>>8), multiActionBars, byte(fieldBytes>>24), 0)
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerAmmoID))
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerPVPMedals))
	for slot := 0; slot < 12; slot++ {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerBuybackPrice1+slot))
		dst = binary.LittleEndian.AppendUint64(dst, uint64(v.field(legacyPlayerBuybackTime1+slot)))
	}
	kills := v.field(legacyPlayerKills)
	for _, value := range []uint16{uint16(kills), 0, uint16(kills >> 16), 0, 0, 0, 0, 0} {
		dst = binary.LittleEndian.AppendUint16(dst, value)
	}
	for _, value := range []uint32{
		v.field(legacyPlayerTodayContrib), v.field(legacyPlayerLifetimeKills), 0, 0,
		v.field(legacyPlayerYesterdayContrib), 0, 0,
	} {
		dst = binary.LittleEndian.AppendUint32(dst, value)
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyPlayerWatchedFaction, ^uint32(0)))
	for index := 0; index < 32; index++ {
		var rating uint32
		if index < 20 {
			rating = v.field(legacyPlayerCombatRating1 + index)
		}
		dst = binary.LittleEndian.AppendUint32(dst, rating)
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.fieldOr(legacyPlayerMaxLevel, 80))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	// PLAYER_NO_REAGENT_COST_1..3. Build 54261 NoReagentCostMask is four
	// uint32s (parent 615, elements 616-619); WotLK only has the first three.
	for index := 0; index < 3; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerNoReagentCost1+index))
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerPetSpellPower))
	for index := 0; index < 2; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	dst = appendFloat32(dst, 0)
	dst = appendFloat32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = appendFloat32(dst, 1)
	// LocalRegenFlags, AuraVision, NumBackpackSlots. AuraVision is "this
	// player sees through stealth"; see encodeActivePlayerValuesDelta for why
	// it comes from PLAYER_FIELD_BYTES2 byte 1 and not byte 3. The next
	// uint32 is OverrideSpellsID (bit 105), the low 16 bits of the same field.
	bytes2 := v.field(legacyPlayerFieldBytes2)
	dst = append(dst, 0, byte(bytes2>>8), 16)
	dst = binary.LittleEndian.AppendUint32(dst, legacyOverrideSpellsID(bytes2))
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint16(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for index := 0; index < 11; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	for index := 0; index < QuestCompletedBlockCount; index++ {
		var value uint64
		if index < len(completedQuestBlocks) {
			value = completedQuestBlocks[index]
		}
		dst = binary.LittleEndian.AppendUint64(dst, value)
	}
	dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerHonorCurrency))
	dst = binary.LittleEndian.AppendUint32(dst, 5500)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, ^uint32(0))
	dst = binary.LittleEndian.AppendUint32(dst, ^uint32(0))
	// PvpRankProgress byte + PerksProgramCurrency int32.
	dst = append(dst, 0, 0, 0, 0, 0)
	for index := 0; index < 16; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for index := 0; index < 6; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerGlyphSlot1+index))
		dst = binary.LittleEndian.AppendUint32(dst, v.field(legacyPlayerGlyph1+index))
	}
	dst = append(dst, byte(v.field(legacyPlayerGlyphsEnabled)), 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	dst = append(dst, 0)
	for index := 0; index < 3; index++ {
		low := uint64(v.field(legacyPlayerKnownTitles + index*2))
		high := uint64(v.field(legacyPlayerKnownTitles + index*2 + 1))
		dst = binary.LittleEndian.AppendUint64(dst, low|high<<32)
	}
	for bracket := 0; bracket < 7; bracket++ {
		dst = append(dst, 0)
		for index := 0; index < 16; index++ {
			dst = binary.LittleEndian.AppendUint32(dst, 0)
		}
		pvpBits := newBitWriter(dst)
		pvpBits.writeBit(false)
		dst = pvpBits.flush()
	}
	trailing := newBitWriter(dst)
	trailing.writeBit(false)
	trailing.writeBit(false)
	trailing.writeBit(false)
	dst = trailing.flush()
	dst = binary.LittleEndian.AppendUint32(dst, 0)
	for index := 0; index < 8; index++ {
		dst = binary.LittleEndian.AppendUint32(dst, 0)
	}
	dst = binary.LittleEndian.AppendUint64(dst, 0)
	last := newBitWriter(dst)
	last.writeBit(false)
	return last.flush()
}

func modernInventoryLegacyField(modernIndex int) int {
	switch {
	case modernIndex >= 0 && modernIndex <= 18:
		return legacyPlayerInvSlotHead + modernIndex*2
	case modernIndex >= 30 && modernIndex <= 33:
		return legacyPlayerInvSlotHead + (19+modernIndex-30)*2
	case modernIndex >= 35 && modernIndex <= 50:
		return legacyPlayerPackSlot1 + (modernIndex-35)*2
	case modernIndex >= 59 && modernIndex <= 86:
		return legacyPlayerBankSlot1 + (modernIndex-59)*2
	case modernIndex >= 87 && modernIndex <= 93:
		return legacyPlayerBankBagSlot1 + (modernIndex-87)*2
	case modernIndex >= 94 && modernIndex <= 105:
		return legacyPlayerBuybackSlot1 + (modernIndex-94)*2
	case modernIndex >= 106 && modernIndex <= 137:
		return legacyPlayerKeyringSlot1 + (modernIndex-106)*2
	default:
		return -1
	}
}

func modernClassPowers(v legacyActivePlayerValues, class byte) ([7]uint32, [7]uint32) {
	var power, maximum [7]uint32
	for legacyType := 0; legacyType < 7; legacyType++ {
		slot := classPowerSlot(class, legacyType)
		if slot < 0 {
			continue
		}
		power[slot] = v.field(legacyUnitPower1 + legacyType)
		maximum[slot] = v.field(legacyUnitMaxPower1 + legacyType)
	}
	// WotLK has no UNIT_FIELD combo power. 3.4 stores it in Power[slot]
	// from GetPowerSlotForClass(class, 14); CREATE must expose Max=5 or
	// the client clamps every combo update to 0.
	if classHasComboPoints(class) {
		slot := comboPowerSlot(class)
		if slot >= 0 && slot < len(maximum) && maximum[slot] == 0 {
			maximum[slot] = legacyComboPointsMax
		}
	}
	return power, maximum
}

func classHasComboPoints(class byte) bool {
	switch class {
	case 1, 4, 11: // warrior (vehicle), rogue, druid
		return true
	default:
		return false
	}
}

func classPowerSlot(class byte, powerType int) int {
	switch class {
	case 1:
		if powerType == 1 {
			return 0
		}
	case 2, 3, 5, 7, 8, 9:
		if powerType == 0 {
			return 0
		}
	case 4:
		if powerType == 3 {
			return 0
		}
	case 6:
		if powerType == 6 {
			return 0
		}
	case 11:
		switch powerType {
		case 0:
			return 0
		case 1:
			return 1
		case 3:
			return 2
		}
	}
	return -1
}

func modernPlayerGUID(guid uint64) (uint64, uint64) {
	return uint64(uint32(guid)), uint64(2)<<58 | uint64(1)<<42
}

func modernLegacyGUID(guid uint64, mapID uint16) (uint64, uint64) {
	if guid == 0 {
		return 0, 0
	}
	highLegacy := uint16(guid >> 48)
	if highLegacy == 0 {
		return modernPlayerGUID(guid)
	}
	counter32 := uint64(uint32(guid))
	entry := uint64((guid >> 24) & 0x00ffffff)
	switch highLegacy {
	case 0x4000, 0x4700:
		return counter32, uint64(3)<<58 | uint64(1)<<42
	case 0xf120:
		// legacy proxy's NewWowGuid128By64 uses 0xffffffff as the missing-entry
		// sentinel for legacy HighGuid::Transport. Keeping the low 32 bits at
		// zero creates a different modern transport identity even though the
		// counter portion looks correct.
		if entry == 0 {
			entry = math.MaxUint32
		}
		return 0, uint64(6)<<58 | (uint64(guid&0x00ffffff)&0xfffff)<<38 | entry
	case 0x1fc0:
		return 0, uint64(6)<<58 | (counter32&0xfffff)<<38
	}
	var modernType uint64
	switch highLegacy {
	case 0xf130:
		modernType = 8
	case 0xf150:
		modernType = 9
	case 0xf140:
		modernType = 10
	case 0xf110:
		modernType = 11
	case 0xf100:
		modernType = 12
	case 0xf101:
		modernType = 14
	default:
		return 0, 0
	}
	low := uint64(guid & 0x00ffffff)
	// Creature/GameObject GUIDs leave the map field at bits 29-41 zero. Hermes
	// spells the builder Create(type, mapId, entry, counter), but its Creature
	// and GameObject call sites pass GameState.GetObjectSpawnCounter there, not
	// a map, so the field is 0 for everything except objects it deliberately
	// re-identifies. legacy proxy's build-54261 creates agree: the captured turret
	// 0xf110031491000068 on map 631 is High=0x2c00040000c52440, with no map
	// bits set. Cast and chat-channel GUIDs do carry the real map, so this is
	// specific to the map-specific object families. mapID stays a parameter
	// because every caller passes one, but no branch consumes it.
	high := modernType<<58 | uint64(1)<<42 | (entry&0x7fffff)<<6
	return low, high
}

func modernUnitDynamicFlags(value uint32) uint32 {
	var result uint32
	if value&0x01 != 0 {
		result |= 0x04
	}
	if value&0x02 != 0 {
		result |= 0x08
	}
	if value&0x04 != 0 {
		result |= 0x10
	}
	if value&0x10 != 0 {
		result |= 0x20
	}
	if value&0x20 != 0 {
		result |= 0x40
	}
	if value&0x40 != 0 {
		result |= 0x80
	}
	if value&0x0c == 0x0c {
		result &^= 0x10
	}
	return result
}

func (v legacyActivePlayerValues) isLegacyGhost() bool {
	return v.field(legacyPlayerFlags)&legacyPlayerFlagGhost != 0 || v.field(legacyUnitFlags)&legacyUnitFlagGhost != 0
}

func modernUnitFlags(legacy uint32) uint32 {
	return legacy &^ legacyUnitFlagGhost
}

// Build 54261 normally derives the stunned pose from UnitData.Flags, but that
// does not happen reliably for translated legacy updates. Mirror the legacy
// flag into the explicit StateAnimID so create and live-update paths agree.
func modernUnitStateAnimID(legacyFlags uint32) uint32 {
	if legacyFlags&legacyUnitFlagStunned != 0 {
		return modernUnitAnimStun
	}
	return 0
}

// UNIT_VIS_FLAGS_STEALTHED has the same 0x02 value as WotLK's
// UNIT_BYTE1_FLAG_CREEP, so VisFlags passes through unchanged.
const unitByte1VisCreep = 0x02

// modernUnitBytes1 maps WotLK UNIT_FIELD_BYTES_1 onto 3.4 StandState / VisFlags /
// AnimTier. WotLK stealth only sets VisFlags CREEP (byte 2 = 0x02). The 3.4
// client stores the stealthed alpha in AnimTier (byte 3): a Prowl/Stealth
// toggle on a live 3.4 descriptor flips byte 3 0x00→0x02 and nothing else
// besides aura slots. Forwarding CREEP only to VisFlags leaves the local
// model opaque even when the Unit Values section is applied.
func modernUnitBytes1(raw uint32) (stand, pet, vis, anim byte) {
	stand = byte(raw)
	pet = byte(raw >> 8)
	vis = byte(raw >> 16)
	anim = modernAnimTier(byte(raw >> 24))
	if vis&unitByte1VisCreep != 0 && anim == 0 {
		anim = unitByte1VisCreep
	}
	return
}

// modernAnimTier maps WotLK UNIT_FIELD_BYTES_1[3]. AzerothCore stores a
// bitfield there (ALWAYS_STAND=0x01, HOVER=0x02, fly-ish 0x04); 3.4.3
// AnimTier is Ground/Swim/Hover/Fly/Submerged. Passing 0x03 through as 3
// makes a hovering NPC look like Fly, and 0x00 with only DisableGravity
// looks like a stand.
func modernAnimTier(legacy byte) byte {
	if legacy&0x04 != 0 {
		return 3
	}
	if legacy&0x02 != 0 {
		return 2
	}
	return legacy
}

// PlayHoverAnim is the hover pose, not "this unit ignores gravity". ICC
// gunship passengers carry MOVEFLAG_DISABLEGRAVITY because the hull is in
// the air; promoting that to Hover made battle-mages float with idle arms
// instead of holding the Below Zero channel. Cannons are vehicles, but the
// live Alliance guns arrive with only MOVEFLAG_ONTRANSPORT, and legacy proxy's
// boarded create leaves Hover clear. Forcing Hover on every VehicleID made
// 3.4 draw a gear cursor, select the gun, and never send SpellClick.
func legacyMovePlaysHoverAnim(flags uint32) bool {
	return flags&legacyMoveHover != 0
}

func createMovementPlaysHoverAnim(move *LegacyMovement) bool {
	if move == nil {
		return false
	}
	return hoverAnimFromMove(move.MoveFlags, move.VehicleID)
}

func createValuesPlayHoverAnim(v legacyActivePlayerValues) bool {
	return hoverAnimFromMove(v.moveFlags, v.vehicleID)
}

func hoverAnimFromMove(flags, vehicleID uint32) bool {
	if flags&legacyMoveHover != 0 {
		return true
	}
	return vehicleID != 0 && flags&legacyMoveDisableGravity != 0
}

func modernUnitHealth(v legacyActivePlayerValues) uint32 {
	health := v.field(legacyUnitHealth)
	if health == 0 && v.isLegacyGhost() {
		return 1
	}
	return health
}

// LegacyPlayerVitalState reports the raw create/update fields used to diagnose
// ghost logins. Health is the value AzerothCore sent, before modernUnitHealth.
func LegacyPlayerVitalState(fields map[int]uint32) (health, unitFlags, playerFlags uint32, ghost bool) {
	v := legacyActivePlayerValues{fields: fields}
	return v.field(legacyUnitHealth), v.field(legacyUnitFlags), v.field(legacyPlayerFlags), v.isLegacyGhost()
}

func LegacyHealthDroppedToZero(changed map[int]uint32) bool {
	health, ok := changed[legacyUnitHealth]
	return ok && health == 0
}

func modernPlayerFlags(value uint32) uint32 {
	result := value & 0x0000333f
	if value&0x00000040 != 0 {
		result |= 0x00000080
	}
	if value&0x00080000 != 0 {
		result |= 0x00400000
	}
	if value&0x00100000 != 0 {
		result |= 0x00200000
	}
	if value&0x00400000 != 0 {
		result |= 0x80000000
	}
	return result
}

// PlayerLocalFlags is the 3.4.3 LocalFlags value encoded on ActivePlayer create.
func PlayerLocalFlags(fields map[int]uint32) uint32 {
	v := legacyActivePlayerValues{fields: fields}
	return modernPlayerLocalFlags(v.field(legacyPlayerFieldBytes), v.isLegacyGhost())
}

// modernPlayerLocalFlags maps 3.3.5 PLAYER_FIELD_BYTES[0] onto 3.4.3 LocalFlags.
// Ghosts who already released spirit must not keep RELEASE_TIMER (0x08); that
// opens the 3.4.3 "release spirit" countdown on a character that is already a
// ghost and the client then Disconnect 7. They do need NO_RELEASE_WINDOW.
func modernPlayerLocalFlags(legacyBytes uint32, ghost bool) uint32 {
	legacy := byte(legacyBytes)
	if ghost {
		flags := modernLocalFlagNoReleaseWindow
		if legacy&legacyPlayerByteTrackStealthed != 0 {
			flags |= modernLocalFlagTrackStealthed
		}
		return flags
	}
	var flags uint32
	if legacy&legacyPlayerByteTrackStealthed != 0 {
		flags |= modernLocalFlagTrackStealthed
	}
	if legacy&legacyPlayerByteReleaseTimer != 0 {
		flags |= modernLocalFlagReleaseTimer
	}
	if legacy&legacyPlayerByteNoReleaseWindow != 0 {
		flags |= modernLocalFlagNoReleaseWindow
	}
	return flags
}

func factionForRace(race byte) byte {
	switch race {
	case 1, 3, 4, 7, 11:
		return 1
	default:
		return 0
	}
}

func appendFloat32(dst []byte, value float32) []byte {
	return binary.LittleEndian.AppendUint32(dst, math.Float32bits(value))
}

func appendRawFloatOr(dst []byte, values legacyActivePlayerValues, index int, fallback float32) []byte {
	return binary.LittleEndian.AppendUint32(dst, values.fieldOr(index, math.Float32bits(fallback)))
}

func appendSignedInt16Pair(dst []byte, packed uint32) []byte {
	dst = binary.LittleEndian.AppendUint32(dst, uint32(int32(int16(packed))))
	return binary.LittleEndian.AppendUint32(dst, uint32(int32(int16(packed>>16))))
}

// quest log slot state flags: the two clients share the low complete (0x1) and
// failed (0x2) bits. The 3.4.3 QuestLog stores flag-backed objective completion
// at bit 8+StorageIndex; state-only quests complete with no progress counter,
// so a COMPLETE legacy slot marks their synthesized StorageIndex 0 AreaTrigger
// objective through modernQuestObjectiveCompleteBit0.
const (
	legacyQuestStateComplete         = uint32(0x1)
	modernQuestObjectiveCompleteBit0 = uint32(0x100)
)

// translateQuestLogStateFlags maps a legacy quest-slot state into the 3.4.3
// QuestLog.StateFlags layout. For WotLK (expansion 80) legacy proxy forwards
// PLAYER_QUEST_LOG_X_Y as-is, and that is still the default here: broadly
// synthesizing objective bits makes the modern client show a false completion
// check before it reconciles the quest template. The one scoped exception is
// stateOnlyQuests: quests with a synthesized StorageIndex 0 AreaTrigger,
// including exploration mixed with item/kill objectives. These objectives never
// advance an ObjectiveProgress counter, so the modern client can only tick
// them from the StateFlags bit; add it only when the legacy slot is actually
// COMPLETE.
func translateQuestLogStateFlags(questID, legacyState uint32, stateOnlyQuests map[uint32]struct{}) uint32 {
	if questID == 0 {
		return 0
	}
	state := legacyState
	if legacyState&legacyQuestStateComplete != 0 {
		if _, stateOnly := stateOnlyQuests[questID]; stateOnly {
			state |= modernQuestObjectiveCompleteBit0
		}
	}
	return state
}

func questLogProgress(lo, hi uint32) [24]uint16 {
	var progress [24]uint16
	progress[0] = uint16(lo)
	progress[1] = uint16(lo >> 16)
	progress[2] = uint16(hi)
	progress[3] = uint16(hi >> 16)
	return progress
}

func appendQuestLogSlot(dst []byte, questID, state, lo, hi, timer uint32, stateOnlyQuests map[uint32]struct{}) []byte {
	progress := questLogProgress(lo, hi)
	dst = binary.LittleEndian.AppendUint64(dst, uint64(timer))
	dst = binary.LittleEndian.AppendUint32(dst, questID)
	dst = binary.LittleEndian.AppendUint32(dst, translateQuestLogStateFlags(questID, state, stateOnlyQuests))
	for _, value := range progress {
		dst = binary.LittleEndian.AppendUint16(dst, value)
	}
	return dst
}
