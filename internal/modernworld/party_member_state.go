package modernworld

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

const maxPartyMemberAuras = 64

type PartyMemberAuraState struct {
	SpellID     int32
	Flags       uint16
	ActiveFlags uint32
}

type PartyMemberPetPartialState struct {
	LegacyGUID    *uint64
	GUID          *GUID128
	Name          *string
	ModelID       *int32
	CurrentHealth *int32
	MaxHealth     *int32
	Auras         *[]PartyMemberAuraState
}

type PartyMemberPartialState struct {
	LegacyGUID    uint64
	MemberGUID    GUID128
	Status        *uint16
	PowerType     *byte
	CurrentHealth *int32
	MaxHealth     *int32
	CurrentPower  *uint16
	MaxPower      *uint16
	Level         *uint16
	ZoneID        *uint16
	Position      *[3]int16
	VehicleSeat   *int32
	Auras         *[]PartyMemberAuraState
	Pet           *PartyMemberPetPartialState
}

type PartyMemberFullState struct {
	ForEnemy bool
	Member   PartyMemberPartialState
}

func ParseLegacyPartyMemberFullState(body []byte) (PartyMemberFullState, error) {
	var full PartyMemberFullState
	if len(body) == 0 {
		return full, fmt.Errorf("read party-member full-update flag: packet is empty")
	}
	full.ForEnemy = body[0] != 0
	member, err := ParseLegacyPartyMemberPartialState(body[1:])
	if err != nil {
		return full, err
	}
	full.Member = member
	return full, nil
}

func ParseLegacyPartyMemberPartialState(body []byte) (PartyMemberPartialState, error) {
	var state PartyMemberPartialState
	r := movementReader{data: body}
	legacyGUID, err := r.guid64()
	if err != nil {
		return state, fmt.Errorf("read party-member GUID: %w", err)
	}
	state.LegacyGUID = legacyGUID
	flags, err := r.u32()
	if err != nil {
		return state, fmt.Errorf("read party-member flags: %w", err)
	}
	if flags&0x000001 != 0 {
		value, readErr := r.u16()
		if readErr != nil {
			return state, fmt.Errorf("read party-member status: %w", readErr)
		}
		state.Status = &value
	}
	if flags&0x000002 != 0 {
		value, readErr := r.i32()
		if readErr != nil {
			return state, fmt.Errorf("read party-member current health: %w", readErr)
		}
		state.CurrentHealth = &value
	}
	if flags&0x000004 != 0 {
		value, readErr := r.i32()
		if readErr != nil {
			return state, fmt.Errorf("read party-member maximum health: %w", readErr)
		}
		state.MaxHealth = &value
	}
	if flags&0x000008 != 0 {
		value, readErr := r.u8()
		if readErr != nil {
			return state, fmt.Errorf("read party-member power type: %w", readErr)
		}
		state.PowerType = &value
	}
	if flags&0x000010 != 0 {
		value, readErr := r.u16()
		if readErr != nil {
			return state, fmt.Errorf("read party-member current power: %w", readErr)
		}
		state.CurrentPower = &value
	}
	if flags&0x000020 != 0 {
		value, readErr := r.u16()
		if readErr != nil {
			return state, fmt.Errorf("read party-member maximum power: %w", readErr)
		}
		state.MaxPower = &value
	}
	if flags&0x000040 != 0 {
		value, readErr := r.u16()
		if readErr != nil {
			return state, fmt.Errorf("read party-member level: %w", readErr)
		}
		state.Level = &value
	}
	if flags&0x000080 != 0 {
		value, readErr := r.u16()
		if readErr != nil {
			return state, fmt.Errorf("read party-member zone: %w", readErr)
		}
		state.ZoneID = &value
	}
	if flags&0x000100 != 0 {
		x, readErr := r.u16()
		if readErr != nil {
			return state, fmt.Errorf("read party-member position X: %w", readErr)
		}
		y, readErr := r.u16()
		if readErr != nil {
			return state, fmt.Errorf("read party-member position Y: %w", readErr)
		}
		state.Position = &[3]int16{int16(x), int16(y), 0}
	}
	if flags&0x000200 != 0 {
		auras, readErr := parseLegacyPartyMemberAuras(&r, "party-member")
		if readErr != nil {
			return state, readErr
		}
		if len(auras) != 0 {
			state.Auras = &auras
		}
	}
	petFlags := flags & 0x07fc00
	if petFlags != 0 {
		state.Pet = &PartyMemberPetPartialState{}
	}
	if flags&0x000400 != 0 {
		value, readErr := r.u64()
		if readErr != nil {
			return state, fmt.Errorf("read party-member pet GUID: %w", readErr)
		}
		state.Pet.LegacyGUID = &value
	}
	if flags&0x000800 != 0 {
		value, readErr := r.cstring()
		if readErr != nil {
			return state, fmt.Errorf("read party-member pet name: %w", readErr)
		}
		if len(value) > 255 {
			return state, fmt.Errorf("party-member pet name has %d bytes", len(value))
		}
		state.Pet.Name = &value
	}
	if flags&0x001000 != 0 {
		value, readErr := r.u16()
		if readErr != nil {
			return state, fmt.Errorf("read party-member pet model: %w", readErr)
		}
		modelID := int32(value)
		state.Pet.ModelID = &modelID
	}
	if flags&0x002000 != 0 {
		value, readErr := r.i32()
		if readErr != nil {
			return state, fmt.Errorf("read party-member pet current health: %w", readErr)
		}
		state.Pet.CurrentHealth = &value
	}
	if flags&0x004000 != 0 {
		value, readErr := r.i32()
		if readErr != nil {
			return state, fmt.Errorf("read party-member pet maximum health: %w", readErr)
		}
		state.Pet.MaxHealth = &value
	}
	if flags&0x008000 != 0 {
		if _, err = r.u8(); err != nil { // no modern PartyMemberPetStats equivalent
			return state, fmt.Errorf("read party-member pet power type: %w", err)
		}
	}
	if flags&0x010000 != 0 {
		if _, err = r.u16(); err != nil {
			return state, fmt.Errorf("read party-member pet current power: %w", err)
		}
	}
	if flags&0x020000 != 0 {
		if _, err = r.u16(); err != nil {
			return state, fmt.Errorf("read party-member pet maximum power: %w", err)
		}
	}
	if flags&0x040000 != 0 {
		auras, readErr := parseLegacyPartyMemberAuras(&r, "party-member pet")
		if readErr != nil {
			return state, readErr
		}
		if len(auras) != 0 {
			state.Pet.Auras = &auras
		}
	}
	if flags&0x080000 != 0 {
		value, readErr := r.i32()
		if readErr != nil {
			return state, fmt.Errorf("read party-member vehicle seat: %w", readErr)
		}
		state.VehicleSeat = &value
	}
	if r.remaining() != 0 {
		return state, fmt.Errorf("party-member partial state has %d trailing bytes", r.remaining())
	}
	return state, nil
}

