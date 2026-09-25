package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	CMSGQueryPetName   = uint16(0x3275)
	CMSGPetSetAction   = uint16(0x348a)
	CMSGPetAction      = uint16(0x348b)
	CMSGPetStopAttack  = uint16(0x348c)
	CMSGPetAbandon     = uint16(0x348d)
	CMSGPetCancelAura  = uint16(0x348e)
	CMSGPetAutocast    = uint16(0x348f)
	CMSGRequestPetInfo = uint16(0x3490)
	CMSGPetRename      = uint16(0x3686)

	// Build 54261 retains the 3.4.3 opcode table introduced by build 51666.
	SMSGPetClearSpells = uint16(0x2c21)
	SMSGPetSpells      = uint16(0x2c22)
	SMSGPetTameFailure = uint16(0x26b3)
	SMSGQueryPetName   = uint16(0x2919)
)

const maxPetDeclinedNameCases = 5

type LegacyPetNameResponse struct {
	PetNumber   uint32
	Name        string
	Timestamp   uint32
	HasDeclined bool
	Declined    [maxPetDeclinedNameCases]string
}

func ParseQueryPetName(body []byte) (GUID128, error) {
	guid, err := ParsePackedGUID128Exact(body)
	if err != nil {
		return GUID128{}, fmt.Errorf("pet-name query GUID: %w", err)
	}
	if guid.Low == 0 && guid.High == 0 {
		return GUID128{}, fmt.Errorf("pet-name query GUID is empty")
	}
	return guid, nil
}

// LegacyPetGUIDFromModern reconstructs the HighGuid::Pet form used by legacy proxy:
// both modern and legacy GUID entries carry pet_number, while ObjectData.Entry
// separately carries creature_template.entry.
func LegacyPetGUIDFromModern(guid GUID128) (uint64, bool) {
	if guid.High>>58 != 10 {
		return 0, false
	}
	entry := (guid.High >> 6) & 0x7fffff
	counter := guid.Low & 0x00ffffff
	if entry == 0 || counter == 0 {
		return 0, false
	}
	return uint64(0xf140)<<48 | entry<<24 | counter, true
}

func LegacyPetNumber(guid uint64) (uint32, bool) {
	if uint16(guid>>48) != 0xf140 {
		return 0, false
	}
	number := uint32((guid >> 24) & 0x00ffffff)
	return number, number != 0
}

func EncodeLegacyPetNameQuery(petNumber uint32, guid uint64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, petNumber)
	return binary.LittleEndian.AppendUint64(body, guid)
}

func ParseLegacyPetNameResponse(body []byte) (LegacyPetNameResponse, error) {
	var response LegacyPetNameResponse
	r := movementReader{data: body}
	var err error
	if response.PetNumber, err = r.u32(); err != nil {
		return response, fmt.Errorf("read pet number: %w", err)
	}
	if response.Name, err = r.cstring(); err != nil {
		return response, fmt.Errorf("read pet name: %w", err)
	}
	if response.Name == "" {
		// AzerothCore SendPetNameQuery writes a zero uint32 timestamp and
		// one declined-name byte after the empty name. Accept the older
		// seven-byte variant too. Rejecting the five-byte form leaves the
		// proxy's pending query occupied and suppresses later name requests.
		if r.remaining() != 5 && r.remaining() != 7 {
			return response, fmt.Errorf("failed pet-name response has %d trailing bytes, want 5 or 7", r.remaining())
		}
		return response, nil
	}
	if response.Timestamp, err = r.u32(); err != nil {
		return response, fmt.Errorf("read pet-name timestamp: %w", err)
	}
	hasDeclined, err := r.u8()
	if err != nil {
		return response, fmt.Errorf("read pet declined-name flag: %w", err)
	}
	if hasDeclined > 1 {
		return response, fmt.Errorf("pet declined-name flag is %d", hasDeclined)
	}
	response.HasDeclined = hasDeclined != 0
	if hasDeclined != 0 {
		for index := range response.Declined {
			response.Declined[index], err = r.cstring()
			if err != nil {
				return response, fmt.Errorf("read pet declined name %d: %w", index, err)
			}
		}
	}
	if r.remaining() != 0 {
		return response, fmt.Errorf("pet-name response has %d trailing bytes", r.remaining())
	}
	return response, nil
}

