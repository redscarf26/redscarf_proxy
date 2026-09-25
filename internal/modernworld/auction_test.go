package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func auctioneerGUID() GUID128 {
	return GUID128{Low: 0x1234, High: 0xF130 << 16}
}

func staticResolve(guid uint64) GUID128 {
	return GUID128{Low: guid & 0xffffffff, High: uint64(guid >> 32)}
}

func TestAuctionHelloRequestRoundTrip(t *testing.T) {
	auctioneer := auctioneerGUID()
	body := appendPackedGUID128(nil, auctioneer.Low, auctioneer.High)
	got, err := ParseAuctionHelloRequest(body)
	if err != nil || got != auctioneer {
		t.Fatalf("hello request=%#v err=%v", got, err)
	}
	if _, err := ParseAuctionHelloRequest(append(body, 0)); err == nil {
		t.Fatal("hello request with trailing bytes must error")
	}
	legacy := EncodeLegacyAuctionHelloRequest(0x1122334455667788)
	if !isUint64At(legacy, 0, 0x1122334455667788) || len(legacy) != 8 {
		t.Fatalf("legacy hello request=%x", legacy)
	}
}

func TestAuctionOwnedAndBidderQueries(t *testing.T) {
	auctioneer := auctioneerGUID()
	prefix := appendPackedGUID128(nil, auctioneer.Low, auctioneer.High)

	owned := append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint32(nil, 25)...)
	request, err := ParseAuctionListOwnedQuery(owned)
	if err != nil || request.Auctioneer != auctioneer || request.Offset != 25 {
		t.Fatalf("owned query=%#v err=%v", request, err)
	}
	if legacy := EncodeLegacyAuctionOffsetQuery(0x55, 25); !isUint64At(legacy, 0, 0x55) || !isUint32At(legacy, 8, 25) || len(legacy) != 12 {
		t.Fatalf("legacy offset query=%x", legacy)
	}

	// Bidder query: guid, offset, 7-bit id count, then aligned ids.
	bidder := append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint32(nil, 3)...)
	bits := newBitWriter(bidder)
	bits.writeBits(2, 7)
	bidder = bits.flush()
	bidder = binary.LittleEndian.AppendUint32(bidder, 100)
	bidder = binary.LittleEndian.AppendUint32(bidder, 200)
	bq, err := ParseAuctionListBidderQuery(bidder)
	if err != nil || bq.Auctioneer != auctioneer || bq.Offset != 3 || len(bq.IDs) != 2 || bq.IDs[0] != 100 || bq.IDs[1] != 200 {
		t.Fatalf("bidder query=%#v err=%v", bq, err)
	}
	legacy := EncodeLegacyAuctionListBidder(0x55, 3, []uint32{100, 200})
	if !isUint64At(legacy, 0, 0x55) || !isUint32At(legacy, 8, 3) || !isUint32At(legacy, 12, 2) || !isUint32At(legacy, 16, 100) || !isUint32At(legacy, 20, 200) {
		t.Fatalf("legacy bidder query=%x", legacy)
	}
}

