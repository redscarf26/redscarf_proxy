package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Auction house translation (build 54261 client <-> 3.3.5a AzerothCore). The
// modern engine drives the AH through the NPC-interaction + AuctionHello handshake
// and a set of CMSG_AUCTION_* controls that all carry the auctioneer GUID128; the
// legacy side opens with MSG_AUCTION_HELLO and answers with legacy SMSG/MSG
// opcodes. Layouts mirror HermesProxy-WOTLK World/Server/Packets
// {InteractWithNPC,AuctionSellItem,AuctionRemoveItem,AuctionPlaceBid,
// AuctionListOwnerItems,AuctionListBidderItems,AuctionCommandResult,
// AuctionHelloResponse,AuctionClosedNotification,AuctionOwnerBidNotification,
// AuctionWonNotification,AuctionOutbidNotification,AuctionBidderNotification,
// ItemInstance}.cs and World/Server/WorldSocket.cs / World/Client/WorldClient.cs.
// AzerothCore serializes ObjectGuid values as raw 8-byte guids in these packets
// (only update-object/combat-log contexts pack them), so the auctioneer/item
// guids are read and written unpacked.

const (
	// Client -> server auction controls (build 54261).
	CMSGAuctionHelloRequest     = uint16(13514) // 0x34CA
	CMSGAuctionSellItem         = uint16(13515) // 0x34CB
	CMSGAuctionRemoveItem       = uint16(13516) // 0x34CC
	CMSGAuctionListItems        = uint16(13517) // 0x34CD
	CMSGAuctionListOwnedItems   = uint16(13519) // 0x34CF
	CMSGAuctionListBiddedItems  = uint16(13520) // 0x34D0
	CMSGAuctionPlaceBid         = uint16(13521) // 0x34D1
	CMSGAuctionListPendingSales = uint16(13522) // 0x34D2; WPP 3.4.3, omitted from Hermes 54261 Opcode.cs

	// Server -> client auction data.
	SMSGAuctionHelloResponse          = uint16(9966)
	SMSGAuctionCommandResult          = uint16(9968)
	SMSGAuctionWonNotification        = uint16(9969)
	SMSGAuctionOutbidNotification     = uint16(9970)
	SMSGAuctionClosedNotification     = uint16(9971)
	SMSGAuctionOwnerBidNotification   = uint16(9972)
	SMSGAuctionListPendingSalesResult = uint16(9973) // 0x26F5; WPP SMSG_AUCTION_LIST_PENDING_SALES_RESULT
	SMSGAuctionListItemsResult        = uint16(10339)
	SMSGAuctionListOwnedItemsResult   = uint16(10364)
	SMSGAuctionListBiddedItemsResult  = uint16(10365)
)

const (
	auctionActionSell   = 0
	auctionActionCancel = 1
	auctionActionBid    = 2

	auctionErrorOk         = 0
	auctionErrorInventory  = 1
	auctionErrorHigherBid  = 5
	auctionMaxSaleItems    = 64
	auctionMaxBidListItems = 128
	// 3.4.3.54261 widened the sell-item count from 5 to 6 bits (HermesProxy
	// AddedInClassicVersion 1.14.3 / 2.5.4). Reading 5 bits treats a one-item
	// listing as count=0 and then rejects the leftover guid+use-count.
	auctionSellItemCountBits = 6
)

// ParseAuctionHelloRequest reads the modern CMSG_AUCTION_HELLO_REQUEST, whose
// body is only the packed auctioneer GUID128.
func ParseAuctionHelloRequest(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

// AuctionOffsetQuery is a modern owned-items request: auctioneer + page offset.
type AuctionOffsetQuery struct {
	Auctioneer GUID128
	Offset     uint32
}

func ParseAuctionListOwnedQuery(body []byte) (AuctionOffsetQuery, error) {
	var request AuctionOffsetQuery
	r := movementReader{data: body}
	var err error
	if request.Auctioneer, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read owned-items auctioneer: %w", err)
	}
	if request.Offset, err = r.u32(); err != nil {
		return request, fmt.Errorf("read owned-items offset: %w", err)
	}
	if err := finishAuctionClientPacket(&r); err != nil {
		return request, fmt.Errorf("owned-items query: %w", err)
	}
	return request, nil
}

