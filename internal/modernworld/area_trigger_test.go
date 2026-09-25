package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestAreaTrigger(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 4711)
	body = append(body, 0xC0)
	request, err := ParseAreaTrigger(body)
	if err != nil || request.ID != 4711 || !request.Entered || !request.FromClient {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	legacy := EncodeLegacyAreaTrigger(request)
	if len(legacy) != 4 || binary.LittleEndian.Uint32(legacy) != 4711 {
		t.Fatalf("legacy=%x", legacy)
	}
	if _, err := ParseAreaTrigger(body[:4]); err == nil {
		t.Fatal("expected a short area-trigger error")
	}
}

func TestAreaTriggerStateBits(t *testing.T) {
	for _, tc := range []struct {
		flags               byte
		entered, fromClient bool
	}{
		{0x80, true, false}, {0x40, false, true}, {0xC0, true, true}, {0x03, false, false},
	} {
		request, err := ParseAreaTrigger([]byte{0x02, 0x11, 0, 0, tc.flags})
		if err != nil || request.ID != 4354 || request.Entered != tc.entered || request.FromClient != tc.fromClient {
			t.Fatalf("flags=%x request=%+v err=%v", tc.flags, request, err)
		}
	}
}
