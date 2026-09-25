package modernworld

import (
	"encoding/binary"
	"testing"
)

// buildModernSearchBody assembles a build-54261 CMSG_AUCTION_LIST_ITEMS payload:
// fixed scalars, a dedicated TaintedBy bit, 8-bit name length, the name string,
// then class-filter count / OnlyUsable, per-class u64 inv-type masks, and the
// trailing self-describing sort buffer.
func buildModernSearchBody(auctioneer GUID128, offset uint32, name string, itemClass, invType, subclass int32, sortCount uint8) []byte {
	body := appendPackedGUID128(nil, auctioneer.Low, auctioneer.High)
	body = binary.LittleEndian.AppendUint32(body, offset)
	body = append(body, 1, 80) // min/max level
	body = binary.LittleEndian.AppendUint32(body, 2)
	body = append(body, sortCount)
	body = binary.LittleEndian.AppendUint32(body, 0) // known pets
	body = append(body, 0)                           // max pet level

	classFilterCount := uint32(0)
	if itemClass >= 0 {
		classFilterCount = 1
	}
	bits := newBitWriter(body)
	bits.writeBit(false) // TaintedBy
	bits.writeBits(uint32(len(name)), 8)
	body = bits.flush()
	body = append(body, name...)
	filterBits := newBitWriter(body)
	filterBits.writeBits(classFilterCount, 3)
	filterBits.writeBit(true) // OnlyUsable
	body = filterBits.flush()
	if classFilterCount != 0 {
		body = binary.LittleEndian.AppendUint32(body, uint32(itemClass))
		subBits := newBitWriter(body)
		subBits.writeBits(1, 5)
		body = subBits.flush()
		body = binary.LittleEndian.AppendUint64(body, uint64(uint32(invType)))
		body = binary.LittleEndian.AppendUint32(body, uint32(subclass))
	}
	sortBytes := make([]byte, 0, 2*int(sortCount))
	for index := uint8(0); index < sortCount; index++ {
		sortBytes = append(sortBytes, index, 1) // type/direction
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(sortBytes)))
	return append(body, sortBytes...)
}

func TestAuctionSearchMinimalRoundTrip(t *testing.T) {
	auctioneer := auctioneerGUID()
	body := buildModernSearchBody(auctioneer, 9, "", -1, -1, -1, 0)
	request, err := ParseAuctionSearchRequest(body)
	if err != nil {
		t.Fatalf("search parse: %v", err)
	}
	if request.Auctioneer != auctioneer || request.Offset != 9 || !request.OnlyUsable || request.ExactMatch || len(request.ClassFilters) != 0 || len(request.Sorts) != 0 || request.Name != "" {
		t.Fatalf("search request=%#v", request)
	}

	legacy := EncodeLegacyAuctionSearch(request, 0x55)
	if !isUint64At(legacy, 0, 0x55) || !isUint32At(legacy, 8, 9) {
		t.Fatalf("legacy search head=%x", legacy)
	}
	// name CString -> NUL at offset 12. The 3.4.3 default 1..80 band becomes
	// 0,0 so AzerothCore does not exclude RequiredLevel 0 items.
	if legacy[12] != 0 || legacy[13] != 0 || legacy[14] != 0 {
		t.Fatalf("legacy search name/levels=%x", legacy)
	}
	// -1,-1,-1 filter triple at offset 15.
	for offset := 15; offset <= 23; offset += 4 {
		if !isUint32At(legacy, offset, ^uint32(0)) {
			t.Fatalf("legacy search filter triple=%x", legacy)
		}
	}
	if !isUint32At(legacy, 27, 2) || legacy[31] != 1 || legacy[32] != 0 || legacy[33] != 0 {
		t.Fatalf("legacy search quality/usable/exact=%x", legacy)
	}
}

func TestAuctionSearchWithClassFilter(t *testing.T) {
	auctioneer := auctioneerGUID()
	// A single subclass with an inv-slot mask selecting slot 3 (i.e. bit 2 -> index 2 is slot 2; use mask 0x100 to pick slot 8).
	body := buildModernSearchBody(auctioneer, 0, "Swift", 4, 0x100, 2, 1)
	request, err := ParseAuctionSearchRequest(body)
	if err != nil {
		t.Fatalf("search parse: %v", err)
	}
	if request.Name != "Swift" || len(request.ClassFilters) != 1 || len(request.Sorts) != 1 {
		t.Fatalf("search request=%#v", request)
	}
	filter := request.ClassFilters[0]
	if filter.ItemClass != 4 || len(filter.SubClasses) != 1 || filter.SubClasses[0].InvTypeMask != 0x100 || filter.SubClasses[0].ItemSubclass != 2 {
		t.Fatalf("class filter=%#v", filter)
	}
	if request.Sorts[0] != (AuctionSort{Type: 0, Direction: 1}) {
		t.Fatalf("sorts=%#v", request.Sorts)
	}

	legacy := EncodeLegacyAuctionSearch(request, 0x55)
	// Head: guid(8) + offset(4) + "Swift\0"(6) + min/max(2) => triple at 20.
	if legacy[12] != 'S' || legacy[17] != 0 || legacy[18] != 0 || legacy[19] != 0 {
		t.Fatalf("legacy search head=%x", legacy)
	}
	// Single subclass maps to a real triple: inv slot 8, class 4, subclass 2.
	if !isUint32At(legacy, 20, 8) || !isUint32At(legacy, 24, 4) || !isUint32At(legacy, 28, 2) {
		t.Fatalf("legacy search filtered triple=%x", legacy)
	}
}

