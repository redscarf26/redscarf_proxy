package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestReadyCheckSemanticTranslation(t *testing.T) {
	bits := newBitWriter(nil)
	bits.writeBit(true)
	bits.writeBit(true)
	requestBody := append(bits.flush(), 1)
	request, err := ParseReadyCheckResponse(requestBody)
	if err != nil || !request.HasParty || request.PartyIndex != 1 || !request.Ready {
		t.Fatalf("ready response=%#v err=%v body=%x", request, err, requestBody)
	}

	party := modernPartyGUID(0x1001)
	player := ModernGUIDForLegacy(0x42, 0)
	started := EncodeReadyCheckStarted(1, party, player)
	r := movementReader{data: started}
	partyIndex, _ := r.u8()
	decodedParty, _ := r.guid128()
	initiator, _ := r.guid128()
	duration, _ := r.u64()
	if partyIndex != 1 || decodedParty != party || initiator != player || duration != readyCheckDurationMS || r.remaining() != 0 {
		t.Fatalf("ready start party=%d/%#v initiator=%#v duration=%d remaining=%d body=%x", partyIndex, decodedParty, initiator, duration, r.remaining(), started)
	}

	response := EncodeReadyCheckResponse(party, player, true)
	r = movementReader{data: response}
	decodedParty, _ = r.guid128()
	decodedPlayer, _ := r.guid128()
	ready, _ := r.bit()
	r.align()
	if decodedParty != party || decodedPlayer != player || !ready || r.remaining() != 0 {
		t.Fatalf("ready response party=%#v player=%#v ready=%v remaining=%d body=%x", decodedParty, decodedPlayer, ready, r.remaining(), response)
	}

	legacyStart := binary.LittleEndian.AppendUint64(nil, 0x42)
	if initiatorGUID, err := ParseLegacyReadyCheckStarted(legacyStart); err != nil || initiatorGUID != 0x42 {
		t.Fatalf("legacy ready start=%x err=%v", legacyStart, err)
	}
	legacyResponse := append(binary.LittleEndian.AppendUint64(nil, 0x43), 1)
	if responder, isReady, err := ParseLegacyReadyCheckResponse(legacyResponse); err != nil || responder != 0x43 || !isReady {
		t.Fatalf("legacy ready response responder=%x ready=%v err=%v", responder, isReady, err)
	}
}

