package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestLFGJoinLeaveProposalAndRoles(t *testing.T) {
	joinBody := []byte{0x00, 8}
	joinBody = binary.LittleEndian.AppendUint32(joinBody, 1)
	joinBody = binary.LittleEndian.AppendUint32(joinBody, 0x123)
	join, err := ParseDFJoin(joinBody)
	if err != nil {
		t.Fatal(err)
	}
	if join.Roles != 8 || len(join.Slots) != 1 || join.Slots[0] != 0x123 {
		t.Fatalf("join = %+v", join)
	}
	wantJoin := binary.LittleEndian.AppendUint32(nil, 8)
	wantJoin = append(wantJoin, 0, 0, 1)
	wantJoin = binary.LittleEndian.AppendUint32(wantJoin, 0x123)
	wantJoin = append(wantJoin, 3, 0, 0, 0, 0)
	if !bytes.Equal(EncodeLegacyLFGJoin(join), wantJoin) {
		t.Fatalf("legacy join = %x", EncodeLegacyLFGJoin(join))
	}

	grouped := []byte{0x40, 2}
	grouped = binary.LittleEndian.AppendUint32(grouped, 1)
	grouped = append(grouped, 7)
	grouped = binary.LittleEndian.AppendUint32(grouped, 0x45)
	parsed, err := ParseDFJoin(grouped)
	if err != nil || parsed.Roles != 2 || len(parsed.Slots) != 1 || parsed.Slots[0] != 0x45 {
		t.Fatalf("grouped join = %+v err=%v", parsed, err)
	}

	if err := ParseDFLeave(testRideTicket(0)); err != nil {
		t.Fatal(err)
	}
	if err := ParseDFLeave(append(testRideTicket(0), 1)); err == nil {
		t.Fatal("expected trailing leave bytes to fail")
	}

	proposal := append(testRideTicket(0), make([]byte, 8)...)
	proposal = binary.LittleEndian.AppendUint32(proposal, 9)
	proposal = append(proposal, 0x80)
	response, err := ParseDFProposalResponse(proposal)
	if err != nil || !response.Accept || response.ProposalID != 9 {
		t.Fatalf("proposal = %+v err=%v", response, err)
	}
	if !bytes.Equal(EncodeLegacyLFGProposalResult(response), []byte{9, 0, 0, 0, 1}) {
		t.Fatalf("legacy proposal = %x", EncodeLegacyLFGProposalResult(response))
	}

	roles, err := ParseDFSetRoles([]byte{0x00, 4})
	if err != nil || roles != 4 {
		t.Fatalf("roles = %d err=%v", roles, err)
	}
	roles, err = ParseDFSetRoles([]byte{0x80, 1, 7})
	if err != nil || roles != 1 {
		t.Fatalf("indexed roles = %d err=%v", roles, err)
	}
	if _, err = ParseDFSetRoles([]byte{0}); err == nil {
		t.Fatal("expected a role byte after the party bit")
	}
	if _, err = ParseDFSetRoles([]byte{4}); err == nil {
		t.Fatal("expected the old one-byte role packet to fail")
	}

	player, err := ParseDFGetSystemInfo([]byte{0x80})
	if err != nil || !player {
		t.Fatalf("player system info = %v err=%v", player, err)
	}
	player, err = ParseDFGetSystemInfo([]byte{0x00})
	if err != nil || player {
		t.Fatalf("party system info = %v err=%v", player, err)
	}
	if err = ParseDFGetJoinStatus(nil); err != nil {
		t.Fatal(err)
	}
	if err = ParseDFGetJoinStatus([]byte{0}); err == nil {
		t.Fatal("expected join status payload to fail")
	}
	agree, err := ParseDFBootPlayerVote([]byte{0x80})
	if err != nil || !agree {
		t.Fatalf("boot = %v err=%v", agree, err)
	}
	out, err := ParseDFTeleport([]byte{0x00})
	if err != nil || out {
		t.Fatalf("teleport = %v err=%v", out, err)
	}
}

