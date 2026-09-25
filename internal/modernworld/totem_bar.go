package modernworld

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"io"
	"strconv"
)

//go:embed totem_spells.csv
var totemSpellsCSV []byte

const (
	// WotLK stores Call of the Elements / Ancestors / Spirits on buttons 132-143.
	// 3.4.3 MultiCastActionBarFrame still reads those 12 slots (GetMultiCastBarOffset=6).
	multiCastActionFirst = 132
	multiCastPageCount   = 3
	multiCastButtonsPage = 4
	multiCastActionCount = multiCastPageCount * multiCastButtonsPage
)

var totemSpellElement = loadTotemSpellElements(totemSpellsCSV)

func loadTotemSpellElements(data []byte) map[uint32]uint8 {
	reader := csv.NewReader(bytes.NewReader(data))
	_, _ = reader.Read()
	slots := make(map[uint32]uint8, 160)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) < 2 {
			continue
		}
		spellID, spellErr := strconv.ParseUint(record[0], 10, 32)
		element, elementErr := strconv.ParseUint(record[1], 10, 8)
		if spellErr != nil || elementErr != nil || spellID == 0 || element > 3 {
			continue
		}
		slots[uint32(spellID)] = uint8(element)
	}
	return slots
}

func totemElementForSpell(spellID uint32) (uint8, bool) {
	element, ok := totemSpellElement[spellID]
	return element, ok
}

// FillMultiCastTotemButtons writes one known totem per element into empty
// Call of the Elements slots (132-135). 3.4.3 MultiCastActionBarFrame only
// unhides when GetMultiCastTotemSpells sees those multicast action IDs.
func FillMultiCastTotemButtons(buttons []int32, known []uint32) []int32 {
	filled := make([]int32, legacyActionButtonCount)
	copy(filled, buttons)
	chosen := chosenTotemByElement(known)
	for element, spellID := range chosen {
		if spellID == 0 {
			continue
		}
		slot := multiCastActionFirst + int(element)
		if slot >= len(filled) || filled[slot] != 0 {
			continue
		}
		filled[slot] = int32(spellID)
	}
	return filled
}

func chosenTotemByElement(known []uint32) [4]uint32 {
	var chosen [4]uint32
	for _, spellID := range known {
		element, ok := totemElementForSpell(spellID)
		if !ok {
			continue
		}
		if spellID > chosen[element] {
			chosen[element] = spellID
		}
	}
	return chosen
}

func MultiCastActionPreview(buttons []int32) string {
	if len(buttons) <= multiCastActionFirst {
		return ""
	}
	n := multiCastActionCount
	if multiCastActionFirst+n > len(buttons) {
		n = len(buttons) - multiCastActionFirst
	}
	return ActionButtonSlotPreview(buttons[multiCastActionFirst:multiCastActionFirst+n], n)
}
