package modernworld

import (
	_ "embed"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const (
	maxAuraUpdates = 64

	legacyAuraEffect0  = 0x01
	legacyAuraEffect1  = 0x02
	legacyAuraEffect2  = 0x04
	legacyAuraNoCaster = 0x08
	legacyAuraPositive = 0x10
	legacyAuraDuration = 0x20
	legacyAuraScalable = 0x40
	legacyAuraNegative = 0x80

	// legacy proxy ConvertAuraFlags for expansion 80 (3.4.x):
	// Negative→0x10, Positive→0x102 (Positive|Cancelable), NoCaster→0x01, Duration→0x04.
	auraFlagNoCaster   uint16 = 0x0001
	auraFlagCancelable uint16 = 0x0002
	auraFlagDuration   uint16 = 0x0004
	auraFlagScalable   uint16 = 0x0008
	auraFlagNegative   uint16 = 0x0010
	auraFlagPositive   uint16 = 0x0100

	modernHighGuidCast        = 47
	modernSpellCastSourceAura = 13

	spellGhost = uint32(8326)
)

type AuraInfo struct {
	Slot            uint8
	HasData         bool
	SpellID         uint32
	VisualID        uint32
	Flags           uint16
	ActiveFlags     uint32
	CastLevel       uint16
	Applications    uint8
	CastUnit        GUID128
	Duration        *int32
	Remaining       *int32
	Points          []float32
	EstimatedPoints []float32
}

func ParseLegacyAuraUpdate(body []byte, updateAll bool) (uint64, []AuraInfo, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, nil, fmt.Errorf("read aura GUID: %w", err)
	}
	auras := make([]AuraInfo, 0, 8)
	for r.remaining() > 0 {
		if len(auras) >= maxAuraUpdates {
			return 0, nil, fmt.Errorf("aura-update exceeds %d entries", maxAuraUpdates)
		}
		aura, err := parseLegacyAura(&r)
		if err != nil {
			return 0, nil, err
		}
		auras = append(auras, aura)
		if !updateAll {
			break
		}
	}
	if r.remaining() != 0 {
		return 0, nil, fmt.Errorf("aura-update has %d trailing bytes", r.remaining())
	}
	if !updateAll && len(auras) != 1 {
		return 0, nil, fmt.Errorf("single aura-update has %d entries, want 1", len(auras))
	}
	return guid, auras, nil
}

func parseLegacyAura(r *movementReader) (AuraInfo, error) {
	var aura AuraInfo
	slot, err := r.u8()
	if err != nil {
		return aura, fmt.Errorf("read aura slot: %w", err)
	}
	spellID, err := r.u32()
	if err != nil {
		return aura, fmt.Errorf("read aura spell: %w", err)
	}
	aura.Slot = slot
	if spellID == 0 {
		return aura, nil
	}
	flags, err := r.u8()
	if err != nil {
		return aura, fmt.Errorf("read aura flags: %w", err)
	}
	level, err := r.u8()
	if err != nil {
		return aura, fmt.Errorf("read aura level: %w", err)
	}
	applications, err := r.u8()
	if err != nil {
		return aura, fmt.Errorf("read aura applications: %w", err)
	}
	aura.HasData = true
	aura.SpellID = spellID
	aura.CastLevel = uint16(level)
	if aura.CastLevel == 0 {
		aura.CastLevel = 1
	}
	aura.Applications = applications
	if aura.Applications == 0 {
		aura.Applications = 1
	}
	aura.Flags = convertAuraFlags(flags)
	aura.ActiveFlags = convertAuraActiveFlags(flags)
	if flags&legacyAuraNoCaster == 0 {
		caster, err := r.guid64()
		if err != nil {
			return aura, fmt.Errorf("read aura caster: %w", err)
		}
		aura.CastUnit = GUID128{Low: caster} // high filled by caller
	}
	if flags&legacyAuraDuration != 0 {
		duration, err := r.i32()
		if err != nil {
			return aura, fmt.Errorf("read aura duration: %w", err)
		}
		remaining, err := r.i32()
		if err != nil {
			return aura, fmt.Errorf("read aura remaining: %w", err)
		}
		aura.Duration = &duration
		aura.Remaining = &remaining
	}
	if flags&legacyAuraScalable != 0 {
		for _, mask := range []byte{legacyAuraEffect0, legacyAuraEffect1, legacyAuraEffect2} {
			if flags&mask == 0 {
				continue
			}
			point, err := r.f32()
			if err != nil {
				return aura, fmt.Errorf("read aura point: %w", err)
			}
			aura.Points = append(aura.Points, point)
		}
	}
	return aura, nil
}

func convertAuraFlags(legacy byte) uint16 {
	var flags uint16
	if legacy&legacyAuraNegative != 0 {
		flags |= auraFlagNegative
	} else if legacy&legacyAuraPositive != 0 {
		flags |= auraFlagPositive | auraFlagCancelable
	} else {
		// AzerothCore AuraApplication::BuildUpdatePacket clears POSITIVE for
		// paladin area auras received from another caster. They remain helpful,
		// but cannot be canceled by the recipient. Modern clients need an
		// explicit positive flag to keep these out of the debuff list.
		flags |= auraFlagPositive
	}
	if legacy&legacyAuraNoCaster != 0 {
		flags |= auraFlagNoCaster
	}
	if legacy&legacyAuraDuration != 0 {
		flags |= auraFlagDuration
	}
	return flags
}

func convertAuraActiveFlags(legacy byte) uint32 {
	active := uint32(1)
	if legacy&legacyAuraEffect1 != 0 {
		active = 3
	}
	if legacy&legacyAuraEffect2 != 0 {
		active |= 4
	}
	return active
}

