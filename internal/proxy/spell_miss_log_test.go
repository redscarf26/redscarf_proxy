package proxy

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

func TestSpellMissLogRelay(t *testing.T) {
	s := guildProxyTestServer()
	c := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: c, legacy: &legacyauth.Session{Username: "test"}}
	body, _ := hex.DecodeString("3412000001000000000000000001000000020000000000000007")
	log, _ := modernworld.ParseLegacySpellMissLog(body)
	want := modernworld.EncodeSpellMissLog(log, session.modernGUIDForLegacyLocked)
	done := make(chan struct{})
	go func() { defer close(done); s.relayLegacyWorld(session, nil, c) }()
	defer func() { c.Close(); <-done }()
	c.reads <- legacyworld.Packet{Opcode: 0x024B, Body: body[:25]}
	c.reads <- legacyworld.Packet{Opcode: 0x024B, Body: body}
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		session.worldMu.Lock()
		packets := append([]modernworld.Packet(nil), session.pendingInstance...)
		session.worldMu.Unlock()
		if len(packets) > 0 {
			if len(packets) != 1 || packets[0].Opcode != 0x2C3E || !bytes.Equal(packets[0].Body, want) {
				t.Fatalf("unexpected packets: %+v", packets)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("spell miss log not forwarded")
		case <-tick.C:
		}
	}
}