// AuctionBidderQuery is a modern bidder-items request: auctioneer + offset plus
// the 7-bit list of auction ids the player is currently bidding on.
type AuctionBidderQuery struct {
	Auctioneer GUID128
	Offset     uint32
	IDs        []uint32
}

func ParseAuctionListBidderQuery(body []byte) (AuctionBidderQuery, error) {
	var request AuctionBidderQuery
	r := movementReader{data: body}
	var err error
	if request.Auctioneer, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read bidder-items auctioneer: %w", err)
	}
	if request.Offset, err = r.u32(); err != nil {
		return request, fmt.Errorf("read bidder-items offset: %w", err)
	}
	count, readErr := r.bits(7)
	if readErr != nil {
		return request, fmt.Errorf("read bidder-items id count: %w", readErr)
	}
	if count > auctionMaxBidListItems {
		return request, fmt.Errorf("bidder-items lists %d ids, maximum is %d", count, auctionMaxBidListItems)
	}
	// Hermes ResetBitPos: the 7-bit count occupies one flushed byte when the
	// list is empty, and remaining() still sees that byte until we align.
	r.align()
	request.IDs = make([]uint32, 0, count)
	for index := uint32(0); index < count; index++ {
		id, readErr := r.u32()
		if readErr != nil {
			return request, fmt.Errorf("read bidder-items id %d: %w", index, readErr)
		}
		request.IDs = append(request.IDs, id)
	}
	if err := finishAuctionClientPacket(&r); err != nil {
		return request, fmt.Errorf("bidder-items query: %w", err)
	}
	return request, nil
}

// AuctionRemoveRequest is a modern cancel-auction request: auctioneer + auction id.
type AuctionRemoveRequest struct {
	Auctioneer GUID128
	AuctionID  uint32
}

func ParseAuctionRemoveRequest(body []byte) (AuctionRemoveRequest, error) {
	var request AuctionRemoveRequest
	r := movementReader{data: body}
	var err error
	if request.Auctioneer, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read remove-item auctioneer: %w", err)
	}
	if request.AuctionID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read remove-item auction id: %w", err)
	}
	// Hermes AuctionRemoveItem.Read stops at AuctionID + TaintedBy. WPP 3.4.4
	// also writes ItemID before that flag; a lone uint32 or uint32+taint byte
	// is that extra field. Leave larger trailers to finishAuctionClientPacket
	// so a real TaintedBy addon still parses.
	if remaining := r.remaining(); remaining == 4 || remaining == 5 {
		if _, err := r.u32(); err != nil {
			return request, fmt.Errorf("read remove-item item id: %w", err)
		}
	}
	if err := finishAuctionClientPacket(&r); err != nil {
		return request, fmt.Errorf("remove-item request: %w", err)
	}
	return request, nil
}

// EncodeAuctionPendingSalesResult writes an empty SMSG_AUCTION_LIST_PENDING_SALES_RESULT.
// AzerothCore's HandleAuctionListPendingSales always replies with count=0; the 3.4
// client frame is MailsCount + TotalNumRecords (WPP 3.4.4) and no mail entries.
func EncodeAuctionPendingSalesResult() []byte {
	body := binary.LittleEndian.AppendUint32(nil, 0)
	return binary.LittleEndian.AppendUint32(body, 0)
}

// AuctionBidRequest is a modern place-bid request: auctioneer + auction id + bid.
type AuctionBidRequest struct {
	Auctioneer GUID128
	AuctionID  uint32
	Bid        uint64
}

