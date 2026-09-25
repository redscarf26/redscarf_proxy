package modernworld

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

// Auction browse/owned/bidder list translation. The modern client searches the
// auction house with CMSG_AUCTION_LIST_ITEMS. Build 54261 writes a dedicated
// TaintedBy bit, the name string, then class-filter bits (HermesProxy's older
// packed name-length/filters/usable/exact-match group misaligns on this build).
// The legacy 3.3.5a CMSG_AUCTION_LIST_ITEMS wants the single-category tail
// HermesProxy WorldSocket.HandleAuctionListItems writes. Results come back as
// legacy SMSG_AUCTION_*_RESULT and are re-emitted with HermesProxy
// {AuctionListItemsResult,AuctionListMyItemsResult,AuctionItem,ItemInstance}.

// AuctionSort is one modern browse sort column (type + direction).
type AuctionSort struct {
	Type      uint8
	Direction uint8
}

// AuctionSubClassFilter is one subclass within a modern class filter; the
// inventory-slot constraint is a 32-bit mask of slot types.
type AuctionSubClassFilter struct {
	InvTypeMask  uint32
	ItemSubclass int32
}

// AuctionClassFilter is one modern item-class filter.
type AuctionClassFilter struct {
	ItemClass  int32
	SubClasses []AuctionSubClassFilter
}

// AuctionSearchRequest is the parsed modern CMSG_AUCTION_LIST_ITEMS payload.
type AuctionSearchRequest struct {
	Auctioneer   GUID128
	Offset       uint32
	MinLevel     uint8
	MaxLevel     uint8
	Quality      int32
	Name         string
	OnlyUsable   bool
	ExactMatch   bool
	ClassFilters []AuctionClassFilter
	Sorts        []AuctionSort
}

const (
	auctionMaxClassFilters    = 8
	auctionMaxSubClassFilters = 32
	auctionMaxSorts           = 64
	auctionMaxKnownPets       = 256
	// AzerothCore AUCTION_SORT_MAX: HandleAuctionListItems returns without a
	// result packet when the sort count is greater than 11.
	auctionLegacySortMax = 11
	// INVTYPE_RELIC is the last 3.3.5a inventory type. Values above this are
	// bitmasks, not a raw InventoryType enum packed into the low bits.
	auctionMaxInventoryType = 28
)

