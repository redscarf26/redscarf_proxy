package proxy

import (
	"bytes"
	"testing"
	"redscarf/internal/modernworld"
)

func TestPVERequestsReachLegacyWithoutOptimisticReply(t *testing.T) {
	for _, tc := range []struct {
		opcode uint16
		body   []byte
		legacy uint32
		want   []byte
	}{
		{0x3684, []byte{1, 0, 0, 0}, 0x329, []byte{0, 0, 0, 0}},
		{0x3684, []byte{2, 0, 0, 0}, 0x329, []byte{1, 0, 0, 0}},
		{0x36e3, []byte{3, 0, 0, 0, 0}, 0x4eb, []byte{0, 0, 0, 0}},
		{0x36e3, []byte{4, 0, 0, 0, 0}, 0x4eb, []byte{1, 0, 0, 0}},
		{0x36e3, []byte{5, 0, 0, 0, 0}, 0x4eb, []byte{2, 0, 0, 0}},
		{0x36e3, []byte{6, 0, 0, 0, 0}, 0x4eb, []byte{3, 0, 0, 0}},
		{0x350b, []byte{0x80}, 0x13f, []byte{1}},
		{0x350b, []byte{0}, 0x13f, []byte{0}},
	} {
		conn := newFakeLegacyWorldConnection()
		session := &proxySession{legacyWorld: conn}
		server := &Server{}
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: tc.opcode, Body: tc.body})
		if !handled || err != nil {
			t.Fatalf("request %x handled=%v err=%v", tc.opcode, handled, err)
		}
		got := conn.nextWrite(t)
		if got.opcode != tc.legacy || !bytes.Equal(got.body, tc.want) {
			t.Fatalf("request %x got %#v", tc.opcode, got)
		}
		if len(session.pendingInstance) != 0 {
			t.Fatal("request fabricated success before legacy confirmation")
		}
		if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: tc.opcode, Body: append(tc.body, 0)}); err == nil {
			t.Fatal("accepted malformed request")
		}
	}
}