func ParseAuctionBidRequest(body []byte) (AuctionBidRequest, error) {
	var request AuctionBidRequest
	r := movementReader{data: body}
	var err error
	if request.Auctioneer, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read place-bid auctioneer: %w", err)
	}
	if request.AuctionID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read place-bid auction id: %w", err)
	}
	if request.Bid, err = r.u64(); err != nil {
		return request, fmt.Errorf("read place-bid amount: %w", err)
	}
	if err := skipOptionalAddOnInfo(&r); err != nil {
		return request, err
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("place-bid request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// AuctionForSale is one item offered in a modern CMSG_AUCTION_SELL_ITEM.
type AuctionForSale struct {
	Item     GUID128
	UseCount uint32
}

// AuctionSellRequest is the modern CMSG_AUCTION_SELL_ITEM payload.
type AuctionSellRequest struct {
	Auctioneer GUID128
	MinBid     uint64
	Buyout     uint64
	Expire     uint32
	Items      []AuctionForSale
}

func ParseAuctionSellRequest(body []byte) (AuctionSellRequest, error) {
	var request AuctionSellRequest
	r := movementReader{data: body}
	var err error
	if request.Auctioneer, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read sell-item auctioneer: %w", err)
	}
	if request.MinBid, err = r.u64(); err != nil {
		return request, fmt.Errorf("read sell-item min bid: %w", err)
	}
	if request.Buyout, err = r.u64(); err != nil {
		return request, fmt.Errorf("read sell-item buyout: %w", err)
	}
	if request.Expire, err = r.u32(); err != nil {
		return request, fmt.Errorf("read sell-item expire time: %w", err)
	}
	tainted, err := r.bit()
	if err != nil {
		return request, fmt.Errorf("read sell-item tainted bit: %w", err)
	}
	count, readErr := r.bits(auctionSellItemCountBits)
	if readErr != nil {
		return request, fmt.Errorf("read sell-item item count: %w", readErr)
	}
	if count > auctionMaxSaleItems {
		return request, fmt.Errorf("sell-item lists %d items, maximum is %d", count, auctionMaxSaleItems)
	}
	if tainted {
		if err := skipAddOnInfo(&r); err != nil {
			return request, err
		}
	}
	request.Items = make([]AuctionForSale, 0, count)
	for index := uint32(0); index < count; index++ {
		var sale AuctionForSale
		if sale.Item, err = r.guid128(); err != nil {
			return request, fmt.Errorf("read sell-item %d item guid: %w", index, err)
		}
		if sale.UseCount, err = r.u32(); err != nil {
			return request, fmt.Errorf("read sell-item %d use count: %w", index, err)
		}
		request.Items = append(request.Items, sale)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("sell-item request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// skipAddOnInfo mirrors HermesProxy AddOnInfo.Read for the controls that carry an
// optional trailing addon trailer (RemoveItem/PlaceBid/SellItem). It re-aligns,
// reads the 10+10+2 bit header and the two length-prefixed strings.
func skipAddOnInfo(r *movementReader) error {
	r.align()
	nameLength, err := r.bits(10)
	if err != nil {
		return fmt.Errorf("read addon name length: %w", err)
	}
	versionLength, err := r.bits(10)
	if err != nil {
		return fmt.Errorf("read addon version length: %w", err)
	}
	if _, err := r.bit(); err != nil { // Loaded
		return fmt.Errorf("read addon loaded bit: %w", err)
	}
	if _, err := r.bit(); err != nil { // Disabled
		return fmt.Errorf("read addon disabled bit: %w", err)
	}
	if nameLength > 1 {
		if _, err := r.stringN(int(nameLength - 1)); err != nil {
			return fmt.Errorf("read addon name: %w", err)
		}
		if _, err := r.u8(); err != nil { // trailing NUL
			return fmt.Errorf("read addon name terminator: %w", err)
		}
	}
	if versionLength > 1 {
		if _, err := r.stringN(int(versionLength - 1)); err != nil {
			return fmt.Errorf("read addon version: %w", err)
		}
		if _, err := r.u8(); err != nil { // trailing NUL
			return fmt.Errorf("read addon version terminator: %w", err)
		}
	}
	return nil
}

// skipOptionalAddOnInfo consumes the one presence bit and then the addon trailer
// when set, mirroring the controls whose payload can end right after the flag.
func skipOptionalAddOnInfo(r *movementReader) error {
	present, err := r.bit()
	if err != nil {
		return fmt.Errorf("read addon presence bit: %w", err)
	}
	if present {
		if err := skipAddOnInfo(r); err != nil {
			return err
		}
	}
	// The presence flag (and any addon trailer) is its own bit group; drop the
	// byte padding so a trailing-bytes check sees a clean packet end.
	r.align()
	return nil
}

// finishAuctionClientPacket aligns any in-progress bit byte and then consumes
// the optional TaintedBy trailer 3.4.3.54261 appends to owned/bidder listing
// queries. A flushed 0x00 byte is TaintedBy=false (and any 2-bit sort count
// packed into the same byte); a set presence bit is followed by AddOnInfo.
func finishAuctionClientPacket(r *movementReader) error {
	r.align()
	if r.remaining() == 0 {
		return nil
	}
	if err := skipOptionalAddOnInfo(r); err != nil {
		return err
	}
	if r.remaining() != 0 {
		return fmt.Errorf("has %d trailing bytes", r.remaining())
	}
	return nil
}

// EncodeLegacyAuctionHelloRequest writes the WotLK MSG_AUCTION_HELLO body, which
// carries only the raw auctioneer GUID64.
func EncodeLegacyAuctionHelloRequest(auctioneer uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, auctioneer)
}

func EncodeLegacyAuctionOffsetQuery(auctioneer uint64, offset uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, auctioneer)
	return binary.LittleEndian.AppendUint32(body, offset)
}

func EncodeLegacyAuctionListBidder(auctioneer uint64, offset uint32, ids []uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, auctioneer)
	body = binary.LittleEndian.AppendUint32(body, offset)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(ids)))
	for _, id := range ids {
		body = binary.LittleEndian.AppendUint32(body, id)
	}
	return body
}

