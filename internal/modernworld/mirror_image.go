package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	// CMSGGetMirrorImageData is WPP V3_4_3_54261 0x3297. Hermes Opcode.cs
	// left it at 0; legacy proxy still has a live handler that forwards the query.
	CMSGGetMirrorImageData = uint16(0x3297)
	// SMSGMirrorImageCreatureData and SMSGMirrorImageComponentedData are
	// 0x2C13 / 0x2C14, the same numbers legacy proxy writes after translating
	// 3.3.5 SMSG_MIRROR_IMAGE_DATA (0x402).
	SMSGMirrorImageCreatureData    = uint16(0x2C13)
	SMSGMirrorImageComponentedData = uint16(0x2C14)
	legacyMirrorImageItemSlots     = 11
	legacyMirrorImageMaxItemSlots  = 19
	// Reference capture: legacy proxy hardcodes ItemDisplayCount=12 then
	// writes 19 uint32 display IDs (the 3.3.5 EQUIPMENT_SLOT_END array).
	modernMirrorImageItemCount = 12
)

type MirrorImageData struct {
	GUID          uint64
	DisplayID     uint32
	Race          byte
	Gender        byte
	Class         byte
	Skin          byte
	Face          byte
	HairStyle     byte
	HairColor     byte
	FacialHair    byte
	GuildID       uint32
	ItemDisplayID []uint32
	HasAppearance bool
}

// ParseGetMirrorImageData reads 54261 CMSG_GET_MIRROR_IMAGE_DATA. legacy proxy only
// consumes PackedGuid128; 3.4.4 added a trailing DisplayID, which 54261 may
// also send. Either form is accepted. The DisplayID is not forwarded: 3.3.5
// CMSG_GET_MIRROR_IMAGE_DATA is GUID-only.
func ParseGetMirrorImageData(body []byte) (GUID128, error) {
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return GUID128{}, fmt.Errorf("read mirror-image GUID: %w", err)
	}
	rest := len(body) - consumed
	if rest != 0 && rest != 4 {
		return GUID128{}, fmt.Errorf("get-mirror-image-data has %d trailing bytes", rest)
	}
	return GUID128{Low: low, High: high}, nil
}

func EncodeLegacyGetMirrorImageData(guid uint64) []byte {
	// AzerothCore HandleMirrorImageDataRequest does recvData >> guid, a
	// raw uint64. legacy proxy's WriteGuid is the same 8-byte store. Packed GUID
	// is shorter than 8 bytes and trips ByteBufferException on opcode 1025.
	return EncodeLegacyUnpackedGUID(guid)
}

// IsLegacyMirrorImageCreature reports UNIT_FLAG2_MIRROR_IMAGE on a creature.
// Create keeps that bit so 54261 will send CMSG_GET_MIRROR_IMAGE_DATA; the
// proxy also asks the legacy server itself in case the client query is late.
func IsLegacyMirrorImageCreature(objectType uint8, fields map[int]uint32) bool {
	if objectType != 3 || fields == nil {
		return false
	}
	return fields[legacyUnitFlags2]&0x10 != 0
}

