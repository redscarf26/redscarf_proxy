package modernworld

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestQueryTimeRoundTrip(t *testing.T) {
	if err := ParseQueryTime(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseQueryTime([]byte{1}); err == nil {
		t.Fatal("expected trailing-byte error")
	}
	legacy := binary.LittleEndian.AppendUint32(nil, 0x12345678)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3600)
	body, err := TranslateQueryTimeResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, legacy) {
		t.Fatalf("body %x, want %x", body, legacy)
	}
	if _, err := TranslateQueryTimeResponse(legacy[:7]); err == nil {
		t.Fatal("expected short query-time response error")
	}
}

func TestRealmQueryResponse(t *testing.T) {
	address, err := ParseQueryRealmName([]byte{0x01, 0x00, 0x01, 0x01})
	if err != nil || address != 0x01010001 {
		t.Fatalf("address=0x%x err=%v", address, err)
	}
	body, err := EncodeRealmQueryResponse(0x01010001, "Azeroth Core")
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 0x01010001 || body[4] != 0 {
		t.Fatalf("header %x", body[:5])
	}
	if !strings.Contains(string(body), "Azeroth Core") || !strings.Contains(string(body), "AzerothCore") {
		t.Fatalf("names missing from %x", body)
	}
	if _, err := EncodeRealmQueryResponse(1, ""); err == nil {
		t.Fatal("expected empty realm name error")
	}
}
