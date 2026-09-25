package modernworld

import "testing"

func TestFillMultiCastTotemButtons(t *testing.T) {
	buttons := make([]int32, legacyActionButtonCount)
	buttons[0] = 0x12034567
	filled := FillMultiCastTotemButtons(buttons, []uint32{
		133,   // Fireball: not a totem
		8071,  // Stoneskin (earth)
		3599,  // Searing (fire)
		5394,  // Healing Stream (water)
		8177,  // Grounding (air)
		58580, // Stoneclaw rank 9 (earth, higher id than 8071)
	})
	if filled[0] != 0x12034567 {
		t.Fatalf("main bar slot 0 changed: %d", filled[0])
	}
	if filled[132] != 3599 || filled[133] != 58580 || filled[134] != 5394 || filled[135] != 8177 {
		t.Fatalf("multicast slots=%d,%d,%d,%d", filled[132], filled[133], filled[134], filled[135])
	}
	if preview := MultiCastActionPreview(filled); preview == "" {
		t.Fatal("expected multicast preview")
	}

	buttons[132] = 8190
	preserved := FillMultiCastTotemButtons(buttons, []uint32{3599, 8071})
	if preserved[132] != 8190 {
		t.Fatalf("existing fire multicast slot was overwritten: %d", preserved[132])
	}
	if preserved[133] != 8071 {
		t.Fatalf("empty earth multicast slot was not filled: %d", preserved[133])
	}
}

func TestStoneclawIsEarthTotem(t *testing.T) {
	element, ok := totemElementForSpell(58580)
	if !ok || element != 1 {
		t.Fatalf("58580 element=%d ok=%v, want earth (1)", element, ok)
	}
}
