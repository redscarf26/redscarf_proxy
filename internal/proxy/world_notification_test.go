package proxy

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

func TestWorldNotificationRelay(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, legacy: &legacyauth.Session{Username: "test"}}
	done := make(chan struct{})
	go func() { defer close(done); s.relayLegacyWorld(session, nil, c) }()
	defer func() { c.Close(); <-done }()
	// Real legacy wire shape: packed GUID, flags, flags2, time, XYZO, fall time.
	move := make([]byte, 32)
	move[0], move[1] = 1, 0x42
	binary.LittleEndian.PutUint32(move[2:], 0x800)
	poi := append(make([]byte, 20), []byte("银行\x00")...)
	inputs := []legacyworld.Packet{
		{Opcode: 0x0320, Body: []byte{0x44, 2, 0, 0}},
		{Opcode: 0x0224, Body: poi},
		{Opcode: 0x00ec, Body: move},
		{Opcode: 0x0518, Body: binary.LittleEndian.AppendUint32(append([]byte(nil), move...), math.Float32bits(2.75))},
	}
	want := []uint16{0x2688, 0x2798, 0x2de0, 0x2ddf}
	for _, p := range inputs {
		c.reads <- legacyworld.Packet{Opcode: p.Opcode, Body: p.Body[:len(p.Body)-1]}
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
				t.Fatalf("unexpected packets: %+v", packets)
			}
			for i, p := range packets {
				if p.Opcode != want[i] {
					t.Fatalf("packet %d opcode=%x want=%x", i, p.Opcode, want[i])
				}
			}
			if h := math.Float32frombits(binary.LittleEndian.Uint32(packets[3].Body[len(packets[3].Body)-8:])); h != 2.75 {
				t.Fatalf("height=%v", h)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("notifications were not relayed")
		case <-tick.C:
		}
	}
}