// EncodeLegacyAuctionRemove writes the WotLK CMSG_AUCTION_REMOVE_ITEM body.
func EncodeLegacyAuctionRemove(auctioneer uint64, auctionID uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, auctioneer)
	return binary.LittleEndian.AppendUint32(body, auctionID)
}

// EncodeLegacyAuctionBid writes the WotLK CMSG_AUCTION_PLACE_BID body with the
// modern 64-bit bid truncated to the legacy 32-bit amount.
func EncodeLegacyAuctionBid(auctioneer uint64, auctionID uint32, bid uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, auctioneer)
	body = binary.LittleEndian.AppendUint32(body, auctionID)
	return binary.LittleEndian.AppendUint32(body, uint32(bid))
}

// EncodeLegacyAuctionSell writes the 3.3.5a CMSG_AUCTION_SELL_ITEM body. Since
// 3.2.2a the legacy layout is count-first: auctioneer, item count, then one
// item-guid/use-count pair each, then min bid, buyout and expiry as uint32.
// 3.2.2a the legacy layout is count-first: auctioneer, item count, then one
// item-guid/use-count pair each, then min bid, buyout and expiry as uint32.
func EncodeLegacyAuctionSell(request AuctionSellRequest, auctioneer uint64, resolve func(GUID128) uint64) []byte {
	resolved := make([]uint64, 0, len(request.Items))
	for _, sale := range request.Items {
		resolved = append(resolved, resolve(sale.Item))
	}
	return EncodeLegacyAuctionSellResolved(request, auctioneer, resolved)
}

