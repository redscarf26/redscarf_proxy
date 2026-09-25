package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

const (
	legacyCharacterVisualItems = 23
	modernCharacterVisualItems = 34
	characterFlagDeclined      = uint32(0x02000000)
)

type LegacyVisualItem struct {
	DisplayID        uint32
	InventoryType    byte
	DisplayEnchantID uint32
}

type LegacyCharacter struct {
	GUID          uint64
	Name          string
	Race          byte
	Class         byte
	Sex           byte
	Skin          byte
	Face          byte
	HairStyle     byte
	HairColor     byte
	FacialHair    byte
	Level         byte
	ZoneID        uint32
	MapID         uint32
	Position      [3]float32
	GuildID       uint32
	Flags         uint32
	Customization uint32
	FirstLogin    bool
	PetDisplayID  uint32
	PetLevel      uint32
	PetFamilyID   uint32
	VisualItems   [legacyCharacterVisualItems]LegacyVisualItem
}

func ParseLegacyCharacterEnum(body []byte) ([]LegacyCharacter, error) {
	reader := legacyCharacterReader{data: body}
	count, err := reader.uint8()
	if err != nil {
		return nil, fmt.Errorf("read legacy character count: %w", err)
	}
	characters := make([]LegacyCharacter, 0, count)
	for index := 0; index < int(count); index++ {
		character, err := reader.character()
		if err != nil {
			return nil, fmt.Errorf("read legacy character %d: %w", index, err)
		}
		characters = append(characters, character)
	}
	if reader.position != len(reader.data) {
		return nil, fmt.Errorf("legacy character enum has %d trailing bytes", len(reader.data)-reader.position)
	}
	return characters, nil
}

// TranslateCharacterEnum converts AzerothCore's 3.3.5a SMSG_CHAR_ENUM body to
// the build-54261 SMSG_ENUM_CHARACTERS_RESULT body. The five legacy appearance
// bytes are mapped to their 3.4.3 ChrCustomizationOption/Choice identifiers.
func TranslateCharacterEnum(body []byte, now time.Time) ([]byte, error) {
	characters, err := ParseLegacyCharacterEnum(body)
	if err != nil {
		return nil, err
	}
	maxLevel := byte(1)
	for _, character := range characters {
		if character.Level > maxLevel {
			maxLevel = character.Level
		}
	}

	bits := newBitWriter(nil)
	bits.writeBit(true)  // success
	bits.writeBit(false) // deleted characters
	bits.writeBit(false) // new-player restriction skipped
	bits.writeBit(false) // new-player restricted
	bits.writeBit(false) // new player
	bits.writeBit(false) // trial account restricted
	bits.writeBit(true)  // disabled-classes mask follows
	result := bits.flush()
	result = binary.LittleEndian.AppendUint32(result, uint32(len(characters)))
	result = binary.LittleEndian.AppendUint32(result, uint32(maxLevel))
	races := []int32{1, 2, 3, 4, 5, 6, 7, 8, 10, 11}
	result = binary.LittleEndian.AppendUint32(result, uint32(len(races)))
	result = binary.LittleEndian.AppendUint32(result, 0) // conditional appearances
	result = binary.LittleEndian.AppendUint32(result, 0) // race-limit disables
	result = binary.LittleEndian.AppendUint32(result, 0) // disabled-classes mask

	for index, character := range characters {
		result = appendModernCharacter(result, character, byte(index), now)
	}
	for _, raceID := range races {
		result = binary.LittleEndian.AppendUint32(result, uint32(raceID))
		result = append(result, 0x80) // has expansion; remaining four flags false
	}
	return result, nil
}

func appendModernCharacter(result []byte, character LegacyCharacter, listPosition byte, now time.Time) []byte {
	customizations := modernCustomizations(character)
	playerCounter := uint64(uint32(character.GUID))
	playerHigh := uint64(2)<<58 | uint64(1)<<42
	result = appendPackedGUID128(result, playerCounter, playerHigh)
	result = binary.LittleEndian.AppendUint64(result, 0) // guild club member ID
	result = append(result, listPosition, character.Race, character.Class, character.Sex)
	result = binary.LittleEndian.AppendUint32(result, uint32(len(customizations)))
	result = append(result, character.Level)
	result = binary.LittleEndian.AppendUint32(result, character.ZoneID)
	result = binary.LittleEndian.AppendUint32(result, character.MapID)
	for _, coordinate := range character.Position {
		result = binary.LittleEndian.AppendUint32(result, math.Float32bits(coordinate))
	}
	if character.GuildID == 0 {
		result = appendPackedGUID128(result, 0, 0)
	} else {
		guildHigh := uint64(28)<<58 | uint64(1)<<42
		result = appendPackedGUID128(result, uint64(character.GuildID), guildHigh)
	}
	result = binary.LittleEndian.AppendUint32(result, character.Flags&^characterFlagDeclined)
	result = binary.LittleEndian.AppendUint32(result, 0) // modern flags2
	result = binary.LittleEndian.AppendUint32(result, 0) // modern flags3
	result = binary.LittleEndian.AppendUint32(result, character.PetDisplayID)
	result = binary.LittleEndian.AppendUint32(result, character.PetLevel)
	result = binary.LittleEndian.AppendUint32(result, character.PetFamilyID)
	result = binary.LittleEndian.AppendUint32(result, 0) // profession 1
	result = binary.LittleEndian.AppendUint32(result, 0) // profession 2
	for index := 0; index < modernCharacterVisualItems; index++ {
		var visual LegacyVisualItem
		if index < len(character.VisualItems) {
			visual = character.VisualItems[index]
		}
		result = binary.LittleEndian.AppendUint32(result, visual.DisplayID)
		result = binary.LittleEndian.AppendUint32(result, visual.DisplayEnchantID)
		result = binary.LittleEndian.AppendUint32(result, 0) // secondary modified appearance
		result = append(result, visual.InventoryType, 0)     // subclass
	}
	result = binary.LittleEndian.AppendUint64(result, uint64(now.Unix()))
	result = binary.LittleEndian.AppendUint16(result, 0)     // specialization ID
	result = binary.LittleEndian.AppendUint32(result, 0)     // save version
	result = binary.LittleEndian.AppendUint32(result, 12340) // last login build
	result = binary.LittleEndian.AppendUint32(result, 0)     // restriction flags
	result = binary.LittleEndian.AppendUint32(result, 0)     // mail senders
	result = binary.LittleEndian.AppendUint32(result, 0)     // mail sender types
	result = binary.LittleEndian.AppendUint32(result, 0)     // select-screen override
	for _, customization := range customizations {
		result = binary.LittleEndian.AppendUint32(result, customization[0])
		result = binary.LittleEndian.AppendUint32(result, customization[1])
	}

	name := character.Name
	if len(name) > 63 {
		name = name[:63]
	}
	bits := newBitWriter(result)
	bits.writeBits(uint32(len(name)), 6)
	bits.writeBit(character.FirstLogin)
	bits.writeBit(false) // boost in progress
	bits.writeBits(0, 5) // cannot-login reason
	bits.writeBits(0, 2)
	bits.writeBit(false) // RPE reset
	bits.writeBit(false) // RPE reset quest clear
	result = bits.flush()
	return append(result, name...)
}