// ParseAuctionSearchRequest reads build 54261 CMSG_AUCTION_LIST_ITEMS. The
// client always writes a dedicated TaintedBy bit, then the 8-bit name length,
// then the name string (which aligns), and only after that the 3-bit
// class-filter count and OnlyUsable. HermesProxy AuctionListItems.Read packed
// name length / filters / usable / exact-match into one bit group and peeked
// TaintedBy from the name-length MSB; that reads the sort-buffer size from the
// middle of a class filter on this build.
func ParseAuctionSearchRequest(body []byte) (AuctionSearchRequest, error) {
	var request AuctionSearchRequest
	r := movementReader{data: body}
	var err error
	if request.Auctioneer, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read search auctioneer: %w", err)
	}
	if request.Offset, err = r.u32(); err != nil {
		return request, fmt.Errorf("read search offset: %w", err)
	}
	if request.MinLevel, err = r.u8(); err != nil {
		return request, fmt.Errorf("read search min level: %w", err)
	}
	if request.MaxLevel, err = r.u8(); err != nil {
		return request, fmt.Errorf("read search max level: %w", err)
	}
	if request.Quality, err = r.i32(); err != nil {
		return request, fmt.Errorf("read search quality: %w", err)
	}
	sortCount, err := r.u8()
	if err != nil {
		return request, fmt.Errorf("read search sort count: %w", err)
	}
	if sortCount > auctionMaxSorts {
		return request, fmt.Errorf("search has %d sorts, maximum is %d", sortCount, auctionMaxSorts)
	}
	knownPetsCount, err := r.u32()
	if err != nil {
		return request, fmt.Errorf("read search known-pet count: %w", err)
	}
	if knownPetsCount > auctionMaxKnownPets {
		return request, fmt.Errorf("search has %d known pets, maximum is %d", knownPetsCount, auctionMaxKnownPets)
	}
	if _, err = r.u8(); err != nil { // MaxPetLevel, dropped (no legacy equivalent)
		return request, fmt.Errorf("read search max pet level: %w", err)
	}
	for index := uint32(0); index < knownPetsCount; index++ {
		if _, err = r.u8(); err != nil { // KnownPets, dropped
			return request, fmt.Errorf("read search known pet %d: %w", index, err)
		}
	}
	tainted, err := r.bit()
	if err != nil {
		return request, fmt.Errorf("read search tainted bit: %w", err)
	}
	nameLength, readErr := r.bits(8)
	if readErr != nil {
		return request, fmt.Errorf("read search name length: %w", readErr)
	}
	if request.Name, err = r.stringN(int(nameLength)); err != nil {
		return request, fmt.Errorf("read search name: %w", err)
	}
	classFiltersCount, readErr := r.bits(3)
	if readErr != nil {
		return request, fmt.Errorf("read search class-filter count: %w", readErr)
	}
	if classFiltersCount > auctionMaxClassFilters {
		return request, fmt.Errorf("search has %d class filters, maximum is %d", classFiltersCount, auctionMaxClassFilters)
	}
	if request.OnlyUsable, err = r.bit(); err != nil {
		return request, fmt.Errorf("read search usable-only: %w", err)
	}
	if tainted {
		if err := skipAddOnInfo(&r); err != nil {
			return request, err
		}
	}
	request.ClassFilters = make([]AuctionClassFilter, 0, classFiltersCount)
	for index := uint32(0); index < classFiltersCount; index++ {
		var filter AuctionClassFilter
		if filter.ItemClass, err = r.i32(); err != nil {
			return request, fmt.Errorf("read search filter %d item class: %w", index, err)
		}
		subCount, readErr := r.bits(5)
		if readErr != nil {
			return request, fmt.Errorf("read search filter %d subclass count: %w", index, readErr)
		}
		if subCount > auctionMaxSubClassFilters {
			return request, fmt.Errorf("search filter %d has %d subclasses, maximum is %d", index, subCount, auctionMaxSubClassFilters)
		}
		filter.SubClasses = make([]AuctionSubClassFilter, 0, subCount)
		for subIndex := uint32(0); subIndex < subCount; subIndex++ {
			var sub AuctionSubClassFilter
			mask, readErr := r.u64()
			if readErr != nil {
				return request, fmt.Errorf("read search filter %d subclass %d inv type: %w", index, subIndex, readErr)
			}
			sub.InvTypeMask = uint32(mask) // 3.4.3 sends a u64 mask; keep the low 32 bits
			if sub.ItemSubclass, err = r.i32(); err != nil {
				return request, fmt.Errorf("read search filter %d subclass %d item subclass: %w", index, subIndex, err)
			}
			filter.SubClasses = append(filter.SubClasses, sub)
		}
		request.ClassFilters = append(request.ClassFilters, filter)
	}
	size, readErr := r.u32()
	if readErr != nil {
		return request, fmt.Errorf("read search sort buffer size: %w", readErr)
	}
	sortBytes, err := r.take(int(size))
	if err != nil {
		return request, fmt.Errorf("read search sort buffer: %w", err)
	}
	sorts := movementReader{data: sortBytes}
	for index := uint32(0); index < uint32(sortCount); index++ {
		var sort AuctionSort
		if sort.Type, err = sorts.u8(); err != nil {
			return request, fmt.Errorf("read search sort %d type: %w", index, err)
		}
		if sort.Direction, err = sorts.u8(); err != nil {
			return request, fmt.Errorf("read search sort %d direction: %w", index, err)
		}
		request.Sorts = append(request.Sorts, sort)
	}
	if sorts.remaining() != 0 {
		return request, fmt.Errorf("search sort buffer has %d trailing bytes", sorts.remaining())
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("search query has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// EncodeLegacyAuctionSearch writes the WotLK CMSG_AUCTION_LIST_ITEMS tail,
// collapsing the modern per-subclass filter list onto the legacy single
// category triple exactly as HermesProxy HandleAuctionListItems does.
func EncodeLegacyAuctionSearch(request AuctionSearchRequest, auctioneer uint64) []byte {
	invType, itemClass, itemSubclass := LegacyAuctionSearchFilters(request)
	minLevel, maxLevel := legacyAuctionSearchLevels(request.MinLevel, request.MaxLevel)
	sorts := legacyAuctionSearchSorts(request.Sorts)

	body := binary.LittleEndian.AppendUint64(nil, auctioneer)
	body = binary.LittleEndian.AppendUint32(body, request.Offset)
	body = appendCString(body, request.Name)
	body = append(body, minLevel, maxLevel)
	body = binary.LittleEndian.AppendUint32(body, uint32(invType))
	body = binary.LittleEndian.AppendUint32(body, uint32(itemClass))
	body = binary.LittleEndian.AppendUint32(body, uint32(itemSubclass))
	body = binary.LittleEndian.AppendUint32(body, uint32(request.Quality))
	if request.OnlyUsable {
		body = append(body, 1)
	} else {
		body = append(body, 0)
	}
	// Hermes writes ExactMatch into the 3.3.5a getAll byte. That flag means
	// "return every auction, ignore filters" on AzerothCore, not name-exact,
	// so keep it clear. Build 54261 does not send ExactMatch.
	body = append(body, 0)
	body = append(body, byte(len(sorts)))
	for _, sort := range sorts {
		body = append(body, sort.Type, sort.Direction)
	}
	return body
}

// LegacyAuctionSearchFilters collapses modern class filters onto the legacy
// (inventoryType, itemClass, itemSubclass) triple Hermes HandleAuctionListItems
// writes. -1 means "any" and is 0xffffffff on the wire.
func LegacyAuctionSearchFilters(request AuctionSearchRequest) (invType, itemClass, itemSubclass int32) {
	invType, itemClass, itemSubclass = -1, -1, -1
	if len(request.ClassFilters) == 0 {
		return
	}
	filter := request.ClassFilters[0]
	if len(filter.SubClasses) == 1 {
		sub := filter.SubClasses[0]
		return modernToLegacyInventorySlotType(sub.InvTypeMask), filter.ItemClass, sub.ItemSubclass
	}
	return -1, filter.ItemClass, -1
}

func legacyAuctionSearchLevels(minLevel, maxLevel uint8) (uint8, uint8) {
	// 3.4.3's browse UI defaults min to 1. AzerothCore treats a nonzero min as
	// "RequiredLevel >= min", which hides RequiredLevel 0 items (recipes, junk,
	// many weapons). Hermes forwards the bytes unchanged; that empties searches
	// that the 3.3.5a client (empty boxes → 0,0) still finds. Once min is 0,
	// AC also ignores max, so clear both for the default 1..N band.
	if minLevel <= 1 {
		return 0, 0
	}
	return minLevel, maxLevel
}

func legacyAuctionSearchSorts(sorts []AuctionSort) []AuctionSort {
	if len(sorts) > auctionLegacySortMax {
		return sorts[:auctionLegacySortMax]
	}
	return sorts
}

// modernToLegacyInventorySlotType maps the modern inventory-type field to a
// single legacy InventoryType, or -1 when the mask means "any slot".
func modernToLegacyInventorySlotType(mask uint32) int32 {
	if mask == 0 || mask == ^uint32(0) {
		return -1
	}
	if bits.OnesCount32(mask) == 1 {
		for index := uint(0); index < 32; index++ {
			if mask&(1<<index) != 0 {
				return int32(index)
			}
		}
	}
	// 3.4.3 sometimes writes the InventoryType enum (e.g. 17 = 2H weapon) into
	// the u64 field instead of 1<<type. First-set-bit would turn 17 into slot 0
	// (INVTYPE_NON_EQUIP) and AzerothCore would match nothing.
	if mask <= auctionMaxInventoryType {
		return int32(mask)
	}
	return -1
}

// LegacyAuctionEnchant is one enchantment slot carried on an auctioned item.
type LegacyAuctionEnchant struct {
	Slot       byte
	ID         uint32
	Expiration uint32
	Charges    int32
}

// LegacyAuctionEntry is one per-auction record in a 3.3.5a SMSG_AUCTION_*_RESULT.
type LegacyAuctionEntry struct {
	AuctionID            uint32
	ItemID               uint32
	Enchants             []LegacyAuctionEnchant
	RandomPropertyID     uint32
	RandomPropertiesSeed uint32
	Count                int32
	Charges              int32
	Flags                uint32
	Owner                uint64
	MinBid               uint32
	MinIncrement         uint32
	Buyout               uint32
	DurationLeft         int32
	Bidder               uint64
	BidAmount            uint32
}

// LegacyAuctionListResult is a parsed 3.3.5a auction list result payload shared
// by the search and owned/bidder listings.
type LegacyAuctionListResult struct {
	Items           []LegacyAuctionEntry
	TotalItemsCount int32
	DesiredDelay    uint32
	HasMoreResults  bool
}

func readLegacyAuctionEntry(r *movementReader) (LegacyAuctionEntry, error) {
	var entry LegacyAuctionEntry
	var err error
	if entry.AuctionID, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction id: %w", err)
	}
	if entry.ItemID, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction item id: %w", err)
	}
	for slot := byte(0); slot < 7; slot++ { // 3.0.2+ enchantment slots
		var enchant LegacyAuctionEnchant
		enchant.Slot = slot
		if enchant.ID, err = r.u32(); err != nil {
			return entry, fmt.Errorf("read auction enchant %d id: %w", slot, err)
		}
		if enchant.Expiration, err = r.u32(); err != nil {
			return entry, fmt.Errorf("read auction enchant %d expiration: %w", slot, err)
		}
		if enchant.Charges, err = r.i32(); err != nil {
			return entry, fmt.Errorf("read auction enchant %d charges: %w", slot, err)
		}
		if enchant.ID != 0 {
			entry.Enchants = append(entry.Enchants, enchant)
		}
	}
	if entry.RandomPropertyID, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction random property: %w", err)
	}
	if entry.RandomPropertiesSeed, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction random property seed: %w", err)
	}
	if entry.Count, err = r.i32(); err != nil {
		return entry, fmt.Errorf("read auction count: %w", err)
	}
	if entry.Charges, err = r.i32(); err != nil {
		return entry, fmt.Errorf("read auction charges: %w", err)
	}
	if entry.Flags, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction flags: %w", err)
	}
	if entry.Owner, err = r.u64(); err != nil {
		return entry, fmt.Errorf("read auction owner: %w", err)
	}
	if entry.MinBid, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction min bid: %w", err)
	}
	if entry.MinIncrement, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction min increment: %w", err)
	}
	if entry.Buyout, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction buyout: %w", err)
	}
	if entry.DurationLeft, err = r.i32(); err != nil {
		return entry, fmt.Errorf("read auction duration left: %w", err)
	}
	if entry.Bidder, err = r.u64(); err != nil {
		return entry, fmt.Errorf("read auction bidder: %w", err)
	}
	if entry.BidAmount, err = r.u32(); err != nil {
		return entry, fmt.Errorf("read auction current bid: %w", err)
	}
	return entry, nil
}