// EncodeLegacyAuctionSellResolved is EncodeLegacyAuctionSell with the item guids
// already resolved, so the caller can log each item as it maps them.
func EncodeLegacyAuctionSellResolved(request AuctionSellRequest, auctioneer uint64, resolved []uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, auctioneer)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(request.Items)))
	for index, sale := range request.Items {
		legacy := uint64(0)
		if index < len(resolved) {
			legacy = resolved[index]
		}
		body = binary.LittleEndian.AppendUint64(body, legacy)
		body = binary.LittleEndian.AppendUint32(body, sale.UseCount)
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(request.MinBid))
	body = binary.LittleEndian.AppendUint32(body, uint32(request.Buyout))
	return binary.LittleEndian.AppendUint32(body, request.Expire)
}

// EncodeAuctionHelloResponse writes the build-54261 SMSG_AUCTION_HELLO_RESPONSE.
func EncodeAuctionHelloResponse(auctioneer GUID128, open bool) []byte {
	body := appendPackedGUID128(nil, auctioneer.Low, auctioneer.High)
	body = binary.LittleEndian.AppendUint32(body, 0) // PurchasedItemDeliveryDelay
	body = binary.LittleEndian.AppendUint32(body, 0) // CancelledItemDeliveryDelay
	bits := newBitWriter(body)
	bits.writeBit(open) // OpenForBusiness
	return bits.flush()
}

// ParseLegacyAuctionHello reads the legacy MSG_AUCTION_HELLO reply: auctioneer
// GUID64, the discarded auction house id, and the open-for-business byte (present
// since 3.3.0, which 3.3.5a always sends).
func ParseLegacyAuctionHello(body []byte) (auctioneer uint64, open bool, err error) {
	r := movementReader{data: body}
	if auctioneer, err = r.u64(); err != nil {
		return 0, false, fmt.Errorf("read auction hello auctioneer: %w", err)
	}
	if _, err = r.u32(); err != nil { // auction house id, not used by the modern client
		return 0, false, fmt.Errorf("read auction hello house id: %w", err)
	}
	openByte, readErr := r.u8()
	if readErr != nil {
		return 0, false, fmt.Errorf("read auction hello open flag: %w", readErr)
	}
	open = openByte != 0
	if r.remaining() != 0 {
		return 0, false, fmt.Errorf("auction hello has %d trailing bytes", r.remaining())
	}
	return auctioneer, open, nil
}

// LegacyAuctionCommandResult mirrors the conditional 3.3.5a SMSG_AUCTION_COMMAND_RESULT.
type LegacyAuctionCommandResult struct {
	AuctionID    uint32
	Command      uint32
	ErrorCode    uint32
	BagResult    uint32
	HigherBidder uint64
	Money        uint32
	MinIncrement uint32
}

func ParseLegacyAuctionCommandResult(body []byte) (LegacyAuctionCommandResult, error) {
	var result LegacyAuctionCommandResult
	r := movementReader{data: body}
	var err error
	if result.AuctionID, err = r.u32(); err != nil {
		return result, fmt.Errorf("read auction command id: %w", err)
	}
	if result.Command, err = r.u32(); err != nil {
		return result, fmt.Errorf("read auction command: %w", err)
	}
	if result.ErrorCode, err = r.u32(); err != nil {
		return result, fmt.Errorf("read auction command error: %w", err)
	}
	switch {
	case result.ErrorCode == auctionErrorOk && result.Command == auctionActionBid:
		if result.MinIncrement, err = r.u32(); err != nil {
			return result, fmt.Errorf("read auction bid increment: %w", err)
		}
	case result.ErrorCode == auctionErrorInventory:
		if result.BagResult, err = r.u32(); err != nil {
			return result, fmt.Errorf("read auction bag result: %w", err)
		}
	case result.ErrorCode == auctionErrorHigherBid:
		if result.HigherBidder, err = r.u64(); err != nil {
			return result, fmt.Errorf("read auction higher-bidder: %w", err)
		}
		if result.Money, err = r.u32(); err != nil {
			return result, fmt.Errorf("read auction higher-bid money: %w", err)
		}
		if result.MinIncrement, err = r.u32(); err != nil {
			return result, fmt.Errorf("read auction higher-bid increment: %w", err)
		}
	}
	// AzerothCore also appends a trailing uint32 when a cancel succeeds; consume
	// it rather than misreporting the packet as malformed.
	if r.remaining() == 4 {
		if _, err := r.u32(); err != nil {
			return result, fmt.Errorf("read auction command trailer: %w", err)
		}
	}
	if r.remaining() != 0 {
		return result, fmt.Errorf("auction command result has %d trailing bytes", r.remaining())
	}
	return result, nil
}

