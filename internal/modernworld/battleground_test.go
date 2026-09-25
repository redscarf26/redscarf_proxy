package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBattlefieldStatusSplitsQueueConfirmActiveAndNone(t *testing.T) {
	ctx := BattlegroundContext{
		Requester: GUID128{Low: 0x11, High: 0x22},
		Now:       100,
		Queues:    map[uint32]BattlegroundQueue{},
		GUID:      func(guid uint64) GUID128 { return GUID128{Low: guid} },
	}
	queued := legacyStatus(0, 0, 0, 2, 10, 80, 7, 0, 1, 30, 12, 0, 0, 0)
	packets, err := TranslateLegacyBattlefieldStatus(queued, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].Opcode != SMSGBattlefieldStatusQueued {
		t.Fatalf("queued = %+v", packets)
	}
	stored := ctx.Queues[1]
	if stored.ArenaType != 0 || stored.Marker != 0 || stored.BgTypeID != 2 || stored.JoinedAt != 100 {
		t.Fatalf("stored queue = %+v", stored)
	}
	reader := movementReader{data: packets[0].Body}
	if err := reader.rideTicket(); err != nil {
		t.Fatal(err)
	}
	ticketID, _ := lastTicketID(packets[0].Body)
	if ticketID != 1 {
		t.Fatalf("ticket = %d", ticketID)
	}
	queueID := battlefieldQueueID(t, packets[0].Body)
	if uint32(queueID) != 2 || queueID&battlegroundQueueMask == 0 {
		t.Fatalf("queue id = %x", queueID)
	}
	if packets[0].Body[len(packets[0].Body)-1] != 0x40 {
		t.Fatalf("queued bits = %x", packets[0].Body[len(packets[0].Body)-1])
	}

	ctx.Now = 250
	none, err := TranslateLegacyBattlefieldStatus(legacyStatus(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 1 || none[0].Opcode != SMSGBattlefieldStatusNone {
		t.Fatalf("none = %+v", none)
	}
	if _, ok := ctx.Queues[1]; ok {
		t.Fatal("none left the queue stored")
	}
	when := ticketTime(t, none[0].Body)
	if when != 100 {
		t.Fatalf("none ticket time = %d, want the original join time", when)
	}
	wantNone := appendBattlegroundTicket(nil, ctx.Requester, 1, 100)
	if !bytes.Equal(none[0].Body, wantNone) {
		t.Fatalf("none body = %x, want %x", none[0].Body, wantNone)
	}

	confirm, err := TranslateLegacyBattlefieldStatus(legacyStatus(1, 0, 0, 2, 10, 80, 9, 0, 2, 40, 0, 529, 0, 0), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(confirm) != 1 || confirm[0].Opcode != SMSGBattlefieldStatusNeed {
		t.Fatalf("confirm = %+v", confirm)
	}
	if ctx.Queues[2].JoinedAt != 250 {
		t.Fatalf("confirm join time = %d", ctx.Queues[2].JoinedAt)
	}

	active, err := TranslateLegacyBattlefieldStatus(legacyStatus(0, 0, 0, 2, 10, 80, 9, 0, 3, 0, 80, 529, 1, 0), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 || active[0].Opcode != SMSGBattlegroundInit || active[1].Opcode != SMSGBattlefieldStatusActive {
		t.Fatalf("active = %+v", active)
	}
	if binary.LittleEndian.Uint32(active[0].Body) != battlegroundInitMilliseconds || binary.LittleEndian.Uint16(active[0].Body[4:]) != 0 {
		t.Fatalf("init = %x", active[0].Body)
	}
	if active[1].Body[len(active[1].Body)-1]&0x80 == 0 {
		t.Fatalf("alliance faction bit missing in %x", active[1].Body[len(active[1].Body)-1])
	}
}

func TestBattlefieldPortEchoesStoredArenaType(t *testing.T) {
	ctx := BattlegroundContext{Now: 10, Queues: map[uint32]BattlegroundQueue{}}
	if _, err := TranslateLegacyBattlefieldStatus(legacyStatus(0, 2, battlegroundArenaMarker, 4, 70, 80, 1, 1, 1, 5, 1, 0, 0, 0), ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := TranslateLegacyBattlefieldStatus(legacyStatus(1, 0, 0, 2, 10, 80, 3, 0, 1, 5, 1, 0, 0, 0), ctx); err != nil {
		t.Fatal(err)
	}
	arena, err := RequireBattlegroundQueue(ctx.Queues, 1)
	if err != nil {
		t.Fatal(err)
	}
	port := EncodeLegacyBattlefieldPort(arena, true)
	if port[0] != 2 || port[1] != battlegroundArenaMarker || port[8] != 1 {
		t.Fatalf("arena port = %x", port)
	}
	battleground, err := RequireBattlegroundQueue(ctx.Queues, 2)
	if err != nil {
		t.Fatal(err)
	}
	declined := EncodeLegacyBattlefieldPort(battleground, false)
	if declined[0] != 0 || declined[8] != 0 {
		t.Fatalf("battleground port = %x", declined)
	}
	if _, err := RequireBattlegroundQueue(ctx.Queues, 99); err == nil {
		t.Fatal("expected unknown port ticket")
	}
	leave := EncodeLegacyBattlefieldLeave(&battleground)
	if len(leave) != 8 || leave[0] != 0 || binary.LittleEndian.Uint32(leave[2:]) != 2 {
		t.Fatalf("leave = %x", leave)
	}
	missing := EncodeLegacyBattlefieldLeave(nil)
	if len(missing) != 8 || !bytes.Equal(missing[2:6], []byte{0, 0, 0, 0}) || binary.LittleEndian.Uint16(missing[6:]) != battlegroundPortConstant {
		t.Fatalf("missing leave = %x", missing)
	}
}

func TestPVPLogUsesScoreboardOpcodeAndSexByte(t *testing.T) {
	body := []byte{0, 1, 1}
	body = binary.LittleEndian.AppendUint32(body, 2)
	body = append(body, []byte{
		0x21, 0, 0, 0, 0, 0, 0, 0,
	}...)
	body = binary.LittleEndian.AppendUint32(body, 4)
	body = binary.LittleEndian.AppendUint32(body, 7)
	body = binary.LittleEndian.AppendUint32(body, 3)
	body = binary.LittleEndian.AppendUint32(body, 11)
	body = binary.LittleEndian.AppendUint32(body, 100)
	body = binary.LittleEndian.AppendUint32(body, 40)
	body = binary.LittleEndian.AppendUint32(body, 2)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 2)
	body = append(body, []byte{0x22, 0, 0, 0, 0, 0, 0, 0}...)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 9)
	body = binary.LittleEndian.AppendUint32(body, 8)
	body = binary.LittleEndian.AppendUint32(body, 0)
	ctx := BattlegroundContext{GUID: func(guid uint64) GUID128 { return GUID128{Low: guid} }, Player: func(guid uint64) BattlegroundPlayer {
		if guid == 0x22 {
			return BattlegroundPlayer{Race: 2, Class: 8, Sex: 1, Known: true}
		}
		return BattlegroundPlayer{}
	}}
	packet, err := TranslateLegacyPVPLog(body, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if SMSGPvpLogData != 0x2934 || packet.Opcode != SMSGPvpLogData {
		t.Fatalf("opcode = %x", packet.Opcode)
	}
	reader := movementReader{data: packet.Body}
	ratings, err := reader.bit()
	if err != nil || ratings {
		t.Fatalf("ratings bit = %v err=%v", ratings, err)
	}
	arena, err := reader.bit()
	if err != nil || arena {
		t.Fatal(arena, err)
	}
	winner, err := reader.bit()
	if err != nil || !winner {
		t.Fatal(winner, err)
	}
	reader.align()
	count, err := reader.u32()
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if side0, err := reader.u8(); err != nil || side0 != 0 {
		t.Fatal(side0, err)
	}
	if side1, err := reader.u8(); err != nil || side1 != 0 {
		t.Fatal(side1, err)
	}
	if won, err := reader.u8(); err != nil || won != 1 {
		t.Fatal(won, err)
	}
	sex, race, class, honorKills := readPVPIdentity(t, &reader)
	if sex != 0 || race != 1 || class != 1 || honorKills != 7 {
		t.Fatalf("default identity sex=%d race=%d class=%d honor=%d", sex, race, class, honorKills)
	}
	sex, race, class, honorKills = readPVPIdentity(t, &reader)
	if sex != 1 || race != 2 || class != 8 || honorKills != 0 {
		t.Fatalf("known identity sex=%d race=%d class=%d honor=%d", sex, race, class, honorKills)
	}
	if err := finishReader(&reader, "pvp log"); err != nil {
		t.Fatal(err)
	}
}

func TestBattlefieldListRewritesZeroLevels(t *testing.T) {
	body := binary.LittleEndian.AppendUint64(nil, 0x55)
	body = append(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 32)
	body = append(body, 0, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 9)
	body = binary.LittleEndian.AppendUint32(body, 8)
	body = binary.LittleEndian.AppendUint32(body, 7)
	body = append(body, 1, 1)
	body = binary.LittleEndian.AppendUint32(body, 3)
	body = binary.LittleEndian.AppendUint32(body, 4)
	body = binary.LittleEndian.AppendUint32(body, 5)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 44)
	packet, err := TranslateLegacyBattlefieldList(body, func(guid uint64) GUID128 { return GUID128{Low: guid} })
	if err != nil {
		t.Fatal(err)
	}
	reader := movementReader{data: packet.Body}
	guid, err := reader.guid128()
	if err != nil || guid.Low != 0x55 {
		t.Fatal(guid, err)
	}
	verification, err := reader.i32()
	if err != nil || verification != battlegroundListVerification {
		t.Fatal(verification, err)
	}
	if listID, err := reader.u32(); err != nil || listID != 32 {
		t.Fatal(listID, err)
	}
	minLevel, _ := reader.u8()
	maxLevel, _ := reader.u8()
	if minLevel != 1 || maxLevel != 80 {
		t.Fatalf("levels = %d %d", minLevel, maxLevel)
	}
	if count, err := reader.u32(); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if instance, err := reader.u32(); err != nil || instance != 44 {
		t.Fatal(instance, err)
	}
	anywhere, _ := reader.bit()
	won, _ := reader.bit()
	if !anywhere || !won {
		t.Fatalf("bits anywhere=%v won=%v", anywhere, won)
	}

	kept := binary.LittleEndian.AppendUint64(nil, 1)
	kept = append(kept, 0)
	kept = binary.LittleEndian.AppendUint32(kept, 1)
	kept = append(kept, 10, 70, 0)
	for range 3 {
		kept = binary.LittleEndian.AppendUint32(kept, 0)
	}
	kept = append(kept, 0)
	kept = binary.LittleEndian.AppendUint32(kept, 0)
	packet, err = TranslateLegacyBattlefieldList(kept, func(uint64) GUID128 { return GUID128{} })
	if err != nil {
		t.Fatal(err)
	}
	reader = movementReader{data: packet.Body}
	if _, err = reader.guid128(); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.u32(); err != nil {
		t.Fatal(err)
	}
	minLevel, _ = reader.u8()
	maxLevel, _ = reader.u8()
	if minLevel != 10 || maxLevel != 70 {
		t.Fatalf("kept levels = %d %d", minLevel, maxLevel)
	}
}

func TestBattlemasterJoinDropsModernFields(t *testing.T) {
	body := binary.LittleEndian.AppendUint64(nil, uint64(6)|battlegroundQueueMask)
	body = append(body, 7)
	body = binary.LittleEndian.AppendUint32(body, 11)
	body = binary.LittleEndian.AppendUint32(body, 12)
	body = appendPackedGUID128(body, 0x99, 0)
	body = binary.LittleEndian.AppendUint32(body, 121761856)
	body = binary.LittleEndian.AppendUint32(body, 3)
	body = appendBits(body, true)
	join, err := ParseBattlegroundJoin(body)
	if err != nil {
		t.Fatal(err)
	}
	if join.BgTypeID != 6 || join.InstanceID != 3 || !join.AsGroup || join.Battlemaster.Low != 0x99 {
		t.Fatalf("join = %+v", join)
	}
	legacy := EncodeLegacyBattlegroundJoin(0x42, join)
	want := binary.LittleEndian.AppendUint64(nil, 0x42)
	want = binary.LittleEndian.AppendUint32(want, 6)
	want = binary.LittleEndian.AppendUint32(want, 3)
	want = append(want, 1)
	if !bytes.Equal(legacy, want) {
		t.Fatalf("legacy join = %x", legacy)
	}
	listID, err := ParseBattlefieldListRequest(binary.LittleEndian.AppendUint32(nil, 32))
	if err != nil || listID != 32 {
		t.Fatal(listID, err)
	}
	if !bytes.Equal(EncodeLegacyBattlefieldListRequest(32), []byte{32, 0, 0, 0, 0, 1}) {
		t.Fatalf("list request = %x", EncodeLegacyBattlefieldListRequest(32))
	}
}

func TestAreaSpiritHealerAndJoinFailure(t *testing.T) {
	body := binary.LittleEndian.AppendUint64(nil, 0x77)
	body = binary.LittleEndian.AppendUint32(body, 1500)
	packet, err := TranslateLegacyAreaSpiritHealerTime(body, func(guid uint64) GUID128 { return GUID128{Low: guid, High: 1} })
	if err != nil {
		t.Fatal(err)
	}
	if packet.Opcode != SMSGAreaSpiritHealerTime {
		t.Fatal(packet.Opcode)
	}
	reader := movementReader{data: packet.Body}
	guid, err := reader.guid128()
	if err != nil || guid.Low != 0x77 || guid.High != 1 {
		t.Fatal(guid, err)
	}
	if remaining, err := reader.u32(); err != nil || remaining != 1500 {
		t.Fatal(remaining, err)
	}
	modern := appendPackedGUID128(nil, 0x77, 1)
	parsed, err := ParseAreaSpiritHealer(modern)
	if err != nil || parsed.Low != 0x77 {
		t.Fatal(parsed, err)
	}
	if !bytes.Equal(EncodeLegacyAreaSpiritHealer(0x77), binary.LittleEndian.AppendUint64(nil, 0x77)) {
		t.Fatal(EncodeLegacyAreaSpiritHealer(0x77))
	}

	ctx := BattlegroundContext{Requester: GUID128{Low: 5}, Now: 9, GUID: func(guid uint64) GUID128 { return GUID128{Low: guid} }}
	failed, err := TranslateLegacyGroupJoinedBattleground(appendInt32(nil, -2), ctx)
	if err != nil || len(failed) != 1 || failed[0].Opcode != SMSGBattlefieldStatusFailed {
		t.Fatal(failed, err)
	}
	reason := failedReason(t, failed[0].Body)
	if reason != -2 {
		t.Fatalf("reason = %d", reason)
	}
	timed := appendInt32(nil, -11)
	timed = binary.LittleEndian.AppendUint64(timed, 0x88)
	failed, err = TranslateLegacyGroupJoinedBattleground(timed, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if failedReason(t, failed[0].Body) != -11 || failedClient(t, failed[0].Body).Low != 0x88 {
		t.Fatalf("timed out body = %x", failed[0].Body)
	}
	ok, err := TranslateLegacyGroupJoinedBattleground(binary.LittleEndian.AppendUint32(nil, 2), ctx)
	if err != nil || len(ok) != 0 {
		t.Fatal(ok, err)
	}
	zero, err := TranslateLegacyGroupJoinedBattleground(binary.LittleEndian.AppendUint32(nil, 0), ctx)
	if err != nil || len(zero) != 1 || failedReason(t, zero[0].Body) != 0 {
		t.Fatal(zero, err)
	}
}

func TestBattlegroundPositionsHonorAndSkinned(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint64(body, 0x10)
	body = appendFloat32(body, 1)
	body = appendFloat32(body, 2)
	body = binary.LittleEndian.AppendUint64(body, 0x20)
	body = appendFloat32(body, 3)
	body = appendFloat32(body, 4)
	ctx := BattlegroundContext{
		GUID: func(guid uint64) GUID128 { return GUID128{Low: guid} },
		Player: func(guid uint64) BattlegroundPlayer {
			if guid == 0x20 {
				return BattlegroundPlayer{Race: 2, Class: 1, Sex: 0, Known: true}
			}
			return BattlegroundPlayer{}
		},
	}
	packet, err := TranslateLegacyBattlegroundPositions(body, ctx)
	if err != nil {
		t.Fatal(err)
	}
	reader := movementReader{data: packet.Body}
	count, err := reader.u32()
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	guid, err := reader.guid128()
	if err != nil || guid.Low != 0x20 {
		t.Fatal(guid, err)
	}
	x, _ := reader.f32()
	y, _ := reader.f32()
	if x != 3 || y != 4 {
		t.Fatal(x, y)
	}
	icon, _ := reader.i8()
	slot, _ := reader.i8()
	if icon != 2 || slot != 2 {
		t.Fatalf("icon=%d slot=%d", icon, slot)
	}

	credit := binary.LittleEndian.AppendUint32(nil, 25)
	credit = binary.LittleEndian.AppendUint64(credit, 0x33)
	credit = binary.LittleEndian.AppendUint32(credit, 6)
	packet, err = TranslateLegacyPVPCredit(credit, func(guid uint64) GUID128 { return GUID128{Low: guid} })
	if err != nil {
		t.Fatal(err)
	}
	if packet.Opcode != SMSGPvpCredit || binary.LittleEndian.Uint32(packet.Body) != 25 || binary.LittleEndian.Uint32(packet.Body[4:]) != 0 {
		t.Fatalf("credit = %x", packet.Body)
	}
	empty, err := TranslateLegacyPlayerSkinned(nil)
	if err != nil || empty.Opcode != SMSGPlayerSkinned || empty.Body[0] != 0 {
		t.Fatal(empty, err)
	}
	free, err := TranslateLegacyPlayerSkinned([]byte{1})
	if err != nil || free.Body[0] != 0x80 {
		t.Fatal(free, err)
	}
	if _, err := TranslateLegacyPlayerSkinned([]byte{1, 2}); err == nil {
		t.Fatal("expected trailing skinned bytes")
	}
	joined, err := TranslateLegacyBattlegroundPlayer(binary.LittleEndian.AppendUint64(nil, 0x44), SMSGBattlegroundPlayerJoin, func(guid uint64) GUID128 {
		return GUID128{Low: guid}
	})
	if err != nil || joined.Opcode != SMSGBattlegroundPlayerJoin {
		t.Fatal(joined, err)
	}
	left, err := TranslateLegacyBattlegroundPlayer(binary.LittleEndian.AppendUint64(nil, 0x44), SMSGBattlegroundPlayerLeft, func(guid uint64) GUID128 {
		return GUID128{Low: guid}
	})
	if err != nil || left.Opcode != SMSGBattlegroundPlayerLeft || !bytes.Equal(joined.Body, left.Body) {
		t.Fatal(left, err)
	}
	if err := ParseEmptyBattlegroundRequest([]byte{1}, "battlefield status request"); err == nil {
		t.Fatal("expected status request body")
	}
}

func legacyStatus(slot uint32, arenaType, marker byte, bgType uint32, min, max byte, instance uint32, rated byte, status, time1, time2, mapID uint32, faction byte, padding uint64) []byte {
	if arenaType == 0 && marker == 0 && bgType == 0 {
		body := binary.LittleEndian.AppendUint32(nil, slot)
		return binary.LittleEndian.AppendUint64(body, 0)
	}
	body := binary.LittleEndian.AppendUint32(nil, slot)
	body = append(body, arenaType, marker)
	body = binary.LittleEndian.AppendUint32(body, bgType)
	body = binary.LittleEndian.AppendUint16(body, battlegroundPortConstant)
	body = append(body, min, max)
	body = binary.LittleEndian.AppendUint32(body, instance)
	body = append(body, rated)
	body = binary.LittleEndian.AppendUint32(body, status)
	switch status {
	case battlegroundStatusQueued:
		body = binary.LittleEndian.AppendUint32(body, time1)
		body = binary.LittleEndian.AppendUint32(body, time2)
	case battlegroundStatusConfirm:
		body = binary.LittleEndian.AppendUint32(body, mapID)
		body = binary.LittleEndian.AppendUint64(body, padding)
		body = binary.LittleEndian.AppendUint32(body, time1)
	case battlegroundStatusActive:
		body = binary.LittleEndian.AppendUint32(body, mapID)
		body = binary.LittleEndian.AppendUint64(body, padding)
		body = binary.LittleEndian.AppendUint32(body, time1)
		body = binary.LittleEndian.AppendUint32(body, time2)
		body = append(body, faction)
	}
	return body
}

func failedReason(t *testing.T, body []byte) int32 {
	t.Helper()
	reader := movementReader{data: body}
	if err := reader.rideTicket(); err != nil {
		t.Fatal(err)
	}
	reader.align()
	if _, err := reader.u64(); err != nil {
		t.Fatal(err)
	}
	reason, err := reader.i32()
	if err != nil {
		t.Fatal(err)
	}
	return reason
}

func failedClient(t *testing.T, body []byte) GUID128 {
	t.Helper()
	reader := movementReader{data: body}
	if err := reader.rideTicket(); err != nil {
		t.Fatal(err)
	}
	reader.align()
	if _, err := reader.u64(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u32(); err != nil {
		t.Fatal(err)
	}
	guid, err := reader.guid128()
	if err != nil {
		t.Fatal(err)
	}
	return guid
}

func lastTicketID(body []byte) (uint32, error) {
	reader := movementReader{data: body}
	if _, err := reader.guid128(); err != nil {
		return 0, err
	}
	return reader.u32()
}

func ticketTime(t *testing.T, body []byte) int64 {
	t.Helper()
	reader := movementReader{data: body}
	if _, err := reader.guid128(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u32(); err != nil {
		t.Fatal(err)
	}
	when, err := reader.u64()
	if err != nil {
		t.Fatal(err)
	}
	return int64(when)
}

func battlefieldQueueID(t *testing.T, body []byte) uint64 {
	t.Helper()
	reader := movementReader{data: body}
	if err := reader.rideTicket(); err != nil {
		t.Fatal(err)
	}
	reader.align()
	if _, err := reader.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u8(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u8(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u8(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u32(); err != nil {
		t.Fatal(err)
	}
	queueID, err := reader.u64()
	if err != nil {
		t.Fatal(err)
	}
	return queueID
}

func readPVPIdentity(t *testing.T, reader *movementReader) (byte, uint32, uint32, uint32) {
	t.Helper()
	if _, err := reader.guid128(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := reader.u32(); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := reader.u32()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u32(); err != nil {
		t.Fatal(err)
	}
	sex, err := reader.u8()
	if err != nil {
		t.Fatal(err)
	}
	race, err := reader.u32()
	if err != nil {
		t.Fatal(err)
	}
	class, err := reader.u32()
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := reader.u32(); err != nil {
			t.Fatal(err)
		}
	}
	for range stats {
		if _, err := reader.u32(); err != nil {
			t.Fatal(err)
		}
	}
	var honor bool
	for index := 0; index < 7; index++ {
		bit, bitErr := reader.bit()
		if bitErr != nil {
			t.Fatal(bitErr)
		}
		if index == 2 {
			honor = bit
		}
	}
	reader.align()
	if !honor {
		return sex, race, class, 0
	}
	kills, err := reader.u32()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u32(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.u32(); err != nil {
		t.Fatal(err)
	}
	return sex, race, class, kills
}
