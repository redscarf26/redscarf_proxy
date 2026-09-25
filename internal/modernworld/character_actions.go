package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGGenerateRandomCharacterName = uint16(13800)
	CMSGCreateCharacter             = uint16(13893)
	CMSGCharDelete                  = uint16(13981)
	SMSGGenerateRandomCharacterName = uint16(9605)
	SMSGCreateCharacter             = uint16(9985)
	SMSGDeleteCharacter             = uint16(9986)
)

type CreateCharacterRequest struct {
	Name           string
	Race           byte
	Class          byte
	Sex            byte
	Skin           byte
	Face           byte
	HairStyle      byte
	HairColor      byte
	FacialHair     byte
	TemplateSet    *uint32
	IsTrialBoost   bool
	UseNewPlayerXP bool
}

func ParseCreateCharacter(body []byte) (CreateCharacterRequest, error) {
	var request CreateCharacterRequest
	if len(body) < 9 {
		return request, fmt.Errorf("create-character packet has %d bytes, want at least 9", len(body))
	}
	nameLength := int(body[0] >> 2)
	hasTemplate := body[0]&0x02 != 0
	request.IsTrialBoost = body[0]&0x01 != 0
	request.UseNewPlayerXP = body[1]&0x80 != 0
	position := 2
	request.Race = body[position]
	request.Class = body[position+1]
	request.Sex = body[position+2]
	position += 3
	customizationCount := int(binary.LittleEndian.Uint32(body[position : position+4]))
	position += 4
	if customizationCount > 50 {
		return request, fmt.Errorf("create-character customization count %d exceeds 50", customizationCount)
	}
	if nameLength == 0 || nameLength > 24 || position+nameLength > len(body) {
		return request, fmt.Errorf("create-character name length %d is invalid", nameLength)
	}
	request.Name = string(body[position : position+nameLength])
	position += nameLength
	if hasTemplate {
		if position+4 > len(body) {
			return request, fmt.Errorf("create-character template is truncated")
		}
		value := binary.LittleEndian.Uint32(body[position : position+4])
		request.TemplateSet = &value
		position += 4
	}
	if position+customizationCount*8 != len(body) {
		return request, fmt.Errorf("create-character packet has %d trailing/missing bytes", len(body)-position-customizationCount*8)
	}
	options, ok := legacyCustomizationOptions[uint16(request.Race)<<8|uint16(request.Sex)]
	if !ok {
		return request, fmt.Errorf("unsupported create-character race/sex %d/%d", request.Race, request.Sex)
	}
	legacyValues := [5]*byte{&request.Skin, &request.Face, &request.HairStyle, &request.HairColor, &request.FacialHair}
	for index := 0; index < customizationCount; index++ {
		optionID := binary.LittleEndian.Uint32(body[position : position+4])
		choiceID := binary.LittleEndian.Uint32(body[position+4 : position+8])
		position += 8
		for optionIndex, expectedOption := range options {
			if optionID != expectedOption {
				continue
			}
			choices := modernCustomizationChoices[optionID]
			for legacyValue, expectedChoice := range choices {
				if choiceID == expectedChoice {
					*legacyValues[optionIndex] = byte(legacyValue)
					break
				}
			}
			break
		}
	}
	return request, nil
}

func EncodeLegacyCreateCharacter(request CreateCharacterRequest) []byte {
	body := append([]byte(nil), request.Name...)
	body = append(body, 0)
	body = append(body, request.Race, request.Class, request.Sex)
	body = append(body, request.Skin, request.Face, request.HairStyle, request.HairColor, request.FacialHair)
	return append(body, 0) // outfit ID
}

func ParseCharacterGUID(body []byte) (uint64, error) {
	low, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		return 0, err
	}
	if consumed != len(body) {
		return 0, fmt.Errorf("character GUID packet has %d trailing bytes", len(body)-consumed)
	}
	return uint64(uint32(low)), nil
}

func EncodeCreateCharacterResult(legacyCode byte, legacyGUID uint64) []byte {
	body := []byte{ConvertLegacyResponseCode(legacyCode)}
	if legacyGUID == 0 {
		return appendPackedGUID128(body, 0, 0)
	}
	high := uint64(2)<<58 | uint64(1)<<42
	return appendPackedGUID128(body, uint64(uint32(legacyGUID)), high)
}

func EncodeDeleteCharacterResult(legacyCode byte) []byte {
	return []byte{ConvertLegacyResponseCode(legacyCode)}
}

func EncodeRandomCharacterNameUnavailable() []byte {
	return []byte{0, 0} // bool success=false, empty 6-bit name length
}

func ConvertLegacyResponseCode(code byte) byte {
	if modern, ok := legacyResponseCodes[code]; ok {
		return modern
	}
	return code
}

var legacyResponseCodes = map[byte]byte{
	47: 24, 48: 25, 49: 26, 50: 27, 51: 28, 52: 29, 53: 30, 54: 31,
	55: 32, 56: 33, 57: 34, 58: 35, 59: 26, 60: 26, 61: 36, 62: 37,
	63: 38, 64: 39, 65: 40, 66: 41, 67: 42, 68: 43, 69: 44,
	70: 62, 71: 63, 72: 64, 73: 65, 74: 66, 75: 67,
	76: 73, 77: 74, 78: 75, 79: 76, 80: 77, 81: 78, 82: 79, 83: 80,
	84: 81, 85: 82,
	87: 90, 88: 91, 89: 92, 90: 93, 91: 94, 92: 95, 93: 96, 94: 97,
	95: 98, 96: 99, 97: 100, 98: 101, 99: 102, 100: 103, 101: 104,
	102: 105, 103: 106,
}

func readPackedGUID128(body []byte) (low, high uint64, consumed int, err error) {
	if len(body) < 2 {
		return 0, 0, 0, fmt.Errorf("packed GUID has %d bytes, want at least 2", len(body))
	}
	lowMask, highMask := body[0], body[1]
	position := 2
	for index := 0; index < 8; index++ {
		if lowMask&(1<<index) == 0 {
			continue
		}
		if position >= len(body) {
			return 0, 0, 0, fmt.Errorf("packed GUID low value is truncated")
		}
		low |= uint64(body[position]) << (8 * index)
		position++
	}
	for index := 0; index < 8; index++ {
		if highMask&(1<<index) == 0 {
			continue
		}
		if position >= len(body) {
			return 0, 0, 0, fmt.Errorf("packed GUID high value is truncated")
		}
		high |= uint64(body[position]) << (8 * index)
		position++
	}
	return low, high, position, nil
}