// ParseLegacyAuctionListResult parses the count + per-auction records shared by
// SMSG_AUCTION_LIST_ITEMS_RESULT / LIST_OWNED_ITEMS_RESULT / LIST_BIDDED_ITEMS_RESULT.
func ParseLegacyAuctionListResult(body []byte) (LegacyAuctionListResult, error) {
	var result LegacyAuctionListResult
	r := movementReader{data: body}
	var err error
	count, readErr := r.u32()
	if readErr != nil {
		return result, fmt.Errorf("read auction list count: %w", readErr)
	}
	if count > 0x7fffffff {
		return result, fmt.Errorf("auction list count %d overflows", count)
	}
	result.Items = make([]LegacyAuctionEntry, 0, count)
	for index := uint32(0); index < count; index++ {
		entry, readErr := readLegacyAuctionEntry(&r)
		if readErr != nil {
			return result, fmt.Errorf("auction entry %d: %w", index, readErr)
		}
		result.Items = append(result.Items, entry)
	}
	if result.TotalItemsCount, err = r.i32(); err != nil {
		return result, fmt.Errorf("read auction list total: %w", err)
	}
	if result.DesiredDelay, err = r.u32(); err != nil {
		return result, fmt.Errorf("read auction list desired delay: %w", err)
	}
	result.HasMoreResults = result.TotalItemsCount > int32(count)
	if r.remaining() != 0 {
		return result, fmt.Errorf("auction list result has %d trailing bytes", r.remaining())
	}
	return result, nil
}