func TestLFGDisabledOfferTeleportAndRoleChosen(t *testing.T) {
	if body, err := TranslateLegacyLFGDisabled(nil); err != nil || len(body) != 0 {
		t.Fatalf("disabled body=%x err=%v", body, err)
	}
	if _, err := TranslateLegacyLFGDisabled([]byte{0}); err == nil {
		t.Fatal("expected trailing disabled bytes to fail")
	}
	offer, err := TranslateLegacyLFGOfferContinue(binary.LittleEndian.AppendUint32(nil, 0x45))
	if err != nil || !bytes.Equal(offer, binary.LittleEndian.AppendUint32(nil, 0x45)) {
		t.Fatalf("offer = %x err=%v", offer, err)
	}
	denied, err := TranslateLegacyLFGTeleportDenied(binary.LittleEndian.AppendUint32(nil, 4))
	if err != nil || !bytes.Equal(denied, []byte{0x40}) {
		t.Fatalf("denied = %x err=%v", denied, err)
	}
	if _, err = TranslateLegacyLFGTeleportDenied(binary.LittleEndian.AppendUint32(nil, 16)); err == nil {
		t.Fatal("expected a 4-bit overflow to fail")
	}

	legacy := binary.LittleEndian.AppendUint64(nil, 0x99)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 8)
	chosen, err := TranslateLegacyLFGRoleChosen(legacy, testLFGContext())
	if err != nil {
		t.Fatal(err)
	}
	want := appendPackedGUID128(nil, 0x99, 1)
	want = append(want, 8, 0x80)
	if !bytes.Equal(chosen, want) {
		t.Fatalf("role chosen = %x want %x", chosen, want)
	}
}

func TestLFGPlayerInfoEmptyAndLocked(t *testing.T) {
	empty := []byte{0}
	empty = binary.LittleEndian.AppendUint32(empty, 0)
	body, err := TranslateLegacyLFGPlayerInfo(empty)
	if err != nil {
		t.Fatal(err)
	}
	want := binary.LittleEndian.AppendUint32(nil, 0)
	want = append(want, 0)
	want = binary.LittleEndian.AppendUint32(want, 0)
	if !bytes.Equal(body, want) {
		t.Fatalf("empty player info = %x want %x", body, want)
	}

	legacy := []byte{1}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x45|(1<<31))
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 50)
	legacy = binary.LittleEndian.AppendUint32(legacy, 60)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x777)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x11111111)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x45)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	body, err = TranslateLegacyLFGPlayerInfo(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, binary.LittleEndian.AppendUint32(nil, 0x11111111)) {
		t.Fatal("display id was forwarded")
	}
	if !bytes.Contains(body, binary.LittleEndian.AppendUint32(nil, 0x777)) {
		t.Fatal("reward item id missing")
	}
	reader := movementReader{data: body}
	count, err := reader.u32()
	if err != nil || count != 1 {
		t.Fatalf("dungeon count = %d err=%v", count, err)
	}
	hasGUID, err := reader.bit()
	if err != nil || hasGUID {
		t.Fatalf("player blacklist guid = %v err=%v", hasGUID, err)
	}
	lockCount, err := reader.u32()
	if err != nil || lockCount != 1 {
		t.Fatalf("lock count = %d err=%v", lockCount, err)
	}
	lockSlot, _ := reader.u32()
	lockReason, _ := reader.u32()
	sub1, _ := reader.u32()
	sub2, _ := reader.u32()
	soft, _ := reader.u32()
	if lockSlot != 0x45 || lockReason != 2 || sub1 != 0 || sub2 != 0 || soft != 0 {
		t.Fatalf("lock = %x %x %x %x %x", lockSlot, lockReason, sub1, sub2, soft)
	}
	slot, _ := reader.u32()
	completion, _ := reader.u32()
	if slot != 0x45 || completion != 1 {
		t.Fatalf("slot=%x completion=%d", slot, completion)
	}
}

