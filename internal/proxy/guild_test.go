package proxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"testing"
	"time"
	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

func guildProxyTestServer() *Server {
	return &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}
func guildProxyNext(t *testing.T, c *fakeLegacyWorldConnection) fakeLegacyWrite {
	t.Helper()
	select {
	case p := <-c.writes:
		return p
	case <-time.After(time.Second):
		t.Fatal("guild legacy write missing")
		return fakeLegacyWrite{}
	}
}
func guildProxyGUID(g modernworld.GUID128) []byte {
	b := []byte{0, 0}
	for i := 0; i < 8; i++ {
		if v := byte(g.Low >> (i * 8)); v != 0 {
			b[0] |= 1 << i
			b = append(b, v)
		}
	}
	for i := 0; i < 8; i++ {
		if v := byte(g.High >> (i * 8)); v != 0 {
			b[1] |= 1 << i
			b = append(b, v)
		}
	}
	return b
}
func TestGuildProxyRosterQueriesAndOfflineMemberManagement(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, guild: modernworld.GuildState{ID: 9, Members: map[uint64]modernworld.GuildMember{42: {Name: "Offline"}}}}
	ok, e := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGuildGetRoster})
	if !ok || e != nil {
		t.Fatalf("roster route %v %v", ok, e)
	}
	for _, op := range []uint32{0x54, 0x87, 0x89} {
		p := guildProxyNext(t, c)
		if p.opcode != op {
			t.Fatalf("query order %x want %x", p.opcode, op)
		}
		if op == 0x54 && binary.LittleEndian.Uint32(p.body) != 9 {
			t.Fatal("wrong guild ID")
		}
	}
	g := modernworld.ModernGUIDForLegacy(42, 0)
	for _, test := range []struct {
		modern uint16
		legacy uint32
	}{{modernworld.CMSGGuildPromoteMember, 0x8B}, {modernworld.CMSGGuildDemoteMember, 0x8C}, {modernworld.CMSGGuildOfficerRemoveMember, 0x8E}} {
		ok, e = s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: test.modern, Body: guildProxyGUID(g)})
		if !ok || e != nil {
			t.Fatal(e)
		}
		p := guildProxyNext(t, c)
		if p.opcode != test.legacy || string(p.body) != "Offline\x00" {
			t.Fatalf("offline resolution %x %q", p.opcode, p.body)
		}
	}
	// PUBLIC_NOTE is a name + CString, not a modern GUID.
	b := append(guildProxyGUID(g), 3, 128, 'n', 'o', 't')
	_, e = s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGuildSetMemberNote, Body: b})
	if e != nil {
		t.Fatal(e)
	}
	p := guildProxyNext(t, c)
	if p.opcode != 0x234 || string(p.body) != "Offline\x00not\x00" {
		t.Fatalf("note %x %q", p.opcode, p.body)
	}
}

func TestGuildProxyRejectsWrongRankUnknownObjectAndOverflow(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, guild: modernworld.GuildState{Ranks: make([]modernworld.GuildRank, 5)}}
	_, e := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGuildDeleteRank, Body: binary.LittleEndian.AppendUint32(nil, 1)})
	if e == nil {
		t.Fatal("would delete last rank for non-last request")
	}
	g := modernworld.ModernGUIDForLegacy(0xF110000001000007, 0)
	b := binary.LittleEndian.AppendUint64(guildProxyGUID(g), 1<<32)
	_, e = s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGuildBankDepositMoney, Body: b})
	if e == nil {
		t.Fatal("money overflow accepted")
	}
	b = binary.LittleEndian.AppendUint64(guildProxyGUID(g), 1)
	_, e = s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGuildBankDepositMoney, Body: b})
	if e == nil {
		t.Fatal("unknown bank accepted")
	}
	if len(c.writes) != 0 {
		t.Fatal("invalid request reached legacy")
	}
	session.objectGUIDs = map[uint64]modernworld.GUID128{0xF110000001000007: g}
	_, e = s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGuildBankDepositMoney, Body: b})
	if e != nil {
		t.Fatal(e)
	}
	p := guildProxyNext(t, c)
	if p.opcode != 0x3EC || len(p.body) != 12 || binary.LittleEndian.Uint64(p.body) != 0xF110000001000007 {
		t.Fatal("bank GUID was truncated")
	}
	_, e = s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGuildDeleteRank, Body: binary.LittleEndian.AppendUint32(nil, 4)})
	if e != nil {
		t.Fatal(e)
	}
	p = guildProxyNext(t, c)
	if p.opcode != 0x233 || len(p.body) != 0 {
		t.Fatal("last rank delete not forwarded")
	}
}

func TestGuildRemainingMoneyReachesLegacyAndReplyQueues(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c}
	_, e := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGGuildBankRemainingWithdrawQuery})
	if e != nil {
		t.Fatal(e)
	}
	if p := guildProxyNext(t, c); p.opcode != 0x3FE {
		t.Fatal("remaining money still swallowed")
	}
	if e = s.handleLegacyGuild(session, 0x3FE, []byte{255, 255, 255, 255}); e != nil {
		t.Fatal(e)
	}
	if len(session.pendingInstance) != 1 || session.pendingInstance[0].Opcode != modernworld.SMSGGuildBankRemainingWithdrawMoney || !bytes.Equal(session.pendingInstance[0].Body, bytes.Repeat([]byte{255}, 8)) {
		t.Fatal("reply queue/unlimited")
	}
}

func TestGuildAutoDeclineAndCharacterReset(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c}
	_, e := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGDeclineGuildInvites, Body: []byte{1}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.handleLegacyGuild(session, 0x83, []byte("Alice\x00Guild\x00")); e != nil {
		t.Fatal(e)
	}
	if p := guildProxyNext(t, c); p.opcode != 0x85 {
		t.Fatal("invite not declined")
	}
	if len(session.pendingInstance) != 0 {
		t.Fatal("blocked invite still displayed")
	}
	session.guild.ID = 9
	session.guild.Members = map[uint64]modernworld.GuildMember{1: {Name: "Old"}}
	session.resetCharacterSessionLocked()
	if session.guild.ID != 0 || session.guild.BlockInvites || session.guild.Members != nil {
		t.Fatal("guild cache leaked to next character")
	}
}

func TestGuildLegacyRelayDispatch(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, legacy: &legacyauth.Session{Username: "test"}}
	done := make(chan struct{})
	go func() { defer close(done); s.relayLegacyWorld(session, nil, c) }()
	c.reads <- legacyworld.Packet{Opcode: 0x3FE, Body: []byte{1, 0, 0, 0}}
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		session.worldMu.Lock()
		n := len(session.pendingInstance)
		session.worldMu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("guild packet never dispatched")
		case <-tick.C:
		}
	}
	c.Close()
	<-done
}
