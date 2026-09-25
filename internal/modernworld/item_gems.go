package modernworld

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/csv"
	"io"
	"strconv"
)

//go:embed item_gems_3.csv
var itemGemsCSV []byte

// WotLK ITEM_FIELD_ENCHANTMENT sock slots 2-4 become ItemData.Gems[i] on 3.4.3.
// Hermes 3.4.3 skips this dynamic field; legacy proxy writes it, and the 3.4.3 tooltip
// reads gem item IDs from Gems rather than Enchantment[2-4].
const (
	legacyItemEnchantSock1 = 2
	itemSocketGemCount     = 3
	itemDataGemsBit        = 2
	socketedGemBonusCount  = 16
)

var gemItemByEnchant = loadGemItems(itemGemsCSV)

func loadGemItems(data []byte) map[uint32]uint32 {
	reader := csv.NewReader(bytes.NewReader(data))
	_, _ = reader.Read()
	gems := make(map[uint32]uint32, 640)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) < 2 {
			continue
		}
		enchantID, enchantErr := strconv.ParseUint(record[0], 10, 32)
		itemID, itemErr := strconv.ParseUint(record[1], 10, 32)
		if enchantErr == nil && itemErr == nil && enchantID != 0 {
			gems[uint32(enchantID)] = uint32(itemID)
		}
	}
	return gems
}

func gemItemIDFromEnchant(enchantID uint32) uint32 {
	if enchantID == 0 {
		return 0
	}
	return gemItemByEnchant[enchantID]
}

func itemSocketEnchantIDs(fields map[int]uint32) [3]uint32 {
	values := legacyActivePlayerValues{fields: fields}
	var ids [3]uint32
	for slot := 0; slot < itemSocketGemCount; slot++ {
		ids[slot] = values.field(itemSocketEnchantField(slot))
	}
	return ids
}

// ItemSocketEnchantIDs returns the three sock-slot SpellItemEnchantment IDs
// from a legacy item create/values map (WotLK slots 2-4).
func ItemSocketEnchantIDs(fields map[int]uint32) (uint32, uint32, uint32) {
	ids := itemSocketEnchantIDs(fields)
	return ids[0], ids[1], ids[2]
}

// ItemSocketGemItemIDs maps those sock enchantments through Gems3.csv.
func ItemSocketGemItemIDs(fields map[int]uint32) []uint32 {
	return itemSocketedGems(legacyActivePlayerValues{fields: fields})
}

func itemSocketEnchantField(slot int) int {
	return legacyItemEnchantment + (legacyItemEnchantSock1+slot)*3
}

func itemSocketGemsChanged(changed map[int]uint32) bool {
	for slot := 0; slot < itemSocketGemCount; slot++ {
		if _, ok := changed[itemSocketEnchantField(slot)]; ok {
			return true
		}
	}
	return false
}

// itemSocketedGems returns the 3.4.3 Gems dynamic-field entries indexed by
// socket slot. Trailing empty sockets are omitted so size matches legacy proxy's
// CREATE count; holes before the last filled socket stay as ItemID 0.
func itemSocketedGems(values legacyActivePlayerValues) []uint32 {
	ids := make([]uint32, itemSocketGemCount)
	last := -1
	for slot := 0; slot < itemSocketGemCount; slot++ {
		ids[slot] = gemItemIDFromEnchant(values.field(itemSocketEnchantField(slot)))
		if ids[slot] != 0 {
			last = slot
		}
	}
	if last < 0 {
		return nil
	}
	return ids[:last+1]
}

func appendSocketedGemsCreate(dst []byte, gems []uint32) []byte {
	for _, itemID := range gems {
		dst = binary.LittleEndian.AppendUint32(dst, itemID)
		dst = append(dst, make([]byte, socketedGemBonusCount*2)...)
		dst = append(dst, 0) // Context
	}
	return dst
}

func encodeSocketedGemUpdate(itemID uint32) []byte {
	// SocketedGem HasChangesMask<20>: 1-bit blocksMask + 32-bit block with
	// parent bit 0 and ItemID bit 1, then the int32 ItemID. legacy proxy's
	// WriteUpdateGem never writes Context or BonusListIDs for 3.3.5 gems.
	bits := newBitWriter(nil)
	bits.writeBits(1, 1)
	bits.writeBits(3, 32)
	return binary.LittleEndian.AppendUint32(bits.flush(), itemID)
}

func encodeSocketedGemsUpdate(gems []uint32) []byte {
	payload := make([]byte, 0, len(gems)*9)
	for _, itemID := range gems {
		payload = append(payload, encodeSocketedGemUpdate(itemID)...)
	}
	return payload
}

func writeItemGemsUpdateMask(bits *bitWriter, gems []uint32) {
	bits.writeBits(uint32(len(gems)), 32)
	for range gems {
		bits.writeBit(true)
	}
}