func TestPartyLeadershipUninviteLootAndRaidConversion(t *testing.T) {
	target := ModernGUIDForLegacy(0x43, 0)

	uninviteBits := newBitWriter(nil)
	uninviteBits.writeBit(true)
	uninviteBits.writeBits(3, 8)
	uninviteBody := appendPackedGUID128(uninviteBits.flush(), target.Low, target.High)
	uninviteBody = append(uninviteBody, 1)
	uninviteBody = append(uninviteBody, "bye"...)
	uninvite, err := ParsePartyUninvite(uninviteBody)
	if err != nil || uninvite.Target != target || uninvite.PartyIndex != 1 || uninvite.Reason != "bye" {
		t.Fatalf("uninvite=%#v err=%v body=%x", uninvite, err, uninviteBody)
	}
	legacyUninvite := EncodeLegacyPartyUninvite(0x43, uninvite.Reason)
	if binary.LittleEndian.Uint64(legacyUninvite) != 0x43 || string(legacyUninvite[8:]) != "bye\x00" {
		t.Fatalf("legacy uninvite=%x", legacyUninvite)
	}

	leaderBits := newBitWriter(nil)
	leaderBits.writeBit(true)
	leaderBody := appendPackedGUID128(leaderBits.flush(), target.Low, target.High)
	leaderBody = append(leaderBody, 1)
	leader, err := ParseSetPartyLeader(leaderBody)
	if err != nil || leader.Target != target || leader.PartyIndex != 1 {
		t.Fatalf("leader=%#v err=%v body=%x", leader, err, leaderBody)
	}
	if got := EncodeLegacySetPartyLeader(0x43); len(got) != 8 || binary.LittleEndian.Uint64(got) != 0x43 {
		t.Fatalf("legacy leader=%x", got)
	}

	lootBody := []byte{1, 2}
	lootBody = appendPackedGUID128(lootBody, target.Low, target.High)
	lootBody = binary.LittleEndian.AppendUint32(lootBody, 4)
	loot, err := ParseSetLootMethod(lootBody)
	if err != nil || loot.PartyIndex != 1 || loot.Method != 2 || loot.LootMaster != target || loot.LootThreshold != 4 {
		t.Fatalf("loot=%#v err=%v body=%x", loot, err, lootBody)
	}
	unspecifiedPartyLootBody := appendPackedGUID128([]byte{0xff, 2}, target.Low, target.High)
	unspecifiedPartyLootBody = binary.LittleEndian.AppendUint32(unspecifiedPartyLootBody, 4)
	unspecifiedPartyLoot, err := ParseSetLootMethod(unspecifiedPartyLootBody)
	if err != nil || unspecifiedPartyLoot.PartyIndex != 0xff || unspecifiedPartyLoot.Method != 2 || unspecifiedPartyLoot.LootMaster != target || unspecifiedPartyLoot.LootThreshold != 4 {
		t.Fatalf("unspecified-party loot=%#v err=%v body=%x", unspecifiedPartyLoot, err, unspecifiedPartyLootBody)
	}
	legacyLoot := EncodeLegacySetLootMethod(loot, 0x43)
	if len(legacyLoot) != 16 || binary.LittleEndian.Uint32(legacyLoot) != 2 || binary.LittleEndian.Uint64(legacyLoot[4:]) != 0x43 || binary.LittleEndian.Uint32(legacyLoot[12:]) != 4 {
		t.Fatalf("legacy loot=%x", legacyLoot)
	}
	artifactLoot := appendTestLootThreshold(lootBody, 6)
	if _, err := ParseSetLootMethod(artifactLoot); err != nil {
		t.Fatalf("artifact loot threshold was rejected: %v", err)
	}
	invalidLoot := appendTestLootThreshold(lootBody, 7)
	if _, err := ParseSetLootMethod(invalidLoot); err == nil {
		t.Fatal("heirloom loot threshold was accepted by the 3.3.5 bridge")
	}

	if raid, err := ParseConvertRaid([]byte{0x80}); err != nil || !raid {
		t.Fatalf("party-to-raid=%v err=%v", raid, err)
	}
	if raid, err := ParseConvertRaid([]byte{0}); err != nil || raid {
		t.Fatalf("raid-to-party=%v err=%v", raid, err)
	}
	if pass, err := ParseOptOutOfLoot([]byte{0x80}); err != nil || !pass {
		t.Fatalf("opt out of loot=%v err=%v", pass, err)
	}
}

func appendTestLootThreshold(body []byte, threshold uint32) []byte {
	result := append([]byte(nil), body[:len(body)-4]...)
	return binary.LittleEndian.AppendUint32(result, threshold)
}

func TestRolesAssistantAssignmentAndSubgroups(t *testing.T) {
	target := ModernGUIDForLegacy(0x43, 0)
	header := newBitWriter(nil)
	header.writeBit(true)
	header.writeBit(true)
	assistantBody := appendPackedGUID128(header.flush(), target.Low, target.High)
	assistantBody = append(assistantBody, 1)
	assistant, err := ParseSetAssistantLeader(assistantBody)
	if err != nil || !assistant.Apply || assistant.Target != target || assistant.PartyIndex != 1 {
		t.Fatalf("assistant=%#v err=%v body=%x", assistant, err, assistantBody)
	}
	legacyAssistant := EncodeLegacySetAssistantLeader(0x43, true)
	if len(legacyAssistant) != 9 || binary.LittleEndian.Uint64(legacyAssistant) != 0x43 || legacyAssistant[8] != 1 {
		t.Fatalf("legacy assistant=%x", legacyAssistant)
	}

	header = newBitWriter(nil)
	header.writeBit(true)
	header.writeBit(true)
	assignmentBody := append(header.flush(), 1)
	assignmentBody = appendPackedGUID128(assignmentBody, target.Low, target.High)
	assignmentBody = append(assignmentBody, 1)
	assignment, err := ParseSetPartyAssignment(assignmentBody)
	if err != nil || !assignment.Apply || assignment.Assignment != 1 || assignment.Target != target || assignment.PartyIndex != 1 {
		t.Fatalf("assignment=%#v err=%v body=%x", assignment, err, assignmentBody)
	}
	legacyAssignment := EncodeLegacySetPartyAssignment(assignment, 0x43)
	if len(legacyAssignment) != 10 || legacyAssignment[0] != 1 || legacyAssignment[1] != 1 || binary.LittleEndian.Uint64(legacyAssignment[2:]) != 0x43 {
		t.Fatalf("legacy assignment=%x", legacyAssignment)
	}

	header = newBitWriter(nil)
	header.writeBit(true)
	roleBody := appendPackedGUID128(header.flush(), target.Low, target.High)
	roleBody = append(roleBody, 4, 1)
	role, err := ParseSetRole(roleBody)
	if err != nil || role.Target != target || role.Role != 4 || role.PartyIndex != 1 {
		t.Fatalf("role=%#v err=%v body=%x", role, err, roleBody)
	}
	roleChanged := EncodeRoleChangedInform(1, ModernGUIDForLegacy(0x42, 0), target, 0, 4)
	if len(roleChanged) < 6 || roleChanged[0] != 1 {
		t.Fatalf("role changed=%x", roleChanged)
	}

	changeBody := appendPackedGUID128(nil, target.Low, target.High)
	changeBody = append(changeBody, 7, 0) // subgroup 7, no optional party index
	change, err := ParseChangeSubGroup(changeBody)
	if err != nil || change.Target != target || change.NewSubGroup != 7 {
		t.Fatalf("change subgroup=%#v err=%v body=%x", change, err, changeBody)
	}
	if got := EncodeLegacyChangeSubGroup("Alice", 7); string(got) != "Alice\x00\x07" {
		t.Fatalf("legacy subgroup=%x", got)
	}

	header = newBitWriter(nil)
	header.writeBit(false)
	swapBody := appendPackedGUID128(header.flush(), target.Low, target.High)
	swapBody = appendPackedGUID128(swapBody, ModernGUIDForLegacy(0x44, 0).Low, ModernGUIDForLegacy(0x44, 0).High)
	swap, err := ParseSwapSubGroups(swapBody)
	if err != nil || swap.FirstTarget != target || swap.SecondTarget.Low != 0x44 {
		t.Fatalf("swap subgroup=%#v err=%v body=%x", swap, err, swapBody)
	}
}

