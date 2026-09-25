package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestPetSpellsLegacyToModern(t *testing.T) {
	const petGUID = uint64(0xf130000007fb231c)
	legacy := binary.LittleEndian.AppendUint64(nil, petGUID)
	legacy = binary.LittleEndian.AppendUint16(legacy, 37)
	legacy = binary.LittleEndian.AppendUint32(legacy, 600000)
	legacy = append(legacy, 1, 2, 0, 4)
	actions := [10]uint32{2 | 7<<24, 19685 | 0x81<<24, 19686 | 0xc1<<24}
	for _, action := range actions {
		legacy = binary.LittleEndian.AppendUint32(legacy, action)
	}
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 19685|0x81<<24)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 19685)
	legacy = binary.LittleEndian.AppendUint16(legacy, 12)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3456)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7890)

	spells, err := ParseLegacyPetSpells(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if spells.PetGUID != petGUID || spells.CreatureFamily != 37 || spells.TimeLimit != 600000 ||
		spells.ReactState != 1 || spells.CommandState != 2 || spells.Flag != 4 ||
		len(spells.Actions) != 1 || len(spells.Cooldowns) != 1 {
		t.Fatalf("unexpected legacy pet spells: %#v", spells)
	}

	guid := ModernGUIDForLegacy(petGUID, 1)
	body := EncodePetSpells(guid, spells)
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil || low != guid.Low || high != guid.High {
		t.Fatalf("modern pet GUID=%016x:%016x want=%016x:%016x err=%v", high, low, guid.High, guid.Low, err)
	}
	position := consumed
	if binary.LittleEndian.Uint16(body[position:]) != 37 || int16(binary.LittleEndian.Uint16(body[position+2:])) != -1 ||
		binary.LittleEndian.Uint32(body[position+4:]) != 600000 || binary.LittleEndian.Uint16(body[position+8:]) != 2 || body[position+10] != 1 {
		t.Fatalf("modern pet-spells header=%x", body[position:position+11])
	}
	position += 11
	for index, action := range actions {
		got := binary.LittleEndian.Uint32(body[position+index*4:])
		if got != modernPetAction(action) {
			t.Fatalf("modern action %d=%#x want=%#x", index, got, modernPetAction(action))
		}
	}
	position += 40
	if binary.LittleEndian.Uint32(body[position:]) != 1 || binary.LittleEndian.Uint32(body[position+4:]) != 1 || binary.LittleEndian.Uint32(body[position+8:]) != 0 {
		t.Fatalf("modern pet-spells counts=%x", body[position:position+12])
	}
	position += 12
	if got := binary.LittleEndian.Uint32(body[position:]); got != modernPetAction(spells.Actions[0]) {
		t.Fatalf("modern pet action=%#x", got)
	}
	position += 4
	if binary.LittleEndian.Uint32(body[position:]) != 19685 || binary.LittleEndian.Uint32(body[position+4:]) != 3456 ||
		binary.LittleEndian.Uint32(body[position+8:]) != 7890 || binary.LittleEndian.Uint32(body[position+12:]) != math.Float32bits(1) ||
		binary.LittleEndian.Uint16(body[position+16:]) != 12 || position+18 != len(body) {
		t.Fatalf("modern cooldown=%x trailing=%d", body[position:], len(body)-(position+18))
	}
}

