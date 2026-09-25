package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestPartyInviteTranslation(t *testing.T) {
	bits := newBitWriter([]byte{1})
	bits.writeBits(5, 9)
	bits.writeBits(5, 9)
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, 0x01010001)
	body = appendPackedGUID128(body, 0x42, uint64(2)<<58|uint64(1)<<42)
	body = append(body, "AliceRealm"...)

	request, err := ParsePartyInvite(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.PartyIndex != 1 || request.TargetName != "Alice" || request.TargetRealm != "Realm" {
		t.Fatalf("unexpected invite: %#v", request)
	}
	legacy := EncodeLegacyPartyInvite(request.TargetName)
	if string(legacy[:6]) != "Alice\x00" || len(legacy) != 10 {
		t.Fatalf("unexpected legacy invite: %x", legacy)
	}

	responseBits := newBitWriter(nil)
	responseBits.writeBit(true)
	responseBits.writeBit(true)
	responseBits.writeBit(true)
	response := responseBits.flush()
	response = append(response, 1, 4)
	parsed, err := ParsePartyInviteResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Accept || parsed.PartyIndex != 1 || parsed.RolesDesired != 4 {
		t.Fatalf("unexpected invite response: %#v", parsed)
	}
	if got := EncodeLegacyPartyInviteResponse(parsed); len(got) != 4 {
		t.Fatalf("legacy accept body has %d bytes", len(got))
	}
}

