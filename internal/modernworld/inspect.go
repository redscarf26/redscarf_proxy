package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGInspect       = uint16(0x3529)
	SMSGInspectResult = uint16(0x2631)
	maxInspectItems   = 19
	inspectGlyphSlots = 6
	// inspectTalentSpecID is the same MAX_SPECIALIZATIONS sentinel EncodeTalentData
	// writes: WotLK has no Cata/MoP spec id, and 3.4.3 still expects the byte.
	inspectTalentSpecID = 4
)

type LegacyInspectTalent struct {
	ID   uint32
	Rank uint8
}

type LegacyInspectSpec struct {
	Talents []LegacyInspectTalent
	Glyphs  []uint16
}

type LegacyInspectItem struct {
	Index            uint8
	ItemID           uint32
	Enchants         []InspectEnchant
	RandomPropertyID uint32
	Creator          uint64
	SuffixFactor     uint32
}

type InspectEnchant struct {
	ID    uint32
	Index uint8
}

type LegacyInspectResult struct {
	Target     uint64
	Unspent    uint32
	ActiveSpec uint8
	Specs      []LegacyInspectSpec
	Items      []LegacyInspectItem
}

type InspectMetadata struct {
	GUID            GUID128
	Name            string
	Sex             uint8
	Race            uint8
	Class           uint8
	GuildID         uint32
	LifetimeMaxRank uint8
}