func TestPetSpellsClearAndPetRequests(t *testing.T) {
	clear, err := ParseLegacyPetSpells(make([]byte, 8))
	if err != nil || clear.PetGUID != 0 {
		t.Fatalf("clear pet spells=%#v err=%v", clear, err)
	}

	pet := GUID128{Low: 0x42, High: uint64(3) << 58}
	target := GUID128{Low: 0x43, High: uint64(3) << 58}
	modernAction := modernPetAction(19685 | 0x81<<24)
	body := appendPackedGUID128(nil, pet.Low, pet.High)
	body = binary.LittleEndian.AppendUint32(body, modernAction)
	body = appendPackedGUID128(body, target.Low, target.High)
	for _, value := range []float32{1, 2, 3} {
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(value))
	}
	request, err := ParsePetAction(body)
	if err != nil || request.Pet != pet || request.Target != target || request.Action != modernAction {
		t.Fatalf("pet action=%#v err=%v", request, err)
	}
	legacy := EncodeLegacyPetAction(0x42, request.Action, 0x43)
	if binary.LittleEndian.Uint64(legacy) != 0x42 || binary.LittleEndian.Uint32(legacy[8:]) != 19685|0x81<<24 || binary.LittleEndian.Uint64(legacy[12:]) != 0x43 {
		t.Fatalf("legacy pet action=%x", legacy)
	}

	guid, err := ParsePetGUID(appendPackedGUID128(nil, pet.Low, pet.High))
	if err != nil || guid != pet {
		t.Fatalf("pet GUID=%#v err=%v", guid, err)
	}

	autocastBody := appendPackedGUID128(nil, pet.Low, pet.High)
	autocastBody = binary.LittleEndian.AppendUint32(autocastBody, 19685)
	autocastBody = append(autocastBody, 0x80)
	autocast, err := ParsePetAutocast(autocastBody)
	if err != nil || autocast.Pet != pet || autocast.SpellID != 19685 || !autocast.Enabled {
		t.Fatalf("pet autocast=%#v err=%v", autocast, err)
	}
	legacyAutocast := EncodeLegacyPetAutocast(0x42, autocast.SpellID, autocast.Enabled)
	if len(legacyAutocast) != 13 || binary.LittleEndian.Uint64(legacyAutocast) != 0x42 ||
		binary.LittleEndian.Uint32(legacyAutocast[8:]) != 19685 || legacyAutocast[12] != 1 {
		t.Fatalf("legacy pet autocast=%x", legacyAutocast)
	}
}

func TestPetSetActionTranslation(t *testing.T) {
	pet := GUID128{Low: 0x42, High: uint64(3) << 58}
	legacyPet := uint64(0xf140)<<48 | 6<<24 | 0x42

	// A pet bar edit the 3.4.3 client sends: pack the pet GUID128, then one
	// (position, action-button) pair. The action is a modern word produced from
	// the legacy 3.3.5 action 0x81<<24|19685 (auto-cast spell on slot).
	legacySpellAction := uint32(19685) | 0x81<<24
	modernAction := modernPetAction(legacySpellAction)

	body := appendPackedGUID128(nil, pet.Low, pet.High)
	body = binary.LittleEndian.AppendUint32(body, 3) // position / slot index
	body = binary.LittleEndian.AppendUint32(body, modernAction)
	// Live 3.4.3 packets carry one trailing byte after the pair; legacy proxy
	// reads it only to detect a second pair and otherwise ignores its value.
	body = append(body, 1)

	request, err := ParsePetSetAction(body)
	if err != nil {
		t.Fatalf("parse pet-set-action: %v", err)
	}
	if request.Pet != pet {
		t.Fatalf("pet-set-action pet=%#v want=%#v", request.Pet, pet)
	}
	if len(request.Entries) != 1 || request.Entries[0].Position != 3 || request.Entries[0].Action != modernAction {
		t.Fatalf("pet-set-action entries=%#v", request.Entries)
	}

	legacy := EncodeLegacyPetSetAction(legacyPet, request.Entries)
	if len(legacy) != 8+8 {
		t.Fatalf("legacy pet-set-action has %d bytes, want 16", len(legacy))
	}
	if binary.LittleEndian.Uint64(legacy) != legacyPet {
		t.Fatalf("legacy pet-set-action pet=%#x", binary.LittleEndian.Uint64(legacy))
	}
	if got := binary.LittleEndian.Uint32(legacy[8:]); got != 3 {
		t.Fatalf("legacy pet-set-action position=%d want 3", got)
	}
	if got := binary.LittleEndian.Uint32(legacy[12:]); got != legacySpellAction {
		t.Fatalf("legacy pet-set-action action=%#x want %#x (auto-cast must round-trip)", got, legacySpellAction)
	}
}

func TestPetSetActionParseErrors(t *testing.T) {
	pet := GUID128{Low: 0x42, High: uint64(3) << 58}
	if _, err := ParsePetSetAction(appendPackedGUID128(nil, pet.Low, pet.High)); err == nil {
		t.Fatal("pet-set-action with no entries must fail")
	}
	bad := appendPackedGUID128(nil, pet.Low, pet.High)
	bad = binary.LittleEndian.AppendUint32(bad, 0) // four leftover bytes is not a full pair
	if _, err := ParsePetSetAction(bad); err == nil {
		t.Fatal("pet-set-action with a partial entry must fail")
	}
}