// EncodeAuctionCommandResult always writes the eight build-54261 fields, widening
// money/increment to 64 bits and packing the higher-bidder guid. BagResult falls
// back to the modern internal-bag-error code 41 when the legacy packet carried none.
func EncodeAuctionCommandResult(result LegacyAuctionCommandResult, resolve func(uint64) GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, result.AuctionID)
	body = binary.LittleEndian.AppendUint32(body, result.Command)
	body = binary.LittleEndian.AppendUint32(body, result.ErrorCode)
	bagResult := result.BagResult
	if result.ErrorCode != auctionErrorInventory {
		bagResult = 41 // InventoryResult.InternalBagError
	}
	body = binary.LittleEndian.AppendUint32(body, bagResult)
	guid := resolve(result.HigherBidder)
	body = appendPackedGUID128(body, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint64(body, uint64(result.MinIncrement))
	body = binary.LittleEndian.AppendUint64(body, uint64(result.Money))
	return binary.LittleEndian.AppendUint32(body, 0) // DesiredDelay
}

// LegacyAuctionOwnerNotification mirrors the legacy SMSG_AUCTION_OWNER_NOTIFICATION.
type LegacyAuctionOwnerNotification struct {
	AuctionID        uint32
	BidAmount        uint32
	MinIncrement     uint32
	Buyer            uint64
	ItemID           uint32
	RandomPropertyID uint32
	MailDelay        float32
}

func ParseLegacyAuctionOwnerNotification(body []byte) (LegacyAuctionOwnerNotification, error) {
	var notification LegacyAuctionOwnerNotification
	r := movementReader{data: body}
	var err error
	if notification.AuctionID, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read owner notification auction id: %w", err)
	}
	if notification.BidAmount, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read owner notification bid: %w", err)
	}
	if notification.MinIncrement, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read owner notification increment: %w", err)
	}
	if notification.Buyer, err = r.u64(); err != nil {
		return notification, fmt.Errorf("read owner notification buyer: %w", err)
	}
	if notification.ItemID, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read owner notification item id: %w", err)
	}
	if notification.RandomPropertyID, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read owner notification random property: %w", err)
	}
	if notification.MailDelay, err = r.f32(); err != nil {
		return notification, fmt.Errorf("read owner notification mail delay: %w", err)
	}
	if r.remaining() != 0 {
		return notification, fmt.Errorf("owner notification has %d trailing bytes", r.remaining())
	}
	return notification, nil
}

// LegacyAuctionBidderNotification mirrors the legacy SMSG_AUCTION_BIDDER_NOTIFICATION.
type LegacyAuctionBidderNotification struct {
	AuctionID        uint32
	Bidder           uint64
	BidAmount        uint32
	MinIncrement     uint32
	ItemID           uint32
	RandomPropertyID uint32
}