func TestLFGPartyInfoAndJoinResult(t *testing.T) {
	emptyParty, err := TranslateLegacyLFGPartyInfo([]byte{0}, testLFGContext())
	if err != nil || !bytes.Equal(emptyParty, binary.LittleEndian.AppendUint32(nil, 0)) {
		t.Fatalf("empty party = %x err=%v", emptyParty, err)
	}

	legacy := []byte{1}
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x99)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x45)
	legacy = binary.LittleEndian.AppendUint32(legacy, 4)
	party, err := TranslateLegacyLFGPartyInfo(legacy, testLFGContext())
	if err != nil {
		t.Fatal(err)
	}
	reader := movementReader{data: party}
	if count, err := reader.u32(); err != nil || count != 1 {
		t.Fatalf("party count = %d err=%v", count, err)
	}
	hasGUID, err := reader.bit()
	if err != nil || !hasGUID {
		t.Fatalf("party guid bit = %v err=%v", hasGUID, err)
	}
	if count, err := reader.u32(); err != nil || count != 1 {
		t.Fatalf("party locks = %d err=%v", count, err)
	}
	guid, err := reader.guid128()
	if err != nil || guid.Low != 0x99 || guid.High != 1 {
		t.Fatalf("party guid = %+v err=%v", guid, err)
	}

	joinLegacy := binary.LittleEndian.AppendUint32(nil, 0)
	joinLegacy = binary.LittleEndian.AppendUint32(joinLegacy, 0)
	joined, err := TranslateLegacyLFGJoinResult(joinLegacy, testLFGContext())
	if err != nil {
		t.Fatal(err)
	}
	suffix := joined[len(appendLFGTicket(nil, testLFGContext().Ticket)):]
	wantSuffix := []byte{0, 0}
	wantSuffix = binary.LittleEndian.AppendUint32(wantSuffix, 0)
	wantSuffix = binary.LittleEndian.AppendUint32(wantSuffix, 0)
	if !bytes.Equal(suffix, wantSuffix) {
		t.Fatalf("empty join = %x want %x", suffix, wantSuffix)
	}

	locked := binary.LittleEndian.AppendUint32(nil, 6)
	locked = binary.LittleEndian.AppendUint32(locked, 0)
	locked = append(locked, legacy...)
	joined, err = TranslateLegacyLFGJoinResult(locked, testLFGContext())
	if err != nil {
		t.Fatal(err)
	}
	suffix = joined[len(appendLFGTicket(nil, testLFGContext().Ticket)):]
	if suffix[0] != 6 || suffix[1] != 0 || binary.LittleEndian.Uint32(suffix[2:6]) != 1 {
		t.Fatalf("locked join header = %x", suffix[:6])
	}
}

func TestLFGQueueStatusOrder(t *testing.T) {
	negativeWait := int32(-5)
	averageWait := uint32(negativeWait)
	legacy := binary.LittleEndian.AppendUint32(nil, 0x45)
	legacy = binary.LittleEndian.AppendUint32(legacy, averageWait)
	legacy = binary.LittleEndian.AppendUint32(legacy, 10)
	legacy = binary.LittleEndian.AppendUint32(legacy, 20)
	legacy = binary.LittleEndian.AppendUint32(legacy, 30)
	legacy = binary.LittleEndian.AppendUint32(legacy, 40)
	legacy = append(legacy, 1, 2, 3)
	legacy = binary.LittleEndian.AppendUint32(legacy, 70)
	body, err := TranslateLegacyLFGQueueStatus(legacy, testLFGContext())
	if err != nil {
		t.Fatal(err)
	}
	got := body[len(appendLFGTicket(nil, testLFGContext().Ticket)):]
	want := binary.LittleEndian.AppendUint32(nil, 0x45)
	want = binary.LittleEndian.AppendUint32(want, averageWait)
	want = binary.LittleEndian.AppendUint32(want, 10)
	want = binary.LittleEndian.AppendUint32(want, 20)
	want = append(want, 1)
	want = binary.LittleEndian.AppendUint32(want, 30)
	want = append(want, 2)
	want = binary.LittleEndian.AppendUint32(want, 40)
	want = append(want, 3)
	want = binary.LittleEndian.AppendUint32(want, 70)
	if !bytes.Equal(got, want) {
		t.Fatalf("queue = %x want %x", got, want)
	}
}

