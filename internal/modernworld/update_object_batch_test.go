package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMergeUpdateObjectBodies(t *testing.T) {
	const mapID = uint16(571)
	first := encodeUpdateObjects(mapID, []byte{0x01, 0x02})
	second := encodeUpdateObjects(mapID, []byte{0x03, 0x04, 0x05})

	merged, err := MergeUpdateObjectBodies(mapID, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(merged[:4]); got != 2 {
		t.Fatalf("object count = %d, want 2", got)
	}
	if got := binary.LittleEndian.Uint16(merged[4:6]); got != mapID {
		t.Fatalf("map id = %d, want %d", got, mapID)
	}
	if got := binary.LittleEndian.Uint32(merged[7:11]); got != 5 {
		t.Fatalf("data size = %d, want 5", got)
	}
	if want := []byte{0x01, 0x02, 0x03, 0x04, 0x05}; !bytes.Equal(merged[11:], want) {
		t.Fatalf("merged object data = %x, want %x", merged[11:], want)
	}
}

func TestMergeUpdateObjectBodiesRejectsNonCreateEnvelope(t *testing.T) {
	body := encodeUpdateObjects(571, []byte{0x01})
	body[6] = 0x80
	if _, err := MergeUpdateObjectBodies(571, body); err == nil {
		t.Fatal("expected removal envelope to be rejected")
	}
}

func TestMergeUpdateObjectBodiesRejectsMismatchedDataSize(t *testing.T) {
	body := encodeUpdateObjects(571, []byte{0x01})
	binary.LittleEndian.PutUint32(body[7:11], 2)
	if _, err := MergeUpdateObjectBodies(571, body); err == nil {
		t.Fatal("expected mismatched data size to be rejected")
	}
}
