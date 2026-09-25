package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestEmptyArenaTeamRoster(t *testing.T) {
	for index, size := range []uint32{2, 3, 5} {
		body := binary.LittleEndian.AppendUint32(nil, uint32(index))
		parsed, err := ParseArenaTeamRoster(body)
		if err != nil || parsed != uint32(index) {
			t.Fatalf("index=%d parsed=%d err=%v", index, parsed, err)
		}
		response := EncodeEmptyArenaTeamRoster(parsed)
		if len(response) != 37 || binary.LittleEndian.Uint32(response[:4]) != 0 ||
			binary.LittleEndian.Uint32(response[4:8]) != size || response[36] != 0 {
			t.Fatalf("index=%d response=%x", index, response)
		}
	}
	if _, err := ParseArenaTeamRoster([]byte{3, 0, 0, 0}); err == nil {
		t.Fatal("invalid arena-team index was accepted")
	}
	if err := ParseBattlePetRequestJournalLock(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseBattlePetRequestJournalLock([]byte{1}); err == nil {
		t.Fatal("battle-pet journal lock accepted trailing data")
	}
}

func TestArenaTeamIDStride(t *testing.T) {
	fields := map[int]uint32{
		LegacyPlayerArenaTeamInfo:                         10,
		LegacyPlayerArenaTeamInfo + ArenaTeamInfoStride:   20,
		LegacyPlayerArenaTeamInfo + 2*ArenaTeamInfoStride: 30,
		LegacyPlayerArenaTeamInfo + 6:                     99, // Hermes stride-6 slot 1
	}
	if got := ArenaTeamIDFromFields(fields, 0); got != 10 {
		t.Fatalf("slot 0 = %d", got)
	}
	if got := ArenaTeamIDFromFields(fields, 1); got != 20 {
		t.Fatalf("slot 1 = %d, stride 6 would have read 99", got)
	}
	if got := ArenaTeamIDFromFields(fields, 2); got != 30 {
		t.Fatalf("slot 2 = %d", got)
	}
	if ArenaTeamIDFromFields(fields, 3) != 0 || ArenaTeamIDFromFields(nil, 0) != 0 {
		t.Fatal("missing arena team id was not empty")
	}
}

func TestArenaTeamRosterTranslation(t *testing.T) {
	legacy := appendUint32(nil, 42)
	legacy = append(legacy, 0) // unk308
	legacy = appendUint32(legacy, 1, 2)
	legacy = appendUint64(legacy, 0x99)
	legacy = append(legacy, 1)
	legacy = appendCString(legacy, "Tester")
	legacy = appendUint32(legacy, 0) // captain
	legacy = append(legacy, 80, 8)
	legacy = appendUint32(legacy, 4, 3, 10, 7, 1500)

	roster, err := ParseLegacyArenaTeamRoster(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if roster.TeamID != 42 || roster.TeamSize != 2 || len(roster.Members) != 1 {
		t.Fatalf("roster = %+v", roster)
	}
	member := roster.Members[0]
	if member.GUID != 0x99 || !member.Online || member.Name != "Tester" || member.Captain != 0 ||
		member.Level != 80 || member.Class != 8 || member.PersonalRating != 1500 || member.HiddenRating != nil {
		t.Fatalf("member = %+v", member)
	}
	stats := ArenaTeamStats{WeekPlayed: 4, WeekWins: 3, SeasonPlayed: 10, SeasonWins: 7, Rating: 1800, Rank: 12}
	encoded, err := EncodeArenaTeamRoster(roster, stats, nil)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(encoded[0:4]) != 42 || binary.LittleEndian.Uint32(encoded[4:8]) != 2 ||
		binary.LittleEndian.Uint32(encoded[8:12]) != 4 || binary.LittleEndian.Uint32(encoded[24:28]) != 1800 ||
		binary.LittleEndian.Uint32(encoded[28:32]) != 12 || binary.LittleEndian.Uint32(encoded[32:36]) != 1 ||
		encoded[36] != 0 {
		t.Fatalf("header = %x", encoded[:37])
	}
	if !bytes.Contains(encoded[37:], []byte("Tester")) {
		t.Fatalf("encoded roster dropped the member name: %x", encoded)
	}

	hidden := appendUint32(nil, 7)
	hidden = append(hidden, 1)
	hidden = appendUint32(hidden, 1, 5)
	hidden = appendUint64(hidden, 1)
	hidden = append(hidden, 0)
	hidden = appendCString(hidden, "M")
	hidden = appendUint32(hidden, 1)
	hidden = append(hidden, 70, 1)
	hidden = appendUint32(hidden, 0, 0, 0, 0, 1)
	hidden = binary.LittleEndian.AppendUint32(hidden, math.Float32bits(1.5))
	hidden = binary.LittleEndian.AppendUint32(hidden, math.Float32bits(2.5))
	parsed, err := ParseLegacyArenaTeamRoster(hidden)
	if err != nil || parsed.Members[0].HiddenRating == nil || parsed.Members[0].HiddenRating[0] != 1.5 {
		t.Fatalf("hidden rating parse = %+v err=%v", parsed, err)
	}
	withFloats, err := EncodeArenaTeamRoster(parsed, ArenaTeamStats{}, nil)
	if err != nil || len(withFloats) < 8 {
		t.Fatal(err)
	}
}

func TestArenaTeamCommandKeepsLegacyErrors(t *testing.T) {
	for _, code := range []uint32{0, 8, 22, 23, 27, 30} {
		legacy := appendUint32(nil, 1)
		legacy = appendCString(legacy, "A")
		legacy = appendCString(legacy, "B")
		legacy = appendUint32(legacy, code)
		command, err := ParseLegacyArenaTeamCommand(legacy)
		if err != nil {
			t.Fatal(err)
		}
		if command.Action != 1 || command.Error != code || command.TeamName != "A" || command.PlayerName != "B" {
			t.Fatalf("command = %+v", command)
		}
		encoded, err := EncodeArenaTeamCommand(command)
		if err != nil {
			t.Fatal(err)
		}
		if encoded[0] != 1 || encoded[1] != byte(code) || encoded[2] != 0x02 || encoded[3] != 0x08 ||
			string(encoded[4:]) != "AB" {
			t.Fatalf("code %d encoded %x", code, encoded)
		}
	}
}

func TestArenaTeamEventAddsOne(t *testing.T) {
	legacy := []byte{3, 2}
	legacy = appendCString(legacy, "Tester")
	legacy = appendCString(legacy, "Team")
	legacy = appendUint64(legacy, 0x55)
	notice, err := ParseLegacyArenaTeamEvent(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if notice.Event != 4 || notice.Params[0] != "Tester" || notice.Params[1] != "Team" || notice.Params[2] != "" {
		t.Fatalf("notice = %+v", notice)
	}
	encoded, err := EncodeArenaTeamEvent(notice)
	if err != nil {
		t.Fatal(err)
	}
	if encoded[0] != 4 || !bytes.Contains(encoded, []byte("Tester")) || !bytes.Contains(encoded, []byte("Team")) {
		t.Fatalf("event = %x", encoded)
	}
	if _, err := ParseLegacyArenaTeamEvent([]byte{8, 4}); err == nil {
		t.Fatal("arena event accepted four strings")
	}
}

func TestArenaTeamQueryAndInvite(t *testing.T) {
	legacy := appendUint32(nil, 42)
	legacy = appendCString(legacy, "红领巾")
	legacy = appendUint32(legacy, 3, 0x11, 0x22, 0x33, 0x44, 0x55)
	emblem, err := ParseLegacyArenaTeamQuery(legacy)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeArenaTeamQueryResponse(emblem)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(encoded[:4]) != 42 || encoded[4] != 0x80 {
		t.Fatalf("query header = %x", encoded[:5])
	}
	if binary.LittleEndian.Uint32(encoded[5:9]) != 42 || binary.LittleEndian.Uint32(encoded[9:13]) != 3 ||
		binary.LittleEndian.Uint32(encoded[13:17]) != 0x11 || !bytes.Contains(encoded, []byte("红领巾")) {
		t.Fatalf("query = %x", encoded)
	}

	inviteLegacy := appendCString(nil, "Tester")
	inviteLegacy = appendCString(inviteLegacy, "红领巾")
	names, err := ParseLegacyArenaTeamInvite(inviteLegacy)
	if err != nil {
		t.Fatal(err)
	}
	player := GUID128{Low: 9, High: uint64(2) << 58}
	team := ModernArenaTeamGUID(1)
	invite, err := EncodeArenaTeamInvite(player, team, 0x01010001, names)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(invite, appendPackedGUID128(nil, player.Low, player.High)) ||
		!bytes.Contains(invite, []byte("Tester")) || !bytes.Contains(invite, []byte("红领巾")) {
		t.Fatalf("invite = %x", invite)
	}
}

func TestArenaTeamClientRequests(t *testing.T) {
	player := appendPackedGUID128(nil, 4, uint64(2)<<58)
	team := appendPackedGUID128(nil, 1, arenaInspectTeamType)
	if err := ParseArenaTeamAccept(append(player, team...)); err != nil {
		t.Fatal(err)
	}
	if err := ParseArenaTeamAccept(player); err == nil {
		t.Fatal("accept with one guid was accepted")
	}
	teamID, err := ParseArenaTeamDisband(appendUint32(nil, 42))
	if err != nil || teamID != 42 {
		t.Fatalf("disband = %d err=%v", teamID, err)
	}
	removedID, removed, err := ParseArenaTeamRemove(append(appendUint32(nil, 42), player...))
	if err != nil || removedID != 42 || removed.Low != 4 {
		t.Fatalf("remove = %d %+v err=%v", removedID, removed, err)
	}

	joinBody := append(appendPackedGUID128(nil, 8, uint64(3)<<58), 1, 9)
	join, err := ParseBattlemasterJoinArena(joinBody)
	if err != nil || join.Slot != 1 || !join.AsGroup || join.GUID.Low != 8 {
		t.Fatalf("join = %+v err=%v", join, err)
	}
	if _, err := ParseBattlemasterJoinArena(append(appendPackedGUID128(nil, 8, uint64(3)<<58), 3, 0)); err == nil {
		t.Fatal("rated join accepted team size 3 as a slot")
	}

	skirmish := append(appendPackedGUID128(nil, 8, uint64(3)<<58), 0, 3, 0x80)
	parsed, err := ParseBattlemasterJoinSkirmish(skirmish)
	if err != nil || parsed.Slot != 1 || !parsed.AsGroup {
		t.Fatalf("size 3 skirmish = %+v err=%v", parsed, err)
	}
	five := append(appendPackedGUID128(nil, 8, uint64(3)<<58), 0, 5, 0)
	parsed, err = ParseBattlemasterJoinSkirmish(five)
	if err != nil || parsed.Slot != 2 || parsed.AsGroup {
		t.Fatalf("size 5 skirmish = %+v err=%v", parsed, err)
	}
	slotTwo := append(appendPackedGUID128(nil, 8, uint64(3)<<58), 0, 2, 0x40)
	parsed, err = ParseBattlemasterJoinSkirmish(slotTwo)
	if err != nil || parsed.Slot != 2 || parsed.AsGroup {
		t.Fatalf("slot 2 skirmish = %+v err=%v", parsed, err)
	}

	legacy := EncodeLegacyBattlemasterJoin(0x1234, parsed.Slot, 0, 0)
	if binary.LittleEndian.Uint64(legacy[:8]) != 0x1234 || legacy[8] != 2 || legacy[9] != 0 || legacy[10] != 0 {
		t.Fatalf("legacy join = %x", legacy)
	}
	if string(EncodeLegacyArenaTeamRemove(42, "Tester")[4:]) != "Tester\x00" {
		t.Fatal("legacy remove name was not a CString")
	}
}

func TestArenaTeamStats(t *testing.T) {
	body := appendUint32(nil, 42, 1800, 4, 3, 10, 7, 12)
	teamID, stats, err := ParseLegacyArenaTeamStats(body)
	if err != nil || teamID != 42 || stats.Rating != 1800 || stats.Rank != 12 || stats.SeasonWins != 7 {
		t.Fatalf("stats = %d %+v err=%v", teamID, stats, err)
	}
	if _, _, err := ParseLegacyArenaTeamStats(body[:len(body)-4]); err == nil {
		t.Fatal("short stats packet was accepted")
	}
}

func appendUint32(dst []byte, values ...uint32) []byte {
	for _, value := range values {
		dst = binary.LittleEndian.AppendUint32(dst, value)
	}
	return dst
}

func appendUint64(dst []byte, value uint64) []byte {
	return binary.LittleEndian.AppendUint64(dst, value)
}