// EncodeAuctionBrowseResult re-emits a search result as SMSG_AUCTION_LIST_ITEMS_RESULT.
// Build 54261 wants count + total + delay, then a single OnlyUsable byte when the
// list is non-empty, then AuctionItem rows. HermesProxy's current
// AuctionListItemsResult.Write still emits the 8.3 Unknown830 / ListType /
// AuctionBucketKey header; that is what the live client was swallowing instead
// of the first item (proxy.log: items=1, UI empty). The older Hermes
// packet_structures.md Write matches 54261.
func EncodeAuctionBrowseResult(result LegacyAuctionListResult, resolve func(uint64) GUID128) []byte {
	body := make([]byte, 0, 64+len(result.Items)*48)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(result.Items)))
	body = binary.LittleEndian.AppendUint32(body, uint32(result.TotalItemsCount))
	body = binary.LittleEndian.AppendUint32(body, result.DesiredDelay)
	if len(result.Items) > 0 {
		body = append(body, 0) // OnlyUsable; 3.4.3 writes this as a byte, not a bit
	}
	for _, entry := range result.Items {
		body = encodeAuctionEntry(body, entry, true, false, resolve)
	}
	return body
}

// EncodeAuctionMyItemsResult re-emits an owned/bidder listing as
// SMSG_AUCTION_LIST_OWNED_ITEMS_RESULT or SMSG_AUCTION_LIST_BIDDED_ITEMS_RESULT
// (the caller picks the modern opcode). Build 54261 (legacy proxy AuctionListMyItemsResult.Write)
// is three uint32s then AuctionItem rows: Items.Count, SoldItems.Count, DesiredDelay.
// Current Hermes AuctionListMyItemsResult.cs still writes a HasMoreResults bit and a
// second sold-items loop; that extra bit is what left the Auctions tab empty so the
// client never sent CMSG_AUCTION_REMOVE_ITEM.
func EncodeAuctionMyItemsResult(result LegacyAuctionListResult, resolve func(uint64) GUID128) []byte {
	body := make([]byte, 0, 64+len(result.Items)*48)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(result.Items)))
	body = binary.LittleEndian.AppendUint32(body, 0) // SoldItems.Count
	body = binary.LittleEndian.AppendUint32(body, result.DesiredDelay)
	for _, entry := range result.Items {
		body = encodeAuctionEntry(body, entry, false, false, resolve)
	}
	return body
}