func parseLegacyPartyMemberAuras(r *movementReader, label string) ([]PartyMemberAuraState, error) {
	mask, err := r.u64()
	if err != nil {
		return nil, fmt.Errorf("read %s aura mask: %w", label, err)
	}
	auras := make([]PartyMemberAuraState, 0, bits.OnesCount64(mask))
	for slot := uint(0); slot < maxPartyMemberAuras; slot++ {
		if mask&(uint64(1)<<slot) == 0 {
			continue
		}
		spellID, readErr := r.u32()
		if readErr != nil {
			return nil, fmt.Errorf("read %s aura %d spell: %w", label, slot, readErr)
		}
		legacyFlags, readErr := r.u8()
		if readErr != nil {
			return nil, fmt.Errorf("read %s aura %d flags: %w", label, slot, readErr)
		}
		if spellID == 0 {
			continue
		}
		// legacy proxy filters mounts from group stats (both full and partial).
		// Remote mount tooltip entries can crash build 54261; ordinary nearby
		// unit aura updates use a separate path and must retain their mounts.
		if _, mount := partyMountAuras[int32(spellID)]; mount {
			continue
		}
		auras = append(auras, PartyMemberAuraState{
			SpellID:     int32(spellID),
			Flags:       convertAuraFlags(legacyFlags),
			ActiveFlags: convertAuraActiveFlags(legacyFlags),
		})
	}
	return auras, nil
}

