package modernworld

import (
	"encoding/binary"
	"reflect"
	"testing"
	"time"
)

func TestRuneResyncTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, runeCount)
	legacy = append(legacy,
		0, 0xff,
		0, 0xff,
		2, 0x40,
		2, 0xff,
		1, 0,
		1, 0xff,
	)
	state, err := ParseLegacyRuneResync(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if state.Available != 0x3f {
		t.Fatalf("available mask = %#x, want 0x3f", state.Available)
	}
	want := []byte{0x3f, 0x3f, 6, 0, 0, 0, 0xff, 0xff, 0x40, 0xff, 0, 0xff}
	if got := EncodeRuneResync(state); !reflect.DeepEqual(got, want) {
		t.Fatalf("rune resync = %x, want %x", got, want)
	}
	if _, err := ParseLegacyRuneResync(append(legacy, 0)); err == nil {
		t.Fatal("rune resync with trailing data was accepted")
	}
}

func TestRuneConversionTranslation(t *testing.T) {
	slot, runeType, err := ParseLegacyRuneConversion([]byte{4, 0})
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, 14)
	want[6], want[10] = 4, 1
	if got := EncodeRuneConversion(slot, runeType); !reflect.DeepEqual(got, want) {
		t.Fatalf("base rune conversion = %x, want %x", got, want)
	}
	want[10] = 3
	if got := EncodeRuneConversion(4, 3); !reflect.DeepEqual(got, want) {
		t.Fatalf("death rune conversion = %x, want %x", got, want)
	}
	if _, _, err := ParseLegacyRuneConversion([]byte{6, 0}); err == nil {
		t.Fatal("out-of-range rune slot was accepted")
	}
}

func TestAddRunePowerTranslation(t *testing.T) {
	power, err := ParseLegacyAddRunePower([]byte{0x78, 0x56, 0x34, 0x12})
	if err != nil {
		t.Fatal(err)
	}
	if got := EncodeAddRunePower(power); !reflect.DeepEqual(got, []byte{0x78, 0x56, 0x34, 0x12}) {
		t.Fatalf("add rune power = %x", got)
	}
	if _, err := ParseLegacyAddRunePower(nil); err == nil {
		t.Fatal("empty add-rune-power packet was accepted")
	}
}

func TestRuneDataEncoding(t *testing.T) {
	got := EncodeRuneData(RuneData{Start: 0x3f, Count: 0x3c, Cooldowns: []byte{0xff, 0xff, 0, 0x40, 0xff, 0xff}})
	want := []byte{0x3f, 0x3c, 6, 0, 0, 0, 0xff, 0xff, 0, 0x40, 0xff, 0xff}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rune data = %x, want %x", got, want)
	}
}

func TestAdvanceRuneCooldowns(t *testing.T) {
	now := time.Unix(100, 0)
	var started [runeCount]time.Time
	first := &RuneData{Start: 0x3f, Count: 0x3e, Cooldowns: []byte{0, 0xff, 0xff, 0xff, 0xff, 0xff}}
	AdvanceRuneCooldowns(first, &started, now)
	if first.Cooldowns[0] != 1 {
		t.Fatalf("just-spent cooldown = %#x, want 1 (legacy proxy rewrites 0 to 1)", first.Cooldowns[0])
	}
	if started[0].IsZero() {
		t.Fatal("newly consumed rune did not start a cooldown")
	}

	second := &RuneData{Start: 0x3e, Count: 0x3e, Cooldowns: make([]byte, runeCount)}
	AdvanceRuneCooldowns(second, &started, now.Add(2*time.Second))
	if second.Cooldowns[0] <= first.Cooldowns[0] || second.Cooldowns[0] == 0xff {
		t.Fatalf("carried cooldown progress = %#x, previous %#x", second.Cooldowns[0], first.Cooldowns[0])
	}
	if second.Cooldowns[1] != 0xff {
		t.Fatalf("ready rune progress = %#x, want 0xff", second.Cooldowns[1])
	}
}

func TestRuneRemainingPower(t *testing.T) {
	// legacy proxy pkt-000779: Count=0x2f (slots 0,1,2,3,5) → blood=2, frost=1, unholy=2.
	got := RuneRemainingPower(0x2f)
	want := []SpellPowerData{
		{Cost: 2, Type: powerRuneBlood},
		{Cost: 1, Type: powerRuneFrost},
		{Cost: 2, Type: powerRuneUnholy},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remaining power = %+v, want %+v", got, want)
	}
	encoded := append(append(append([]byte{}, EncodeSpellPowerData(got[0])...), EncodeSpellPowerData(got[1])...), EncodeSpellPowerData(got[2])...)
	if !reflect.DeepEqual(encoded, []byte{2, 0, 0, 0, 0x15, 1, 0, 0, 0, 0x16, 2, 0, 0, 0, 0x17}) {
		t.Fatalf("remaining power bytes = %x", encoded)
	}
}

func TestRuneStateForCreate(t *testing.T) {
	if got := RuneStateForCreate(1, nil); got != nil {
		t.Fatal("non-DK without cache must omit the rune block")
	}
	cached := LegacyRuneState{Available: 0x15, Types: [runeCount]byte{3, 3, 3, 3, 3, 3}}
	if got := RuneStateForCreate(6, &cached); got == nil || *got != cached {
		t.Fatalf("cached rune state = %#v", got)
	}
	got := RuneStateForCreate(6, nil)
	want := DefaultDeathKnightRuneState()
	if got == nil || *got != want {
		t.Fatalf("default DK runes = %#v want %#v", got, want)
	}
	if encoded := EncodeRuneResync(want); !reflect.DeepEqual(encoded, []byte{0x3f, 0x3f, 6, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("default DK resync = %x", encoded)
	}
}