func TestAuctionRemoveAndBidRequests(t *testing.T) {
	auctioneer := auctioneerGUID()
	prefix := appendPackedGUID128(nil, auctioneer.Low, auctioneer.High)

	removeBody := append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint32(nil, 42)...)
	removeBody = append(removeBody, 0) // trailing no-addon presence bit byte
	remove, err := ParseAuctionRemoveRequest(removeBody)
	if err != nil || remove.Auctioneer != auctioneer || remove.AuctionID != 42 {
		t.Fatalf("remove request=%#v err=%v", remove, err)
	}
	if legacy := EncodeLegacyAuctionRemove(0x55, 42); !isUint64At(legacy, 0, 0x55) || !isUint32At(legacy, 8, 42) {
		t.Fatalf("legacy remove=%x", legacy)
	}

	// WPP 3.4.4 inserts ItemID between AuctionID and TaintedBy.
	removeWithItem := append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint32(nil, 42)...)
	removeWithItem = binary.LittleEndian.AppendUint32(removeWithItem, 0x1234)
	removeWithItem = append(removeWithItem, 0)
	remove, err = ParseAuctionRemoveRequest(removeWithItem)
	if err != nil || remove.Auctioneer != auctioneer || remove.AuctionID != 42 {
		t.Fatalf("remove request with item id=%#v err=%v", remove, err)
	}

	removeExact, err := ParseAuctionRemoveRequest(append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint32(nil, 7)...))
	if err != nil || removeExact.AuctionID != 7 {
		t.Fatalf("remove request without taint=%#v err=%v", removeExact, err)
	}

	pending := EncodeAuctionPendingSalesResult()
	if len(pending) != 8 || !isUint32At(pending, 0, 0) || !isUint32At(pending, 4, 0) {
		t.Fatalf("pending sales result=%x", pending)
	}

	bidBody := append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint32(nil, 42)...)
	bidBody = binary.LittleEndian.AppendUint64(bidBody, 0x1_0000_0001)
	bidBody = append(bidBody, 0) // trailing no-addon presence bit byte
	bid, err := ParseAuctionBidRequest(bidBody)
	if err != nil || bid.Auctioneer != auctioneer || bid.AuctionID != 42 || bid.Bid != 0x1_0000_0001 {
		t.Fatalf("bid request=%#v err=%v", bid, err)
	}
	if legacy := EncodeLegacyAuctionBid(0x55, 42, 0x1_0000_0001); !isUint64At(legacy, 0, 0x55) || !isUint32At(legacy, 8, 42) || !isUint32At(legacy, 12, 1) {
		t.Fatalf("legacy bid (truncated)=%x", legacy)
	}
}

func TestAuctionSellRequestRoundTrip(t *testing.T) {
	auctioneer := auctioneerGUID()
	item1 := GUID128{Low: 0x681, High: uint64(1) << 58}
	item2 := GUID128{Low: 0x682, High: uint64(1) << 58}
	body := appendPackedGUID128(nil, auctioneer.Low, auctioneer.High)
	body = binary.LittleEndian.AppendUint64(body, 0x1000) // MinBid
	body = binary.LittleEndian.AppendUint64(body, 0x5000) // Buyout
	body = binary.LittleEndian.AppendUint32(body, 1440)   // Expire
	bits := newBitWriter(body)
	bits.writeBit(false) // no TaintedBy
	bits.writeBits(2, auctionSellItemCountBits)
	body = bits.flush()
	body = appendPackedGUID128(body, item1.Low, item1.High)
	body = binary.LittleEndian.AppendUint32(body, 5)
	body = appendPackedGUID128(body, item2.Low, item2.High)
	body = binary.LittleEndian.AppendUint32(body, 3)

	request, err := ParseAuctionSellRequest(body)
	if err != nil {
		t.Fatalf("sell request parse: %v", err)
	}
	if request.Auctioneer != auctioneer || request.MinBid != 0x1000 || request.Buyout != 0x5000 || request.Expire != 1440 || len(request.Items) != 2 {
		t.Fatalf("sell request=%#v", request)
	}
	// Item count sits in a bit field right after the fixed header: rebuild the
	// equivalent expectation by decoding the packet again through the encoder.
	legacy := EncodeLegacyAuctionSell(request, 0x55, func(item GUID128) uint64 {
		if item == item1 {
			return 0x4000000000000681
		}
		return 0x4000000000000682
	})
	if !isUint64At(legacy, 0, 0x55) || !isUint32At(legacy, 8, 2) {
		t.Fatalf("legacy sell count-first header=%x", legacy)
	}
	if !isUint64At(legacy, 12, 0x4000000000000681) || !isUint32At(legacy, 20, 5) {
		t.Fatalf("legacy sell item 1=%x", legacy)
	}
	if !isUint64At(legacy, 24, 0x4000000000000682) || !isUint32At(legacy, 32, 3) {
		t.Fatalf("legacy sell item 2=%x", legacy)
	}
	if !isUint32At(legacy, 36, 0x1000) || !isUint32At(legacy, 40, 0x5000) || !isUint32At(legacy, 44, 1440) {
		t.Fatalf("legacy sell money tail=%x", legacy)
	}
}

