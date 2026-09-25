package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGTrainerList       = uint16(0x34AD)
	CMSGTrainerBuySpell   = uint16(0x34AE)
	CMSGLearnTalent       = uint16(0x3552)
	CMSGConfirmRespecWipe = uint16(12813)

	SMSGUpdateTalentData  = uint16(0x25D7)
	SMSGTrainerList       = uint16(0x26DF)
	SMSGTrainerBuyFailed  = uint16(0x26E0)
	SMSGRespecWipeConfirm = uint16(9746)

	maxTrainerSpells = 4096
	maxTalentGroups  = 8
	maxTalents       = 256
	maxGlyphs        = 16

	SpecResetTalents    = uint8(0)
	SpecResetPetTalents = uint8(3)
)

type TrainerBuySpellRequest struct {
	Trainer   GUID128
	TrainerID uint32
	SpellID   uint32
}

type LearnTalentRequest struct {
	TalentID uint32
	Rank     uint16
}

type ConfirmRespecWipeRequest struct {
	Trainer    GUID128
	RespecType uint8
}

type LegacyTalentWipeConfirm struct {
	Trainer uint64
	Cost    uint32
}

type LegacyTrainerSpell struct {
	SpellID      uint32
	MoneyCost    uint32
	ReqSkillLine uint32
	ReqSkillRank uint32
	ReqAbility   [3]uint32
	Usable       uint8
	ReqLevel     uint8
}

type LegacyTrainerList struct {
	Trainer     uint64
	TrainerType int32
	Spells      []LegacyTrainerSpell
	Greeting    string
}

type LegacyTrainerBuyFailed struct {
	Trainer uint64
	SpellID uint32
	Reason  uint32
}

type TalentInfo struct {
	TalentID uint32
	Rank     uint8
}

type TalentGroup struct {
	Talents []TalentInfo
	Glyphs  []uint16
}

type LegacyTalentData struct {
	IsPet   bool
	Unspent uint32
	Active  uint8
	Groups  []TalentGroup
}

func ParseTrainerList(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseTrainerBuySpell(body []byte) (TrainerBuySpellRequest, error) {
	var request TrainerBuySpellRequest
	r := movementReader{data: body}
	var err error
	if request.Trainer, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read trainer GUID: %w", err)
	}
	if request.TrainerID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read trainer ID: %w", err)
	}
	if request.SpellID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read trainer spell ID: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("trainer-buy-spell has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyTrainerBuySpell(trainer uint64, spellID uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, trainer)
	return binary.LittleEndian.AppendUint32(body, spellID)
}

func ParseLearnTalent(body []byte) (LearnTalentRequest, error) {
	if len(body) != 6 {
		return LearnTalentRequest{}, fmt.Errorf("learn-talent has %d bytes, want 6", len(body))
	}
	return LearnTalentRequest{
		TalentID: binary.LittleEndian.Uint32(body[:4]),
		Rank:     binary.LittleEndian.Uint16(body[4:]),
	}, nil
}

func EncodeLegacyLearnTalent(request LearnTalentRequest) []byte {
	body := binary.LittleEndian.AppendUint32(nil, request.TalentID)
	return binary.LittleEndian.AppendUint32(body, uint32(request.Rank))
}

