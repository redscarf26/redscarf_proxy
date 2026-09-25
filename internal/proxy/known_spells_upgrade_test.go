package proxy

import (
	"encoding/binary"
	"reflect"
	"testing"

	"redscarf/internal/modernworld"
)

func TestSupercededSpellRegistersNewRankBeforeActionReplacement(t *testing.T) {
	session := &proxySession{knownSpells: []uint32{45477, 3714}}
	body := binary.LittleEndian.AppendUint32(nil, 45477)
	body = binary.LittleEndian.AppendUint32(body, 49896)
	// No legacy LEARNED packet follows an upgrade. Repeated notifications
	// must also leave exactly one copy of the new rank in the login cache.
	for i := 0; i < 2; i++ {
		session.pendingInstance = nil
		if err := session.forwardSupercededSpells(body); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(session.knownSpells, []uint32{3714, 49896}) {
			t.Fatalf("cached spells: %v", session.knownSpells)
		}
		packets := session.pendingInstance
		if len(packets) != 2 {
			t.Fatalf("expected learning then replacement, got %d packets", len(packets))
		}
		learned := packets[0]
		if learned.Opcode != modernworld.SMSGLearnedSpells || len(learned.Body) != 14 || binary.LittleEndian.Uint32(learned.Body[:4]) != 1 || learned.Body[8] != 0x80 || binary.LittleEndian.Uint32(learned.Body[9:13]) != 49896 {
			t.Fatalf("new rank not registered silently before replacement: %+v", learned)
		}
		replaced := packets[1]
		if replaced.Opcode != modernworld.SMSGSupercededSpells || len(replaced.Body) != 13 || binary.LittleEndian.Uint32(replaced.Body[4:8]) != 49896 || replaced.Body[8] != 0x20 || binary.LittleEndian.Uint32(replaced.Body[9:13]) != 45477 {
			t.Fatalf("incorrect rank replacement: %+v", replaced)
		}
	}
}

func TestMalformedSupercededSpellDoesNotChangeState(t *testing.T) {
	session := &proxySession{knownSpells: []uint32{45477}}
	if err := session.forwardSupercededSpells([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected malformed packet error")
	}
	if !reflect.DeepEqual(session.knownSpells, []uint32{45477}) || len(session.pendingInstance) != 0 {
		t.Fatal("malformed replacement modified state or sent packets")
	}
}