func TestAuctionSellRequestSixBitSingleItem(t *testing.T) {
	// Live 3.4.3.54261 CMSG_AUCTION_SELL_ITEM uses a 6-bit item count. A 5-bit
	// reader treats count=1 as count=0 and then errors on the leftover guid+use
	// count (the proxy.log "sell-item request has 11 trailing bytes").
	auctioneer := auctioneerGUID()
	item := GUID128{Low: 0x681, High: uint64(1) << 58}
	body := appendPackedGUID128(nil, auctioneer.Low, auctioneer.High)
	body = binary.LittleEndian.AppendUint64(body, 0x1000)
	body = binary.LittleEndian.AppendUint64(body, 0x5000)
	body = binary.LittleEndian.AppendUint32(body, 1440)
	bits := newBitWriter(body)
	bits.writeBit(false)
	bits.writeBits(1, auctionSellItemCountBits)
	body = bits.flush()
	body = appendPackedGUID128(body, item.Low, item.High)
	body = binary.LittleEndian.AppendUint32(body, 1)

	request, err := ParseAuctionSellRequest(body)
	if err != nil {
		t.Fatalf("6-bit single-item sell must parse: %v", err)
	}
	if len(request.Items) != 1 || request.Items[0].Item != item || request.Items[0].UseCount != 1 {
		t.Fatalf("sell request=%#v", request)
	}
}

func TestAuctionOwnedAndBidderTolerateFlushedBitTrailer(t *testing.T) {
	auctioneer := auctioneerGUID()
	prefix := appendPackedGUID128(nil, auctioneer.Low, auctioneer.High)

	// 3.4.3 owned query appends a flushed TaintedBy=false byte after offset.
	owned := append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint32(nil, 0)...)
	owned = append(owned, 0)
	request, err := ParseAuctionListOwnedQuery(owned)
	if err != nil || request.Auctioneer != auctioneer || request.Offset != 0 {
		t.Fatalf("owned query with trailer=%#v err=%v", request, err)
	}

	// Empty bidder query: 7-bit count=0 occupies one flushed bit byte. remaining()
	// still counted that byte until ResetBitPos/align, matching the live
	// "bidder-items query has 1 trailing bytes".
	bidder := append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint32(nil, 0)...)
	bits := newBitWriter(bidder)
	bits.writeBits(0, 7)
	bidder = bits.flush()
	bq, err := ParseAuctionListBidderQuery(bidder)
	if err != nil || bq.Auctioneer != auctioneer || bq.Offset != 0 || len(bq.IDs) != 0 {
		t.Fatalf("empty bidder query=%#v err=%v", bq, err)
	}
}

func TestLegacyAuctionHelloToModern(t *testing.T) {
	body := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	body = binary.LittleEndian.AppendUint32(body, 3) // house id, ignored
	body = append(body, 1)                           // open for business
	guid, open, err := ParseLegacyAuctionHello(body)
	if err != nil || guid != 0xf130000001000043 || !open {
		t.Fatalf("hello guid=%x open=%v err=%v", guid, open, err)
	}
	// Re-read the synthesized response with the movement reader.
	response := EncodeAuctionHelloResponse(GUID128{Low: 0x43, High: 1 << 58}, true)
	r := movementReader{data: response}
	readGuid, err := r.guid128()
	if err != nil || readGuid != (GUID128{Low: 0x43, High: 1 << 58}) {
		t.Fatalf("hello response guid=%#v err=%v", readGuid, err)
	}
	if delay, err := r.u32(); err != nil || delay != 0 {
		t.Fatalf("hello response delay=%d err=%v", delay, err)
	}
	if delay, err := r.u32(); err != nil || delay != 0 {
		t.Fatalf("hello response cancel delay=%d err=%v", delay, err)
	}
	openBit, err := r.bit()
	if err != nil || !openBit {
		t.Fatalf("hello response open=%v err=%v", openBit, err)
	}
}

func auctionCommandFixture(action, errorCode uint32) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 7)
	body = binary.LittleEndian.AppendUint32(body, action)
	return binary.LittleEndian.AppendUint32(body, errorCode)
}

