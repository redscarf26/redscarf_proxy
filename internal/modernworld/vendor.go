package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGListInventory = uint16(0x34A1)
	CMSGSellItem      = uint16(0x34A2)
	CMSGBuyItem       = uint16(0x34A3)
	CMSGBuyBackItem   = uint16(0x34A4)

	SMSGVendorInventory = uint16(0x25B8)
	SMSGSellResponse    = uint16(0x26C5)
	SMSGBuySucceeded    = uint16(0x26C6)
	SMSGBuyFailed       = uint16(0x26C7)

	maxVendorItems = 255
)

type BuyItemRequest struct {
	Vendor    GUID128
	Container GUID128
	Quantity  uint32
	Muid      uint32
	Slot      uint32
	ItemType  int32
	ItemID    uint32
}

type SellItemRequest struct {
	Vendor GUID128
	Item   GUID128
	Amount uint32
}

type BuyBackItemRequest struct {
	Vendor GUID128
	Slot   uint32
}

type LegacyVendorItem struct {
	Slot         int32
	Muid         uint32
	ItemID       uint32
	Quantity     int32
	Price        uint32
	Durability   int32
	BuyCount     uint32
	ExtendedCost int32
}

type LegacyVendorInventory struct {
	Vendor uint64
	Reason uint8
	Items  []LegacyVendorItem
}

type LegacyBuyResponse struct {
	Vendor         uint64
	Muid           uint32
	NewQuantity    int32
	QuantityBought uint32
	Reason         uint8
}

type LegacySellResponse struct {
	Vendor uint64
	Item   uint64
	Reason uint8
}

func ParseListInventory(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseBuyItem(body []byte) (BuyItemRequest, error) {
	var request BuyItemRequest
	r := movementReader{data: body}
	var err error
	if request.Vendor, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read buy vendor GUID: %w", err)
	}
	if request.Container, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read buy container GUID: %w", err)
	}
	if request.Quantity, err = r.u32(); err != nil {
		return request, fmt.Errorf("read buy quantity: %w", err)
	}
	if request.Muid, err = r.u32(); err != nil {
		return request, fmt.Errorf("read buy Muid: %w", err)
	}
	if request.Slot, err = r.u32(); err != nil {
		return request, fmt.Errorf("read buy slot: %w", err)
	}
	if request.ItemType, err = r.i32(); err != nil {
		return request, fmt.Errorf("read buy item type: %w", err)
	}
	if request.ItemID, err = parseItemInstance(&r); err != nil {
		return request, fmt.Errorf("read buy item instance: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("buy-item has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyBuyItem(request BuyItemRequest, vendor uint64, buyCount uint32) []byte {
	if buyCount == 0 {
		buyCount = 1
	}
	quantity := request.Quantity / buyCount
	if quantity == 0 && request.Quantity != 0 {
		quantity = 1
	}
	body := binary.LittleEndian.AppendUint64(nil, vendor)
	body = binary.LittleEndian.AppendUint32(body, request.ItemID)
	body = binary.LittleEndian.AppendUint32(body, request.Muid)
	body = binary.LittleEndian.AppendUint32(body, quantity)
	return append(body, 0) // WotLK BagSlot; 3.4.3 does not send one
}

func ParseSellItem(body []byte) (SellItemRequest, error) {
	var request SellItemRequest
	r := movementReader{data: body}
	var err error
	if request.Vendor, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read sell vendor GUID: %w", err)
	}
	if request.Item, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read sold item GUID: %w", err)
	}
	if request.Amount, err = r.u32(); err != nil {
		return request, fmt.Errorf("read sell amount: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("sell-item has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacySellItem(request SellItemRequest, vendor, item uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, vendor)
	body = binary.LittleEndian.AppendUint64(body, item)
	return binary.LittleEndian.AppendUint32(body, request.Amount)
}

func ParseBuyBackItem(body []byte) (BuyBackItemRequest, error) {
	var request BuyBackItemRequest
	r := movementReader{data: body}
	var err error
	if request.Vendor, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read buyback vendor GUID: %w", err)
	}
	if request.Slot, err = r.u32(); err != nil {
		return request, fmt.Errorf("read buyback slot: %w", err)
	}
	if request.Slot < 94 || request.Slot > 105 {
		return request, fmt.Errorf("buyback slot %d is outside 94..105", request.Slot)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("buyback-item has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyBuyBackItem(request BuyBackItemRequest, vendor uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, vendor)
	// Build 54261 moved the 12 buyback descriptor slots from 74..85 to 94..105.
	return binary.LittleEndian.AppendUint32(body, request.Slot-20)
}

func ParseLegacyVendorInventory(body []byte) (LegacyVendorInventory, error) {
	var vendor LegacyVendorInventory
	r := movementReader{data: body}
	var err error
	if vendor.Vendor, err = r.u64(); err != nil {
		return vendor, fmt.Errorf("read vendor GUID: %w", err)
	}
	count, err := r.u8()
	if err != nil {
		return vendor, fmt.Errorf("read vendor item count: %w", err)
	}
	if count == 0 {
		if r.remaining() != 0 {
			if vendor.Reason, err = r.u8(); err != nil {
				return vendor, fmt.Errorf("read empty vendor reason: %w", err)
			}
		}
		if r.remaining() != 0 {
			return vendor, fmt.Errorf("empty vendor inventory has %d trailing bytes", r.remaining())
		}
		return vendor, nil
	}
	if int(count) > maxVendorItems {
		return vendor, fmt.Errorf("vendor item count %d exceeds %d", count, maxVendorItems)
	}
	vendor.Items = make([]LegacyVendorItem, 0, count)
	for index := uint8(0); index < count; index++ {
		var item LegacyVendorItem
		if item.Slot, err = r.i32(); err != nil {
			return vendor, fmt.Errorf("read vendor item %d slot: %w", index, err)
		}
		// The server slot is one-based and can be sparse after class, faction,
		// stock or condition filtering. The client echoes Muid when buying.
		item.Muid = uint32(item.Slot)
		if item.ItemID, err = r.u32(); err != nil {
			return vendor, fmt.Errorf("read vendor item %d ID: %w", index, err)
		}
		if _, err = r.u32(); err != nil { // display ID
			return vendor, fmt.Errorf("read vendor item %d display: %w", index, err)
		}
		if item.Quantity, err = r.i32(); err != nil {
			return vendor, fmt.Errorf("read vendor item %d quantity: %w", index, err)
		}
		if item.Price, err = r.u32(); err != nil {
			return vendor, fmt.Errorf("read vendor item %d price: %w", index, err)
		}
		if item.Durability, err = r.i32(); err != nil {
			return vendor, fmt.Errorf("read vendor item %d durability: %w", index, err)
		}
		if item.BuyCount, err = r.u32(); err != nil {
			return vendor, fmt.Errorf("read vendor item %d buy count: %w", index, err)
		}
		if item.ExtendedCost, err = r.i32(); err != nil {
			return vendor, fmt.Errorf("read vendor item %d extended cost: %w", index, err)
		}
		vendor.Items = append(vendor.Items, item)
	}
	if r.remaining() != 0 {
		return vendor, fmt.Errorf("vendor inventory has %d trailing bytes", r.remaining())
	}
	return vendor, nil
}

func EncodeVendorInventory(guid GUID128, vendor LegacyVendorInventory) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = append(body, vendor.Reason)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(vendor.Items)))
	for _, item := range vendor.Items {
		body = binary.LittleEndian.AppendUint64(body, uint64(item.Price))
		body = binary.LittleEndian.AppendUint32(body, item.Muid)
		body = binary.LittleEndian.AppendUint32(body, 1) // item type
		body = binary.LittleEndian.AppendUint32(body, uint32(item.Durability))
		body = binary.LittleEndian.AppendUint32(body, item.BuyCount)
		body = binary.LittleEndian.AppendUint32(body, uint32(item.Quantity))
		body = binary.LittleEndian.AppendUint32(body, uint32(item.ExtendedCost))
		body = binary.LittleEndian.AppendUint32(body, 0) // player condition failed
		bits := newBitWriter(body)
		bits.writeBit(false) // locked
		bits.writeBit(false) // do not filter
		bits.writeBit(false) // refundable
		body = bits.flush()
		body = appendItemInstance(body, item.ItemID, 0, 0)
	}
	return body
}

