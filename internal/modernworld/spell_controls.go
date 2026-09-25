package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Modern spell controls (build 54261 client -> server). Layouts mirror
// HermesProxy-WOTLK World/Server/Packets {CancelAutoRepeatSpell,
// CancelChannelling,PetCastSpell,SelfRes,SpellClick,TotemDestroyed}.cs and
// World/Server/WorldSocket.cs handlers. The cast payload shared by the player
// and pet cast paths is already modelled by SpellCastRequest /
// ParseCastSpell / EncodeLegacyCastSpell.

const (
	CMSGCancelAutoRepeatSpell = uint16(13543)
	CMSGCancelChannelling     = uint16(12906)
	CMSGPetCastSpell          = uint16(12955)
	CMSGPetLearnTalent        = uint16(13652) // 0x3554; omitted from Hermes 54261 Opcode.cs but present on 3.4.3
	CMSGSelfRes               = uint16(13617)
	CMSGSpellClick            = uint16(13461)
	CMSGTotemDestroyed        = uint16(13560)
)

// ParseEmptyControl validates a modern control whose payload must be empty and
// returns no data.
func ParseEmptyControl(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("control has %d bytes, want 0", len(body))
	}
	return nil
}

// ParseCancelChannelling reads the modern CMSG_CANCEL_CHANNELLING (spell id and
// reason). The reason has no legacy equivalent. The caller forwards the tracked
// channeled spell id rather than trusting this one (the 3.4.3 client re-sends it
// on every escape press).
func ParseCancelChannelling(body []byte) (uint32, error) {
	r := movementReader{data: body}
	spellID, err := r.i32()
	if err != nil {
		return 0, fmt.Errorf("read cancel-channel spell: %w", err)
	}
	if _, err := r.i32(); err != nil { // reason
		return 0, fmt.Errorf("read cancel-channel reason: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("cancel-channelling has %d trailing bytes", r.remaining())
	}
	return uint32(spellID), nil
}

// ParseSelfRes validates the modern 4-byte self-res spell id (dropped; the
// legacy server casts the player's stored self-res spell).
func ParseSelfRes(body []byte) error {
	if len(body) != 4 {
		return fmt.Errorf("self-res has %d bytes, want 4", len(body))
	}
	return nil
}

// ParseSpellClick reads the modern CMSG_SPELL_CLICK target GUID128 and the
// try-auto-dismount bit (dropped; 3.3.5a clicks have no such flag).
func ParseSpellClick(body []byte) (GUID128, error) {
	r := movementReader{data: body}
	target, err := r.guid128()
	if err != nil {
		return target, fmt.Errorf("read spell-click target: %w", err)
	}
	if _, err := r.bit(); err != nil { // TryAutoDismount
		return target, fmt.Errorf("read spell-click auto-dismount bit: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return target, fmt.Errorf("spell-click has %d trailing bytes", r.remaining())
	}
	return target, nil
}

// ParseTotemDestroyed reads the modern totem slot and ignores the totem GUID
// (3.3.5a CMSG_TOTEM_DESTROYED carries only the slot).
func ParseTotemDestroyed(body []byte) (uint8, error) {
	r := movementReader{data: body}
	slot, err := r.u8()
	if err != nil {
		return 0, fmt.Errorf("read totem slot: %w", err)
	}
	if slot > 4 {
		return 0, fmt.Errorf("totem slot %d out of range", slot)
	}
	if _, err := r.guid128(); err != nil { // totem GUID, ignored
		return 0, fmt.Errorf("read totem GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("totem-destroyed has %d trailing bytes", r.remaining())
	}
	return slot, nil
}

// PetLearnTalentRequest is the modern CMSG_PET_LEARN_TALENT payload.
// Rank is the build-54261 uint16, widened to the uint32 the legacy server expects.
type PetLearnTalentRequest struct {
	Pet    GUID128
	Talent uint32
	Rank   uint32
}

func ParsePetLearnTalent(body []byte) (PetLearnTalentRequest, error) {
	var request PetLearnTalentRequest
	r := movementReader{data: body}
	var err error
	if request.Pet, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read pet-learn-talent pet: %w", err)
	}
	if request.Talent, err = r.u32(); err != nil {
		return request, fmt.Errorf("read pet-learn-talent id: %w", err)
	}
	rank, err := r.u16()
	if err != nil {
		return request, fmt.Errorf("read pet-learn-talent rank: %w", err)
	}
	request.Rank = uint32(rank)
	if r.remaining() != 0 {
		return request, fmt.Errorf("pet-learn-talent has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyPetLearnTalent(pet uint64, talent, rank uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, pet)
	body = binary.LittleEndian.AppendUint32(body, talent)
	return binary.LittleEndian.AppendUint32(body, rank)
}

// PetCastRequest is the modern CMSG_PET_CAST_SPELL payload: the pet GUID128
// followed by the same SpellCastRequest the player cast path uses.
type PetCastRequest struct {
	Pet  GUID128
	Cast SpellCastRequest
}

// ParsePetCastSpell reads the pet GUID then delegates the rest to ParseCastSpell.
func ParsePetCastSpell(body []byte) (PetCastRequest, error) {
	var request PetCastRequest
	r := movementReader{data: body}
	var err error
	if request.Pet, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read pet-cast pet: %w", err)
	}
	if request.Cast, err = ParseCastSpell(body[r.offset:]); err != nil {
		return request, fmt.Errorf("pet cast: %w", err)
	}
	return request, nil
}

// EncodeLegacyPetCastSpell prepends the raw pet GUID64 to the standard legacy
// cast body (cast count 0 + spell + flags + targets + optional trajectory).
func EncodeLegacyPetCastSpell(pet uint64, request SpellCastRequest, unit, item, srcTransport, dstTransport uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, pet)
	return append(body, EncodeLegacyCastSpell(request, unit, item, srcTransport, dstTransport)...)
}

// EncodeLegacyTotemDestroyed writes the one-byte 3.3.5a totem slot.
func EncodeLegacyTotemDestroyed(slot uint8) []byte {
	return []byte{slot}
}