func TestPetNameQueryTranslation(t *testing.T) {
	if SMSGQueryPetName != 0x2919 {
		t.Fatalf("54261 pet-name response opcode = %#x, want 0x2919", SMSGQueryPetName)
	}
	const (
		legacyGUID = uint64(0xf140000003000003)
		petNumber  = uint32(3)
	)
	guid := ModernGUIDForLegacy(legacyGUID, 1)
	request, err := ParseQueryPetName(appendPackedGUID128(nil, guid.Low, guid.High))
	if err != nil || request != guid {
		t.Fatalf("pet-name request=%#v err=%v", request, err)
	}
	reconstructed, ok := LegacyPetGUIDFromModern(request)
	if !ok || reconstructed != legacyGUID {
		t.Fatalf("legacy pet=%#x ok=%v", reconstructed, ok)
	}
	number, ok := LegacyPetNumber(reconstructed)
	if !ok || number != petNumber {
		t.Fatalf("pet number=%d ok=%v", number, ok)
	}
	legacyRequest := EncodeLegacyPetNameQuery(number, reconstructed)
	if len(legacyRequest) != 12 || binary.LittleEndian.Uint32(legacyRequest) != petNumber || binary.LittleEndian.Uint64(legacyRequest[4:]) != legacyGUID {
		t.Fatalf("legacy pet-name request=%x", legacyRequest)
	}

	legacyResponse := binary.LittleEndian.AppendUint32(nil, petNumber)
	legacyResponse = append(legacyResponse, "Wolf"...)
	legacyResponse = append(legacyResponse, 0)
	legacyResponse = binary.LittleEndian.AppendUint32(legacyResponse, 0x12345678)
	legacyResponse = append(legacyResponse, 1)
	for _, name := range []string{"A", "BC", "", "D", "EFG"} {
		legacyResponse = append(legacyResponse, name...)
		legacyResponse = append(legacyResponse, 0)
	}
	response, err := ParseLegacyPetNameResponse(legacyResponse)
	if err != nil || response.PetNumber != petNumber || response.Name != "Wolf" || response.Timestamp != 0x12345678 || response.Declined[1] != "BC" {
		t.Fatalf("legacy pet-name response=%#v err=%v", response, err)
	}
	modern, err := EncodeQueryPetNameResponse(guid, response)
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: modern}
	gotGUID, err := r.guid128()
	allow, _ := r.bit()
	nameLength, _ := r.bits(8)
	hasDeclined, _ := r.bit()
	var lengths [maxPetDeclinedNameCases]uint32
	for index := range lengths {
		lengths[index], _ = r.bits(7)
	}
	if err != nil || gotGUID != guid || !allow || nameLength != 4 || !hasDeclined || lengths != [5]uint32{1, 2, 0, 1, 3} {
		t.Fatalf("modern pet-name header guid=%#v allow=%v name=%d declined=%v lengths=%v err=%v", gotGUID, allow, nameLength, hasDeclined, lengths, err)
	}
	for index, want := range response.Declined {
		got, readErr := r.stringN(int(lengths[index]))
		if readErr != nil || got != want {
			t.Fatalf("declined name %d=%q want=%q err=%v", index, got, want, readErr)
		}
	}
	timestamp, err := r.u64()
	name, nameErr := r.stringN(int(nameLength))
	if err != nil || nameErr != nil || timestamp != 0x12345678 || name != "Wolf" || r.remaining() != 0 {
		t.Fatalf("modern pet-name timestamp=%#x name=%q remaining=%d err=%v/%v", timestamp, name, r.remaining(), err, nameErr)
	}
}

func TestPetNameQueryFailure(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 7)
	legacy = append(legacy, 0)
	legacy = append(legacy, make([]byte, 7)...)
	response, err := ParseLegacyPetNameResponse(legacy)
	if err != nil || response.Name != "" {
		t.Fatalf("failure response=%#v err=%v", response, err)
	}
	body, err := EncodeQueryPetNameResponse(GUID128{Low: 1, High: uint64(10) << 58}, response)
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: body}
	if _, err = r.guid128(); err != nil {
		t.Fatal(err)
	}
	allow, err := r.bit()
	if err != nil || allow || r.remaining() != 1 {
		t.Fatalf("failure allow=%v remaining=%d err=%v body=%x", allow, r.remaining(), err, body)
	}
}