func ParseLegacyBuySucceeded(body []byte) (LegacyBuyResponse, error) {
	var response LegacyBuyResponse
	r := movementReader{data: body}
	var err error
	if response.Vendor, err = r.u64(); err != nil {
		return response, fmt.Errorf("read buy-success vendor: %w", err)
	}
	if response.Muid, err = r.u32(); err != nil {
		return response, fmt.Errorf("read buy-success Muid: %w", err)
	}
	if response.NewQuantity, err = r.i32(); err != nil {
		return response, fmt.Errorf("read buy-success new quantity: %w", err)
	}
	if response.QuantityBought, err = r.u32(); err != nil {
		return response, fmt.Errorf("read buy-success quantity: %w", err)
	}
	if r.remaining() != 0 {
		return response, fmt.Errorf("buy-success has %d trailing bytes", r.remaining())
	}
	return response, nil
}

func EncodeBuySucceeded(guid GUID128, response LegacyBuyResponse) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, response.Muid)
	body = binary.LittleEndian.AppendUint32(body, uint32(response.NewQuantity))
	return binary.LittleEndian.AppendUint32(body, response.QuantityBought)
}

func ParseLegacyBuyFailed(body []byte) (LegacyBuyResponse, error) {
	var response LegacyBuyResponse
	r := movementReader{data: body}
	var err error
	if response.Vendor, err = r.u64(); err != nil {
		return response, fmt.Errorf("read buy-failed vendor: %w", err)
	}
	if response.Muid, err = r.u32(); err != nil {
		return response, fmt.Errorf("read buy-failed Muid: %w", err)
	}
	if response.Reason, err = r.u8(); err != nil {
		return response, fmt.Errorf("read buy-failed reason: %w", err)
	}
	if r.remaining() != 0 {
		return response, fmt.Errorf("buy-failed has %d trailing bytes", r.remaining())
	}
	return response, nil
}

func EncodeBuyFailed(guid GUID128, response LegacyBuyResponse) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, response.Muid)
	return append(body, response.Reason)
}

func ParseLegacySellResponse(body []byte) (LegacySellResponse, error) {
	var response LegacySellResponse
	r := movementReader{data: body}
	var err error
	if response.Vendor, err = r.u64(); err != nil {
		return response, fmt.Errorf("read sell-response vendor: %w", err)
	}
	if response.Item, err = r.u64(); err != nil {
		return response, fmt.Errorf("read sell-response item: %w", err)
	}
	if response.Reason, err = r.u8(); err != nil {
		return response, fmt.Errorf("read sell-response reason: %w", err)
	}
	if r.remaining() != 0 {
		return response, fmt.Errorf("sell-response has %d trailing bytes", r.remaining())
	}
	return response, nil
}

func EncodeSellResponse(vendor, item GUID128, response LegacySellResponse) []byte {
	body := appendPackedGUID128(nil, vendor.Low, vendor.High)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, uint32(response.Reason))
	return appendPackedGUID128(body, item.Low, item.High)
}
