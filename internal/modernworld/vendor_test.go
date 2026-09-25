package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestBuyAndSellRequests(t *testing.T) {
	vendor := GUID128{Low: 0x42, High: 8 << 58}
	container := GUID128{}
	body := appendPackedGUID128(nil, vendor.Low, vendor.High)
	body = appendPackedGUID128(body, container.Low, container.High)
	body = binary.LittleEndian.AppendUint32(body, 20)
	body = binary.LittleEndian.AppendUint32(body, 3)
	body = binary.LittleEndian.AppendUint32(body, 2)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = appendItemInstance(body, 117, 0, 0)
	request, err := ParseBuyItem(body)
	if err != nil || request.ItemID != 117 || request.Quantity != 20 || request.Muid != 3 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	legacy := EncodeLegacyBuyItem(request, 0xf130001234000042, 5)
	if len(legacy) != 21 || binary.LittleEndian.Uint32(legacy[16:20]) != 4 {
		t.Fatalf("legacy buy=%x", legacy)
	}

	item := GUID128{Low: 0x99, High: 3 << 58}
	sellBody := appendPackedGUID128(nil, vendor.Low, vendor.High)
	sellBody = appendPackedGUID128(sellBody, item.Low, item.High)
	sellBody = binary.LittleEndian.AppendUint32(sellBody, 2)
	sell, err := ParseSellItem(sellBody)
	if err != nil || sell.Amount != 2 {
		t.Fatalf("sell=%+v err=%v", sell, err)
	}
	legacySell := EncodeLegacySellItem(sell, 0xf130001234000042, 0x4000000000000099)
	if len(legacySell) != 20 || binary.LittleEndian.Uint32(legacySell[16:]) != 2 {
		t.Fatalf("legacy sell=%x", legacySell)
	}

	buybackBody := appendPackedGUID128(nil, vendor.Low, vendor.High)
	buybackBody = binary.LittleEndian.AppendUint32(buybackBody, 94)
	buyback, err := ParseBuyBackItem(buybackBody)
	if err != nil || buyback.Slot != 94 || buyback.Vendor != vendor {
		t.Fatalf("buyback=%+v err=%v", buyback, err)
	}
	legacyBuyback := EncodeLegacyBuyBackItem(buyback, 0xf130001234000042)
	if len(legacyBuyback) != 12 || binary.LittleEndian.Uint32(legacyBuyback[8:]) != 74 {
		t.Fatalf("legacy buyback=%x", legacyBuyback)
	}
}

func TestVendorInventoryAndResponses(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130001234000042)
	legacy = append(legacy, 1)
	for _, value := range []uint32{1, 117, 999, 20, 500, 35, 5, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	vendor, err := ParseLegacyVendorInventory(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(vendor.Items) != 1 || vendor.Items[0].ItemID != 117 || vendor.Items[0].BuyCount != 5 || vendor.Items[0].Muid != 1 {
		t.Fatalf("vendor=%+v", vendor)
	}
	guid := ModernGUIDForLegacy(vendor.Vendor, 0)
	modern := EncodeVendorInventory(guid, vendor)
	r := movementReader{data: modern}
	got, err := r.guid128()
	if err != nil || got != guid {
		t.Fatalf("guid=%+v err=%v", got, err)
	}
	if reason, _ := r.u8(); reason != 0 {
		t.Fatalf("reason=%d", reason)
	}
	if count, _ := r.u32(); count != 1 {
		t.Fatalf("count=%d", count)
	}

	buySuccess := binary.LittleEndian.AppendUint64(nil, vendor.Vendor)
	buySuccess = binary.LittleEndian.AppendUint32(buySuccess, 1)
	buySuccess = binary.LittleEndian.AppendUint32(buySuccess, 15)
	buySuccess = binary.LittleEndian.AppendUint32(buySuccess, 5)
	response, err := ParseLegacyBuySucceeded(buySuccess)
	if err != nil || response.NewQuantity != 15 || response.QuantityBought != 5 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if got := EncodeBuySucceeded(guid, response); len(got) < 12 {
		t.Fatalf("buy-success=%x", got)
	}
}

// Class-filtered vendors retain their original slots, not their display indices.
// Exercise the complete list -> client selection -> legacy purchase path.
func TestFilteredVendorPurchasePreservesServerSlot(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130001234000042)
	legacy = append(legacy, 3)
	items := []struct{ slot, id uint32 }{{7, 34443}, {19, 34549}, {28, 34570}}
	for _, item := range items {
		for _, value := range []uint32{item.slot, item.id, 999, 0xffffffff, 0, 80, 1, 1} {
			legacy = binary.LittleEndian.AppendUint32(legacy, value)
		}
	}
	vendor, err := ParseLegacyVendorInventory(legacy)
	if err != nil {
		t.Fatal(err)
	}
	guid := ModernGUIDForLegacy(vendor.Vendor, 0)
	r := movementReader{data: EncodeVendorInventory(guid, vendor)}
	if _, err := r.guid128(); err != nil {
		t.Fatal(err)
	}
	r.u8()
	r.u32()
	for _, item := range items {
		r.u64() // price
		muid, err := r.u32()
		if err != nil || muid != item.slot {
			t.Fatalf("item %d: merchant ID=%d, want server slot %d: %v", item.id, muid, item.slot, err)
		}
		for i := 0; i < 6; i++ {
			r.u32()
		}
		r.u8() // flags
		id, err := parseItemInstance(&r)
		if err != nil || id != item.id {
			t.Fatalf("item=%d err=%v", id, err)
		}
		body := appendPackedGUID128(nil, guid.Low, guid.High)
		body = appendPackedGUID128(body, 0, 0)
		for _, value := range []uint32{1, muid, 0xffffffff, 1} {
			body = binary.LittleEndian.AppendUint32(body, value)
		}
		body = appendItemInstance(body, id, 0, 0)
		request, err := ParseBuyItem(body)
		if err != nil {
			t.Fatal(err)
		}
		purchase := EncodeLegacyBuyItem(request, vendor.Vendor, 1)
		if got := binary.LittleEndian.Uint32(purchase[12:16]); got != item.slot {
			t.Fatalf("legacy purchase slot=%d want=%d", got, item.slot)
		}
	}
}