func EncodeQueryPetNameResponse(guid GUID128, response LegacyPetNameResponse) ([]byte, error) {
	if len(response.Name) > 0xff {
		return nil, fmt.Errorf("pet name has %d bytes", len(response.Name))
	}
	for _, name := range response.Declined {
		if len(name) > 0x7f {
			return nil, fmt.Errorf("pet declined name has %d bytes", len(name))
		}
	}
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	bits := newBitWriter(body)
	bits.writeBit(response.Name != "")
	if response.Name == "" {
		return bits.flush(), nil
	}
	bits.writeBits(uint32(len(response.Name)), 8)
	bits.writeBit(response.HasDeclined)
	for _, name := range response.Declined {
		bits.writeBits(uint32(len(name)), 7)
	}
	body = bits.flush()
	for _, name := range response.Declined {
		body = append(body, name...)
	}
	body = binary.LittleEndian.AppendUint64(body, uint64(response.Timestamp))
	return append(body, response.Name...), nil
}

type LegacyPetSpellCooldown struct {
	SpellID          uint32
	Category         uint16
	Duration         uint32
	CategoryDuration uint32
}

type LegacyPetSpells struct {
	PetGUID        uint64
	CreatureFamily uint16
	TimeLimit      uint32
	ReactState     byte
	CommandState   byte
	Flag           byte
	ActionButtons  [10]uint32
	Actions        []uint32
	Cooldowns      []LegacyPetSpellCooldown
}

// ParseLegacyPetSpells decodes the WotLK SMSG_PET_SPELLS body. A zero GUID is
// the legacy "clear pet spells" form and intentionally has no trailing body.
func ParseLegacyPetSpells(body []byte) (LegacyPetSpells, error) {
	var result LegacyPetSpells
	if len(body) < 8 {
		return result, fmt.Errorf("pet-spells has %d bytes, want at least 8", len(body))
	}
	result.PetGUID = binary.LittleEndian.Uint64(body)
	position := 8
	if result.PetGUID == 0 {
		if len(body) != position {
			return result, fmt.Errorf("clear pet-spells has %d trailing bytes", len(body)-position)
		}
		return result, nil
	}
	if len(body)-position < 2+4+4+10*4+1+1 {
		return result, fmt.Errorf("pet-spells has %d bytes after GUID, want at least 52", len(body)-position)
	}
	result.CreatureFamily = binary.LittleEndian.Uint16(body[position:])
	position += 2
	result.TimeLimit = binary.LittleEndian.Uint32(body[position:])
	position += 4
	result.ReactState = body[position]
	result.CommandState = body[position+1]
	// Byte 2 is unused in 3.3.5; byte 3 carries the legacy pet mode flag.
	result.Flag = body[position+3]
	position += 4
	for index := range result.ActionButtons {
		result.ActionButtons[index] = binary.LittleEndian.Uint32(body[position:])
		position += 4
	}
	actionCount := int(body[position])
	position++
	if actionCount > (len(body)-position-1)/4 {
		return result, fmt.Errorf("pet-spells action count %d exceeds %d remaining bytes", actionCount, len(body)-position)
	}
	result.Actions = make([]uint32, actionCount)
	for index := range result.Actions {
		result.Actions[index] = binary.LittleEndian.Uint32(body[position:])
		position += 4
	}
	if position >= len(body) {
		return result, fmt.Errorf("pet-spells is missing cooldown count")
	}
	cooldownCount := int(body[position])
	position++
	const legacyCooldownBytes = 14
	if cooldownCount > (len(body)-position)/legacyCooldownBytes {
		return result, fmt.Errorf("pet-spells cooldown count %d exceeds %d remaining bytes", cooldownCount, len(body)-position)
	}
	result.Cooldowns = make([]LegacyPetSpellCooldown, cooldownCount)
	for index := range result.Cooldowns {
		cooldown := &result.Cooldowns[index]
		cooldown.SpellID = binary.LittleEndian.Uint32(body[position:])
		cooldown.Category = binary.LittleEndian.Uint16(body[position+4:])
		cooldown.Duration = binary.LittleEndian.Uint32(body[position+6:])
		cooldown.CategoryDuration = binary.LittleEndian.Uint32(body[position+10:])
		position += legacyCooldownBytes
	}
	if position != len(body) {
		return result, fmt.Errorf("pet-spells has %d trailing bytes", len(body)-position)
	}
	return result, nil
}

// modernPetAction converts 3.3.5's high action-type byte to 3.4.3's nine-bit
// action type at bits 23..31. This mirrors legacy proxy's ActionButtonOldTo343.
func modernPetAction(action uint32) uint32 {
	value := action & 0x00ffffff
	actionType := action >> 24
	if actionType == 0x81 {
		actionType = 1
	}
	if value > 2 {
		actionType |= 0x100
	}
	return value | actionType<<23
}

func legacyPetAction(action uint32) uint32 {
	value := action & 0x007fffff
	actionType := action >> 23
	if value > 2 {
		actionType &^= 0x100
	}
	switch actionType {
	case 0x81:
		actionType = 0xc1
	case 1, 0x41:
		actionType = 0x81
	}
	return value | actionType<<24
}