func ParseInspectRequest(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseLegacyInspectResult(body []byte) (LegacyInspectResult, error) {
	var result LegacyInspectResult
	r := movementReader{data: body}
	var err error
	if result.Target, err = r.guid64(); err != nil {
		return result, fmt.Errorf("read inspect target: %w", err)
	}
	if result.Unspent, err = r.u32(); err != nil {
		return result, fmt.Errorf("read inspect unspent talents: %w", err)
	}
	specCount, err := r.u8()
	if err != nil {
		return result, fmt.Errorf("read inspect spec count: %w", err)
	}
	if specCount > 2 {
		return result, fmt.Errorf("inspect spec count %d exceeds 2", specCount)
	}
	if result.ActiveSpec, err = r.u8(); err != nil {
		return result, fmt.Errorf("read inspect active spec: %w", err)
	}
	if specCount != 0 && result.ActiveSpec >= specCount {
		return result, fmt.Errorf("inspect active spec %d exceeds count %d", result.ActiveSpec, specCount)
	}
	result.Specs = make([]LegacyInspectSpec, specCount)
	for specIndex := range result.Specs {
		talentCount, readErr := r.u8()
		if readErr != nil {
			return result, fmt.Errorf("read inspect spec %d talent count: %w", specIndex, readErr)
		}
		if talentCount > 100 {
			return result, fmt.Errorf("inspect spec %d has %d talents", specIndex, talentCount)
		}
		result.Specs[specIndex].Talents = make([]LegacyInspectTalent, talentCount)
		for talentIndex := range result.Specs[specIndex].Talents {
			talent := &result.Specs[specIndex].Talents[talentIndex]
			if talent.ID, err = r.u32(); err != nil {
				return result, fmt.Errorf("read inspect spec %d talent %d ID: %w", specIndex, talentIndex, err)
			}
			if talent.Rank, err = r.u8(); err != nil {
				return result, fmt.Errorf("read inspect spec %d talent %d rank: %w", specIndex, talentIndex, err)
			}
		}
		glyphCount, readErr := r.u8()
		if readErr != nil {
			return result, fmt.Errorf("read inspect spec %d glyph count: %w", specIndex, readErr)
		}
		if glyphCount > 16 {
			return result, fmt.Errorf("inspect spec %d has %d glyphs", specIndex, glyphCount)
		}
		result.Specs[specIndex].Glyphs = make([]uint16, glyphCount)
		for glyphIndex := range result.Specs[specIndex].Glyphs {
			if result.Specs[specIndex].Glyphs[glyphIndex], err = r.u16(); err != nil {
				return result, fmt.Errorf("read inspect spec %d glyph %d: %w", specIndex, glyphIndex, err)
			}
		}
	}
	slotMask, err := r.u32()
	if err != nil {
		return result, fmt.Errorf("read inspect item mask: %w", err)
	}
	if slotMask>>maxInspectItems != 0 {
		return result, fmt.Errorf("inspect item mask 0x%x exceeds %d slots", slotMask, maxInspectItems)
	}
	for slot := uint8(0); slot < maxInspectItems; slot++ {
		if slotMask&(uint32(1)<<slot) == 0 {
			continue
		}
		item := LegacyInspectItem{Index: slot}
		if item.ItemID, err = r.u32(); err != nil {
			return result, fmt.Errorf("read inspect item %d ID: %w", slot, err)
		}
		enchantMask, readErr := r.u16()
		if readErr != nil {
			return result, fmt.Errorf("read inspect item %d enchant mask: %w", slot, readErr)
		}
		if enchantMask>>12 != 0 {
			return result, fmt.Errorf("inspect item %d enchant mask 0x%x exceeds 12 slots", slot, enchantMask)
		}
		for enchantSlot := uint8(0); enchantSlot < 12; enchantSlot++ {
			if enchantMask&(uint16(1)<<enchantSlot) == 0 {
				continue
			}
			enchantID, readErr := r.u16()
			if readErr != nil {
				return result, fmt.Errorf("read inspect item %d enchant %d: %w", slot, enchantSlot, readErr)
			}
			item.Enchants = append(item.Enchants, InspectEnchant{ID: uint32(enchantID), Index: enchantSlot})
		}
		randomProperty, readErr := r.u16()
		if readErr != nil {
			return result, fmt.Errorf("read inspect item %d random property: %w", slot, readErr)
		}
		item.RandomPropertyID = uint32(int32(int16(randomProperty)))
		if item.Creator, err = r.guid64(); err != nil {
			return result, fmt.Errorf("read inspect item %d creator: %w", slot, err)
		}
		if item.SuffixFactor, err = r.u32(); err != nil {
			return result, fmt.Errorf("read inspect item %d suffix: %w", slot, err)
		}
		result.Items = append(result.Items, item)
	}
	if r.remaining() != 0 {
		return result, fmt.Errorf("inspect result has %d trailing bytes", r.remaining())
	}
	return result, nil
}

func InspectMetadataFromFields(guid GUID128, name string, fields map[int]uint32) InspectMetadata {
	bytes0 := fields[legacyUnitBytes0]
	playerBytes := fields[legacyPlayerBytes]
	return InspectMetadata{
		GUID:            guid,
		Name:            truncateWireString(name, 63),
		Race:            uint8(bytes0),
		Class:           uint8(bytes0 >> 8),
		Sex:             uint8(bytes0 >> 16),
		GuildID:         fields[legacyPlayerGuildID],
		LifetimeMaxRank: uint8(playerBytes >> 24),
	}
}

func EncodeInspectResult(result LegacyInspectResult, metadata InspectMetadata, resolve func(uint64) GUID128) []byte {
	body := appendPackedGUID128(nil, metadata.GUID.Low, metadata.GUID.High)
	body = binary.LittleEndian.AppendUint32(body, 0) // SpecializationID has no WotLK equivalent.
	body = binary.LittleEndian.AppendUint32(body, uint32(len(result.Items)))
	nameBits := newBitWriter(body)
	nameBits.writeBits(uint32(len(metadata.Name)), 6)
	body = nameBits.flush()
	body = append(body, metadata.Sex, metadata.Race, metadata.Class)
	body = binary.LittleEndian.AppendUint32(body, 0) // customization count
	body = append(body, metadata.Name...)
	for _, item := range result.Items {
		creator := resolve(item.Creator)
		body = appendPackedGUID128(body, creator.Low, creator.High)
		body = append(body, item.Index)
		body = binary.LittleEndian.AppendUint32(body, 0) // Azerite powers
		body = binary.LittleEndian.AppendUint32(body, 0) // Azerite essences
		body = appendItemInstance(body, item.ItemID, item.SuffixFactor, item.RandomPropertyID)
		itemBits := newBitWriter(body)
		itemBits.writeBit(false) // Usable is not present in the legacy response.
		itemBits.writeBits(uint32(len(item.Enchants)), 4)
		itemBits.writeBits(0, 2) // gems
		body = itemBits.flush()
		for _, enchant := range item.Enchants {
			body = binary.LittleEndian.AppendUint32(body, enchant.ID)
			body = append(body, enchant.Index)
		}
	}
	// 54261 inspect follows legacy proxy Write343: PvpTalentsCount=0, honor scalars,
	// then ClassicTalentInfoUpdate groups (no trailing IsPet bit). Hermes'
	// empty talent-rank array left GetNumTalentGroups(inspect) at 0 and greyed
	// both the talent and glyph tabs.
	body = binary.LittleEndian.AppendUint32(body, 0) // PvpTalentsCount
	body = binary.LittleEndian.AppendUint32(body, 0) // ItemLevel
	body = append(body, metadata.LifetimeMaxRank)
	body = binary.LittleEndian.AppendUint16(body, 0) // today HK
	body = binary.LittleEndian.AppendUint16(body, 0) // yesterday HK
	body = binary.LittleEndian.AppendUint32(body, 0) // lifetime HK
	body = binary.LittleEndian.AppendUint32(body, 1) // HonorLevel
	body = binary.LittleEndian.AppendUint32(body, result.Unspent)
	body = append(body, result.ActiveSpec)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(result.Specs)))
	for _, spec := range result.Specs {
		body = appendInspectTalentGroup(body, spec)
	}
	optional := newBitWriter(body)
	optional.writeBit(metadata.GuildID != 0)
	optional.writeBit(false) // AzeriteLevel
	body = optional.flush()
	for bracket := 0; bracket < 6; bracket++ {
		// The legacy response has no modern rated-bracket records. Keep each
		// of the six fixed entries at its protocol default value.
		body = append(body, 0)
		body = append(body, make([]byte, 12*4)...)
		bracketBits := newBitWriter(body)
		bracketBits.writeBit(false)
		body = bracketBits.flush()
	}
	if metadata.GuildID != 0 {
		guild := GUID128{Low: uint64(metadata.GuildID), High: uint64(28) << 58}
		body = appendPackedGUID128(body, guild.Low, guild.High)
		body = binary.LittleEndian.AppendUint32(body, 0) // member count
		body = binary.LittleEndian.AppendUint32(body, 0) // achievement points
	}
	return body
}

func appendInspectTalentGroup(body []byte, spec LegacyInspectSpec) []byte {
	body = append(body, byte(len(spec.Talents)))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(spec.Talents)))
	body = append(body, inspectGlyphSlots)
	body = binary.LittleEndian.AppendUint32(body, inspectGlyphSlots)
	body = append(body, inspectTalentSpecID)
	for _, talent := range spec.Talents {
		body = binary.LittleEndian.AppendUint32(body, talent.ID)
		body = append(body, talent.Rank)
	}
	var glyphs [inspectGlyphSlots]uint16
	copy(glyphs[:], spec.Glyphs)
	for _, glyph := range glyphs {
		body = binary.LittleEndian.AppendUint16(body, glyph)
	}
	return body
}