func EncodePartyMemberPartialState(state PartyMemberPartialState) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(false) // ForEnemy
	bits.writeBit(false) // FullUpdate
	bits.writeBit(false) // FromServer
	bits.writeBit(false) // PartyType
	bits.writeBit(state.Status != nil)
	bits.writeBit(state.PowerType != nil)
	bits.writeBit(false) // PowerDisplayID
	bits.writeBit(state.CurrentHealth != nil)
	bits.writeBit(state.MaxHealth != nil)
	bits.writeBit(state.CurrentPower != nil)
	bits.writeBit(state.MaxPower != nil)
	bits.writeBit(state.Level != nil)
	bits.writeBit(false) // SpecID
	bits.writeBit(state.ZoneID != nil)
	bits.writeBit(false) // WmoGroupID
	bits.writeBit(false) // WmoDoodadPlacementID
	bits.writeBit(state.Position != nil)
	bits.writeBit(state.VehicleSeat != nil)
	bits.writeBit(state.Auras != nil)
	bits.writeBit(state.Pet != nil)
	bits.writeBit(false) // PhaseStates
	bits.writeBit(false) // ChromieTime/CTROptions
	body := bits.flush()
	if state.Pet != nil {
		body = encodePartyMemberPetPartialState(body, *state.Pet)
	}
	body = appendPackedGUID128(body, state.MemberGUID.Low, state.MemberGUID.High)
	if state.Status != nil {
		body = binary.LittleEndian.AppendUint16(body, *state.Status)
	}
	if state.PowerType != nil {
		body = append(body, *state.PowerType)
	}
	if state.CurrentHealth != nil {
		body = binary.LittleEndian.AppendUint32(body, uint32(*state.CurrentHealth))
	}
	if state.MaxHealth != nil {
		body = binary.LittleEndian.AppendUint32(body, uint32(*state.MaxHealth))
	}
	if state.CurrentPower != nil {
		body = binary.LittleEndian.AppendUint16(body, *state.CurrentPower)
	}
	if state.MaxPower != nil {
		body = binary.LittleEndian.AppendUint16(body, *state.MaxPower)
	}
	if state.Level != nil {
		body = binary.LittleEndian.AppendUint16(body, *state.Level)
	}
	if state.ZoneID != nil {
		body = binary.LittleEndian.AppendUint16(body, *state.ZoneID)
	}
	if state.Position != nil {
		for _, coordinate := range state.Position {
			body = binary.LittleEndian.AppendUint16(body, uint16(coordinate))
		}
	}
	if state.VehicleSeat != nil {
		body = binary.LittleEndian.AppendUint32(body, uint32(*state.VehicleSeat))
	}
	if state.Auras != nil {
		body = appendPartyMemberAuras(body, *state.Auras)
	}
	return body
}

func EncodePartyMemberFullState(state PartyMemberFullState, partyType [2]byte) []byte {
	member := state.Member
	bits := newBitWriter(nil)
	bits.writeBit(state.ForEnemy)
	body := bits.flush()
	body = append(body, partyType[:]...)
	body = binary.LittleEndian.AppendUint16(body, partyMemberUint16(member.Status))
	body = append(body, partyMemberByte(member.PowerType))
	body = binary.LittleEndian.AppendUint16(body, 0) // PowerDisplayID
	body = binary.LittleEndian.AppendUint32(body, uint32(partyMemberInt32(member.CurrentHealth)))
	body = binary.LittleEndian.AppendUint32(body, uint32(partyMemberInt32(member.MaxHealth)))
	body = binary.LittleEndian.AppendUint16(body, partyMemberUint16(member.CurrentPower))
	body = binary.LittleEndian.AppendUint16(body, partyMemberUint16(member.MaxPower))
	body = binary.LittleEndian.AppendUint16(body, partyMemberUint16(member.Level))
	body = binary.LittleEndian.AppendUint16(body, 0) // SpecID unavailable in 3.3.5
	body = binary.LittleEndian.AppendUint16(body, partyMemberUint16(member.ZoneID))
	body = binary.LittleEndian.AppendUint16(body, 0) // WmoGroupID
	body = binary.LittleEndian.AppendUint32(body, 0) // WmoDoodadPlacementID
	position := [3]int16{}
	if member.Position != nil {
		position = *member.Position
	}
	for _, coordinate := range position {
		body = binary.LittleEndian.AppendUint16(body, uint16(coordinate))
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(partyMemberInt32(member.VehicleSeat)))
	auras := partyMemberAuras(member.Auras)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(auras)))
	body = binary.LittleEndian.AppendUint32(body, 8) // PhaseShiftFlags
	body = binary.LittleEndian.AppendUint32(body, 0) // phase count
	body = appendPackedGUID128(body, 0, 0)           // personal phase owner
	body = binary.LittleEndian.AppendUint32(body, 0) // content tuning condition mask
	body = binary.LittleEndian.AppendUint32(body, 0) // unused 9.0.1 field
	body = binary.LittleEndian.AppendUint32(body, 0) // expansion level mask
	body = appendPartyMemberAuraEntries(body, auras)
	petBits := newBitWriter(body)
	petBits.writeBit(member.Pet != nil)
	body = petBits.flush()
	if member.Pet != nil {
		body = appendPartyMemberPetFullState(body, *member.Pet)
	}
	return appendPackedGUID128(body, member.MemberGUID.Low, member.MemberGUID.High)
}

