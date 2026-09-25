package proxy

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

func TestRuntimeMiscRejectsMalformedWithoutSending(t *testing.T) {
	s := guildProxyTestServer()
	session := &proxySession{}
	for _, p := range []legacyworld.Packet{
		{Opcode: 0x369, Body: []byte{2}}, {Opcode: 0x369},
		{Opcode: 0x1df, Body: make([]byte, 7)}, {Opcode: 0x1df, Body: make([]byte, 9)},
		{Opcode: 0x278, Body: make([]byte, 11)}, {Opcode: 0x278, Body: make([]byte, 13)},
	} {
		if err := s.handleLegacyRuntimeMisc(session, p); !errors.Is(err, errInvalidRuntimeMiscPacket) {
			t.Fatal("malformed packet accepted", err)
		}
	}
	if len(session.pendingInstance) != 0 {
		t.Fatal("malformed notification sent")
	}
}

func TestRuntimeMiscClientRoutes(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c}
	ok, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGMountSpecialAnim, Body: make([]byte, 8)})
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	if p := guildProxyNext(t, c); p.opcode != 0x171 || len(p.body) != 0 {
		t.Fatal("mount not forwarded")
	}
	ok, err = s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGEmote})
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	select {
	case <-c.writes:
		t.Fatal("empty notification invented an emote")
	default:
	}
}

func TestRuntimeMiscRelayAndMovementBatch(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, legacy: &legacyauth.Session{Username: "test"}, objectPositions: map[uint64][3]float32{42: {1, 2, 3}}}
	done := make(chan struct{})
	go func() { defer close(done); s.relayLegacyWorld(session, nil, c) }()
	defer func() { c.Close(); <-done }()
	c.reads <- legacyworld.Packet{Opcode: 0x369, Body: []byte{1}}
	c.reads <- legacyworld.Packet{Opcode: 0x1df, Body: binary.LittleEndian.AppendUint64(nil, 42)}
	sound := binary.LittleEndian.AppendUint32(nil, 123)
	sound = binary.LittleEndian.AppendUint64(sound, 42)
	c.reads <- legacyworld.Packet{Opcode: 0x278, Body: sound}
	c.reads <- legacyworld.Packet{Opcode: 0x51e, Body: []byte{18, 0, 0, 0, 8, 0xe8, 0, 1, 42, 1, 0, 0, 0, 8, 0xde, 0, 1, 42, 2, 0, 0, 0}}
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		session.worldMu.Lock()
		packets := append([]modernworld.Packet(nil), session.pendingInstance...)
		session.worldMu.Unlock()
		if len(packets) >= 4 {
			want := []uint16{modernworld.SMSGPageText, modernworld.SMSGPlayObjectSound, modernworld.SMSGMoveRoot, modernworld.SMSGMoveSetWaterWalk}
			if len(packets) != len(want) {
				t.Fatal("unexpected number of replies")
			}
			for i, p := range packets {
				if p.Opcode != want[i] {
					t.Fatalf("reply %d = %x want %x", i, p.Opcode, want[i])
				}
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("runtime notifications not dispatched")
		case <-tick.C:
		}
	}
}
