package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestWhoRequestRoundTripToLegacy(t *testing.T) {
	bits := newBitWriter(nil)
	bits.writeBits(2, 4)
	bits.writeBit(true)
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, 10)
	body = binary.LittleEndian.AppendUint32(body, 80)
	body = binary.LittleEndian.AppendUint64(body, 0x1122334455667788)
	body = binary.LittleEndian.AppendUint32(body, 0x1234)
	bits = newBitWriter(body)
	bits.writeBits(3, 6)
	bits.writeBits(5, 9)
	bits.writeBits(5, 7)
	bits.writeBits(0, 9)
	bits.writeBits(2, 3)
	bits.writeBit(false)
	bits.writeBit(false)
	bits.writeBit(true)
	bits.writeBit(false)
	body = bits.flush()
	for _, word := range []string{"tank", "heal"} {
		bits = newBitWriter(body)
		bits.writeBits(uint32(len(word)), 7)
		body = append(bits.flush(), word...)
	}
	body = append(body, "Ali"...)
	body = append(body, "Realm"...)
	body = append(body, "Guild"...)
	body = binary.LittleEndian.AppendUint32(body, 0xAABBCCDD)
	body = append(body, 2) // SocialQueue origin in build 54261
	body = binary.LittleEndian.AppendUint32(body, 12)
	body = binary.LittleEndian.AppendUint32(body, 1519)

	request, err := ParseWhoRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.MinLevel != 10 || request.MaxLevel != 80 || request.Name != "Ali" || request.Guild != "Guild" ||
		request.RequestID != 0xAABBCCDD || len(request.Words) != 2 || request.Words[1] != "heal" ||
		request.Origin != 2 || !request.FromAddon || len(request.Areas) != 2 || request.Areas[1] != 1519 {
		t.Fatalf("unexpected WHO request: %#v", request)
	}
	legacy := EncodeLegacyWhoRequest(request)
	if binary.LittleEndian.Uint32(legacy[:4]) != 10 || binary.LittleEndian.Uint32(legacy[4:8]) != 80 {
		t.Fatalf("unexpected legacy WHO header: %x", legacy[:8])
	}
}

func TestLegacyWhoResponseToModern(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 27)
	legacy = append(legacy, "Alice"...)
	legacy = append(legacy, 0)
	legacy = append(legacy, "Knights"...)
	legacy = append(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 80)
	legacy = binary.LittleEndian.AppendUint32(legacy, 8)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1519)

	entries, err := ParseLegacyWhoResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "Alice" || entries[0].GuildName != "Knights" || entries[0].Level != 80 {
		t.Fatalf("unexpected WHO entries: %#v", entries)
	}
	modern := EncodeWhoResponse(entries, 0x12345678, 0x01010001)
	if got := binary.LittleEndian.Uint32(modern); got != 0x12345678 {
		t.Fatalf("request ID = %#x", got)
	}
	if modern[4] != 0x04 { // one result in a six-bit MSB-first field
		t.Fatalf("result count byte = %#x", modern[4])
	}
}
