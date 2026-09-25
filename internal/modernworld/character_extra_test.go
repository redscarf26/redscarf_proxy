package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestTitleEarned(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 77)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	index, earned, err := ParseLegacyTitleEarned(legacy)
	if err != nil || index != 77 || !earned {
		t.Fatalf("title=%d earned=%v err=%v", index, earned, err)
	}
	body := EncodeTitleEarned(index)
	if !isUint32At(body, 0, 77) || len(body) != 4 {
		t.Fatalf("modern title=%x", body)
	}
	index, earned, err = ParseLegacyTitleEarned(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 5), 0))
	if err != nil || index != 5 || earned {
		t.Fatalf("lost title parsed as earned: %v", earned)
	}
	if _, _, err := ParseLegacyTitleEarned([]byte{1}); err == nil {
		t.Fatal("title with trailing garbage must error")
	}
}

func TestHonorInspectRoundTrip(t *testing.T) {
	body := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	body = append(body, 7) // lifetime highest rank
	body = binary.LittleEndian.AppendUint16(body, 3)
	body = binary.LittleEndian.AppendUint16(body, 9) // yesterday kills
	body = binary.LittleEndian.AppendUint32(body, 10)
	body = binary.LittleEndian.AppendUint32(body, 20)
	body = binary.LittleEndian.AppendUint32(body, 150)
	honor, err := ParseLegacyHonorInspect(body)
	if err != nil {
		t.Fatalf("honor parse: %v", err)
	}
	if honor.GUID != 0xf130000001000043 || honor.LifetimeHighestRank != 7 || honor.YesterdayHK != 9 || honor.LifetimeHK != 150 {
		t.Fatalf("honor=%#v", honor)
	}
	encoded := EncodeHonorInspectResult(honor, func(uint64) GUID128 {
		return GUID128{Low: 0x43, High: 1 << 58}
	})
	// pack player + rank at [var..], then u16(0), u16 yesterday, u16(0), u16 lifetime.
	r := movementReader{data: encoded}
	player, err := r.guid128()
	if err != nil || player != (GUID128{Low: 0x43, High: 1 << 58}) {
		t.Fatalf("honor player=%#v err=%v", player, err)
	}
	rank, _ := r.u8()
	if rank != 7 {
		t.Fatalf("honor rank=%d", rank)
	}
	if unused, _ := r.u16(); unused != 0 {
		t.Fatalf("unused1=%d", unused)
	}
	if yesterday, _ := r.u16(); yesterday != 9 {
		t.Fatalf("yesterday=%d", yesterday)
	}
	if unused, _ := r.u16(); unused != 0 {
		t.Fatalf("unused3=%d", unused)
	}
	if lifetime, _ := r.u16(); lifetime != 150 {
		t.Fatalf("lifetime=%d", lifetime)
	}
}

func TestArenaTeamInspect(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	legacy = append(legacy, 1) // slot 1
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x1234)
	for _, value := range []int32{1800, 100, 80, 90, 1750} {
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(value))
	}
	guid, team, err := ParseLegacyArenaTeamInspect(legacy)
	if err != nil || guid != 0xf130000001000043 || team.Slot != 1 || team.TeamID != 0x1234 ||
		team.Rating != 1800 || team.PersonalRating != 1750 {
		t.Fatalf("arena guid=%x team=%#v err=%v", guid, team, err)
	}
	arenaGUID := ModernArenaTeamGUID(team.TeamID)
	if arenaGUID.Low != 0x1234 || arenaGUID.High != uint64(52)<<58 {
		t.Fatalf("arena team guid=%#v", arenaGUID)
	}
	if ModernArenaTeamGUID(0) != (GUID128{}) {
		t.Fatal("empty team id must yield an empty guid")
	}

	var teams [ArenaSlotCount]ArenaTeamInspect
	teams[1] = team
	encoded := EncodeInspectPvp(GUID128{Low: 0x43, High: 1 << 58}, teams)
	r := movementReader{data: encoded}
	player, err := r.guid128()
	if err != nil || player != (GUID128{Low: 0x43, High: 1 << 58}) {
		t.Fatalf("pvp player=%#v err=%v", player, err)
	}
	brackets, _ := r.bits(3)
	teamCount, _ := r.bits(2)
	if brackets != 0 || teamCount != ArenaSlotCount {
		t.Fatalf("pvp counts=%d/%d", brackets, teamCount)
	}
	for slot := 0; slot < ArenaSlotCount; slot++ {
		g, err := r.guid128()
		if err != nil {
			t.Fatal(err)
		}
		if slot == 1 && g != (GUID128{Low: 0x1234, High: uint64(52) << 58}) {
			t.Fatalf("slot %d team guid=%#v", slot, g)
		}
		if slot != 1 && g != (GUID128{}) {
			t.Fatalf("empty slot %d team guid=%#v", slot, g)
		}
		for index := 0; index < 5; index++ {
			if _, err := r.i32(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func buildModernRenameBody(guid GUID128, name string) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(name)), 6)
	body = bits.flush()
	return append(body, name...)
}

