package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGStablePet          = uint16(0x3168)
	CMSGUnstablePet        = uint16(0x3169)
	CMSGStableSwapPet      = uint16(0x316A)
	CMSGBuyStableSlot      = uint16(0x316B)
	CMSGRequestStabledPets = uint16(0x3491)

	SMSGPetGuids        = uint16(0x2704)
	SMSGPetStableResult = uint16(0x2593)
	SMSGPetActionSound  = uint16(0x26A0)

	petStableFlagActive = uint8(1)
	// 54261 distinguishes current vs stabled with Flags, not PetSlot.
	// Reference capture: current Flags=1, stabled Flags=3,
	// both keep PetSlot=6. AC uses Flags=1/2 and PET_SAVE_AS_CURRENT=0.
	petStableFlagStabled  = uint8(3)
	petSaveCurrentSlot    = uint32(6)
	maxStabledPetList     = 32
	maxStablePetNameBytes = 255

	// AzerothCore StableResultCode success values. Failures stay on the
	// 1-byte SMSG_PET_STABLE_RESULT toast; success also needs a fresh
	// MSG_LIST_STABLED_PETS so 54261 can refresh NumStableSlots/StableInfo.
	PetStableSuccessStable   = uint8(0x08)
	PetStableSuccessUnstable = uint8(0x09)
	PetStableSuccessBuySlot  = uint8(0x0A)
)

func PetStableResultSucceeded(result uint8) bool {
	switch result {
	case PetStableSuccessStable, PetStableSuccessUnstable, PetStableSuccessBuySlot:
		return true
	default:
		return false
	}
}

// StablePetNumberRequest is CMSG_UNSTABLE_PET / CMSG_STABLE_SWAP_PET:
// uint32 PetNumber then PackedGuid128 StableMaster.
type StablePetNumberRequest struct {
	PetNumber uint32
	Master    GUID128
}

type LegacyStabledPet struct {
	PetNumber  uint32
	CreatureID uint32
	DisplayID  uint32
	Level      uint32
	Name       string
	Flags      uint8
	PetSlot    uint32
}

type LegacyStabledPets struct {
	Master         uint64
	NumStableSlots uint8
	Pets           []LegacyStabledPet
}

type LegacyPetActionSound struct {
	UnitGUID uint64
	Action   uint32
}

func ParseStableMaster(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseStablePetNumberRequest(body []byte) (StablePetNumberRequest, error) {
	var request StablePetNumberRequest
	if len(body) < 4 {
		return request, fmt.Errorf("stable-pet-number request has %d bytes, want at least 4", len(body))
	}
	request.PetNumber = binary.LittleEndian.Uint32(body[:4])
	master, err := ParsePackedGUID128Exact(body[4:])
	if err != nil {
		return request, fmt.Errorf("stable-pet-number master GUID: %w", err)
	}
	request.Master = master
	return request, nil
}

func EncodeLegacyStableMaster(guid uint64) []byte {
	return EncodeLegacyUnpackedGUID(guid)
}

func EncodeLegacyStablePetNumber(guid uint64, petNumber uint32) []byte {
	return binary.LittleEndian.AppendUint32(EncodeLegacyUnpackedGUID(guid), petNumber)
}

func EncodePetStableResult(result uint8) []byte {
	return []byte{result}
}

func ParseLegacyPetStableResult(body []byte) (uint8, error) {
	if len(body) != 1 {
		return 0, fmt.Errorf("pet-stable-result has %d bytes, want 1", len(body))
	}
	return body[0], nil
}

func ParseLegacyPetActionSound(body []byte) (LegacyPetActionSound, error) {
	var sound LegacyPetActionSound
	// AzerothCore PetActionSound::Write uses ObjectGuid << (unpacked uint64)
	// plus int32. Some forks pack the GUID; accept both and require an exact body.
	if len(body) == 12 {
		sound.UnitGUID = binary.LittleEndian.Uint64(body[:8])
		sound.Action = binary.LittleEndian.Uint32(body[8:12])
		return sound, nil
	}
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return sound, fmt.Errorf("pet-action-sound GUID: %w", err)
	}
	if sound.Action, err = r.u32(); err != nil {
		return sound, fmt.Errorf("pet-action-sound action: %w", err)
	}
	if r.remaining() != 0 {
		return sound, fmt.Errorf("pet-action-sound has %d trailing bytes", r.remaining())
	}
	sound.UnitGUID = guid
	return sound, nil
}