func ParseLegacyMirrorImageData(body []byte) (MirrorImageData, error) {
	var data MirrorImageData
	r := movementReader{data: body}
	guid, err := r.u64()
	if err != nil {
		return data, fmt.Errorf("read mirror-image GUID: %w", err)
	}
	data.GUID = guid
	if data.DisplayID, err = r.u32(); err != nil {
		return data, fmt.Errorf("read mirror-image display id: %w", err)
	}
	if r.remaining() == 0 {
		return data, nil
	}
	if r.remaining() < 12 {
		return data, fmt.Errorf("mirror-image appearance has %d bytes, want at least 12", r.remaining())
	}
	if data.Race, err = r.u8(); err != nil {
		return data, fmt.Errorf("read mirror-image race: %w", err)
	}
	if data.Gender, err = r.u8(); err != nil {
		return data, fmt.Errorf("read mirror-image gender: %w", err)
	}
	if data.Class, err = r.u8(); err != nil {
		return data, fmt.Errorf("read mirror-image class: %w", err)
	}
	if data.Skin, err = r.u8(); err != nil {
		return data, fmt.Errorf("read mirror-image skin: %w", err)
	}
	if data.Face, err = r.u8(); err != nil {
		return data, fmt.Errorf("read mirror-image face: %w", err)
	}
	if data.HairStyle, err = r.u8(); err != nil {
		return data, fmt.Errorf("read mirror-image hair style: %w", err)
	}
	if data.HairColor, err = r.u8(); err != nil {
		return data, fmt.Errorf("read mirror-image hair color: %w", err)
	}
	if data.FacialHair, err = r.u8(); err != nil {
		return data, fmt.Errorf("read mirror-image facial hair: %w", err)
	}
	if data.GuildID, err = r.u32(); err != nil {
		return data, fmt.Errorf("read mirror-image guild: %w", err)
	}
	data.HasAppearance = true
	for r.remaining() >= 4 && len(data.ItemDisplayID) < legacyMirrorImageMaxItemSlots {
		item, readErr := r.u32()
		if readErr != nil {
			return data, fmt.Errorf("read mirror-image item display: %w", readErr)
		}
		data.ItemDisplayID = append(data.ItemDisplayID, item)
	}
	if r.remaining() != 0 {
		return data, fmt.Errorf("mirror-image data has %d trailing bytes", r.remaining())
	}
	return data, nil
}

// TranslateLegacyMirrorImageData converts 3.3.5 SMSG_MIRROR_IMAGE_DATA into
// either SMSG_MIRROR_IMAGE_COMPONENTED_DATA (player copies) or
// SMSG_MIRROR_IMAGE_CREATURE_DATA (display-only clones). The componented
// layout matches legacy proxy / WPP 3.4.3: GUID, DisplayID, race/gender/class,
// customization count, guild GUID, item count, then the customization pairs
// and item display IDs.
func TranslateLegacyMirrorImageData(body []byte, resolve func(uint64) GUID128) (uint16, []byte, error) {
	data, err := ParseLegacyMirrorImageData(body)
	if err != nil {
		return 0, nil, err
	}
	unit := resolve(data.GUID)
	if !data.HasAppearance {
		return SMSGMirrorImageCreatureData, encodeMirrorImageCreatureData(unit, data.DisplayID), nil
	}
	return SMSGMirrorImageComponentedData, encodeMirrorImageComponentedData(unit, data), nil
}

func encodeMirrorImageCreatureData(unit GUID128, displayID uint32) []byte {
	body := appendPackedGUID128(nil, unit.Low, unit.High)
	body = binary.LittleEndian.AppendUint32(body, displayID)
	return binary.LittleEndian.AppendUint32(body, 0)
}

func encodeMirrorImageComponentedData(unit GUID128, data MirrorImageData) []byte {
	customizations := modernCustomizations(LegacyCharacter{
		Race: data.Race, Class: data.Class, Sex: data.Gender,
		Skin: data.Skin, Face: data.Face,
		HairStyle: data.HairStyle, HairColor: data.HairColor,
		FacialHair: data.FacialHair,
	})
	items := make([]uint32, legacyMirrorImageMaxItemSlots)
	copy(items, data.ItemDisplayID)
	body := appendPackedGUID128(nil, unit.Low, unit.High)
	body = binary.LittleEndian.AppendUint32(body, data.DisplayID)
	body = append(body, data.Race, data.Gender, data.Class)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(customizations)))
	guild := modernMirrorGuildGUID(data.GuildID)
	body = appendPackedGUID128(body, guild.Low, guild.High)
	body = binary.LittleEndian.AppendUint32(body, modernMirrorImageItemCount)
	for _, pair := range customizations {
		body = binary.LittleEndian.AppendUint32(body, pair[0])
		body = binary.LittleEndian.AppendUint32(body, pair[1])
	}
	for _, item := range items {
		body = binary.LittleEndian.AppendUint32(body, item)
	}
	return body
}

func modernMirrorGuildGUID(guildID uint32) GUID128 {
	if guildID == 0 {
		return GUID128{}
	}
	return GUID128{Low: uint64(guildID), High: uint64(28)<<58 | uint64(1)<<42}
}
