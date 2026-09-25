package modernworld

import (
	"encoding/binary"
	"sort"
)

const (
	// SMSG_SETUP_CURRENCY is build 54261 opcode 9587. The 3.4.3 currency
	// frame reads this packet; it does not read PLAYER_FIELD_CURRENCYTOKEN.
	SMSGSetupCurrency = uint16(9587)

	// Classic currency ids from build 54261 CurrencyTypes. Honor and arena
	// are the scalar player fields. Emblems and marks are item entries.
	CurrencyArena = uint32(1900)
	CurrencyHonor = uint32(1901)
)

// currencyByItem maps a 3.3.5 currency-token item entry to the 3.4.3
// CurrencyTypes id. Honor and arena points are not items; they use
// CurrencyHonor and CurrencyArena.
var currencyByItem = map[uint32]uint32{
	29434: 42,  // Badge of Justice
	41596: 61,  // Dalaran Jewelcrafter's Token
	43016: 81,  // Dalaran Cooking Award
	40752: 101, // Emblem of Heroism
	40753: 102, // Emblem of Valor
	20560: 121, // Alterac Valley Mark of Honor
	20559: 122, // Arathi Basin Mark of Honor
	29024: 123, // Eye of the Storm Mark of Honor
	42425: 124, // Strand of the Ancients Mark of Honor
	20558: 125, // Warsong Gulch Mark of Honor
	43589: 126, // Wintergrasp Mark of Honor
	43228: 161, // Stone Keeper's Shard
	37836: 201, // Venture Coin
	45624: 221, // Emblem of Conquest
	44990: 241, // Champion's Seal
	47241: 301, // Emblem of Triumph
	47395: 321, // Isle of Conquest Mark of Honor
	49426: 341, // Emblem of Frost
}

// CurrencyIDForItem reports the 3.4.3 currency id for a legacy currency-token
// item. Other items, including the deprecated honor and arena item displays,
// are not currencies.
func CurrencyIDForItem(entry uint32) (uint32, bool) {
	id, ok := currencyByItem[entry]
	return id, ok
}

// CurrencyRecord is one absolute quantity in SMSG_SETUP_CURRENCY.
type CurrencyRecord struct {
	Type     uint32
	Quantity uint32
}

// CollectCurrencies sums currency-token item stacks and copies honor and
// arena when those player fields are present. A present field of 0 is kept
// so a later spend can be told apart from a field the server has not sent.
// owner, when non-zero, drops item stacks that belong to someone else.
func CollectCurrencies(owner uint64, playerFields map[int]uint32, items []map[int]uint32) map[uint32]uint32 {
	next := make(map[uint32]uint32)
	for _, fields := range items {
		if owner != 0 && !legacyItemOwnedBy(fields, owner) {
			continue
		}
		entry := fields[legacyObjectEntry]
		id, ok := CurrencyIDForItem(entry)
		if !ok {
			continue
		}
		count := uint32(1)
		if stack, present := fields[legacyItemStackCount]; present {
			count = stack
		}
		if count == 0 {
			continue
		}
		next[id] += count
	}
	if qty, ok := playerFields[legacyPlayerHonorCurrency]; ok {
		next[CurrencyHonor] = qty
	}
	if qty, ok := playerFields[legacyPlayerArenaCurrency]; ok {
		next[CurrencyArena] = qty
	}
	return next
}

func legacyItemOwnedBy(fields map[int]uint32, owner uint64) bool {
	if fields == nil {
		return false
	}
	low, lowOK := fields[legacyItemOwner]
	high, highOK := fields[legacyItemOwner+1]
	if !lowOK && !highOK {
		return true
	}
	guid := uint64(low) | uint64(high)<<32
	return guid == 0 || guid == owner
}

// DiffCurrencies returns records whose absolute quantity changed. A currency
// that disappears is sent as quantity 0. A currency the client has never
// been told about is not introduced as 0.
func DiffCurrencies(prev, next map[uint32]uint32) []CurrencyRecord {
	seen := make(map[uint32]struct{}, len(next))
	records := make([]CurrencyRecord, 0)
	for id, qty := range next {
		seen[id] = struct{}{}
		old, had := prev[id]
		if had && old == qty {
			continue
		}
		if !had && qty == 0 {
			continue
		}
		records = append(records, CurrencyRecord{Type: id, Quantity: qty})
	}
	for id := range prev {
		if _, still := seen[id]; still {
			continue
		}
		records = append(records, CurrencyRecord{Type: id, Quantity: 0})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Type < records[j].Type })
	return records
}

// EncodeSetupCurrency writes the build-54261 SMSG_SETUP_CURRENCY body.
// Optional weekly, cap, and tracked fields stay absent. Flags are 0.
func EncodeSetupCurrency(records []CurrencyRecord) []byte {
	if len(records) == 0 {
		return nil
	}
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(records)))
	for _, record := range records {
		body = binary.LittleEndian.AppendUint32(body, record.Type)
		body = binary.LittleEndian.AppendUint32(body, record.Quantity)
		bits := newBitWriter(body)
		for range 5 {
			bits.writeBit(false)
		}
		bits.writeBits(0, 5)
		body = bits.flush()
	}
	return body
}
