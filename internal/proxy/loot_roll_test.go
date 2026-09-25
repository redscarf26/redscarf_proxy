package proxy

import (
	"encoding/binary"
	"testing"
	"redscarf/internal/legacyauth"
	"redscarf/internal/modernworld"
)

func TestLootRollIdenticalDropsStayIndependent(t *testing.T) {
	session := &proxySession{currentMapID: 631}
	key := modernworld.LootRollKey{Slot: 0, ItemID: 50780}
	a := session.rememberLootRollLocked(key, 0x4000000000000042)
	b := session.rememberLootRollLocked(key, 0x4000000000000043)
	for _, ref := range []lootRollReference{a, b} {
		guid, completed := session.legacyLootRollRequestLocked(modernworld.LootRollRequest{LootObj: ref.modernGUID, Slot: 0})
		if guid != ref.legacyGUID || completed {
			t.Fatalf("wrong roll: %x %v", guid, completed)
		}
	}
	if _, found := session.resolveLootRollLocked(key, 0); found {
		t.Fatal("ambiguous GUID-less event resolved arbitrarily")
	}
	session.completeLootRollLocked(a)
	if got, found := session.resolveLootRollLocked(key, 0); !found || got != b {
		t.Fatal("completion removed the other drop")
	}
	if guid, complete := session.legacyLootRollRequestLocked(modernworld.LootRollRequest{LootObj: a.modernGUID}); guid != 0 || !complete {
		t.Fatal("late reply not recognized")
	}
	if guid, complete := session.legacyLootRollRequestLocked(modernworld.LootRollRequest{LootObj: b.modernGUID, Slot: 1}); guid != 0 || complete {
		t.Fatal("wrong slot accepted")
	}
	for i := uint64(1); i <= 300; i++ {
		session.completeLootRollLocked(lootRollReference{modernGUID: modernworld.GUID128{Low: i}})
	}
	if len(session.completedLootRolls) != 256 {
		t.Fatal("completed cache is not bounded")
	}
}

func TestLootRollClientRouteAndLateReply(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, legacy: &legacyauth.Session{Username: "test"}, currentMapID: 631}
	key := modernworld.LootRollKey{Slot: 2, ItemID: 50780}
	a := session.rememberLootRollLocked(key, 0x4000000000000042)
	session.rememberLootRollLocked(key, 0x4000000000000043)
	request := append(modernworld.EncodeCancelAutoRepeat(a.modernGUID), 2, 1)
	handled, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGLootRoll, Body: request})
	if !handled || err != nil {
		t.Fatal(handled, err)
	}
	p := guildProxyNext(t, c)
	if p.opcode != 0x2a0 || len(p.body) != 13 || binary.LittleEndian.Uint64(p.body) != a.legacyGUID || binary.LittleEndian.Uint32(p.body[8:]) != 2 || p.body[12] != 1 {
		t.Fatalf("wrong legacy vote: %#v", p)
	}
	session.completeLootRollLocked(a)
	if _, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGLootRoll, Body: request}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.writes:
		t.Fatal("late reply forwarded")
	default:
	}
	request[len(request)-2] = 3
	if _, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGLootRoll, Body: request}); err == nil {
		t.Fatal("unknown slot accepted")
	}
}