func EncodePetSpells(guid GUID128, spells LegacyPetSpells) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint16(body, spells.CreatureFamily)
	body = binary.LittleEndian.AppendUint16(body, uint16(0xffff)) // no specialization
	body = binary.LittleEndian.AppendUint32(body, spells.TimeLimit)
	body = binary.LittleEndian.AppendUint16(body, uint16(spells.CommandState))
	body = append(body, spells.ReactState)
	for _, action := range spells.ActionButtons {
		body = binary.LittleEndian.AppendUint32(body, modernPetAction(action))
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(spells.Actions)))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(spells.Cooldowns)))
	body = binary.LittleEndian.AppendUint32(body, 0) // SpellHistoryCount
	for _, action := range spells.Actions {
		body = binary.LittleEndian.AppendUint32(body, modernPetAction(action))
	}
	for _, cooldown := range spells.Cooldowns {
		body = binary.LittleEndian.AppendUint32(body, cooldown.SpellID)
		body = binary.LittleEndian.AppendUint32(body, cooldown.Duration)
		body = binary.LittleEndian.AppendUint32(body, cooldown.CategoryDuration)
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(1))
		body = binary.LittleEndian.AppendUint16(body, cooldown.Category)
	}
	return body
}

func ParsePetGUID(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

type PetActionRequest struct {
	Pet    GUID128
	Action uint32
	Target GUID128
}

func ParsePetAction(body []byte) (PetActionRequest, error) {
	var request PetActionRequest
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return request, fmt.Errorf("pet-action pet GUID: %w", err)
	}
	request.Pet = GUID128{Low: low, High: high}
	position := consumed
	if len(body)-position < 4 {
		return request, fmt.Errorf("pet-action is missing action")
	}
	request.Action = binary.LittleEndian.Uint32(body[position:])
	position += 4
	low, high, consumed, err = readPackedGUID128(body[position:])
	if err != nil {
		return request, fmt.Errorf("pet-action target GUID: %w", err)
	}
	request.Target = GUID128{Low: low, High: high}
	position += consumed
	if len(body)-position != 12 {
		return request, fmt.Errorf("pet-action position has %d bytes, want 12", len(body)-position)
	}
	return request, nil
}

type PetCancelAuraRequest struct {
	Pet     GUID128
	SpellID uint32
}

// PetSetActionEntry is one action-button edit the client sends through
// CMSG_PET_SET_ACTION. The legacy server consumes the same (position, action)
// pair when it stores the pet action bar and toggles pet-spell auto-cast.
type PetSetActionEntry struct {
	Position uint32
	Action   uint32
}

type PetSetActionRequest struct {
	Pet     GUID128
	Entries []PetSetActionEntry
}

// ParsePetSetAction decodes the build-54261 CMSG_PET_SET_ACTION body: the pet
// GUID128 followed by one or more 8-byte (position, action-button) pairs. The
// 3.4.3 client emits this packet both when the pet bar is rearranged and when
// a pet spell's auto-cast is toggled, matching legacy proxy's
// CMSG_PET_SET_ACTION_343 handler.
//
// A single-entry packet carries exactly one pair plus one trailing byte that
// legacy proxy consumes only to detect an optional second pair (its value is
// otherwise ignored), so the trailing remainder below 8 bytes is skipped.
func ParsePetSetAction(body []byte) (PetSetActionRequest, error) {
	var request PetSetActionRequest
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return request, fmt.Errorf("pet-set-action pet GUID: %w", err)
	}
	request.Pet = GUID128{Low: low, High: high}
	entries := body[consumed:]
	if len(entries) == 0 {
		return request, fmt.Errorf("pet-set-action has no action entries")
	}
	request.Entries = make([]PetSetActionEntry, 0, len(entries)/8)
	for len(entries) >= 8 {
		request.Entries = append(request.Entries, PetSetActionEntry{
			Position: binary.LittleEndian.Uint32(entries),
			Action:   binary.LittleEndian.Uint32(entries[4:]),
		})
		entries = entries[8:]
	}
	if len(request.Entries) == 0 {
		return request, fmt.Errorf("pet-set-action has no complete action entry")
	}
	return request, nil
}

func EncodeLegacyPetSetAction(pet uint64, entries []PetSetActionEntry) []byte {
	body := binary.LittleEndian.AppendUint64(nil, pet)
	for _, entry := range entries {
		body = binary.LittleEndian.AppendUint32(body, entry.Position)
		// The modern 9-bit action type at bits 23..31 maps back to the legacy
		// 3.3.5 high-byte state, exactly as legacy proxy's ActionButton343ToOld.
		body = binary.LittleEndian.AppendUint32(body, legacyPetAction(entry.Action))
	}
	return body
}