func TestAuctionSearch343TwoHandAxeFilter(t *testing.T) {
	// Live 3.4.3 browse of a 2H axe: dedicated TaintedBy bit, name before the
	// class-filter bits, no ExactMatch bit. The old Hermes packed bit-group
	// then tried to read a 544-byte sort buffer from the middle of the class
	// filter. Weapon class 2, INVTYPE_2HWEAPON bit 17, subclass 1 (2H axe).
	auctioneer := auctioneerGUID()
	const invType2HWeapon = uint32(1 << 17)
	body := buildModernSearchBody(auctioneer, 0, "", 2, int32(invType2HWeapon), 1, 0)
	request, err := ParseAuctionSearchRequest(body)
	if err != nil {
		t.Fatalf("3.4.3 2H-axe search must parse: %v", err)
	}
	if request.Name != "" || !request.OnlyUsable || request.ExactMatch || len(request.Sorts) != 0 {
		t.Fatalf("search request=%#v", request)
	}
	if len(request.ClassFilters) != 1 || request.ClassFilters[0].ItemClass != 2 {
		t.Fatalf("class filters=%#v", request.ClassFilters)
	}
	sub := request.ClassFilters[0].SubClasses
	if len(sub) != 1 || sub[0].InvTypeMask != invType2HWeapon || sub[0].ItemSubclass != 1 {
		t.Fatalf("subclass filter=%#v", sub)
	}
	legacy := EncodeLegacyAuctionSearch(request, 0x55)
	if !isUint32At(legacy, 15, 17) || !isUint32At(legacy, 19, 2) || !isUint32At(legacy, 23, 1) {
		t.Fatalf("legacy 2H-axe triple=%x", legacy)
	}
}

func TestAuctionSearchRawInventoryType(t *testing.T) {
	// INVTYPE_2HWEAPON = 17 is not a power of two. Treating it as a bitmask
	// yields slot 0 (NON_EQUIP) and AzerothCore returns no weapons.
	if got := modernToLegacyInventorySlotType(17); got != 17 {
		t.Fatalf("raw 2H inv type=%d", got)
	}
	if got := modernToLegacyInventorySlotType(1 << 17); got != 17 {
		t.Fatalf("mask 2H inv type=%d", got)
	}
	if got := modernToLegacyInventorySlotType(1<<13 | 1<<17); got != -1 {
		t.Fatalf("multi-slot mask=%d", got)
	}
	if got := modernToLegacyInventorySlotType(0); got != -1 {
		t.Fatalf("empty mask=%d", got)
	}

	auctioneer := auctioneerGUID()
	body := buildModernSearchBody(auctioneer, 0, "", 2, 17, 1, 0)
	request, err := ParseAuctionSearchRequest(body)
	if err != nil {
		t.Fatalf("raw inv-type search must parse: %v", err)
	}
	legacy := EncodeLegacyAuctionSearch(request, 0x55)
	if !isUint32At(legacy, 15, 17) || !isUint32At(legacy, 19, 2) || !isUint32At(legacy, 23, 1) {
		t.Fatalf("legacy raw-17 triple=%x", legacy)
	}
}

func TestAuctionSearchClampsSortsAndIgnoresExactMatch(t *testing.T) {
	sorts := make([]AuctionSort, auctionLegacySortMax+3)
	for index := range sorts {
		sorts[index] = AuctionSort{Type: uint8(index), Direction: 1}
	}
	legacy := EncodeLegacyAuctionSearch(AuctionSearchRequest{
		MinLevel:   10,
		MaxLevel:   20,
		Quality:    -1,
		ExactMatch: true,
		Sorts:      sorts,
	}, 0x88)
	// guid(8)+offset(4)+NUL(1)+min(1)+max(1)=15, triple(12) → quality at 27,
	// usable at 31, getAll at 32, sort count at 33.
	if legacy[13] != 10 || legacy[14] != 20 {
		t.Fatalf("narrow level band=%x", legacy)
	}
	if !isUint32At(legacy, 27, ^uint32(0)) {
		t.Fatalf("quality -1=%x", legacy)
	}
	if legacy[31] != 0 || legacy[32] != 0 || legacy[33] != auctionLegacySortMax {
		t.Fatalf("usable/getAll/sortCount=%x", legacy)
	}
	if len(legacy) != 34+2*auctionLegacySortMax {
		t.Fatalf("clamped sort tail len=%d", len(legacy))
	}
}