func TestLFGRoleCheckAndProposal(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 2)
	legacy = append(legacy, 1, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x45)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x99)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 4)
	legacy = append(legacy, 80)
	body, err := TranslateLegacyLFGRoleCheckUpdate(legacy, testLFGContext())
	if err != nil {
		t.Fatal(err)
	}
	reader := movementReader{data: body}
	party, _ := reader.u8()
	status, _ := reader.u8()
	slots, _ := reader.u32()
	bg, _ := reader.u32()
	activity, _ := reader.u32()
	members, _ := reader.u32()
	slot, _ := reader.u32()
	beginning, _ := reader.bit()
	requeue, _ := reader.bit()
	guid, _ := reader.guid128()
	roles, _ := reader.u8()
	level, _ := reader.u8()
	ready, _ := reader.bit()
	if party != 0 || status != 2 || slots != 1 || bg != 0 || activity != 0 || members != 1 || slot != 0x45 {
		t.Fatalf("role-check header party=%d status=%d slots=%d bg=%d activity=%d members=%d slot=%x", party, status, slots, bg, activity, members, slot)
	}
	if !beginning || requeue || guid.Low != 0x99 || roles != 4 || level != 80 || !ready {
		t.Fatalf("role-check member beginning=%v requeue=%v guid=%+v roles=%d level=%d ready=%v", beginning, requeue, guid, roles, level, ready)
	}

	proposal := binary.LittleEndian.AppendUint32(nil, 0x45)
	proposal = append(proposal, 3)
	proposal = binary.LittleEndian.AppendUint32(proposal, 9)
	proposal = binary.LittleEndian.AppendUint32(proposal, 0xAB)
	proposal = append(proposal, 1, 1)
	proposal = binary.LittleEndian.AppendUint32(proposal, 8)
	proposal = append(proposal, 1, 1, 0, 1, 0)
	body, err = TranslateLegacyLFGProposalUpdate(proposal, testLFGContext())
	if err != nil {
		t.Fatal(err)
	}
	reader = movementReader{data: body[len(appendLFGTicket(nil, testLFGContext().Ticket)):]}
	instance, _ := reader.u64()
	id, _ := reader.u32()
	dungeon, _ := reader.u32()
	state, _ := reader.u8()
	completed, _ := reader.u32()
	encounter, _ := reader.u32()
	count, _ := reader.u32()
	unused, _ := reader.u8()
	valid, _ := reader.bit()
	silent, _ := reader.bit()
	isRequeue, _ := reader.bit()
	role, _ := reader.u8()
	me, _ := reader.bit()
	same, _ := reader.bit()
	mine, _ := reader.bit()
	responded, _ := reader.bit()
	accepted, _ := reader.bit()
	if instance != 0 || id != 9 || dungeon != 0x45 || state != 3 || completed != 0xAB || encounter != 0 || count != 1 || unused != 0 {
		t.Fatalf("proposal header instance=%d id=%d dungeon=%x state=%d completed=%x encounter=%d count=%d unused=%d", instance, id, dungeon, state, completed, encounter, count, unused)
	}
	if !valid || !silent || isRequeue || role != 8 || !me || same || !mine || !responded || accepted {
		t.Fatalf("proposal bits valid=%v silent=%v requeue=%v role=%d me=%v same=%v mine=%v responded=%v accepted=%v", valid, silent, isRequeue, role, me, same, mine, responded, accepted)
	}
}

func TestLFGPlayerRewardUsesItemCount(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 0x11)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x22)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 40)
	legacy = binary.LittleEndian.AppendUint32(legacy, 99)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x777)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x11111111)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	body, err := TranslateLegacyLFGPlayerReward(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, binary.LittleEndian.AppendUint32(nil, 0x11111111)) {
		t.Fatal("display id was forwarded")
	}
	reader := movementReader{data: body}
	randomDungeon, _ := reader.u32()
	dungeon, _ := reader.u32()
	money, _ := reader.u32()
	xp, _ := reader.u32()
	count, _ := reader.u32()
	if randomDungeon != 0x11 || dungeon != 0x22 || money != 40 || xp != 99 || count != 1 {
		t.Fatalf("reward header random=%x dungeon=%x money=%d xp=%d count=%d", randomDungeon, dungeon, money, xp, count)
	}
	hasItem, _ := reader.bit()
	hasBonus, _ := reader.bit()
	item, _ := reader.u32()
	if !hasItem || hasBonus || item != 0x777 {
		t.Fatalf("reward item has=%v bonus=%v id=%x", hasItem, hasBonus, item)
	}
}

