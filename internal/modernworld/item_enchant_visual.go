package modernworld

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"io"
	"strconv"
)

// Visual IDs sent by AzerothCore's character enum, exported from field 31 of
// SpellItemEnchantment.dbc. See docs/channel-enchant-investigation.md and
// tools/export-enchant-visuals.py; the older Hermes CSV omitted Wrath enchants.
//
//go:embed item_enchant_visuals_3.csv
var itemEnchantVisualsCSV []byte

var itemEnchantVisuals = loadItemEnchantVisuals(itemEnchantVisualsCSV)

func loadItemEnchantVisuals(data []byte) map[uint32]uint16 {
	reader := csv.NewReader(bytes.NewReader(data))
	_, _ = reader.Read() // header
	visuals := make(map[uint32]uint16, 192)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) != 2 {
			continue
		}
		enchantID, idErr := strconv.ParseUint(record[0], 10, 32)
		visualID, visualErr := strconv.ParseUint(record[1], 10, 16)
		if idErr == nil && visualErr == nil {
			visuals[uint32(enchantID)] = uint16(visualID)
		}
	}
	return visuals
}

func itemEnchantVisual(enchantID uint32) uint16 {
	return itemEnchantVisuals[enchantID]
}

// AzerothCore PlayerStorage.cpp stores permanent and temporary enchant IDs in
// the low and high uint16 of PLAYER_VISIBLE_ITEM_*_ENCHANTMENT. Use the same
// permanent-first selection as Player::BuildEnumData; the whole uint32 is not
// an enchant ID. A nonzero permanent enchant without a visual stays invisible.
func visibleItemEnchantVisual(packed uint32) uint16 {
	enchantID := packed & 0xffff
	if enchantID == 0 {
		enchantID = packed >> 16
	}
	return itemEnchantVisual(enchantID)
}
