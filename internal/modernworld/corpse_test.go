package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestCorpseQueryTranslation(t *testing.T) {
	if loc, err := ParseLegacyCorpseQuery([]byte{0}); err != nil || loc.Valid {
		t.Fatalf("empty corpse query valid=%v err=%v", loc.Valid, err)
	}
	legacy := []byte{1}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1.5))
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(2.5))
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(3.5))
	legacy = binary.LittleEndian.AppendUint32(legacy, 571)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	loc, err := ParseLegacyCorpseQuery(legacy)
	if err != nil || !loc.Valid || loc.MapID != 0 || loc.ActualMapID != 571 || loc.X != 1.5 || loc.Y != 2.5 || loc.Z != 3.5 {
		t.Fatalf("corpse query %#v err=%v", loc, err)
	}

	player := GUID128{Low: 0x42, High: 1}
	body := EncodeCorpseLocation(player, loc)
	if body[0]&0x80 == 0 {
		t.Fatalf("valid bit missing: %x", body)
	}
	low, high, consumed, err := readPackedGUID128(body[1:])
	if err != nil || low != 0x42 || high != 1 {
		t.Fatalf("player guid %x err=%v", body, err)
	}
	rest := body[1+consumed:]
	if int32(binary.LittleEndian.Uint32(rest[:4])) != 571 {
		t.Fatalf("actual map %x", rest)
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(rest[4:8])) != 1.5 {
		t.Fatalf("x %x", rest)
	}

	empty := EncodeCorpseLocation(player, CorpseLocation{})
	if empty[0]&0x80 != 0 {
		t.Fatalf("empty location should not set Valid: %x", empty)
	}
}

func TestDeathReleaseAndReclaimDelay(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(10))
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(20))
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(30))
	body, err := TranslateDeathReleaseLoc(legacy)
	if err != nil || string(body) != string(legacy) {
		t.Fatalf("death release %x err=%v", body, err)
	}
	if _, err := TranslateDeathReleaseLoc(legacy[:15]); err == nil {
		t.Fatal("expected short death-release-loc to fail")
	}
	delay, err := TranslateCorpseReclaimDelay([]byte{0xe8, 0x03, 0, 0})
	if err != nil || binary.LittleEndian.Uint32(delay) != 1000 {
		t.Fatalf("reclaim delay %x err=%v", delay, err)
	}
}

func TestRepopAndCorpseClientPackets(t *testing.T) {
	checkInstance, err := ParseRepopRequest(nil)
	if err != nil || checkInstance || string(EncodeLegacyRepopRequest(checkInstance)) != "\x00" {
		t.Fatalf("empty repop checkInstance=%v legacy=%x err=%v", checkInstance, EncodeLegacyRepopRequest(checkInstance), err)
	}
	bits := newBitWriter(nil)
	bits.writeBit(true)
	checkInstance, err = ParseRepopRequest(bits.flush())
	if err != nil || !checkInstance || string(EncodeLegacyRepopRequest(checkInstance)) != "\x01" {
		t.Fatalf("repop checkInstance=%v legacy=%x err=%v", checkInstance, EncodeLegacyRepopRequest(checkInstance), err)
	}
	if _, err := ParseRepopRequest([]byte{0, 1}); err == nil {
		t.Fatal("expected trailing-byte error")
	}
	guid, err := ParseQueryCorpseLocation(appendPackedGUID128(nil, 0x42, 1))
	if err != nil || guid.Low != 0x42 {
		t.Fatalf("query corpse guid %#v err=%v", guid, err)
	}
	reclaim, err := ParseReclaimCorpse(appendPackedGUID128(nil, 9, 1))
	if err != nil || reclaim.Low != 9 {
		t.Fatalf("reclaim %#v err=%v", reclaim, err)
	}
	if guid, err := ParseReclaimCorpse(nil); err != nil || guid.Low != 0 {
		t.Fatalf("empty reclaim %#v err=%v", guid, err)
	}
	if delay := EncodeCorpseReclaimDelay(0); binary.LittleEndian.Uint32(delay) != 0 {
		t.Fatalf("zero reclaim delay %x", delay)
	}
}