func ParseLegacyAuctionBidderNotification(body []byte) (LegacyAuctionBidderNotification, error) {
	var notification LegacyAuctionBidderNotification
	r := movementReader{data: body}
	var err error
	if _, err = r.u32(); err != nil { // auction house id, unused
		return notification, fmt.Errorf("read bidder notification house id: %w", err)
	}
	if notification.AuctionID, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read bidder notification auction id: %w", err)
	}
	if notification.Bidder, err = r.u64(); err != nil {
		return notification, fmt.Errorf("read bidder notification bidder: %w", err)
	}
	if notification.BidAmount, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read bidder notification bid: %w", err)
	}
	if notification.MinIncrement, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read bidder notification increment: %w", err)
	}
	if notification.ItemID, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read bidder notification item id: %w", err)
	}
	if notification.RandomPropertyID, err = r.u32(); err != nil {
		return notification, fmt.Errorf("read bidder notification random property: %w", err)
	}
	if r.remaining() != 0 {
		return notification, fmt.Errorf("bidder notification has %d trailing bytes", r.remaining())
	}
	return notification, nil
}

// EncodeAuctionClosedNotification (buyer empty) maps to SMSG_AUCTION_CLOSED_NOTIFICATION.
func EncodeAuctionClosedNotification(notification LegacyAuctionOwnerNotification) []byte {
	body := encodeAuctionNotificationItem(notification.AuctionID, notification.BidAmount, notification.ItemID, notification.RandomPropertyID)
	body = binary.LittleEndian.AppendUint32(body, math.Float32bits(notification.MailDelay)) // ProceedsMailDelay
	bits := newBitWriter(body)
	bits.writeBit(notification.BidAmount != 0) // Sold
	return bits.flush()
}

// EncodeAuctionOwnerBidNotification (buyer set) maps to SMSG_AUCTION_OWNER_BID_NOTIFICATION.
func EncodeAuctionOwnerBidNotification(notification LegacyAuctionOwnerNotification, resolve func(uint64) GUID128) []byte {
	body := encodeAuctionNotificationItem(notification.AuctionID, notification.BidAmount, notification.ItemID, notification.RandomPropertyID)
	body = binary.LittleEndian.AppendUint64(body, uint64(notification.MinIncrement))
	bidder := resolve(notification.Buyer)
	return appendPackedGUID128(body, bidder.Low, bidder.High)
}

// EncodeAuctionWonNotification (bid zero) maps to SMSG_AUCTION_WON_NOTIFICATION.
func EncodeAuctionWonNotification(notification LegacyAuctionBidderNotification, resolve func(uint64) GUID128) []byte {
	body := encodeAuctionBidderItem(notification.AuctionID, notification.Bidder, notification.ItemID, notification.RandomPropertyID, resolve)
	return body
}

// EncodeAuctionOutbidNotification (bid nonzero) maps to SMSG_AUCTION_OUTBID_NOTIFICATION.
func EncodeAuctionOutbidNotification(notification LegacyAuctionBidderNotification, resolve func(uint64) GUID128) []byte {
	body := encodeAuctionBidderItem(notification.AuctionID, notification.Bidder, notification.ItemID, notification.RandomPropertyID, resolve)
	body = binary.LittleEndian.AppendUint64(body, uint64(notification.BidAmount))
	return binary.LittleEndian.AppendUint64(body, uint64(notification.MinIncrement))
}

// encodeAuctionNotificationItem writes the shared AuctionOwnerNotification body:
// auction id, widened bid amount, then the item instance (seed always zero).
func encodeAuctionNotificationItem(auctionID, bid uint32, itemID, randomPropertyID uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, auctionID)
	body = binary.LittleEndian.AppendUint64(body, uint64(bid))
	return appendItemInstance(body, itemID, 0, randomPropertyID)
}

// encodeAuctionBidderItem writes the shared AuctionBidderNotification body: the
// fixed command code 2 (Bid), auction id, packed bidder, then the item instance.
func encodeAuctionBidderItem(auctionID uint32, bidder uint64, itemID, randomPropertyID uint32, resolve func(uint64) GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 2) // Command = AuctionHouseAction.Bid
	body = binary.LittleEndian.AppendUint32(body, auctionID)
	modern := resolve(bidder)
	body = appendPackedGUID128(body, modern.Low, modern.High)
	return appendItemInstance(body, itemID, 0, randomPropertyID)
}