func ParseConfirmRespecWipe(body []byte) (ConfirmRespecWipeRequest, error) {
	var request ConfirmRespecWipeRequest
	r := movementReader{data: body}
	var err error
	if request.Trainer, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read respec-wipe trainer: %w", err)
	}
	if request.RespecType, err = r.u8(); err != nil {
		return request, fmt.Errorf("read respec-wipe type: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("confirm-respec-wipe has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyTalentWipeConfirm(trainer uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, trainer)
}

func ParseLegacyTalentWipeConfirm(body []byte) (LegacyTalentWipeConfirm, error) {
	var confirm LegacyTalentWipeConfirm
	if len(body) != 12 {
		return confirm, fmt.Errorf("talent-wipe-confirm has %d bytes, want 12", len(body))
	}
	confirm.Trainer = binary.LittleEndian.Uint64(body[:8])
	confirm.Cost = binary.LittleEndian.Uint32(body[8:])
	return confirm, nil
}

func EncodeRespecWipeConfirm(guid GUID128, confirm LegacyTalentWipeConfirm) []byte {
	body := []byte{SpecResetTalents}
	body = binary.LittleEndian.AppendUint32(body, confirm.Cost)
	return appendPackedGUID128(body, guid.Low, guid.High)
}

func ParseLegacyTrainerList(body []byte) (LegacyTrainerList, error) {
	var trainer LegacyTrainerList
	r := movementReader{data: body}
	var err error
	if trainer.Trainer, err = r.u64(); err != nil {
		return trainer, fmt.Errorf("read trainer-list GUID: %w", err)
	}
	if trainer.TrainerType, err = r.i32(); err != nil {
		return trainer, fmt.Errorf("read trainer-list type: %w", err)
	}
	count, err := r.i32()
	if err != nil {
		return trainer, fmt.Errorf("read trainer spell count: %w", err)
	}
	if count < 0 || count > maxTrainerSpells {
		return trainer, fmt.Errorf("trainer spell count %d is invalid", count)
	}
	trainer.Spells = make([]LegacyTrainerSpell, 0, count)
	for index := int32(0); index < count; index++ {
		var spell LegacyTrainerSpell
		if spell.SpellID, err = r.u32(); err != nil {
			return trainer, fmt.Errorf("read trainer spell %d ID: %w", index, err)
		}
		legacyState, readErr := r.u8()
		if readErr != nil {
			return trainer, fmt.Errorf("read trainer spell %d state: %w", index, readErr)
		}
		switch legacyState {
		case 0:
			spell.Usable = 1 // available
		case 1:
			spell.Usable = 2 // unavailable
		case 2:
			spell.Usable = 0 // known
		default:
			spell.Usable = 2
		}
		if spell.MoneyCost, err = r.u32(); err != nil {
			return trainer, fmt.Errorf("read trainer spell %d cost: %w", index, err)
		}
		if _, err = r.i32(); err != nil {
			return trainer, fmt.Errorf("read trainer spell %d primary-profession flag: %w", index, err)
		}
		if _, err = r.i32(); err != nil {
			return trainer, fmt.Errorf("read trainer spell %d primary-profession value: %w", index, err)
		}
		if spell.ReqLevel, err = r.u8(); err != nil {
			return trainer, fmt.Errorf("read trainer spell %d level: %w", index, err)
		}
		if spell.ReqSkillLine, err = r.u32(); err != nil {
			return trainer, fmt.Errorf("read trainer spell %d skill line: %w", index, err)
		}
		if spell.ReqSkillRank, err = r.u32(); err != nil {
			return trainer, fmt.Errorf("read trainer spell %d skill rank: %w", index, err)
		}
		for ability := range spell.ReqAbility {
			if spell.ReqAbility[ability], err = r.u32(); err != nil {
				return trainer, fmt.Errorf("read trainer spell %d ability %d: %w", index, ability, err)
			}
		}
		trainer.Spells = append(trainer.Spells, spell)
	}
	if trainer.Greeting, err = r.cstring(); err != nil {
		return trainer, fmt.Errorf("read trainer greeting: %w", err)
	}
	if r.remaining() != 0 {
		return trainer, fmt.Errorf("trainer-list has %d trailing bytes", r.remaining())
	}
	return trainer, nil
}

func EncodeTrainerList(guid GUID128, trainer LegacyTrainerList) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(trainer.TrainerType))
	body = binary.LittleEndian.AppendUint32(body, 1) // TrainerID expected back in buy request
	body = binary.LittleEndian.AppendUint32(body, uint32(len(trainer.Spells)))
	for _, spell := range trainer.Spells {
		body = binary.LittleEndian.AppendUint32(body, spell.SpellID)
		body = binary.LittleEndian.AppendUint32(body, spell.MoneyCost)
		body = binary.LittleEndian.AppendUint32(body, spell.ReqSkillLine)
		body = binary.LittleEndian.AppendUint32(body, spell.ReqSkillRank)
		for _, ability := range spell.ReqAbility {
			body = binary.LittleEndian.AppendUint32(body, ability)
		}
		body = append(body, spell.Usable, spell.ReqLevel)
	}
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(trainer.Greeting)), 11)
	body = bits.flush()
	return append(body, trainer.Greeting...)
}

func ParseLegacyTrainerBuyFailed(body []byte) (LegacyTrainerBuyFailed, error) {
	var response LegacyTrainerBuyFailed
	r := movementReader{data: body}
	var err error
	if response.Trainer, err = r.u64(); err != nil {
		return response, fmt.Errorf("read trainer-buy-failed GUID: %w", err)
	}
	if response.SpellID, err = r.u32(); err != nil {
		return response, fmt.Errorf("read trainer-buy-failed spell: %w", err)
	}
	if response.Reason, err = r.u32(); err != nil {
		return response, fmt.Errorf("read trainer-buy-failed reason: %w", err)
	}
	if r.remaining() != 0 {
		return response, fmt.Errorf("trainer-buy-failed has %d trailing bytes", r.remaining())
	}
	return response, nil
}

func EncodeTrainerBuyFailed(guid GUID128, response LegacyTrainerBuyFailed) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, response.SpellID)
	return binary.LittleEndian.AppendUint32(body, response.Reason)
}