func EncodePetActionSound(unit GUID128, action uint32) []byte {
	body := appendPackedGUID128(nil, unit.Low, unit.High)
	return binary.LittleEndian.AppendUint32(body, action)
}

func EncodePetGuids(guids []GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(guids)))
	for _, guid := range guids {
		body = appendPackedGUID128(body, guid.Low, guid.High)
	}
	return body
}

func ParseLegacyStabledPets(body []byte) (LegacyStabledPets, error) {
	var list LegacyStabledPets
	r := movementReader{data: body}
	var err error
	if list.Master, err = r.u64(); err != nil {
		return list, fmt.Errorf("stabled-pets master GUID: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return list, fmt.Errorf("stabled-pets count: %w", err)
	}
	if list.NumStableSlots, err = r.u8(); err != nil {
		return list, fmt.Errorf("stabled-pets slot count: %w", err)
	}
	if int(count) > maxStabledPetList {
		return list, fmt.Errorf("stabled-pets count %d exceeds %d", count, maxStabledPetList)
	}
	list.Pets = make([]LegacyStabledPet, 0, count)
	for i := 0; i < int(count); i++ {
		var pet LegacyStabledPet
		if pet.PetNumber, err = r.u32(); err != nil {
			return list, fmt.Errorf("stabled-pet %d number: %w", i, err)
		}
		if pet.CreatureID, err = r.u32(); err != nil {
			return list, fmt.Errorf("stabled-pet %d creature: %w", i, err)
		}
		if pet.Level, err = r.u32(); err != nil {
			return list, fmt.Errorf("stabled-pet %d level: %w", i, err)
		}
		if pet.Name, err = r.cstring(); err != nil {
			return list, fmt.Errorf("stabled-pet %d name: %w", i, err)
		}
		if len(pet.Name) > maxStablePetNameBytes {
			return list, fmt.Errorf("stabled-pet %d name has %d bytes, want at most %d", i, len(pet.Name), maxStablePetNameBytes)
		}
		if pet.Flags, err = r.u8(); err != nil {
			return list, fmt.Errorf("stabled-pet %d flags: %w", i, err)
		}
		mapStablePetFor54261(&pet)
		list.Pets = append(list.Pets, pet)
	}
	if r.remaining() != 0 {
		return list, fmt.Errorf("stabled-pets has %d trailing bytes", r.remaining())
	}
	return list, nil
}

func AssignStablePetDisplays(list *LegacyStabledPets, displays map[uint32]uint32, currentDisplay uint32) []uint32 {
	if list == nil {
		return nil
	}
	var missing []uint32
	seen := make(map[uint32]struct{}, len(list.Pets))
	for i := range list.Pets {
		pet := &list.Pets[i]
		if pet.Flags == petStableFlagActive && currentDisplay != 0 {
			pet.DisplayID = currentDisplay
		}
		if pet.DisplayID == 0 && displays != nil {
			pet.DisplayID = displays[pet.CreatureID]
		}
		if pet.DisplayID != 0 || pet.CreatureID == 0 {
			continue
		}
		if _, dup := seen[pet.CreatureID]; dup {
			continue
		}
		seen[pet.CreatureID] = struct{}{}
		missing = append(missing, pet.CreatureID)
	}
	return missing
}

// mapStablePetFor54261 applies the reference capture layout:
// PetSlot stays 6 for current and stabled pets; AC Flags=2 becomes 3.
func mapStablePetFor54261(pet *LegacyStabledPet) {
	if pet == nil {
		return
	}
	pet.PetSlot = petSaveCurrentSlot
	if pet.Flags != petStableFlagActive {
		pet.Flags = petStableFlagStabled
	}
}

func stablePetModelID(pet LegacyStabledPet) uint32 {
	if pet.DisplayID != 0 {
		return pet.DisplayID
	}
	return pet.CreatureID
}

func LegacySummonGUID(fields map[int]uint32) uint64 {
	if fields == nil {
		return 0
	}
	return legacyActivePlayerValues{fields: fields}.legacyGUID(legacyUnitSummon)
}

func LegacyUnitDisplayID(fields map[int]uint32) uint32 {
	if fields == nil {
		return 0
	}
	return fields[legacyUnitDisplayID]
}

// EncodePetStableValuesUpdate writes an ActivePlayer values update that sets
// HasPetStable, NumStableSlots and the nested StableInfo payload. Group-102
// glyph padding in encodeActivePlayerFieldDeltas is left unchanged.
func EncodePetStableValuesUpdate(mapID uint16, player, master GUID128, list LegacyStabledPets) ([]byte, error) {
	if player.Low == 0 && player.High == 0 {
		return nil, fmt.Errorf("pet-stable player GUID is empty")
	}
	masterGUID := master
	if masterGUID.Low == 0 && masterGUID.High == 0 {
		masterGUID = ModernGUIDForLegacy(list.Master, mapID)
	}
	section := encodePetStableActivePlayerSection(list, masterGUID)
	valuesData := binary.LittleEndian.AppendUint32(nil, 0x80)
	valuesData = append(valuesData, section...)
	objectData := []byte{0}
	objectData = appendPackedGUID128(objectData, player.Low, player.High)
	objectData = binary.LittleEndian.AppendUint32(objectData, uint32(len(valuesData)))
	objectData = append(objectData, valuesData...)
	return encodeUpdateObjects(mapID, objectData), nil
}

func encodePetStableActivePlayerSection(list LegacyStabledPets, master GUID128) []byte {
	blocks := make([]uint32, 48)
	setMaskBit(blocks, playerAuraVisionParentBit)
	setMaskBit(blocks, 122)
	setMaskBit(blocks, 123)
	var mask0 uint32
	for index := 0; index < 32; index++ {
		if blocks[index] != 0 {
			mask0 |= 1 << index
		}
	}
	dst := binary.LittleEndian.AppendUint32(nil, mask0)
	bits := newBitWriter(dst)
	bits.writeBits(0, 16)
	for _, block := range blocks {
		if block != 0 {
			bits.writeBits(block, 32)
		}
	}
	dst = bits.flush()
	dst = append(dst, list.NumStableSlots)
	// HasPetStable is a single bit; the rest of the byte is discarded by
	// WPP's ResetBitReader before ReadUpdateStableInfo.
	dst = append(dst, 0x80)
	return append(dst, encodeStableInfoUpdate(list.Pets, master)...)
}

func encodeStableInfoUpdate(pets []LegacyStabledPet, master GUID128) []byte {
	bits := newBitWriter(nil)
	bits.writeBits(0x7, 3)
	bits.writeBits(uint32(len(pets)), 32)
	for range pets {
		bits.writeBit(true)
	}
	dst := bits.flush()
	for _, pet := range pets {
		dst = append(dst, encodeStablePetInfoUpdate(pet)...)
	}
	return appendPackedGUID128(dst, master.Low, master.High)
}

func encodeStablePetInfoUpdate(pet LegacyStabledPet) []byte {
	// 54261 UPDATE StablePetInfo writes four uint32s. Reference capture
	// seq 524 is PetSlot, CreatureID, DisplayID, Level: 6, 521 (Lupos), 11412, 69.
	// PetNumber is not in this 4-word UPDATE; putting AC pet number 6 in the
	// second word makes the client load creature 6 (Kobold Vermin).
	mapStablePetFor54261(&pet)
	mask := newBitWriter(nil)
	mask.writeBits(0xff, 8)
	dst := mask.flush()
	dst = binary.LittleEndian.AppendUint32(dst, pet.PetSlot)
	dst = binary.LittleEndian.AppendUint32(dst, pet.CreatureID)
	dst = binary.LittleEndian.AppendUint32(dst, stablePetModelID(pet))
	dst = binary.LittleEndian.AppendUint32(dst, pet.Level)
	dst = append(dst, pet.Flags, 0)
	nameBits := newBitWriter(dst)
	nameBits.writeBits(uint32(len(pet.Name)), 8)
	dst = nameBits.flush()
	return append(dst, pet.Name...)
}
