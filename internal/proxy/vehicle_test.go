package proxy

import (
	"testing"

	"redscarf/internal/modernworld"
)

func TestRequestVehicleExitForwardsToLegacy(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{}
	session := &proxySession{legacyWorld: legacyConnection}
	// Pin both wire opcodes independently of the implementation constants.
	handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: 0x3237})
	if err != nil || !handled {
		t.Fatalf("vehicle exit handled=%v err=%v", handled, err)
	}
	write := legacyConnection.nextWrite(t)
	if write.opcode != 0x0476 || len(write.body) != 0 {
		t.Fatalf("unexpected vehicle exit write: %#v", write)
	}
	if _, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: 0x3237, Body: []byte{0}}); err == nil {
		t.Fatal("accepted malformed vehicle exit request")
	}
}