func ParseLegacyTalentData(body []byte) (LegacyTalentData, error) {
	var data LegacyTalentData
	r := movementReader{data: body}
	isPet, err := r.u8()
	if err != nil {
		return data, fmt.Errorf("read talent is-pet: %w", err)
	}
	data.IsPet = isPet != 0
	if data.Unspent, err = r.u32(); err != nil {
		return data, fmt.Errorf("read unspent talent points: %w", err)
	}
	groupCount, err := r.u8()
	if err != nil {
		return data, fmt.Errorf("read talent group count: %w", err)
	}
	if data.IsPet {
		// For a 3.3.5 pet this byte is the talent count, not the player
		// specialization-group count. Pets have no Active or glyph section.
		if int(groupCount) > maxTalents {
			return data, fmt.Errorf("pet talent count %d exceeds %d", groupCount, maxTalents)
		}
		group := TalentGroup{Talents: make([]TalentInfo, 0, groupCount)}
		for talentIndex := uint8(0); talentIndex < groupCount; talentIndex++ {
			var talent TalentInfo
			if talent.TalentID, err = r.u32(); err != nil {
				return data, fmt.Errorf("read pet talent %d ID: %w", talentIndex, err)
			}
			if talent.Rank, err = r.u8(); err != nil {
				return data, fmt.Errorf("read pet talent %d rank: %w", talentIndex, err)
			}
			group.Talents = append(group.Talents, talent)
		}
		if len(group.Talents) != 0 {
			data.Groups = []TalentGroup{group}
		}
		if r.remaining() != 0 {
			return data, fmt.Errorf("pet talent-data has %d trailing bytes", r.remaining())
		}
		return data, nil
	}
	if groupCount > maxTalentGroups {
		return data, fmt.Errorf("talent group count %d exceeds %d", groupCount, maxTalentGroups)
	}
	if data.Active, err = r.u8(); err != nil {
		return data, fmt.Errorf("read active talent group: %w", err)
	}
	data.Groups = make([]TalentGroup, 0, groupCount)
	for groupIndex := uint8(0); groupIndex < groupCount; groupIndex++ {
		var group TalentGroup
		talentCount, readErr := r.u8()
		if readErr != nil {
			return data, fmt.Errorf("read talent group %d count: %w", groupIndex, readErr)
		}
		if int(talentCount) > maxTalents {
			return data, fmt.Errorf("talent count %d exceeds %d", talentCount, maxTalents)
		}
		group.Talents = make([]TalentInfo, 0, talentCount)
		for talentIndex := uint8(0); talentIndex < talentCount; talentIndex++ {
			var talent TalentInfo
			if talent.TalentID, err = r.u32(); err != nil {
				return data, fmt.Errorf("read talent %d/%d ID: %w", groupIndex, talentIndex, err)
			}
			if talent.Rank, err = r.u8(); err != nil {
				return data, fmt.Errorf("read talent %d/%d rank: %w", groupIndex, talentIndex, err)
			}
			group.Talents = append(group.Talents, talent)
		}
		glyphCount, readErr := r.u8()
		if readErr != nil {
			return data, fmt.Errorf("read glyph count for group %d: %w", groupIndex, readErr)
		}
		if glyphCount > maxGlyphs {
			return data, fmt.Errorf("glyph count %d exceeds %d", glyphCount, maxGlyphs)
		}
		group.Glyphs = make([]uint16, glyphCount)
		for glyphIndex := range group.Glyphs {
			if group.Glyphs[glyphIndex], err = r.u16(); err != nil {
				return data, fmt.Errorf("read glyph %d/%d: %w", groupIndex, glyphIndex, err)
			}
		}
		data.Groups = append(data.Groups, group)
	}
	if r.remaining() != 0 {
		return data, fmt.Errorf("talent-data has %d trailing bytes", r.remaining())
	}
	return data, nil
}

func EncodeTalentData(data LegacyTalentData) []byte {
	body := binary.LittleEndian.AppendUint32(nil, data.Unspent)
	body = append(body, data.Active)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(data.Groups)))
	for _, group := range data.Groups {
		body = append(body, byte(len(group.Talents)))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(group.Talents)))
		body = append(body, byte(len(group.Glyphs)))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(group.Glyphs)))
		body = append(body, 4) // MAX_SPECIALIZATIONS sentinel: no Cata spec
		for _, talent := range group.Talents {
			body = binary.LittleEndian.AppendUint32(body, talent.TalentID)
			body = append(body, talent.Rank)
		}
		for _, glyph := range group.Glyphs {
			body = binary.LittleEndian.AppendUint16(body, glyph)
		}
	}
	bits := newBitWriter(body)
	bits.writeBit(data.IsPet)
	return bits.flush()
}