func TestLegacyPartyInviteAndGroupListTranslation(t *testing.T) {
	invite := []byte{1}
	invite = append(invite, "Inviter"...)
	invite = append(invite, 0)
	invite = binary.LittleEndian.AppendUint32(invite, 2)
	invite = append(invite, 0)
	invite = binary.LittleEndian.AppendUint32(invite, 0)
	parsed, err := ParseLegacyPartyInvite(invite)
	if err != nil {
		t.Fatal(err)
	}
	modernInvite := EncodePartyInvite(parsed, 0x01010001, "Test Realm")
	if len(modernInvite) == 0 || !parsed.CanAccept || parsed.InviterName != "Inviter" {
		t.Fatalf("unexpected translated invite: %#v / %x", parsed, modernInvite)
	}
	inviteReader := movementReader{data: modernInvite}
	canAccept, _ := inviteReader.bit()
	for range 5 {
		_, _ = inviteReader.bit()
	}
	nameLength, _ := inviteReader.bits(6)
	realmAddress, _ := inviteReader.u32()
	hasRealm, _ := inviteReader.bit()
	_, _ = inviteReader.bit()
	realmLength, _ := inviteReader.bits(8)
	normalizedLength, _ := inviteReader.bits(8)
	_, _ = inviteReader.stringN(int(realmLength))
	_, _ = inviteReader.stringN(int(normalizedLength))
	_, _ = inviteReader.guid128()
	_, _ = inviteReader.guid128()
	_, _ = inviteReader.u16()
	roles, _ := inviteReader.u8()
	slotCount, _ := inviteReader.u32()
	_, _ = inviteReader.i32()
	inviterName, nameErr := inviteReader.stringN(int(nameLength))
	if nameErr != nil || !canAccept || !hasRealm || realmAddress != 0x01010001 || roles != 2 || slotCount != 0 || inviterName != "Inviter" || inviteReader.remaining() != 0 {
		t.Fatalf("modern invite layout name=%q roles=%d slots=%d remaining=%d err=%v body=%x", inviterName, roles, slotCount, inviteReader.remaining(), nameErr, modernInvite)
	}

	const (
		selfGUID   = uint64(0x42)
		memberGUID = uint64(0x43)
		partyGUID  = uint64(0x1001)
	)
	group := []byte{2, 0, 0, 0} // live raid roster
	group = binary.LittleEndian.AppendUint64(group, partyGUID)
	group = binary.LittleEndian.AppendUint32(group, 7)
	group = binary.LittleEndian.AppendUint32(group, 1)
	group = append(group, "Bob"...)
	group = append(group, 0)
	group = binary.LittleEndian.AppendUint64(group, memberGUID)
	group = append(group, 2, 0, 0, 0) // PvP flag without the online bit
	group = binary.LittleEndian.AppendUint64(group, selfGUID)
	group = append(group, 2)
	group = binary.LittleEndian.AppendUint64(group, 0)
	group = append(group, 2, 0, 1, 2) // threshold, dungeon, raid, dynamic raid
	self := PartyPlayer{GUID: ModernGUIDForLegacy(selfGUID, 0), Name: "Alice", ClassID: 8, FactionGroup: 2}
	update, _, err := TranslateLegacyGroupList(group, self, selfGUID, 3, func(guid uint64) PartyPlayer {
		return PartyPlayer{GUID: ModernGUIDForLegacy(guid, 0)}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(update) < 20 || binary.LittleEndian.Uint16(update) != 0x02 {
		t.Fatalf("unexpected party update flags=%#x body=%x", binary.LittleEndian.Uint16(update), update)
	}
	updateReader := movementReader{data: update}
	_, _ = updateReader.u16()
	_, _ = updateReader.u8()
	_, _ = updateReader.u8()
	myIndex, _ := updateReader.i32()
	_, _ = updateReader.guid128()
	_, _ = updateReader.i32()
	_, _ = updateReader.guid128()
	_, _ = updateReader.u8()
	playerCount, _ := updateReader.u32()
	_, _ = updateReader.bit()
	hasLoot, _ := updateReader.bit()
	hasDifficulty, _ := updateReader.bit()
	updateReader.align()
	names := make([]string, 0, playerCount)
	connected := make([]bool, 0, playerCount)
	subgroups := make([]byte, 0, playerCount)
	flags := make([]byte, 0, playerCount)
	memberRoleValues := make([]byte, 0, playerCount)
	classes := make([]byte, 0, playerCount)
	factions := make([]byte, 0, playerCount)
	for range playerCount {
		length, _ := updateReader.bits(6)
		_, _ = updateReader.bits(6)
		isConnected, _ := updateReader.bit()
		connected = append(connected, isConnected)
		_, _ = updateReader.bit()
		_, _ = updateReader.bit()
		_, _ = updateReader.guid128()
		subgroup, _ := updateReader.u8()
		memberFlags, _ := updateReader.u8()
		memberRoles, _ := updateReader.u8()
		classID, _ := updateReader.u8()
		faction, _ := updateReader.u8()
		subgroups = append(subgroups, subgroup)
		flags = append(flags, memberFlags)
		memberRoleValues = append(memberRoleValues, memberRoles)
		classes = append(classes, classID)
		factions = append(factions, faction)
		name, readErr := updateReader.stringN(int(length))
		if readErr != nil {
			t.Fatal(readErr)
		}
		names = append(names, name)
	}
	_, _ = updateReader.u8()
	_, _ = updateReader.guid128()
	_, _ = updateReader.u8()
	dungeon, _ := updateReader.u32()
	raid, _ := updateReader.u32()
	dynamicRaid, _ := updateReader.u32()
	if myIndex != 0 || playerCount != 2 || !hasLoot || !hasDifficulty || names[0] != "Alice" || names[1] != "Bob" ||
		connected[0] || connected[1] || subgroups[0] != 0 || subgroups[1] != 0 ||
		flags[0] != 0 || flags[1] != 0 || memberRoleValues[0] != 0 || memberRoleValues[1] != 0 ||
		classes[0] != 8 || classes[1] != 0 || factions[0] != 0 || factions[1] != 0 ||
		dungeon != 1 || raid != 4 || dynamicRaid != 5 || updateReader.remaining() != 0 {
		t.Fatalf("party roster my_index=%d count=%d names=%v connected=%v subgroups=%v flags=%v roles=%v classes=%v factions=%v loot=%v difficulty=%v values=%d/%d/%d remaining=%d body=%x",
			myIndex, playerCount, names, connected, subgroups, flags, memberRoleValues, classes, factions, hasLoot, hasDifficulty, dungeon, raid, dynamicRaid, updateReader.remaining(), update)
	}

	destroyed := []byte{0, 0, 0, 0}
	destroyed = binary.LittleEndian.AppendUint64(destroyed, partyGUID)
	destroyed = binary.LittleEndian.AppendUint32(destroyed, 8)
	destroyed = binary.LittleEndian.AppendUint32(destroyed, 0)
	destroyed = binary.LittleEndian.AppendUint64(destroyed, selfGUID)
	destroyedUpdate, _, err := TranslateLegacyGroupList(destroyed, self, selfGUID, 4, func(guid uint64) PartyPlayer {
		return PartyPlayer{GUID: ModernGUIDForLegacy(guid, 0)}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(destroyedUpdate); got != 0x10 {
		t.Fatalf("destroyed party flags=%#x body=%x", got, destroyedUpdate)
	}
}

func TestEmptyInviteResponseIsADecline(t *testing.T) {
	response, err := ParsePartyInviteResponse([]byte{0})
	if err != nil {
		t.Fatal(err)
	}
	if response.Accept {
		t.Fatal("empty invite response unexpectedly accepted")
	}
}

func TestPartyMemberStatsAndRaidTargetTranslation(t *testing.T) {
	if err := ParsePartyJoinUpdates([]byte{0, 1}); err != nil {
		t.Fatal(err)
	}
	if err := ParsePartyJoinUpdates([]byte{0}); err == nil {
		t.Fatal("short party-join-updates probe was accepted")
	}

	target := GUID128{Low: 0x43, High: uint64(2)<<58 | uint64(1)<<42}
	statsBody := []byte{0}
	statsBody = appendPackedGUID128(statsBody, target.Low, target.High)
	stats, err := ParsePartyMemberStatsRequest(statsBody)
	if err != nil || stats.PartyIndex != 0 || stats.Target != target {
		t.Fatalf("stats=%#v err=%v body=%x", stats, err, statsBody)
	}
	statsBody = appendPackedGUID128([]byte{1}, target.Low, target.High)
	stats, err = ParsePartyMemberStatsRequest(statsBody)
	if err != nil || stats.PartyIndex != 1 || stats.Target != target {
		t.Fatalf("indexed stats=%#v err=%v body=%x", stats, err, statsBody)
	}
	statsBody = appendPackedGUID128([]byte{0}, target.Low, target.High)
	statsBody = append(statsBody, 1)
	stats, err = ParsePartyMemberStatsRequest(statsBody)
	if err != nil || stats.PartyIndex != 0 || stats.Target != target {
		t.Fatalf("stats with trailing flag=%#v err=%v body=%x", stats, err, statsBody)
	}
	modernStatsBody := appendPackedGUID128([]byte{0x80}, target.Low, target.High)
	modernStatsBody = append(modernStatsBody, 1)
	stats, err = ParsePartyMemberStatsRequest(modernStatsBody)
	if err != nil || stats.PartyIndex != 1 || stats.Target != target {
		t.Fatalf("modern indexed stats=%#v err=%v body=%x", stats, err, modernStatsBody)
	}
	if got := EncodeLegacyPartyMemberStatsRequest(0x43); len(got) != 8 || binary.LittleEndian.Uint64(got) != 0x43 {
		t.Fatalf("legacy party-member request=%x", got)
	}

	raidBody := []byte{0}
	raidBody = appendPackedGUID128(raidBody, target.Low, target.High)
	raidBody = append(raidBody, 7)
	raid, err := ParseRaidTargetRequest(raidBody)
	if err != nil || raid.PartyIndex != 0 || raid.Target != target || raid.Symbol != 7 {
		t.Fatalf("raid=%#v err=%v body=%x", raid, err, raidBody)
	}
	modernRaidBody := appendPackedGUID128([]byte{0x80}, target.Low, target.High)
	modernRaidBody = append(modernRaidBody, 7, 1)
	modernRaid, err := ParseRaidTargetRequest(modernRaidBody)
	if err != nil || modernRaid.PartyIndex != 1 || modernRaid.Target != target || modernRaid.Symbol != 7 {
		t.Fatalf("modern raid=%#v err=%v body=%x", modernRaid, err, modernRaidBody)
	}
	legacyRaid := EncodeLegacyRaidTargetRequest(raid, 0x43)
	if len(legacyRaid) != 9 || legacyRaid[0] != 7 || binary.LittleEndian.Uint64(legacyRaid[1:]) != 0x43 {
		t.Fatalf("legacy raid target=%x", legacyRaid)
	}

	legacySingle := []byte{0}
	legacySingle = binary.LittleEndian.AppendUint64(legacySingle, 0x42)
	legacySingle = append(legacySingle, 7)
	legacySingle = binary.LittleEndian.AppendUint64(legacySingle, 0x43)
	single, err := ParseLegacyRaidTargetUpdate(legacySingle)
	if err != nil || single.All || single.ChangedBy != 0x42 || single.Symbol != 7 || single.Target != 0x43 {
		t.Fatalf("single=%#v err=%v", single, err)
	}
	modernSingle := EncodeRaidTargetUpdateSingle(0, single.Symbol, target, GUID128{Low: 0x42, High: 1})
	r := movementReader{data: modernSingle}
	partyIndex, _ := r.u8()
	symbol, _ := r.u8()
	decodedTarget, _ := r.guid128()
	changedBy, _ := r.guid128()
	if partyIndex != 0 || symbol != 7 || decodedTarget != target || changedBy != (GUID128{Low: 0x42, High: 1}) || r.remaining() != 0 {
		t.Fatalf("modern single party=%d symbol=%d target=%#v changer=%#v remaining=%d body=%x", partyIndex, symbol, decodedTarget, changedBy, r.remaining(), modernSingle)
	}

	legacyAll := []byte{1, 0}
	legacyAll = binary.LittleEndian.AppendUint64(legacyAll, 0x43)
	legacyAll = append(legacyAll, 7)
	legacyAll = binary.LittleEndian.AppendUint64(legacyAll, 0x44)
	all, err := ParseLegacyRaidTargetUpdate(legacyAll)
	if err != nil || !all.All || len(all.Targets) != 2 || all.Targets[0].Symbol != 0 || all.Targets[1].Target != 0x44 {
		t.Fatalf("all=%#v err=%v", all, err)
	}
	modernAll := EncodeRaidTargetUpdateAll(0, []RaidTarget{{Symbol: 0, Target: target}, {Symbol: 7, Target: GUID128{Low: 0x44, High: 1}}})
	if modernAll[0] != 0 || binary.LittleEndian.Uint32(modernAll[1:5]) != 2 {
		t.Fatalf("modern all=%x", modernAll)
	}

	invalid := append([]byte(nil), raidBody...)
	invalid[len(invalid)-1] = 8
	if _, err := ParseRaidTargetRequest(invalid); err == nil {
		t.Fatal("invalid raid symbol was accepted")
	}
}

func decodePartyUpdateNames(t *testing.T, update []byte) ([]string, []byte, int32) {
	t.Helper()
	r := movementReader{data: update}
	_, _ = r.u16()
	_, _ = r.u8()
	_, _ = r.u8()
	myIndex, _ := r.i32()
	_, _ = r.guid128()
	_, _ = r.i32()
	_, _ = r.guid128()
	_, _ = r.u8()
	playerCount, _ := r.u32()
	_, _ = r.bit()
	_, _ = r.bit()
	_, _ = r.bit()
	r.align()
	names := make([]string, 0, playerCount)
	groups := make([]byte, 0, playerCount)
	for range playerCount {
		length, _ := r.bits(6)
		_, _ = r.bits(6)
		_, _ = r.bit()
		_, _ = r.bit()
		_, _ = r.bit()
		_, _ = r.guid128()
		subgroup, _ := r.u8()
		groups = append(groups, subgroup)
		_, _ = r.u8()
		_, _ = r.u8()
		_, _ = r.u8()
		_, _ = r.u8()
		name, err := r.stringN(int(length))
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return names, groups, myIndex
}

// Each recipient has an independent, differently ordered leave-one-out packet.
// Moving subgroups must preserve the same raid indices and each member's group.
func TestLegacyGroupListCanonicalOrderAcrossRecipients(t *testing.T) {
	members := []uint64{0x42, 0x43, 0x44, 0x142}
	names := []string{"Zulu", "Bob", "Alice", "HighGUID"}
	resolve := func(guid uint64) PartyPlayer { return PartyPlayer{GUID: ModernGUIDForLegacy(guid, 0)} }
	for moved := 0; moved < 2; moved++ {
		groups := []byte{0, 0, 1, 2}
		if moved != 0 {
			groups[0], groups[2] = 1, 0
		}
		for viewer, selfGUID := range members {
			for reverse := 0; reverse < 2; reverse++ {
				group := []byte{2, groups[viewer], 0, 0}
				group = binary.LittleEndian.AppendUint64(group, 0x1001)
				group = binary.LittleEndian.AppendUint32(group, 9)
				group = binary.LittleEndian.AppendUint32(group, uint32(len(members)-1))
				for n := range members {
					i := n
					if reverse != 0 {
						i = len(members) - 1 - n
					}
					if i == viewer {
						continue
					}
					group = append(group, names[i]...)
					group = append(group, 0)
					group = binary.LittleEndian.AppendUint64(group, members[i])
					group = append(group, 1, groups[i], 0, 0)
				}
				group = binary.LittleEndian.AppendUint64(group, members[0])
				group = append(group, 0)
				group = binary.LittleEndian.AppendUint64(group, 0)
				group = append(group, 2, 0, 1, 2)
				self := resolve(selfGUID)
				self.Name = names[viewer]
				update, order, err := TranslateLegacyGroupList(group, self, selfGUID, 1, resolve)
				if err != nil {
					t.Fatal(err)
				}
				gotNames, gotGroups, myIndex := decodePartyUpdateNames(t, update)
				if int(myIndex) != viewer || len(gotNames) != len(members) || len(order) != len(members) {
					t.Fatalf("viewer=%d index=%d names=%v order=%v", viewer, myIndex, gotNames, order)
				}
				for i := range members {
					if order[i] != members[i] || gotNames[i] != names[i] || gotGroups[i] != groups[i] {
						t.Fatalf("viewer=%d moved=%d reverse=%d order=%v names=%v groups=%v", viewer, moved, reverse, order, gotNames, gotGroups)
					}
				}
			}
		}
	}
}