func TestAuctionCommandResultBidOK(t *testing.T) {
	legacy := auctionCommandFixture(auctionActionBid, auctionErrorOk)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x100) // min increment
	result, err := ParseLegacyAuctionCommandResult(legacy)
	if err != nil || result.AuctionID != 7 || result.Command != 2 || result.MinIncrement != 0x100 {
		t.Fatalf("command=%#v err=%v", result, err)
	}
	body := EncodeAuctionCommandResult(result, staticResolve)
	if !isUint32At(body, 0, 7) || !isUint32At(body, 4, 2) || !isUint32At(body, 8, 0) || !isUint32At(body, 12, 41) {
		t.Fatalf("modern command result=%x", body)
	}
	// Trailing 20 bytes: minIncrement u64, money u64, desiredDelay u32.
	if !isUint64At(body, len(body)-20, 0x100) || !isUint64At(body, len(body)-12, 0) || !isUint32At(body, len(body)-4, 0) {
		t.Fatalf("modern command result tail=%x", body)
	}
}

func TestAuctionCommandResultInventory(t *testing.T) {
	legacy := auctionCommandFixture(0, auctionErrorInventory)
	legacy = binary.LittleEndian.AppendUint32(legacy, 5) // bag result
	result, err := ParseLegacyAuctionCommandResult(legacy)
	if err != nil || result.BagResult != 5 {
		t.Fatalf("command=%#v err=%v", result, err)
	}
	body := EncodeAuctionCommandResult(result, staticResolve)
	if !isUint32At(body, 12, 5) {
		t.Fatalf("modern bag result=%x", body)
	}
}

func TestAuctionCommandResultHigherBid(t *testing.T) {
	legacy := auctionCommandFixture(auctionActionBid, auctionErrorHigherBid)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130000001000043) // higher bidder
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x500)              // money
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x100)              // min increment
	result, err := ParseLegacyAuctionCommandResult(legacy)
	if err != nil || result.HigherBidder != 0xf130000001000043 || result.Money != 0x500 || result.MinIncrement != 0x100 {
		t.Fatalf("command=%#v err=%v", result, err)
	}
	body := EncodeAuctionCommandResult(result, staticResolve)
	if !isUint64At(body, len(body)-20, 0x100) || !isUint64At(body, len(body)-12, 0x500) {
		t.Fatalf("modern higher-bid money tail=%x", body)
	}
}

func TestAuctionCommandResultCancelTrailer(t *testing.T) {
	legacy := auctionCommandFixture(auctionActionCancel, auctionErrorOk)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0) // trailing field AC appends on cancel
	if _, err := ParseLegacyAuctionCommandResult(legacy); err != nil {
		t.Fatalf("cancel command with trailer must parse: %v", err)
	}
}

func TestAuctionOwnerClosedNotification(t *testing.T) {
	// buyer empty: auction ended without a sale (or unsold removal).
	legacy := binary.LittleEndian.AppendUint32(nil, 9)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0) // bid amount 0 -> not sold
	legacy = binary.LittleEndian.AppendUint32(legacy, 0) // min increment
	legacy = binary.LittleEndian.AppendUint64(legacy, 0) // buyer empty
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x1234)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(3600))
	notification, err := ParseLegacyAuctionOwnerNotification(legacy)
	if err != nil || notification.AuctionID != 9 || notification.ItemID != 0x1234 {
		t.Fatalf("owner notification=%#v err=%v", notification, err)
	}
	body := EncodeAuctionClosedNotification(notification)
	head := encodeAuctionNotificationItem(9, 0, 0x1234, 0)
	if !bytes.HasPrefix(body, head) {
		t.Fatalf("closed notification does not start with its info head\nbody=%x\nhead=%x", body, head)
	}
	// After the info head comes the proceeds-mail delay float and the Sold bit.
	tail := body[len(head):]
	if len(tail) != 5 || !isUint32At(tail, 0, math.Float32bits(3600)) {
		t.Fatalf("closed notification tail=%x", tail)
	}
	if tail[4]&0x80 != 0 { // Sold = BidAmount != 0, and this fixture bid 0
		t.Fatalf("closed notification sold bit set for an unsold auction: %x", tail)
	}
}