type PetAutocastRequest struct {
	Pet     GUID128
	SpellID uint32
	Enabled bool
}

func ParsePetAutocast(body []byte) (PetAutocastRequest, error) {
	var request PetAutocastRequest
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return request, err
	}
	if len(body)-consumed != 5 {
		return request, fmt.Errorf("pet-autocast has %d trailing bytes, want 5", len(body)-consumed)
	}
	request.Pet = GUID128{Low: low, High: high}
	request.SpellID = binary.LittleEndian.Uint32(body[consumed:])
	request.Enabled = body[consumed+4]&0x80 != 0
	return request, nil
}

func ParsePetCancelAura(body []byte) (PetCancelAuraRequest, error) {
	var request PetCancelAuraRequest
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return request, err
	}
	if len(body)-consumed != 4 {
		return request, fmt.Errorf("pet-cancel-aura has %d trailing bytes, want 4", len(body)-consumed)
	}
	request.Pet = GUID128{Low: low, High: high}
	request.SpellID = binary.LittleEndian.Uint32(body[consumed:])
	return request, nil
}

func EncodeLegacyPetAction(pet uint64, action uint32, target uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, pet)
	body = binary.LittleEndian.AppendUint32(body, legacyPetAction(action))
	return binary.LittleEndian.AppendUint64(body, target)
}

func EncodeLegacyPetCancelAura(pet uint64, spellID uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, pet)
	return binary.LittleEndian.AppendUint32(body, spellID)
}

func EncodeLegacyPetAutocast(pet uint64, spellID uint32, enabled bool) []byte {
	body := binary.LittleEndian.AppendUint64(nil, pet)
	body = binary.LittleEndian.AppendUint32(body, spellID)
	if enabled {
		return append(body, 1)
	}
	return append(body, 0)
}

// PetRenameRequest is the build-54261 CMSG_PET_RENAME body: PackedGuid128,
// pet number, then a bit-packed name (8-bit length) and optional declined
// names. This matches legacy proxy's PetRename.Read / WowPacketParser
// ReadPetRenameData used from 6.x through 3.4.4.
type PetRenameRequest struct {
	Pet         GUID128
	PetNumber   uint32
	Name        string
	HasDeclined bool
	Declined    [maxPetDeclinedNameCases]string
}

func ParsePetRename(body []byte) (PetRenameRequest, error) {
	var request PetRenameRequest
	r := movementReader{data: body}
	var err error
	if request.Pet, err = r.guid128(); err != nil {
		return request, fmt.Errorf("pet-rename pet GUID: %w", err)
	}
	if request.Pet.Low == 0 && request.Pet.High == 0 {
		return request, fmt.Errorf("pet-rename pet GUID is empty")
	}
	if request.PetNumber, err = r.u32(); err != nil {
		return request, fmt.Errorf("pet-rename pet number: %w", err)
	}
	nameLength, err := r.bits(8)
	if err != nil {
		return request, fmt.Errorf("pet-rename name length: %w", err)
	}
	if request.HasDeclined, err = r.bit(); err != nil {
		return request, fmt.Errorf("pet-rename declined flag: %w", err)
	}
	var declinedLengths [maxPetDeclinedNameCases]uint32
	if request.HasDeclined {
		for index := range declinedLengths {
			declinedLengths[index], err = r.bits(7)
			if err != nil {
				return request, fmt.Errorf("pet-rename declined name %d length: %w", index, err)
			}
		}
		for index := range request.Declined {
			request.Declined[index], err = r.stringN(int(declinedLengths[index]))
			if err != nil {
				return request, fmt.Errorf("pet-rename declined name %d: %w", index, err)
			}
		}
	}
	if request.Name, err = r.stringN(int(nameLength)); err != nil {
		return request, fmt.Errorf("pet-rename name: %w", err)
	}
	if request.Name == "" {
		return request, fmt.Errorf("pet-rename name is empty")
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("pet-rename has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// EncodeLegacyPetRename writes WotLK CMSG_PET_RENAME: unpacked pet GUID64,
// NUL-terminated name, a declined-name flag, and five declined CStrings when
// the flag is set.
func EncodeLegacyPetRename(pet uint64, request PetRenameRequest) []byte {
	body := binary.LittleEndian.AppendUint64(nil, pet)
	body = appendCString(body, request.Name)
	if !request.HasDeclined {
		return append(body, 0)
	}
	body = append(body, 1)
	for _, name := range request.Declined {
		body = appendCString(body, name)
	}
	return body
}
