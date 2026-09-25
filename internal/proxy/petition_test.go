package proxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

func TestPetitionProxyRoutesAllRequests(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	const item = uint64(0x4000000000000042)
	const npc = uint64(0xF130000ABC000001)
	ng := modernworld.ModernGUIDForLegacy(npc, 0)
	session := &proxySession{legacyWorld: c, objectGUIDs: map[uint64]modernworld.GUID128{npc: ng}}
	g := guildProxyGUID(modernworld.ModernGUIDForLegacy(item, 0))
	// No item in the session's object map: recipient must still sign/decline it.
	for _, f := range []struct {
		modern uint16
		legacy uint32
		body   []byte
	}{
		{modernworld.CMSGPetitionShowSignatures, 0x1BE, g},
		{modernworld.CMSGQueryPetition, 0x1C6, append([]byte{123, 0, 0, 0}, g...)},
		{modernworld.CMSGSignPetition, 0x1C0, append(bytes.Clone(g), 0)},
		{modernworld.CMSGDeclinePetition, 0x1C2, g},
		{modernworld.CMSGTurnInPetition, 0x1C4, g},
		{modernworld.CMSGPetitionRenameGuild, 0x2C1, append(bytes.Clone(g), 2, 'G')},
		{modernworld.CMSGOfferPetition, 0x1C3, append(append([]byte{0, 0, 0, 0}, g...), guildProxyGUID(modernworld.ModernGUIDForLegacy(99, 0))...)},
		{modernworld.CMSGPetitionBuy, 0x1BD, append(append([]byte{2}, guildProxyGUID(ng)...), 1, 0, 0, 0, 'G')},
	} {
		ok, e := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: f.modern, Body: f.body})
		if !ok || e != nil {
			t.Fatalf("route %x %v", f.modern, e)
		}
		p := guildProxyNext(t, c)
		if p.opcode != f.legacy {
			t.Fatalf("opcode %x want %x", p.opcode, f.legacy)
		}
		off := 0
		if f.modern == modernworld.CMSGQueryPetition || f.modern == modernworld.CMSGOfferPetition {
			off = 4
		}
		want := item
		if f.modern == modernworld.CMSGPetitionBuy {
			want = npc
		}
		if binary.LittleEndian.Uint64(p.body[off:]) != want {
			t.Fatal("GUID conversion")
		}
		if f.modern == modernworld.CMSGQueryPetition && binary.LittleEndian.Uint32(p.body) != 123 {
			t.Fatal("petition ID replaced by item counter")
		}
		if f.modern == modernworld.CMSGOfferPetition && binary.LittleEndian.Uint64(p.body[12:]) != 99 {
			t.Fatal("offer target")
		}
	}
}

func TestPetitionProxyRejectsWrongGUIDTypesAndPartialPackets(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, currentCharacter: 42}
	for _, g := range []modernworld.GUID128{
		{}, modernworld.ModernGUIDForLegacy(42, 0),
		{Low: 42, High: 0}, // must not hit generic currentCharacter fallback
		{Low: uint64(math.MaxUint32) + 42, High: modernworld.ModernItemHigh},
	} {
		if e := s.handlePetitionRequest(session, modernworld.Packet{Opcode: modernworld.CMSGPetitionShowSignatures, Body: guildProxyGUID(g)}); e == nil {
			t.Fatalf("accepted invalid item %#v", g)
		}
	}
	if e := s.handlePetitionRequest(session, modernworld.Packet{Opcode: modernworld.CMSGPetitionBuy, Body: []byte{2, 1, 0, 42, 1, 0, 0, 0, 'G'}}); e == nil {
		t.Fatal("accepted unknown NPC")
	}
	if e := s.handlePetitionRequest(session, modernworld.Packet{Opcode: modernworld.CMSGSignPetition, Body: []byte{1}}); e == nil {
		t.Fatal("accepted partial packet")
	}
	select {
	case p := <-c.writes:
		t.Fatalf("invalid request forwarded %x", p.opcode)
	default:
	}
}

func TestPetitionSignaturesQueueAccountIdentityAndRecipientReply(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	const item = uint64(0x4000000000000042)
	b := binary.LittleEndian.AppendUint64(nil, item)
	b = binary.LittleEndian.AppendUint64(b, 8)
	b = binary.LittleEndian.AppendUint32(b, 123)
	b = append(b, 0)
	for _, own := range []bool{false, true} {
		session := &proxySession{legacyWorld: c, gameAccountID: 900}
		if own {
			session.knownCharacters = map[uint64]struct{}{8: {}}
		}
		if e := s.handleLegacyPetition(session, 0x1BF, b); e != nil {
			t.Fatal(e)
		}
		if len(session.pendingInstance) != 1 {
			t.Fatal("not queued before instance connection")
		}
		p := session.pendingInstance[0]
		wantAccount := modernworld.ModernWowAccountGUIDForLegacy(8)
		if own {
			wantAccount = modernworld.ModernWowAccountGUID(900)
		}
		want := append(guildProxyGUID(modernworld.ModernGUIDForLegacy(item, 0)), guildProxyGUID(modernworld.ModernGUIDForLegacy(8, 0))...)
		want = append(want, guildProxyGUID(wantAccount)...)
		want = append(want, 123, 0, 0, 0, 0, 0, 0, 0)
		if p.Opcode != modernworld.SMSGPetitionShowSignatures || !bytes.Equal(p.Body, want) {
			t.Fatalf("signature window %x want %x", p.Body, want)
		}
		req := append(guildProxyGUID(modernworld.ModernGUIDForLegacy(item, 0)), 0)
		if e := s.handlePetitionRequest(session, modernworld.Packet{Opcode: modernworld.CMSGSignPetition, Body: req}); e != nil {
			t.Fatal(e)
		}
		if p := guildProxyNext(t, c); p.opcode != 0x1C0 || binary.LittleEndian.Uint64(p.body) != item {
			t.Fatal("offered charter signature not forwarded")
		}
		if e := s.handleLegacyPetition(session, 0x1BF, b[:len(b)-1]); !errors.Is(e, errInvalidPetitionPacket) {
			t.Fatal("invalid response not classified")
		}
		if len(session.pendingInstance) != 1 {
			t.Fatal("malformed packet queued")
		}
	}
}

func TestPetitionLegacyRelayDispatchRecoversAfterMalformedPacket(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, legacy: &legacyauth.Session{Username: "test"}}
	done := make(chan struct{})
	go func() { defer close(done); s.relayLegacyWorld(session, nil, c) }()
	defer func() { c.Close(); <-done }()
	c.reads <- legacyworld.Packet{Opcode: 0x1C5, Body: []byte{4}}
	c.reads <- legacyworld.Packet{Opcode: 0x1C5, Body: []byte{4, 0, 0, 0}}
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		session.worldMu.Lock()
		n := len(session.pendingInstance)
		var p modernworld.Packet
		if n > 0 {
			p = session.pendingInstance[0]
		}
		session.worldMu.Unlock()
		if n > 0 {
			if n != 1 || p.Opcode != modernworld.SMSGTurnInPetitionResult || !bytes.Equal(p.Body, []byte{0x40}) {
				t.Fatal("wrong relay response")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("petition response not dispatched")
		case <-tick.C:
		}
	}
}
