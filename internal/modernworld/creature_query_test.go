package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestCreatureQueryRoundTrip(t *testing.T) {
	entry, err := ParseQueryCreature([]byte{0x45, 0x00, 0x00, 0x00})
	if err != nil || entry != 69 {
		t.Fatalf("entry=%d err=%v", entry, err)
	}
	legacyReq := EncodeLegacyCreatureQuery(69)
	if binary.LittleEndian.Uint32(legacyReq[:4]) != 69 || binary.LittleEndian.Uint64(legacyReq[4:]) != 0 {
		t.Fatalf("legacy creature query %x", legacyReq)
	}

	legacy := binary.LittleEndian.AppendUint32(nil, 69)
	legacy = appendCString(legacy, "Thing")
	legacy = append(legacy, 0, 0, 0) // name2-4
	legacy = append(legacy, 0)       // title
	legacy = append(legacy, 0)       // cursor
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 123)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1.5))
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1))
	legacy = append(legacy, 1) // leader
	for range 6 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 42)
	body, err := TranslateCreatureQueryResponse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 69 || body[4] != 0x80 {
		t.Fatalf("modern creature header %x", body[:5])
	}
	if !bytes.Contains(body, []byte("Thing\x00")) {
		t.Fatalf("creature name missing after Allow flush: %x", body)
	}
	if _, err := TranslateCreatureQueryResponse(binary.LittleEndian.AppendUint32(nil, 69|0x80000000)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseQueryCreature([]byte{0x45, 0x00, 0x00, 0x00, 0x00, 0x00}); err != nil {
		t.Fatalf("creature query with trailing GUID bytes: %v", err)
	}

	entry, display, err := LegacyCreatureQueryFirstDisplay(legacy)
	if err != nil || entry != 69 || display != 123 {
		t.Fatalf("first display entry=%d display=%d err=%v", entry, display, err)
	}
	missing := binary.LittleEndian.AppendUint32(nil, 69|legacyCreatureQueryNotFound)
	entry, display, err = LegacyCreatureQueryFirstDisplay(missing)
	if err != nil || entry != 69 || display != 0 {
		t.Fatalf("missing template entry=%d display=%d err=%v", entry, display, err)
	}
}

func TestCreatureEntryFromLegacy(t *testing.T) {
	if got := CreatureEntryFromLegacy(0xf130000782000371, nil); got != 0x782 {
		t.Fatalf("guid entry=%d", got)
	}
	if got := CreatureEntryFromLegacy(0xf130000782000371, map[int]uint32{legacyObjectEntry: 1922}); got != 1922 {
		t.Fatalf("field entry=%d", got)
	}
}

func TestNpcTextQueryRoundTrip(t *testing.T) {
	guidBody := appendPackedGUID128(nil, 9, 1)
	textID, guid, err := ParseQueryNpcText(append([]byte{0x22, 0x00, 0x00, 0x00}, guidBody...))
	if err != nil || textID != 0x22 || guid.Low != 9 {
		t.Fatalf("text=%d guid=%#v err=%v", textID, guid, err)
	}
	legacy := binary.LittleEndian.AppendUint32(nil, 0x22)
	for index := range 8 {
		legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1))
		if index == 0 {
			legacy = appendCString(legacy, "欢迎回来，$N！")
			legacy = appendCString(legacy, "欢迎回来，$N！")
			legacy = binary.LittleEndian.AppendUint32(legacy, 7)
		} else {
			legacy = append(legacy, 0, 0) // two empty strings
			legacy = binary.LittleEndian.AppendUint32(legacy, 0)
		}
		for emote := range 3 {
			delay := uint32(0)
			emoteID := uint32(0)
			if index == 0 && emote == 0 {
				delay, emoteID = 10, 5
			}
			legacy = binary.LittleEndian.AppendUint32(legacy, delay)
			legacy = binary.LittleEndian.AppendUint32(legacy, emoteID)
		}
	}
	body, records, err := TranslateNpcTextResponseWithBroadcasts(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 0x22 || body[4]&0x80 == 0 {
		t.Fatalf("npc-text header %x", body[:5])
	}
	if len(records) != 1 || records[0].MaleText != "欢迎回来，$N！" || records[0].Language != 7 || records[0].Emotes[0] != 5 || records[0].EmoteDelays[0] != 10 {
		t.Fatalf("broadcast records = %+v", records)
	}
	if got := binary.LittleEndian.Uint32(body[41:45]); got != records[0].ID {
		t.Fatalf("npc-text broadcast ID = %d, want %d", got, records[0].ID)
	}
}

func TestGameObjectQueryRoundTrip(t *testing.T) {
	entry, guid, err := ParseQueryGameObject(append([]byte{0x10, 0x00, 0x00, 0x00}, appendPackedGUID128(nil, 9, 1)...))
	if err != nil || entry != 16 || guid.Low != 9 {
		t.Fatalf("entry=%d guid=%#v err=%v", entry, guid, err)
	}
	legacy := binary.LittleEndian.AppendUint32(nil, 16)
	legacy = binary.LittleEndian.AppendUint32(legacy, 3)
	legacy = binary.LittleEndian.AppendUint32(legacy, 77)
	for range 4 {
		legacy = append(legacy, 0)
	}
	legacy = appendCString(legacy, "Icon")
	legacy = append(legacy, 0, 0) // caption + unk
	for range 24 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1))
	for range 6 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	body, err := TranslateGameObjectQueryResponse(legacy, GUID128{Low: 9, High: 1})
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(body[:4]) != 16 {
		t.Fatalf("modern game-object header %x", body[:4])
	}
}