// encodeAuctionEntry writes one build-54261 AuctionItem. It mirrors
// HermesProxy AuctionItem.Write bit for bit: the leading bit block, the item
// instance, the fixed scalars, enchantments and the optional 64-bit money and
// server/bidder blocks.
func encodeAuctionEntry(dst []byte, entry LegacyAuctionEntry, censorServerSide, censorBid bool, resolve func(uint64) GUID128) []byte {
	hasItem := entry.ItemID != 0
	hasBidder := entry.Bidder != 0
	bits := newBitWriter(dst)
	bits.writeBit(hasItem)
	bits.writeBits(uint32(len(entry.Enchants)), 4)
	bits.writeBits(0, 2) // Gems.Count
	bits.writeBit(true)  // MinBid present
	bits.writeBit(true)  // MinIncrement present
	bits.writeBit(true)  // BuyoutPrice present
	bits.writeBit(false) // UnitPrice absent
	bits.writeBit(censorServerSide)
	bits.writeBit(censorBid)
	bits.writeBit(false) // AuctionBucketKey null
	bits.writeBit(false) // Creator null
	if !censorBid {
		bits.writeBit(hasBidder)
		bits.writeBit(true) // BidAmount present
	}
	dst = bits.flush()
	if hasItem {
		dst = appendItemInstance(dst, entry.ItemID, entry.RandomPropertiesSeed, entry.RandomPropertyID)
	}
	dst = binary.LittleEndian.AppendUint32(dst, uint32(entry.Count))
	dst = binary.LittleEndian.AppendUint32(dst, uint32(entry.Charges))
	dst = binary.LittleEndian.AppendUint32(dst, entry.Flags)
	dst = binary.LittleEndian.AppendUint32(dst, entry.AuctionID)
	owner := resolve(entry.Owner)
	dst = appendPackedGUID128(dst, owner.Low, owner.High)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(entry.DurationLeft))
	dst = append(dst, 0) // DeleteReason
	for _, enchant := range entry.Enchants {
		dst = binary.LittleEndian.AppendUint32(dst, enchant.ID)
		dst = binary.LittleEndian.AppendUint32(dst, enchant.Expiration)
		dst = binary.LittleEndian.AppendUint32(dst, uint32(enchant.Charges))
		dst = append(dst, enchant.Slot)
	}
	dst = binary.LittleEndian.AppendUint64(dst, uint64(entry.MinBid))
	dst = binary.LittleEndian.AppendUint64(dst, uint64(entry.MinIncrement))
	dst = binary.LittleEndian.AppendUint64(dst, uint64(entry.Buyout))
	if !censorServerSide {
		dst = appendPackedGUID128(dst, 0, 0) // ItemGuid empty (legacy has none)
		account := ModernWowAccountGUIDForLegacy(entry.Owner)
		dst = appendPackedGUID128(dst, account.Low, account.High)
		dst = binary.LittleEndian.AppendUint32(dst, 0) // EndTime
	}
	if !censorBid {
		if hasBidder {
			bidder := resolve(entry.Bidder)
			dst = appendPackedGUID128(dst, bidder.Low, bidder.High)
		}
		dst = binary.LittleEndian.AppendUint64(dst, uint64(entry.BidAmount))
	}
	return dst
}