func TestAuctionOwnerClosedNotificationSold(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 9)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x2000) // bid amount -> sold
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x100)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0) // buyer empty
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x1234)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(60))
	notification, err := ParseLegacyAuctionOwnerNotification(legacy)
	if err != nil {
		t.Fatalf("owner notification=%#v err=%v", notification, err)
	}
	body := EncodeAuctionClosedNotification(notification)
	head := encodeAuctionNotificationItem(9, 0x2000, 0x1234, 0)
	tail := body[len(head):]
	if len(tail) != 5 || tail[4]&0x80 == 0 {
		t.Fatalf("closed sold notification tail=%x", tail)
	}
}

func TestAuctionOwnerBidNotification(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 9)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x1000)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x100)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x1234)
	legacy = binary.LittleEndian.AppendUint32(legacy, 77)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(0))
	notification, err := ParseLegacyAuctionOwnerNotification(legacy)
	if err != nil || notification.BidAmount != 0x1000 || notification.Buyer != 0xf130000001000043 || notification.RandomPropertyID != 77 {
		t.Fatalf("owner notification=%#v err=%v", notification, err)
	}
	body := EncodeAuctionOwnerBidNotification(notification, staticResolve)
	head := encodeAuctionNotificationItem(9, 0x1000, 0x1234, 77)
	if !bytes.HasPrefix(body, head) {
		t.Fatalf("owner-bid notification does not start with its info head\nbody=%x\nhead=%x", body, head)
	}
	tail := body[len(head):]
	if !isUint64At(tail, 0, 0x100) { // MinIncrement
		t.Fatalf("owner-bid increment tail=%x", tail)
	}
	wantBidder := staticResolve(notification.Buyer)
	low, high, consumed, err := readPackedGUID128(tail[8:])
	if err != nil || consumed != len(tail)-8 || low != wantBidder.Low || high != wantBidder.High {
		t.Fatalf("owner-bid bidder low=%x high=%x consumed=%d len=%d err=%v", low, high, consumed, len(tail)-8, err)
	}
}

func TestAuctionBidderWonAndOutbidNotifications(t *testing.T) {
	bidderFixture := func(bid uint32) []byte {
		body := binary.LittleEndian.AppendUint32(nil, 2) // house id
		body = binary.LittleEndian.AppendUint32(body, 7)
		body = binary.LittleEndian.AppendUint64(body, 0xf130000001000043)
		body = binary.LittleEndian.AppendUint32(body, bid)
		body = binary.LittleEndian.AppendUint32(body, 0x100)
		body = binary.LittleEndian.AppendUint32(body, 0x1234)
		return binary.LittleEndian.AppendUint32(body, 5)
	}
	notification, err := ParseLegacyAuctionBidderNotification(bidderFixture(0))
	if err != nil || notification.AuctionID != 7 || notification.BidAmount != 0 || notification.RandomPropertyID != 5 {
		t.Fatalf("bidder notification=%#v err=%v", notification, err)
	}
	won := EncodeAuctionWonNotification(notification, staticResolve)
	info := encodeAuctionBidderItem(7, notification.Bidder, 0x1234, 5, staticResolve)
	if !bytes.Equal(won, info) {
		t.Fatalf("won notification != info head\nwon=%x\ninfo=%x", won, info)
	}
	outbidLegacy := bidderFixture(0x900)
	notification, err = ParseLegacyAuctionBidderNotification(outbidLegacy)
	if err != nil || notification.BidAmount != 0x900 {
		t.Fatalf("bidder notification=%#v err=%v", notification, err)
	}
	outbid := EncodeAuctionOutbidNotification(notification, staticResolve)
	info = encodeAuctionBidderItem(7, notification.Bidder, 0x1234, 5, staticResolve)
	if !bytes.HasPrefix(outbid, info) {
		t.Fatalf("outbid notification does not start with its info head\noutbid=%x\ninfo=%x", outbid, info)
	}
	tail := outbid[len(info):]
	if !isUint64At(tail, 0, 0x900) || !isUint64At(tail, 8, 0x100) {
		t.Fatalf("outbid tail=%x", tail)
	}
}