func EncodeAuraUpdate(guid GUID128, mapID uint16, updateAll bool, auras []AuraInfo) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(updateAll)
	bits.writeBits(uint32(len(auras)), 9)
	body := bits.flush()
	for _, aura := range auras {
		// legacy proxy AuraInfo.Write uses Buffer.WriteByte for Slot, then
		// WriteMSBit(HasData), FlushBits, optional AuraDataInfo.Write.
		body = append(body, aura.Slot)
		entryBits := newBitWriter(body)
		entryBits.writeBit(aura.HasData)
		body = entryBits.flush()
		if !aura.HasData {
			continue
		}
		castLow, castHigh := auraCastGUID(aura.SpellID, guid, mapID)
		body = appendPackedGUID128(body, castLow, castHigh)
		body = binary.LittleEndian.AppendUint32(body, aura.SpellID)
		body = binary.LittleEndian.AppendUint32(body, aura.VisualID)
		body = binary.LittleEndian.AppendUint16(body, aura.Flags)
		body = binary.LittleEndian.AppendUint32(body, aura.ActiveFlags)
		body = binary.LittleEndian.AppendUint16(body, aura.CastLevel)
		body = append(body, aura.Applications)
		body = binary.LittleEndian.AppendUint32(body, 0) // ContentTuningID
		fieldBits := newBitWriter(body)
		writeCastUnit := aura.Flags&auraFlagNoCaster == 0 && (aura.CastUnit.Low != 0 || aura.CastUnit.High != 0)
		fieldBits.writeBit(writeCastUnit)
		fieldBits.writeBit(aura.Duration != nil)
		fieldBits.writeBit(aura.Remaining != nil)
		fieldBits.writeBit(false) // TimeMod
		fieldBits.writeBits(uint32(len(aura.Points)), 6)
		fieldBits.writeBits(uint32(len(aura.EstimatedPoints)), 6)
		fieldBits.writeBit(false) // ContentTuning
		body = fieldBits.flush()
		if writeCastUnit {
			body = appendPackedGUID128(body, aura.CastUnit.Low, aura.CastUnit.High)
		}
		if aura.Duration != nil {
			body = binary.LittleEndian.AppendUint32(body, uint32(*aura.Duration))
		}
		if aura.Remaining != nil {
			body = binary.LittleEndian.AppendUint32(body, uint32(*aura.Remaining))
		}
		for _, point := range aura.Points {
			body = appendFloat32(body, point)
		}
		for _, point := range aura.EstimatedPoints {
			body = appendFloat32(body, point)
		}
	}
	return appendPackedGUID128(body, guid.Low, guid.High)
}

func auraCastGUID(spellID uint32, owner GUID128, mapID uint16) (uint64, uint64) {
	// legacy proxy SMSG_AURA_UPDATE_ALL calls wow_guid.MapSpecificCreate(
	// type=47 Cast, subtype=13 Aura, mapId, spellId, counter=owner&0xffffff).
	// High layout matches creature GUIDs: type<<58 | realm 1<<42 | map<<29 | entry<<6 | subtype.
	low := owner.Low & 0xffffff
	high := uint64(modernHighGuidCast)<<58 | uint64(1)<<42 | uint64(mapID&0x1fff)<<29 | (uint64(spellID)&0x7fffff)<<6 | modernSpellCastSourceAura
	return low, high
}

func EncodeEmptyAuraUpdateAll(playerGUID uint64) []byte {
	low, high := modernPlayerGUID(playerGUID)
	return EncodeAuraUpdate(GUID128{Low: low, High: high}, 0, true, nil)
}

func MarkLocalPlayerAuraFlags(auras []AuraInfo) {
	const ghostFlags = auraFlagNoCaster | auraFlagCancelable | auraFlagPositive
	for index := range auras {
		if auras[index].HasData && auras[index].SpellID == spellGhost {
			auras[index].Flags = ghostFlags
		}
	}
}

//go:embed spell_visual_v3_4_3_54261.csv
var spellVisualCSV string

var spellVisuals = sync.OnceValue(func() map[uint32]uint32 {
	reader := csv.NewReader(strings.NewReader(spellVisualCSV))
	records, err := reader.ReadAll()
	if err != nil {
		panic(fmt.Sprintf("parse embedded SpellXSpellVisual data: %v", err))
	}
	visuals := make(map[uint32]uint32, len(records)-1)
	for index, record := range records {
		if index == 0 {
			if len(record) != 2 || record[0] != "SpellId" || record[1] != "SpellXSpellVisualId" {
				panic("embedded SpellXSpellVisual data has an invalid header")
			}
			continue
		}
		if len(record) != 2 {
			panic(fmt.Sprintf("embedded SpellXSpellVisual row %d has %d columns", index+1, len(record)))
		}
		spellID, spellErr := strconv.ParseUint(record[0], 10, 32)
		visualID, visualErr := strconv.ParseUint(record[1], 10, 32)
		if spellErr != nil || visualErr != nil {
			panic(fmt.Sprintf("embedded SpellXSpellVisual row %d is invalid", index+1))
		}
		visuals[uint32(spellID)] = uint32(visualID)
	}
	return visuals
})

// KnownSpellVisual returns the build-54261 SpellXSpellVisualID used when a
// server-originated cast or aura has no preceding modern CMSG_CAST_SPELL from
// which the proxy could learn it. legacy proxy carries this complete per-build table;
// using the same data avoids spell-specific visual fixes.
func KnownSpellVisual(spellID uint32) uint32 {
	return spellVisuals()[spellID]
}

func IsStealthSpell(spellID uint32) bool {
	switch spellID {
	case 1784, 1785, 1786, 1787, 5215, 6783, 9913:
		return true
	default:
		return false
	}
}

const StealthCooldownMS uint32 = 10000