func TestCharacterRenameRoundTrip(t *testing.T) {
	guid := GUID128{Low: 0x9abc, High: 1 << 58}
	request, err := ParseCharacterRenameRequest(buildModernRenameBody(guid, "Chad"))
	if err != nil || request.GUID != guid || request.Name != "Chad" {
		t.Fatalf("rename request=%#v err=%v", request, err)
	}
	legacy := EncodeLegacyCharacterRename(0x4000000000000011, "Chad")
	if !isUint64At(legacy, 0, 0x4000000000000011) {
		t.Fatalf("legacy rename head=%x", legacy)
	}
	if !bytes.Equal(legacy[8:], []byte{'C', 'h', 'a', 'd', 0}) {
		t.Fatalf("legacy rename name=%q", legacy[8:])
	}

	// Legacy success result: result byte 0 + guid + CString.
	resultBody := []byte{0}
	resultBody = binary.LittleEndian.AppendUint64(resultBody, 0x4000000000000011)
	resultBody = append(resultBody, 'C', 'h', 'a', 'd', 0)
	result, err := ParseLegacyCharacterRenameResult(resultBody)
	if err != nil || result.Result != 0 || result.GUID != 0x4000000000000011 || result.Name != "Chad" {
		t.Fatalf("rename result=%#v err=%v", result, err)
	}
	modern := EncodeCharacterRenameResult(result, func(uint64) GUID128 { return guid })
	// u8 result, then has-guid bit + 6-bit name length, flush, packed guid, name.
	r := movementReader{data: modern}
	if code, _ := r.u8(); code != 0 {
		t.Fatalf("rename modern result=%d", code)
	}
	hasGUID, _ := r.bit()
	nameLen, _ := r.bits(6)
	if !hasGUID || nameLen != 4 {
		t.Fatalf("rename modern header hasGuid=%v len=%d", hasGUID, nameLen)
	}
	gotGuid, err := r.guid128()
	if err != nil || gotGuid != guid {
		t.Fatalf("rename modern guid=%#v err=%v", gotGuid, err)
	}
	if name := string(mustReadN(t, r, 4)); name != "Chad" {
		t.Fatalf("rename modern name=%q", name)
	}

	// Failure result carries just the byte.
	failed, err := ParseLegacyCharacterRenameResult([]byte{52})
	if err != nil || failed.Result != 52 {
		t.Fatalf("rename failure=%#v err=%v", failed, err)
	}
	failedBody := EncodeCharacterRenameResult(failed, func(uint64) GUID128 { return guid })
	// u8 result then the flushed has-guid+length bit group occupies one more byte.
	if len(failedBody) != 2 || failedBody[0] != 52 {
		t.Fatalf("rename failure modern=%x", failedBody)
	}
}

func mustReadN(t *testing.T, r movementReader, n int) []byte {
	t.Helper()
	value, err := r.stringN(n)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(value)
}
