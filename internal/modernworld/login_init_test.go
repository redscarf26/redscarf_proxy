package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestLoginSetTimeSpeed(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 0x12345678)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x3c888889)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	body, err := EncodeLoginSetTimeSpeed(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 20 || binary.LittleEndian.Uint32(body[:4]) != 0x12345678 || binary.LittleEndian.Uint32(body[4:8]) != 0x12345678 || binary.LittleEndian.Uint32(body[12:16]) != 7 || binary.LittleEndian.Uint32(body[16:]) != 7 {
		t.Fatalf("unexpected login-set-time-speed: %x", body)
	}
}

func TestEncodePlayerBound(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1519)
	body, err := EncodePlayerBound(legacy, GUID128{Low: 0x43, High: 1})
	if err != nil {
		t.Fatal(err)
	}
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil || low != 0x43 || high != 1 || binary.LittleEndian.Uint32(body[consumed:]) != 1519 || consumed+4 != len(body) {
		t.Fatalf("player-bound guid=%x:%x consumed=%d body=%x err=%v", high, low, consumed, body, err)
	}
	if _, err := EncodePlayerBound(legacy[:11], GUID128{}); err == nil {
		t.Fatal("truncated player-bound unexpectedly accepted")
	}
}

func TestInitializeFactionsPadsToBuild54261Count(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 2)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 100)
	legacy = append(legacy, 2)
	negativeStanding := int32(-50)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(negativeStanding))
	body, err := EncodeInitializeFactions(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 6125 || binary.LittleEndian.Uint16(body[:2]) != 1 || int32(binary.LittleEndian.Uint32(body[2:6])) != 100 || binary.LittleEndian.Uint16(body[6:8]) != 2 || int32(binary.LittleEndian.Uint32(body[8:12])) != -50 {
		t.Fatalf("unexpected initialized factions prefix: %x", body[:18])
	}
}
