package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestDarkPortalHotfixDelivery(t *testing.T) {
	var portal HotfixRecord
	for _, record := range customizationHotfixes {
		if record.TableHash == areaTriggerTableHash && record.RecordID == 4354 {
			portal = record
		}
	}
	if portal.PushID != 100033 || len(portal.Content) != 50 {
		t.Fatalf("portal=%+v", portal)
	}
	b := portal.Content
	if b[0] != 0 || binary.LittleEndian.Uint32(b[13:17]) != 4354 || binary.LittleEndian.Uint16(b[17:19]) != 0 || b[44] != 1 {
		t.Fatalf("portal layout=%x", b)
	}
	for offset, want := range map[int]float32{1: -11908.9, 5: -3209.1, 9: -14.7905, 28: 3.833, 32: 15.47, 36: 25.83, 40: 3.281} {
		if got := math.Float32frombits(binary.LittleEndian.Uint32(b[offset:])); got != want {
			t.Fatalf("offset %d=%v want %v", offset, got, want)
		}
	}
	index, err := EncodeAvailableHotfixes(1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := 8; i < len(index); i += 8 {
		if binary.LittleEndian.Uint32(index[i:]) == portal.PushID {
			found = true
		}
	}
	if !found {
		t.Fatal("portal missing from advertised hotfixes")
	}
	body, matched, err := EncodeHotfixConnect([]uint32{portal.PushID})
	if err != nil || matched != 1 || !bytes.Equal(body[29:], b) || binary.LittleEndian.Uint32(body[12:]) != areaTriggerTableHash {
		t.Fatalf("portal delivery matched=%d err=%v body=%x", matched, err, body)
	}
}

func TestCustomizationHotfixIndexAndConnect(t *testing.T) {
	if customizationHotfixError != nil {
		t.Fatal(customizationHotfixError)
	}
	if len(customizationHotfixes) != 1112 {
		t.Fatalf("customization hotfix count = %d, want 1112", len(customizationHotfixes))
	}
	index, err := EncodeAvailableHotfixes(0x01010001)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 8+1112*8 || binary.LittleEndian.Uint32(index[:4]) != 0x01010001 || binary.LittleEndian.Uint32(index[4:8]) != 1112 {
		t.Fatalf("unexpected hotfix index header: bytes=%d header=%x", len(index), index[:8])
	}
	first := customizationHotfixes[0]
	last := customizationHotfixes[len(customizationHotfixes)-1]
	body, matched, err := EncodeHotfixConnect([]uint32{first.PushID, 1, last.PushID})
	if err != nil {
		t.Fatal(err)
	}
	if matched != 2 || binary.LittleEndian.Uint32(body[:4]) != 2 {
		t.Fatalf("matched=%d body count=%d", matched, binary.LittleEndian.Uint32(body[:4]))
	}
	if binary.LittleEndian.Uint32(body[4:8]) != first.PushID || body[24] != 0x20 {
		t.Fatalf("unexpected first hotfix metadata: %x", body[:25])
	}
	totalOffset := 4 + 2*21
	if got := binary.LittleEndian.Uint32(body[totalOffset : totalOffset+4]); got != uint32(len(first.Content)+len(last.Content)) {
		t.Fatalf("total hotfix data size = %d", got)
	}
}

func TestParseHotfixRequest(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 54261)
	body = binary.LittleEndian.AppendUint32(body, 123)
	body = binary.LittleEndian.AppendUint32(body, 2)
	body = binary.LittleEndian.AppendUint32(body, 3_000_001)
	body = binary.LittleEndian.AppendUint32(body, 3_100_001)
	ids, err := ParseHotfixRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 3_000_001 || ids[1] != 3_100_001 {
		t.Fatalf("unexpected hotfix IDs: %v", ids)
	}
	if _, err := ParseHotfixRequest(body[:len(body)-1]); err == nil {
		t.Fatal("expected truncated request to fail")
	}
}

func TestDBQueryBulkAndReply(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, hashTactKey)
	body = append(body, 0, 0x10) // 2 as a 13-bit MSB-first value
	body = binary.LittleEndian.AppendUint32(body, 11)
	body = binary.LittleEndian.AppendUint32(body, 22)
	query, err := ParseDBQueryBulk(body)
	if err != nil {
		t.Fatal(err)
	}
	if query.TableHash != hashTactKey || len(query.RecordIDs) != 2 || query.RecordIDs[1] != 22 {
		t.Fatalf("unexpected DB query: %#v", query)
	}
	reply := EncodeDBReply(query.TableHash, query.RecordIDs[0], 1234)
	if len(reply) != 17 || reply[12] != 0x80 || binary.LittleEndian.Uint32(reply[13:]) != 0 {
		t.Fatalf("unexpected DB reply: %x", reply)
	}
}

func TestBroadcastTextDBReply(t *testing.T) {
	record := BroadcastTextRecord{
		ID:          1_000_000_123,
		MaleText:    "你好，$N！",
		FemaleText:  "您好，$N！",
		Language:    7,
		Emotes:      [3]uint16{5, 1, 0},
		EmoteDelays: [3]uint16{10, 3, 0},
	}
	body := EncodeBroadcastTextDBReply(record, 1234)
	if binary.LittleEndian.Uint32(body[:4]) != BroadcastTextTableHash || binary.LittleEndian.Uint32(body[4:8]) != record.ID || body[12] != 0x20 {
		t.Fatalf("broadcast DB reply header = %x", body[:13])
	}
	size := binary.LittleEndian.Uint32(body[13:17])
	if int(size) != len(body)-17 || !bytes.Contains(body[17:], []byte("你好，$N！\x00")) || !bytes.Contains(body[17:], []byte("您好，$N！\x00")) {
		t.Fatalf("broadcast DB reply size=%d body=%x", size, body[17:])
	}
}