func modernCustomizations(character LegacyCharacter) [][2]uint32 {
	key := uint16(character.Race)<<8 | uint16(character.Sex)
	options, ok := legacyCustomizationOptions[key]
	if !ok {
		return nil
	}
	legacyValues := [5]byte{character.Skin, character.Face, character.HairStyle, character.HairColor, character.FacialHair}
	result := make([][2]uint32, 0, len(options))
	for index, optionID := range options {
		choices := modernCustomizationChoices[optionID]
		legacyValue := int(legacyValues[index])
		var choiceID uint32
		if legacyValue < len(choices) {
			choiceID = choices[legacyValue]
		}
		result = append(result, [2]uint32{optionID, choiceID})
	}
	return result
}

func appendPackedGUID128(destination []byte, low, high uint64) []byte {
	lowMask, lowBytes := packUint64(low)
	highMask, highBytes := packUint64(high)
	destination = append(destination, lowMask, highMask)
	destination = append(destination, lowBytes...)
	return append(destination, highBytes...)
}

func packUint64(value uint64) (byte, []byte) {
	var mask byte
	packed := make([]byte, 0, 8)
	for index := 0; index < 8; index++ {
		current := byte(value >> (8 * index))
		if current != 0 {
			mask |= 1 << index
			packed = append(packed, current)
		}
	}
	return mask, packed
}

type legacyCharacterReader struct {
	data     []byte
	position int
}

func (r *legacyCharacterReader) character() (LegacyCharacter, error) {
	var character LegacyCharacter
	var err error
	if character.GUID, err = r.uint64(); err != nil {
		return character, err
	}
	if character.Name, err = r.cstring(); err != nil {
		return character, err
	}
	byteFields := []*byte{&character.Race, &character.Class, &character.Sex, &character.Skin, &character.Face, &character.HairStyle, &character.HairColor, &character.FacialHair, &character.Level}
	for _, field := range byteFields {
		if *field, err = r.uint8(); err != nil {
			return character, err
		}
	}
	uintFields := []*uint32{&character.ZoneID, &character.MapID}
	for _, field := range uintFields {
		if *field, err = r.uint32(); err != nil {
			return character, err
		}
	}
	for index := range character.Position {
		value, readErr := r.uint32()
		if readErr != nil {
			return character, readErr
		}
		character.Position[index] = math.Float32frombits(value)
	}
	uintFields = []*uint32{&character.GuildID, &character.Flags, &character.Customization}
	for _, field := range uintFields {
		if *field, err = r.uint32(); err != nil {
			return character, err
		}
	}
	firstLogin, err := r.uint8()
	if err != nil {
		return character, err
	}
	character.FirstLogin = firstLogin != 0
	uintFields = []*uint32{&character.PetDisplayID, &character.PetLevel, &character.PetFamilyID}
	for _, field := range uintFields {
		if *field, err = r.uint32(); err != nil {
			return character, err
		}
	}
	for index := range character.VisualItems {
		visual := &character.VisualItems[index]
		if visual.DisplayID, err = r.uint32(); err != nil {
			return character, err
		}
		if visual.InventoryType, err = r.uint8(); err != nil {
			return character, err
		}
		if visual.DisplayEnchantID, err = r.uint32(); err != nil {
			return character, err
		}
	}
	return character, nil
}

func (r *legacyCharacterReader) take(count int) ([]byte, error) {
	if count < 0 || r.position+count > len(r.data) {
		return nil, fmt.Errorf("need %d bytes at offset %d, body has %d", count, r.position, len(r.data))
	}
	value := r.data[r.position : r.position+count]
	r.position += count
	return value, nil
}

func (r *legacyCharacterReader) uint8() (byte, error) {
	value, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return value[0], nil
}

func (r *legacyCharacterReader) uint32() (uint32, error) {
	value, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(value), nil
}

func (r *legacyCharacterReader) uint64() (uint64, error) {
	value, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(value), nil
}

func (r *legacyCharacterReader) cstring() (string, error) {
	start := r.position
	for r.position < len(r.data) && r.data[r.position] != 0 {
		r.position++
	}
	if r.position == len(r.data) {
		return "", fmt.Errorf("unterminated string at offset %d", start)
	}
	value := string(r.data[start:r.position])
	r.position++
	return value, nil
}
