package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestLevelUpInfoTranslation(t *testing.T) {
	legacy := make([]byte, 0, 56)
	for _, value := range []int32{11, 20, 1, 2, 3, 4, 5, 6, 7, 10, 11, 12, 13, 14} {
		legacy = binary.LittleEndian.AppendUint32(legacy, uint32(value))
	}
	info, err := ParseLegacyLevelUpInfo(legacy)
	if err != nil || info.Level != 11 || info.PowerDelta[6] != 7 || info.PowerDelta[7] != 0 || info.StatDelta[4] != 14 {
		t.Fatalf("info=%#v err=%v", info, err)
	}
	SetLevelUpTalentDelta(&info, 10, 1)
	modern := EncodeLevelUpInfo(info)
	if len(modern) != 76 {
		t.Fatalf("modern level-up size=%d, want 76", len(modern))
	}
	if got := binary.LittleEndian.Uint32(modern[8+7*4:]); got != 0 {
		t.Fatalf("padded power delta=%d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(modern[8+10*4+4*4:]); got != 14 {
		t.Fatalf("fifth stat delta=%d, want 14", got)
	}
	if got := binary.LittleEndian.Uint32(modern[68:]); got != 1 {
		t.Fatalf("new-talent count=%d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(modern[72:]); got != 0 {
		t.Fatalf("new-pvp-talent-slot count=%d, want 0", got)
	}
}

func TestSetLevelUpTalentDelta(t *testing.T) {
	tests := []struct {
		name          string
		level         int32
		previousLevel int32
		class         byte
		want          int32
	}{
		{name: "before talents", level: 9, previousLevel: 8, class: 1, want: 0},
		{name: "first normal talent", level: 10, previousLevel: 9, class: 1, want: 1},
		{name: "skipped normal levels", level: 12, previousLevel: 9, class: 1, want: 3},
		{name: "death knight level 55", level: 55, previousLevel: 54, class: 6, want: 0},
		{name: "first death knight talent", level: 56, previousLevel: 55, class: 6, want: 1},
		{name: "level decrease", level: 10, previousLevel: 12, class: 1, want: -2},
		{name: "missing previous level", level: 20, previousLevel: 0, class: 1, want: 1},
		{name: "custom level capped to client table", level: 81, previousLevel: 80, class: 1, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := LevelUpInfo{Level: test.level, NumNewPvpTalentSlots: 99}
			SetLevelUpTalentDelta(&info, test.previousLevel, test.class)
			if info.NumNewTalents != test.want || info.NumNewPvpTalentSlots != 0 {
				t.Fatalf("talents=%d pvp-slots=%d, want %d and 0", info.NumNewTalents, info.NumNewPvpTalentSlots, test.want)
			}
		})
	}
}
