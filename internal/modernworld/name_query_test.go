package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestQueryPlayerNamesAndResponse(t *testing.T) {
	player := GUID128{Low: 0x42, High: uint64(2)<<58 | uint64(1)<<42}
	request := binary.LittleEndian.AppendUint32(nil, 1)
	request = appendPackedGUID128(request, player.Low, player.High)
	guids, err := ParseQueryPlayerNames(request)
	if err != nil || len(guids) != 1 || guids[0] != player {
		t.Fatalf("guids=%#v err=%v", guids, err)
	}
	legacyGUID, ok := LegacyPlayerGUIDFromModern(player)
	if !ok || legacyGUID != 0x42 {
		t.Fatalf("legacy=%x ok=%v", legacyGUID, ok)
	}

	legacy := appendLegacyPackedGUID(nil, 0x42)
	legacy = append(legacy, 0) // success
	legacy = appendCString(legacy, "Topaz")
	legacy = append(legacy, 0) // realm
	legacy = append(legacy, 1, 0, 1, 0)
	body, err := TranslateNameQueryResponse(legacy, 0x01010001)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 1 || body[4] != 0 {
		t.Fatalf("name response header %x", body[:5])
	}
	identityGUID, name, found, err := LegacyNameQueryIdentity(legacy)
	if err != nil || !found || identityGUID != 0x42 || name != "Topaz" {
		t.Fatalf("identity guid=%x name=%q found=%v err=%v", identityGUID, name, found, err)
	}
	if _, err := TranslateNameQueryResponse(append(appendLegacyPackedGUID(nil, 1), 1), 1); err != nil {
		t.Fatal(err)
	}
}

func TestParseQueryPlayerNamesIgnoresTrailingBytes(t *testing.T) {
	player := GUID128{Low: 0x42, High: uint64(2)<<58 | uint64(1)<<42}
	request := binary.LittleEndian.AppendUint32(nil, 1)
	request = appendPackedGUID128(request, player.Low, player.High)
	request = append(request, 0x00, 0x01)
	guids, err := ParseQueryPlayerNames(request)
	if err != nil || len(guids) != 1 || guids[0] != player {
		t.Fatalf("trailing query guids=%#v err=%v", guids, err)
	}
}

func TestEncodeQueryPlayerNameLookupUsesQueriedGUID(t *testing.T) {
	queried := GUID128{Low: 0x42, High: uint64(2)<<58 | uint64(1)<<42 | 7}
	identity := LegacyNameIdentity{GUID: 0x42, Name: "Topaz", Race: 4, Sex: 1, Class: 3}
	body, err := EncodeQueryPlayerNameLookup(queried, identity, 27, 0x01010001, GUID128{}, GUID128{})
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: body}
	_, _ = r.u32()
	_, _ = r.u8()
	player, err := r.guid128()
	if err != nil || player != queried {
		t.Fatalf("lookup GUID=%#v err=%v", player, err)
	}
}

func TestEncodeQueryPlayerNameIdentityUsesKnownCharacterMetadata(t *testing.T) {
	identity := LegacyNameIdentity{GUID: 0x42, Name: "Topaz", Race: 4, Sex: 1, Class: 3}
	wowAccount := GUID128{Low: 7, High: uint64(29) << 58}
	bnetAccount := GUID128{Low: 8, High: uint64(30) << 58}
	body, err := EncodeQueryPlayerNameIdentity(identity, 27, 0x01010001, wowAccount, bnetAccount)
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: body}
	count, _ := r.u32()
	result, _ := r.u8()
	player, _ := r.guid128()
	hasData, _ := r.bit()
	_, _ = r.bit()
	r.align()
	deleted, _ := r.bit()
	nameLength, _ := r.bits(6)
	for range maxDeclinedNameCases {
		_, _ = r.bits(7)
	}
	r.align()
	account, _ := r.guid128()
	bnet, _ := r.guid128()
	actual, _ := r.guid128()
	_, _ = r.u64()
	realmAddress, _ := r.u32()
	race, _ := r.u8()
	sex, _ := r.u8()
	classID, _ := r.u8()
	level, _ := r.u8()
	_, _ = r.u8()
	name, _ := r.stringN(int(nameLength))
	wantPlayer := ModernGUIDForLegacy(identity.GUID, 0)
	if count != 1 || result != 0 || player != wantPlayer || !hasData || deleted || account != wowAccount || bnet != bnetAccount || actual != wantPlayer ||
		realmAddress != 0x01010001 || race != 4 || sex != 1 || classID != 3 || level != 27 || name != "Topaz" || r.remaining() != 0 {
		t.Fatalf("identity response count=%d result=%d player=%#v has_data=%v deleted=%v account=%#v bnet=%#v actual=%#v realm=%#x race=%d sex=%d class=%d level=%d name=%q remaining=%d body=%x",
			count, result, player, hasData, deleted, account, bnet, actual, realmAddress, race, sex, classID, level, name, r.remaining(), body)
	}
}
