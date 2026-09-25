package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestSummonResponseTranslation(t *testing.T) {
	summoner := GUID128{Low: 0x42, High: 1}
	body := appendPackedGUID128(nil, summoner.Low, summoner.High)
	bits := newBitWriter(body)
	bits.writeBit(true)
	body = bits.flush()

	response, err := ParseSummonResponse(body)
	if err != nil || response.Summoner != summoner || !response.Accept {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	legacy := EncodeLegacySummonResponse(0x42, response.Accept)
	if len(legacy) != 9 || binary.LittleEndian.Uint64(legacy) != 0x42 || legacy[8] != 1 {
		t.Fatalf("legacy=%x", legacy)
	}
	if _, err := ParseSummonResponse(append(body, 0)); err == nil {
		t.Fatal("expected trailing-byte error")
	}
}

func TestSummonRequestTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000042)
	legacy = binary.LittleEndian.AppendUint32(legacy, 12)
	legacy = binary.LittleEndian.AppendUint32(legacy, 120000)
	legacyGUID, areaID, err := ParseLegacySummonRequest(legacy)
	if err != nil || legacyGUID != 0xf130000001000042 || areaID != 12 {
		t.Fatalf("guid=%x area=%d err=%v", legacyGUID, areaID, err)
	}

	summoner := GUID128{Low: 0x42, High: 1}
	body := EncodeSummonRequest(summoner, 0x01020304, areaID)
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil || low != summoner.Low || high != summoner.High {
		t.Fatalf("summoner=%x:%x consumed=%d err=%v", low, high, consumed, err)
	}
	if binary.LittleEndian.Uint32(body[consumed:]) != 0x01020304 || int32(binary.LittleEndian.Uint32(body[consumed+4:])) != areaID || body[consumed+8] != 0 || body[consumed+9] != 0 {
		t.Fatalf("modern summon-request=%x", body)
	}
	if _, _, err := ParseLegacySummonRequest(legacy[:15]); err == nil {
		t.Fatal("expected truncated legacy request error")
	}
}