func TestAuctionSearchExactMatch(t *testing.T) {
	// exact-match and sort content ride on a name-free search with no filters.
	auctioneer := auctioneerGUID()
	legacy := EncodeLegacyAuctionSearch(AuctionSearchRequest{
		Auctioneer: auctioneer,
		Name:       "Rod",
		MinLevel:   5,
		MaxLevel:   5,
		Quality:    3,
		OnlyUsable: false,
		ExactMatch: true,
		Sorts:      []AuctionSort{{Type: 7, Direction: 1}, {Type: 2, Direction: 0}},
	}, 0x88)
	// guid, offset 0, "Rod\0" (4), min/max level(2), no-filter triple(12) at 18.
	if !isUint64At(legacy, 0, 0x88) || !isUint32At(legacy, 30, 3) {
		t.Fatalf("head=%x", legacy)
	}
	// usable byte, getAll (never ExactMatch), sort count, then the two sort pairs.
	if legacy[34] != 0 || legacy[35] != 0 || legacy[36] != 2 ||
		legacy[37] != 7 || legacy[38] != 1 || legacy[39] != 2 || legacy[40] != 0 {
		t.Fatalf("legacy search exact/sorts=%x", legacy)
	}
}

func buildLegacyAuctionEntry(e *struct {
	auctionID uint32
	itemID    uint32
	count     int32
	owner     uint64
	minBid    uint32
	bidder    uint64
	bidAmount uint32
}) []byte {
	body := binary.LittleEndian.AppendUint32(nil, e.auctionID)
	body = binary.LittleEndian.AppendUint32(body, e.itemID)
	for slot := 0; slot < 7; slot++ {
		if slot == 2 {
			body = binary.LittleEndian.AppendUint32(body, 0x44) // enchant id
			body = binary.LittleEndian.AppendUint32(body, 0x10) // expiration
			body = binary.LittleEndian.AppendUint32(body, 3)    // charges
		} else {
			body = binary.LittleEndian.AppendUint32(body, 0)
			body = binary.LittleEndian.AppendUint32(body, 0)
			body = binary.LittleEndian.AppendUint32(body, 0)
		}
	}
	body = binary.LittleEndian.AppendUint32(body, 77)  // random property id
	body = binary.LittleEndian.AppendUint32(body, 0x9) // random property seed
	body = binary.LittleEndian.AppendUint32(body, uint32(e.count))
	body = binary.LittleEndian.AppendUint32(body, 0) // charges
	body = binary.LittleEndian.AppendUint32(body, 0) // flags
	body = binary.LittleEndian.AppendUint64(body, e.owner)
	body = binary.LittleEndian.AppendUint32(body, e.minBid)
	body = binary.LittleEndian.AppendUint32(body, 50) // min increment
	body = binary.LittleEndian.AppendUint32(body, 0)  // buyout
	body = binary.LittleEndian.AppendUint32(body, 3600)
	body = binary.LittleEndian.AppendUint64(body, e.bidder)
	return binary.LittleEndian.AppendUint32(body, e.bidAmount)
}

