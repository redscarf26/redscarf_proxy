package proxy

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

func TestRuntimeNotificationClientRoutes(t *testing.T) {
	s, c := guildProxyTestServer(), newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, legacy: &legacyauth.Session{Username: "test"}}
	for i, op := range []uint16{modernworld.CMSGOpeningCinematic, modernworld.CMSGNextCinematicCamera, modernworld.CMSGCompleteCinematic} {
		if ok, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: op}); !ok || err != nil {
			t.Fatal(ok, err)
		}
		p := guildProxyNext(t, c)
		if p.opcode != []uint32{0xf9, 0xfb, 0xfc}[i] || len(p.body) != 0 {
			t.Fatal(p)
		}
		if _, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: op, Body: []byte{1}}); err == nil {
			t.Fatal("cinematic trailer accepted")
		}
	}
	// legacy proxy expansion 80 uses an 11-bit length, no language field.
	text := []byte("挥手")
	body := append([]byte{byte(len(text) >> 3), byte(len(text) << 5)}, text...)
	if ok, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGChatMessageEmote, Body: body}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	p := guildProxyNext(t, c)
	if p.opcode != legacyworld.CMSGMessageChat || binary.LittleEndian.Uint32(p.body) != 10 || binary.LittleEndian.Uint32(p.body[4:]) != 7 || !bytes.Equal(p.body[8:], append(text, 0)) {
		t.Fatal(p)
	}
	if _, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGChatMessageEmote, Body: body[:len(body)-1]}); err == nil {
		t.Fatal("truncated emote accepted")
	}
}

func TestRuntimeNotificationRelay(t *testing.T) {
	s, c := guildProxyTestServer(), newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, legacy: &legacyauth.Session{Username: "test"}}
	done := make(chan struct{})
	go func() { defer close(done); s.relayLegacyWorld(session, nil, c) }()
	defer func() { c.Close(); <-done }()
	hover := modernworld.EncodeLegacyPackedGUID(42)
	hover = binary.LittleEndian.AppendUint32(hover, 0x40000000)
	hover = append(hover, make([]byte, 26)...)
	first := append([]byte("Player\x00"), make([]byte, 16)...)
	impact := binary.LittleEndian.AppendUint64(nil, 42)
	impact = binary.LittleEndian.AppendUint32(impact, 123)
	inputs := []legacyworld.Packet{
		{Opcode: 0x152, Body: []byte{1, 42}},
		{Opcode: 0x319, Body: []byte{1, 42, 123, 0, 0, 0}},
		{Opcode: 0xfa, Body: []byte{42, 0, 0, 0}},
		{Opcode: 0x43e, Body: make([]byte, 24)},
		{Opcode: 0x498, Body: first},
		{Opcode: 0x1f7, Body: impact},
		{Opcode: 0xf7, Body: hover},
	}
	want := []uint16{modernworld.SMSGBreakTarget, modernworld.SMSGMoveSkipTime, modernworld.SMSGTriggerCinematic, modernworld.SMSGCalendarRaidLockoutAdded, modernworld.SMSGChat, modernworld.SMSGPlaySpellVisualKit, modernworld.SMSGMoveUpdate}
	for _, p := range inputs {
		c.reads <- p
	}
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		session.worldMu.Lock()
		packets := append([]modernworld.Packet(nil), session.pendingInstance...)
		session.worldMu.Unlock()
		if len(packets) >= len(want) {
			if len(packets) != len(want) {
				t.Fatal("extra packets", len(packets))
			}
			for i, p := range packets {
				if p.Opcode != want[i] {
					t.Fatalf("packet %d opcode %x want %x", i, p.Opcode, want[i])
				}
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("missing relay packets", len(packets))
		case <-tick.C:
		}
	}
}