func TestMinimapPingRandomRollAndPartySnapshot(t *testing.T) {
	header := newBitWriter(nil)
	header.writeBit(true)
	pingBody := header.flush()
	pingBody = binary.LittleEndian.AppendUint32(pingBody, math.Float32bits(0.25))
	pingBody = binary.LittleEndian.AppendUint32(pingBody, math.Float32bits(0.75))
	pingBody = append(pingBody, 1)
	ping, err := ParseMinimapPing(pingBody)
	if err != nil || ping.PartyIndex != 1 || ping.X != 0.25 || ping.Y != 0.75 {
		t.Fatalf("ping=%#v err=%v body=%x", ping, err, pingBody)
	}
	if got := EncodeLegacyMinimapPing(ping); len(got) != 8 || math.Float32frombits(binary.LittleEndian.Uint32(got)) != 0.25 {
		t.Fatalf("legacy ping=%x", got)
	}

	header = newBitWriter(nil)
	header.writeBit(false)
	rollBody := binary.LittleEndian.AppendUint32(header.flush(), 1)
	rollBody = binary.LittleEndian.AppendUint32(rollBody, 100)
	roll, err := ParseRandomRoll(rollBody)
	if err != nil || roll.Minimum != 1 || roll.Maximum != 100 {
		t.Fatalf("roll=%#v err=%v body=%x", roll, err, rollBody)
	}

	group := []byte{0, 0, 0, 4}
	group = binary.LittleEndian.AppendUint64(group, 0x1001)
	group = binary.LittleEndian.AppendUint32(group, 7)
	group = binary.LittleEndian.AppendUint32(group, 1)
	group = append(group, "Bob"...)
	group = append(group, 0)
	group = binary.LittleEndian.AppendUint64(group, 0x43)
	group = append(group, 1, 2, 1, 8)
	group = binary.LittleEndian.AppendUint64(group, 0x42) // leader
	group = append(group, 2)                              // master loot
	group = binary.LittleEndian.AppendUint64(group, 0x42)
	group = append(group, 3, 1, 2, 3) // threshold and difficulties
	snapshot, err := ParseLegacyPartySnapshot(group, 0x42, "Alice")
	wantPartyGUID := GUID128{Low: 0x1001, High: uint64(33) << 58}
	if err != nil || snapshot.LegacyGUID != 0x1001 || snapshot.GUID != wantPartyGUID || snapshot.Destroyed || len(snapshot.Members) != 2 || snapshot.Members[0].Roles != 4 || snapshot.Members[1].Name != "Bob" || snapshot.Members[1].Subgroup != 2 || snapshot.LootMethod != 2 || snapshot.LootMaster != 0x42 || snapshot.LootThreshold != 3 {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
}