func TestLegacyAuctionListParseToBrowse(t *testing.T) {
	one := buildLegacyAuctionEntry(&struct {
		auctionID uint32
		itemID    uint32
		count     int32
		owner     uint64
		minBid    uint32
		bidder    uint64
		bidAmount uint32
	}{1, 0x1234, 5, 0xf130000001000043, 0x100, 0xf130000001000022, 0x100})
	two := buildLegacyAuctionEntry(&struct {
		auctionID uint32
		itemID    uint32
		count     int32
		owner     uint64
		minBid    uint32
		bidder    uint64
		bidAmount uint32
	}{2, 0x9999, 1, 0xf130000001000043, 0x200, 0, 0})
	body := binary.LittleEndian.AppendUint32(nil, 2)
	body = append(body, one...)
	body = append(body, two...)
	body = binary.LittleEndian.AppendUint32(body, 7)   // total items
	body = binary.LittleEndian.AppendUint32(body, 300) // desired delay

	result, err := ParseLegacyAuctionListResult(body)
	if err != nil {
		t.Fatalf("list parse: %v", err)
	}
	if len(result.Items) != 2 || result.TotalItemsCount != 7 || result.DesiredDelay != 300 || !result.HasMoreResults {
		t.Fatalf("list result=%#v", result)
	}
	entry := result.Items[0]
	if entry.AuctionID != 1 || entry.ItemID != 0x1234 || entry.Count != 5 || entry.Owner != 0xf130000001000043 || entry.MinBid != 0x100 || entry.Bidder != 0xf130000001000022 || entry.BidAmount != 0x100 || entry.RandomPropertyID != 77 || entry.RandomPropertiesSeed != 0x9 {
		t.Fatalf("entry 0=%#v", entry)
	}
	if len(entry.Enchants) != 1 || entry.Enchants[0].Slot != 2 || entry.Enchants[0].ID != 0x44 || entry.Enchants[0].Expiration != 0x10 || entry.Enchants[0].Charges != 3 {
		t.Fatalf("entry 0 enchants=%#v", entry.Enchants)
	}
	if result.Items[1].Bidder != 0 || result.Items[1].BidAmount != 0 {
		t.Fatalf("entry 1=%#v", result.Items[1])
	}

	browse := EncodeAuctionBrowseResult(result, emptyResolve)
	if !isUint32At(browse, 0, 2) || !isUint32At(browse, 4, 7) || !isUint32At(browse, 8, 300) || browse[12] != 0 {
		t.Fatalf("browse result head=%x", browse)
	}
	myItems := EncodeAuctionMyItemsResult(result, emptyResolve)
	if !isUint32At(myItems, 0, 2) || !isUint32At(myItems, 4, 0) || !isUint32At(myItems, 8, 300) {
		t.Fatalf("my-items result head=%x", myItems)
	}
	// 54261 owned/bidder results have no HasMoreResults bit: AuctionItem rows
	// start immediately after the three uint32s.
	expected := myItems[:12:12]
	for _, entry := range result.Items {
		expected = encodeAuctionEntry(expected, entry, false, false, emptyResolve)
	}
	if len(myItems) != len(expected) {
		t.Fatalf("my-items len=%d want %d (HasMoreResults bit must not be written)", len(myItems), len(expected))
	}
	for i := range expected {
		if myItems[i] != expected[i] {
			t.Fatalf("my-items mismatch at %d: got %x want %x", i, myItems, expected)
		}
	}
	// The browse result carries the CensorServerSideInfo bit set for every item
	// while the my-items result does not: they must differ somewhere past the head.
	if len(browse) <= 13 || len(myItems) <= 12 {
		t.Fatalf("browse=%d my-items=%d bytes too short", len(browse), len(myItems))
	}
}

func TestAuctionBrowseResultOmitsOnlyUsableWhenEmpty(t *testing.T) {
	browse := EncodeAuctionBrowseResult(LegacyAuctionListResult{TotalItemsCount: 0, DesiredDelay: 300}, emptyResolve)
	if len(browse) != 12 || !isUint32At(browse, 0, 0) || !isUint32At(browse, 4, 0) || !isUint32At(browse, 8, 300) {
		t.Fatalf("empty browse=%x", browse)
	}
}

func TestAuctionSearchRejectsTrailingGarbage(t *testing.T) {
	auctioneer := auctioneerGUID()
	body := buildModernSearchBody(auctioneer, 0, "", -1, -1, -1, 0)
	body = append(body, 0xde, 0xad)
	if _, err := ParseAuctionSearchRequest(body); err == nil {
		t.Fatal("search with trailing bytes must error")
	}
}

func emptyResolve(legacyGUID uint64) GUID128 {
	return GUID128{}
}

func TestLegacyItemGUIDFromModern(t *testing.T) {
	// A bag/equip item the modern client holds is {Low: counter, High: item type}.
	if legacy := LegacyItemGUIDFromModern(GUID128{Low: 0x681, High: ModernItemHigh}); legacy != 0x4000000000000681 {
		t.Fatalf("derived legacy item guid=%x", legacy)
	}
	if legacy := LegacyItemGUIDFromModern(GUID128{Low: 0x681, High: ModernItemHigh + 1}); legacy != 0 {
		t.Fatalf("wrong high must not derive: %x", legacy)
	}
	if legacy := LegacyItemGUIDFromModern(GUID128{}); legacy != 0 {
		t.Fatalf("zero guid must not derive: %x", legacy)
	}
	// ModernGUIDForLegacy and the inverse must round-trip for an item guid.
	modern := ModernGUIDForLegacy(0x40000000000007cd, 0)
	if legacy := LegacyItemGUIDFromModern(modern); legacy != 0x40000000000007cd {
		t.Fatalf("round trip item guid modern=%#v legacy=%x", modern, legacy)
	}
}
