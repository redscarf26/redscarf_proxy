package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func pveWords(values ...uint32) []byte {
	var out []byte
	for _, v := range values {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	return out
}

func TestPVEDifficultySelections(t *testing.T) {
	for modern := uint32(1); modern <= 2; modern++ {
		got, err := TranslateSetDungeonDifficulty(pveWords(modern))
		if err != nil || !bytes.Equal(got, pveWords(modern-1)) {
			t.Fatalf("dungeon %d: %x %v", modern, got, err)
		}
		confirmed, err := ParseLegacyDungeonDifficulty(pveWords(modern-1, 1, 1))
		if err != nil || confirmed != modern {
			t.Fatalf("dungeon confirmation %d %v", confirmed, err)
		}
	}
	for modern := uint32(3); modern <= 6; modern++ {
		for _, legacyFlag := range []byte{0, 1} {
			got, err := TranslateSetRaidDifficulty(append(pveWords(modern), legacyFlag))
			if err != nil || !bytes.Equal(got, pveWords(modern-3)) {
				t.Fatalf("raid %d: %x %v", modern, got, err)
			}
		}
		got, err := TranslateLegacyRaidDifficulty(pveWords(modern-3, 1, 1))
		if err != nil || !bytes.Equal(got, append(pveWords(modern), 0)) {
			t.Fatalf("raid confirmation: %x %v", got, err)
		}
	}
}

func TestPVEMalformedPackets(t *testing.T) {
	for _, tc := range []struct {
		fn    func([]byte) ([]byte, error)
		valid []byte
	}{
		{TranslateSetDungeonDifficulty, pveWords(1)},
		{TranslateSetRaidDifficulty, append(pveWords(3), 0)},
		{TranslateLegacyRaidDifficulty, pveWords(0, 1, 0)},
		{TranslateInstanceLockResponse, []byte{0x80}},
		{TranslateInstanceSaveCreated, pveWords(0)},
		{TranslatePendingRaidLock, append(pveWords(60000, 7), 0)},
		{TranslateRaidInstanceMessage, append(pveWords(4, 631, 2, 60000), 1, 0)},
	} {
		for n := 0; n < len(tc.valid); n++ {
			if _, err := tc.fn(tc.valid[:n]); err == nil {
				t.Fatalf("accepted truncated packet %x", tc.valid[:n])
			}
		}
		if _, err := tc.fn(append(append([]byte(nil), tc.valid...), 0)); err == nil {
			t.Fatal("accepted trailing byte")
		}
	}
	for _, id := range []uint32{0, 3, 6, 23, 0xffffffff} {
		if _, err := TranslateSetDungeonDifficulty(pveWords(id)); err == nil {
			t.Fatalf("accepted dungeon %d", id)
		}
	}
	for _, id := range []uint32{0, 1, 2, 7, 9, 148, 0xffffffff} {
		if _, err := TranslateSetRaidDifficulty(append(pveWords(id), 0)); err == nil {
			t.Fatalf("accepted raid %d", id)
		}
	}
	if _, err := ParseLegacyDungeonDifficulty(pveWords(2, 1, 0)); err == nil {
		t.Fatal("accepted invalid legacy dungeon")
	}
	if _, err := TranslateLegacyRaidDifficulty(pveWords(4, 1, 0)); err == nil {
		t.Fatal("accepted invalid legacy raid")
	}
	if err := ValidateLegacyInstanceDifficulty(pveWords(0, 0)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLegacyInstanceDifficulty(pveWords(0, 1, 0)); err == nil {
		t.Fatal("accepted selection as map difficulty")
	}
}

func TestPVELockLifecycle(t *testing.T) {
	for _, tc := range []struct {
		input byte
		want  byte
	}{{0, 0}, {0x80, 1}} {
		got, err := TranslateInstanceLockResponse([]byte{tc.input})
		if err != nil || !bytes.Equal(got, []byte{tc.want}) {
			t.Fatalf("lock response %x %v", got, err)
		}
	}
	for _, gm := range []uint32{0, 1, 2} {
		got, err := TranslateInstanceSaveCreated(pveWords(gm))
		want := byte(0)
		if gm != 0 {
			want = 0x80
		}
		if err != nil || !bytes.Equal(got, []byte{want}) {
			t.Fatalf("save %x %v", got, err)
		}
	}
	got, err := TranslatePendingRaidLock(append(pveWords(60000, 0x12345678), 1))
	if err != nil || !bytes.Equal(got, []byte{0x60, 0xea, 0, 0, 0x78, 0x56, 0x34, 0x12, 0x80}) {
		t.Fatalf("pending %x %v", got, err)
	}
	for kind := uint32(1); kind <= 5; kind++ {
		in := pveWords(kind, 631, 2, 3600)
		flags := byte(0)
		if kind == 4 {
			in = append(in, 1, 1)
			flags = 0xc0
		}
		got, err := TranslateRaidInstanceMessage(in)
		want := []byte{byte(kind), 0x77, 2, 0, 0, 5, 0, 0, 0, flags}
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("warning %d: %x %v", kind, got, err)
		}
	}
}

func TestPVEMapDifficultyAndSavedLocks(t *testing.T) {
	for _, tc := range []struct{ mapID, legacy, want uint32 }{{574, 0, 1}, {574, 1, 2}, {533, 0, 3}, {631, 1, 4}, {631, 2, 5}, {724, 3, 6}, {409, 0, 9}, {309, 0, 148}, {532, 0, 3}, {580, 0, 4}} {
		if got := InstanceDifficultyForMap(tc.mapID, tc.legacy); got != tc.want {
			t.Fatalf("map %d mode %d = %d", tc.mapID, tc.legacy, got)
		}
		body := EncodeInstanceInfo([]InstanceLock{{MapID: tc.mapID, DifficultyID: tc.legacy}})
		if got := binary.LittleEndian.Uint32(body[8:12]); got != tc.want {
			t.Fatalf("saved lock difficulty %d want %d", got, tc.want)
		}
	}
}