func TestLFGUpdateStatusBitsAndQueueMode(t *testing.T) {
	player := []byte{5, 1, 1, 0, 0, 1}
	player = binary.LittleEndian.AppendUint32(player, 0x45)
	player = append(player, "skip\x00"...)
	ctx := testLFGContext()
	ctx.RequestedRoles = 8
	body, parsed, err := TranslateLegacyLFGUpdate(player, false, ctx)
	if err != nil || !parsed.HasExtra || !parsed.Queued || parsed.Join || parsed.UpdateType != 5 {
		t.Fatalf("player update = %+v err=%v", parsed, err)
	}
	if bytes.Contains(body, []byte("skip")) {
		t.Fatal("comment was forwarded")
	}
	reader := movementReader{data: body[len(appendLFGTicket(nil, ctx.Ticket)):]}
	kind, _ := reader.u8()
	reason, _ := reader.u8()
	slots, _ := reader.u32()
	roles, _ := reader.u8()
	suspended, _ := reader.u32()
	queueMap, _ := reader.u32()
	slot, _ := reader.u32()
	isParty, _ := reader.bit()
	notify, _ := reader.bit()
	joined, _ := reader.bit()
	lfgJoined, _ := reader.bit()
	queued, _ := reader.bit()
	unused, _ := reader.bit()
	if kind != 5 || reason != 5 || slots != 1 || roles != 8 || suspended != 0 || queueMap != 0 || slot != 0x45 {
		t.Fatalf("status header kind=%d reason=%d slots=%d roles=%d suspended=%d map=%d slot=%x", kind, reason, slots, roles, suspended, queueMap, slot)
	}
	if isParty || !notify || !joined || lfgJoined || !queued || unused {
		t.Fatalf("player bits party=%v notify=%v joined=%v lfg=%v queued=%v unused=%v", isParty, notify, joined, lfgJoined, queued, unused)
	}

	party := []byte{13, 1, 1, 0, 0, 0, 0, 0, 0, 0}
	party = append(party, 0)
	body, parsed, err = TranslateLegacyLFGUpdate(party, true, ctx)
	if err != nil || !parsed.Join || parsed.Queued {
		t.Fatalf("party update = %+v err=%v", parsed, err)
	}
	reader = movementReader{data: body[len(appendLFGTicket(nil, ctx.Ticket)):]}
	_, _ = reader.u8()
	_, _ = reader.u8()
	if count, _ := reader.u32(); count != 0 {
		t.Fatalf("party slots = %d", count)
	}
	_, _ = reader.u8()
	_, _ = reader.u32()
	_, _ = reader.u32()
	_, _ = reader.bit()
	_, _ = reader.bit()
	partyJoined, _ := reader.bit()
	partyLFG, _ := reader.bit()
	partyQueued, _ := reader.bit()
	if partyJoined || !partyLFG || partyQueued {
		t.Fatalf("party bits joined=%v lfg=%v queued=%v", partyJoined, partyLFG, partyQueued)
	}

	bare, parsed, err := TranslateLegacyLFGUpdate([]byte{14, 0}, false, ctx)
	if err != nil || parsed.HasExtra {
		t.Fatal(err)
	}
	reader = movementReader{data: bare[len(appendLFGTicket(nil, ctx.Ticket)):]}
	for index := 0; index < 2; index++ {
		_, _ = reader.u8()
	}
	_, _ = reader.u32()
	_, _ = reader.u8()
	_, _ = reader.u32()
	_, _ = reader.u32()
	_, _ = reader.bit()
	notify, _ = reader.bit()
	joined, _ = reader.bit()
	if !notify || joined {
		t.Fatalf("bare notify=%v joined=%v", notify, joined)
	}

	mode, send := NextLFGQueueMode(LFGQueueIdle, false, 5, true)
	if mode != LFGQueuePlayer || !send {
		t.Fatalf("player enter mode=%d send=%v", mode, send)
	}
	mode, send = NextLFGQueueMode(mode, true, 14, false)
	if mode != LFGQueuePlayer || send {
		t.Fatalf("party suppressed mode=%d send=%v", mode, send)
	}
	mode, send = NextLFGQueueMode(mode, true, 13, true)
	if mode != LFGQueueParty || !send {
		t.Fatalf("party enter mode=%d send=%v", mode, send)
	}
	mode, send = NextLFGQueueMode(mode, false, 14, false)
	if mode != LFGQueueParty || send {
		t.Fatalf("player suppressed mode=%d send=%v", mode, send)
	}
	mode, send = NextLFGQueueMode(mode, false, 7, false)
	if mode != LFGQueueIdle || !send {
		t.Fatalf("leave mode=%d send=%v", mode, send)
	}
}

func testLFGContext() LFGContext {
	return LFGContext{
		Ticket: NewLFGTicket(GUID128{Low: 0x10, High: 0x20}, 1_700_000_000),
		GUID: func(legacy uint64) GUID128 {
			return GUID128{Low: legacy, High: 1}
		},
	}
}

func testRideTicket(bit byte) []byte {
	body := appendPackedGUID128(nil, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 2)
	body = binary.LittleEndian.AppendUint64(body, 0)
	return append(body, bit)
}