func appendPartyMemberPetFullState(body []byte, pet PartyMemberPetPartialState) []byte {
	guid := GUID128{}
	if pet.GUID != nil {
		guid = *pet.GUID
	}
	body = appendPackedGUID128(body, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(partyMemberInt32(pet.ModelID)))
	body = binary.LittleEndian.AppendUint32(body, uint32(partyMemberInt32(pet.CurrentHealth)))
	body = binary.LittleEndian.AppendUint32(body, uint32(partyMemberInt32(pet.MaxHealth)))
	auras := partyMemberAuras(pet.Auras)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(auras)))
	body = appendPartyMemberAuraEntries(body, auras)
	name := ""
	if pet.Name != nil {
		name = truncateWireString(*pet.Name, 255)
	}
	nameBits := newBitWriter(body)
	nameBits.writeBits(uint32(len(name)), 8)
	body = nameBits.flush()
	return append(body, name...)
}

func partyMemberByte(value *byte) byte {
	if value == nil {
		return 0
	}
	return *value
}

func partyMemberUint16(value *uint16) uint16 {
	if value == nil {
		return 0
	}
	return *value
}

func partyMemberInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func partyMemberAuras(value *[]PartyMemberAuraState) []PartyMemberAuraState {
	if value == nil {
		return nil
	}
	return *value
}

func encodePartyMemberPetPartialState(body []byte, pet PartyMemberPetPartialState) []byte {
	bits := newBitWriter(body)
	bits.writeBit(pet.GUID != nil)
	bits.writeBit(pet.Name != nil)
	bits.writeBit(pet.ModelID != nil)
	bits.writeBit(pet.CurrentHealth != nil)
	bits.writeBit(pet.MaxHealth != nil)
	bits.writeBit(pet.Auras != nil)
	body = bits.flush()
	if pet.Name != nil {
		name := truncateWireString(*pet.Name, 255)
		nameBits := newBitWriter(body)
		nameBits.writeBits(uint32(len(name)), 8)
		body = append(nameBits.flush(), name...)
	}
	if pet.GUID != nil {
		body = appendPackedGUID128(body, pet.GUID.Low, pet.GUID.High)
	}
	if pet.ModelID != nil {
		body = binary.LittleEndian.AppendUint32(body, uint32(*pet.ModelID))
	}
	if pet.CurrentHealth != nil {
		body = binary.LittleEndian.AppendUint32(body, uint32(*pet.CurrentHealth))
	}
	if pet.MaxHealth != nil {
		body = binary.LittleEndian.AppendUint32(body, uint32(*pet.MaxHealth))
	}
	if pet.Auras != nil {
		body = appendPartyMemberAuras(body, *pet.Auras)
	}
	return body
}

func appendPartyMemberAuras(body []byte, auras []PartyMemberAuraState) []byte {
	body = binary.LittleEndian.AppendUint32(body, uint32(len(auras)))
	return appendPartyMemberAuraEntries(body, auras)
}

func appendPartyMemberAuraEntries(body []byte, auras []PartyMemberAuraState) []byte {
	for _, aura := range auras {
		body = binary.LittleEndian.AppendUint32(body, uint32(aura.SpellID))
		body = binary.LittleEndian.AppendUint16(body, aura.Flags)
		body = binary.LittleEndian.AppendUint32(body, aura.ActiveFlags)
		body = binary.LittleEndian.AppendUint32(body, 0) // Points count
	}
	return body
}