func TestPetNameQueryAzerothCoreFailure(t *testing.T) {
	// SendPetNameQuery: pet number, empty C string, timestamp, declined flag.
	body := binary.LittleEndian.AppendUint32(nil, 8)
	body = append(body, 0, 0, 0, 0, 0, 0)
	response, err := ParseLegacyPetNameResponse(body)
	if err != nil || response.PetNumber != 8 || response.Name != "" {
		t.Fatalf("AzerothCore failure=%#v err=%v", response, err)
	}
	for _, size := range []int{5, 6, 7, 8, 9, 11, 13} {
		malformed := make([]byte, size)
		if _, err := ParseLegacyPetNameResponse(malformed); err == nil {
			t.Fatalf("accepted invalid failure length %d", size)
		}
	}
}

func encodeModernPetRename(request PetRenameRequest) []byte {
	body := appendPackedGUID128(nil, request.Pet.Low, request.Pet.High)
	body = binary.LittleEndian.AppendUint32(body, request.PetNumber)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(request.Name)), 8)
	bits.writeBit(request.HasDeclined)
	if request.HasDeclined {
		for _, name := range request.Declined {
			bits.writeBits(uint32(len(name)), 7)
		}
	}
	body = bits.flush()
	if request.HasDeclined {
		for _, name := range request.Declined {
			body = append(body, name...)
		}
	}
	return append(body, request.Name...)
}

func TestPetRenameTranslation(t *testing.T) {
	if CMSGPetRename != 0x3686 {
		t.Fatalf("54261 pet-rename opcode = %#x, want 0x3686", CMSGPetRename)
	}
	const (
		legacyPet = uint64(0xf140000003000003)
		petNumber = uint32(3)
		mapID     = uint16(571)
	)
	pet := ModernGUIDForLegacy(legacyPet, mapID)
	request := PetRenameRequest{Pet: pet, PetNumber: petNumber, Name: "小白"}
	body := encodeModernPetRename(request)
	got, err := ParsePetRename(body)
	if err != nil {
		t.Fatalf("parse pet-rename: %v", err)
	}
	if got.Pet != pet || got.PetNumber != petNumber || got.Name != "小白" || got.HasDeclined {
		t.Fatalf("pet-rename=%#v", got)
	}

	legacy := EncodeLegacyPetRename(legacyPet, got)
	want := binary.LittleEndian.AppendUint64(nil, legacyPet)
	want = append(want, "小白"...)
	want = append(want, 0, 0)
	if string(legacy) != string(want) {
		t.Fatalf("legacy pet-rename body=%x want=%x", legacy, want)
	}

	reconstructed, ok := LegacyPetGUIDFromModern(got.Pet)
	if !ok || reconstructed != legacyPet {
		t.Fatalf("legacy pet=%#x ok=%v", reconstructed, ok)
	}
}

func TestPetRenameDeclinedNames(t *testing.T) {
	pet := GUID128{Low: 0x42, High: uint64(10) << 58}
	request := PetRenameRequest{
		Pet:         pet,
		PetNumber:   6,
		Name:        "Wolf",
		HasDeclined: true,
		Declined:    [maxPetDeclinedNameCases]string{"A", "BC", "", "D", "EFG"},
	}
	got, err := ParsePetRename(encodeModernPetRename(request))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Wolf" || !got.HasDeclined || got.Declined != request.Declined {
		t.Fatalf("declined pet-rename=%#v", got)
	}
	legacy := EncodeLegacyPetRename(0x42, got)
	want := append(binary.LittleEndian.AppendUint64(nil, 0x42), "Wolf"...)
	want = append(want, 0, 1)
	for _, name := range request.Declined {
		want = append(want, name...)
		want = append(want, 0)
	}
	if string(legacy) != string(want) {
		t.Fatalf("legacy declined pet-rename=%x want=%x", legacy, want)
	}
}

func TestPetRenameParseErrors(t *testing.T) {
	pet := GUID128{Low: 0x42, High: uint64(10) << 58}
	if _, err := ParsePetRename(nil); err == nil {
		t.Fatal("empty pet-rename must fail")
	}
	emptyName := PetRenameRequest{Pet: pet, PetNumber: 1}
	if _, err := ParsePetRename(encodeModernPetRename(emptyName)); err == nil {
		t.Fatal("empty pet-rename name must fail")
	}
	trailing := encodeModernPetRename(PetRenameRequest{Pet: pet, PetNumber: 1, Name: "Wolf"})
	trailing = append(trailing, 0)
	if _, err := ParsePetRename(trailing); err == nil {
		t.Fatal("pet-rename with trailing bytes must fail")
	}
}
