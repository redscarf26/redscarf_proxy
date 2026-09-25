package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAdjustInventorySlot(t *testing.T) {
	cases := []struct {
		in, want uint8
	}{
		{0xFF, 0xFF},
		{5, 5},
		{18, 18},
		{30, 19},
		{33, 22},
		{35, 23},
		{50, 38},
		{59, 39},
		{86, 66},
		{87, 67},
		{94, 74},
		{106, 86},
	}
	for _, test := range cases {
		if got := AdjustInventorySlot(test.in); got != test.want {
			t.Fatalf("slot %d -> %d, want %d", test.in, got, test.want)
		}
	}
}

func TestParseAndEncodeRepairItem(t *testing.T) {
	vendor := GUID128{Low: 0x42, High: 1}
	item := GUID128{Low: 0x51, High: 2}
	body := appendPackedGUID128(nil, vendor.Low, vendor.High)
	body = appendPackedGUID128(body, item.Low, item.High)
	bits := newBitWriter(body)
	bits.writeBit(true)
	body = bits.flush()
	request, err := ParseRepairItem(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Vendor != vendor || request.Item != item || !request.UseGuildBank {
		t.Fatalf("request %#v", request)
	}
	legacy := EncodeLegacyRepairItem(request, 0xf130000000000042, 0x4000000000000051)
	if len(legacy) != 17 || binary.LittleEndian.Uint64(legacy[:8]) != 0xf130000000000042 || binary.LittleEndian.Uint64(legacy[8:16]) != 0x4000000000000051 || legacy[16] != 1 {
		t.Fatalf("legacy %x", legacy)
	}
}

func TestParseSwapItemBackpack(t *testing.T) {
	body := []byte{0x00, 0xFF, 0xFF, 35, 36}
	swap, err := ParseSwapItem(body)
	if err != nil {
		t.Fatal(err)
	}
	if swap != (InventorySwap{DstBag: 0xFF, DstSlot: 23, SrcBag: 0xFF, SrcSlot: 24}) {
		t.Fatalf("swap=%#v", swap)
	}
	legacy := EncodeLegacySwapItem(swap)
	if len(legacy) != 4 || legacy[0] != 0xFF || legacy[1] != 23 || legacy[2] != 0xFF || legacy[3] != 24 {
		t.Fatalf("legacy %x", legacy)
	}
}

func TestParseSwapItemWithInvUpdate(t *testing.T) {
	body := []byte{0x80, 0xFF, 35, 0xFF, 36, 0xFF, 0xFF, 35, 36}
	swap, err := ParseSwapItem(body)
	if err != nil {
		t.Fatal(err)
	}
	if swap.DstSlot != 23 || swap.SrcSlot != 24 || swap.DstBag != 0xFF {
		t.Fatalf("swap=%#v", swap)
	}
}

func TestParseSwapInvItem(t *testing.T) {
	body := []byte{0x00, 36, 35}
	swap, err := ParseSwapInvItem(body)
	if err != nil {
		t.Fatal(err)
	}
	if swap != (InventoryInvSwap{SrcSlot: 23, DstSlot: 24}) {
		t.Fatalf("swap=%#v", swap)
	}
	legacy := EncodeLegacySwapInvItem(swap)
	if len(legacy) != 2 || legacy[0] != 24 || legacy[1] != 23 {
		t.Fatalf("legacy %x", legacy)
	}
}

func TestParseSwapItemRejectsTrailing(t *testing.T) {
	if _, err := ParseSwapItem([]byte{0x00, 0xFF, 0xFF, 35}); err == nil {
		t.Fatal("expected short swap-item error")
	}
	if _, err := ParseSwapItem([]byte{0x00, 0xFF, 0xFF, 35, 36, 0}); err == nil {
		t.Fatal("expected trailing swap-item error")
	}
}

func TestParseAutoEquipItems(t *testing.T) {
	// Empty InvUpdate is one zero-padded bit byte.
	item, err := ParseAutoEquipItem([]byte{0, 0xff, 35})
	if err != nil || item.PackSlot != 0xff || item.Slot != 23 {
		t.Fatalf("item=%+v err=%v", item, err)
	}
	legacy := EncodeLegacyAutoEquipItem(item)
	if string(legacy) != string([]byte{0xff, 23}) {
		t.Fatalf("legacy=%x", legacy)
	}

	guid := GUID128{Low: 0x42, High: 3 << 58}
	body := []byte{0}
	body = appendPackedGUID128(body, guid.Low, guid.High)
	body = append(body, 35)
	slot, err := ParseAutoEquipItemSlot(body)
	if err != nil || slot.Item != guid || slot.DstSlot != 23 {
		t.Fatalf("slot=%+v err=%v", slot, err)
	}
	legacySlot := EncodeLegacyAutoEquipItemSlot(0x4000000000000042, slot.DstSlot)
	if len(legacySlot) != 9 || legacySlot[8] != 23 {
		t.Fatalf("legacy slot=%x", legacySlot)
	}
}

func TestParseAndEncodeSetAmmo(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 0x12345678)
	itemID, err := ParseSetAmmo(body)
	if err != nil || itemID != 0x12345678 {
		t.Fatalf("item=%#x err=%v", itemID, err)
	}
	if legacy := EncodeLegacySetAmmo(itemID); string(legacy) != string(body) {
		t.Fatalf("legacy set-ammo=%x", legacy)
	}

	if itemID, err = ParseSetAmmo([]byte{0, 0, 0, 0}); err != nil || itemID != 0 {
		t.Fatalf("remove ammo item=%#x err=%v", itemID, err)
	}
	if _, err = ParseSetAmmo([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected short set-ammo error")
	}
	if _, err = ParseSetAmmo([]byte{1, 2, 3, 4, 5}); err == nil {
		t.Fatal("expected trailing set-ammo error")
	}
}

func TestParseDestroyItem(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 0)
	body = append(body, 0xff, 35)
	request, err := ParseDestroyItem(body)
	if err != nil {
		t.Fatal(err)
	}
	if request != (DestroyItemRequest{Container: 0xff, Slot: 23}) {
		t.Fatalf("request=%+v", request)
	}
	if legacy := EncodeLegacyDestroyItem(request); string(legacy) != string([]byte{0xff, 23, 0, 0, 0, 0}) {
		t.Fatalf("legacy destroy=%x", legacy)
	}

	stack := binary.LittleEndian.AppendUint32(nil, 3)
	stack = append(stack, 35, 4)
	request, err = ParseDestroyItem(stack)
	if err != nil || request != (DestroyItemRequest{Count: 3, Container: 23, Slot: 4}) {
		t.Fatalf("stack request=%+v err=%v", request, err)
	}
	if _, err := ParseDestroyItem(append(body, 0)); err == nil {
		t.Fatal("expected trailing destroy-item error")
	}
	tooMany := binary.LittleEndian.AppendUint32(nil, 256)
	if _, err := ParseDestroyItem(append(tooMany, 0xff, 35)); err == nil {
		t.Fatal("expected oversized destroy-item count error")
	}
}

func TestParseAndEncodeOpenItem(t *testing.T) {
	// Right-clicking a container sat in the main backpack: modern slot 40 is a
	// backpack item, so the legacy body carries 0xFF plus the adjusted slot.
	request, err := ParseOpenItem([]byte{0xff, 40})
	if err != nil || request != (OpenItemRequest{PackSlot: 0xff, Slot: 40}) {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	if legacy := EncodeLegacyOpenItem(request); string(legacy) != string([]byte{0xff, 28}) {
		t.Fatalf("legacy open (backpack)=%x, want ff1c", legacy)
	}
	// A container inside an equipped bag keeps bag 0..3 and its slot untouched.
	bagged, err := ParseOpenItem([]byte{2, 5})
	if err != nil || bagged != (OpenItemRequest{PackSlot: 2, Slot: 5}) {
		t.Fatalf("bagged=%+v err=%v", bagged, err)
	}
	if legacy := EncodeLegacyOpenItem(bagged); string(legacy) != string([]byte{2, 5}) {
		t.Fatalf("legacy open (bag)=%x, want 0205", legacy)
	}
	if _, err := ParseOpenItem([]byte{0xff}); err == nil {
		t.Fatal("expected short open-item error")
	}
	if _, err := ParseOpenItem([]byte{0xff, 1, 2}); err == nil {
		t.Fatal("expected trailing open-item error")
	}
}

func TestInventoryChangeFailure(t *testing.T) {
	body := []byte{1}
	body = binary.LittleEndian.AppendUint64(body, 0x4000000000000042)
	body = binary.LittleEndian.AppendUint64(body, 0)
	body = append(body, 0xff)
	body = binary.LittleEndian.AppendUint32(body, 80)
	failure, err := ParseLegacyInventoryChangeFailure(body)
	if err != nil || failure.Result != 1 || !failure.HasExtra || failure.Extra != 80 {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
	item := ModernGUIDForLegacy(failure.ItemA, 0)
	modern := EncodeInventoryChangeFailure(failure, item, GUID128{}, GUID128{}, GUID128{})
	if binary.LittleEndian.Uint32(modern[:4]) != 1 {
		t.Fatalf("modern failure=%x", modern)
	}
}

func TestInventoryChangeFailureMapsLegacyInventoryFull(t *testing.T) {
	body := []byte{50} // WotLK EQUIP_ERR_INVENTORY_FULL
	body = binary.LittleEndian.AppendUint64(body, 0)
	body = binary.LittleEndian.AppendUint64(body, 0)
	body = append(body, 0xff)
	failure, err := ParseLegacyInventoryChangeFailure(body)
	if err != nil {
		t.Fatal(err)
	}
	modern := EncodeInventoryChangeFailure(failure, GUID128{}, GUID128{}, GUID128{}, GUID128{})
	if got := int32(binary.LittleEndian.Uint32(modern)); got != 51 {
		t.Fatalf("modern inventory-full result=%d, want 51", got)
	}
	if ModernInventoryResult(29) != 30 || ModernInventoryResult(72) != 73 || ModernInventoryResult(84) != 85 {
		t.Fatal("legacy inventory result shift is incomplete")
	}
}

func TestInventoryChangeFailureLegacyExtraLayouts(t *testing.T) {
	for _, result := range []byte{84, 85, 87, 89} {
		body := []byte{result}
		body = binary.LittleEndian.AppendUint64(body, 0)
		body = binary.LittleEndian.AppendUint64(body, 0)
		body = append(body, 0xff)
		body = binary.LittleEndian.AppendUint32(body, 7)
		failure, err := ParseLegacyInventoryChangeFailure(body)
		if err != nil || !failure.HasExtra || failure.Extra != 7 {
			t.Fatalf("result=%d failure=%+v err=%v", result, failure, err)
		}
	}
}

func TestParseSplitItem(t *testing.T) {
	body := []byte{0x00, 0x04, 0x05, 0x0B, 0x06}
	body = binary.LittleEndian.AppendUint32(body, 7)
	split, err := ParseSplitItem(body)
	if err != nil {
		t.Fatal(err)
	}
	want := SplitItemRequest{SrcPack: 0x04, SrcSlot: 0x05, DstPack: 0x0B, DstSlot: 0x06, Quantity: 7}
	if split != want {
		t.Fatalf("split=%+v want=%+v", split, want)
	}
	wantBody := []byte{0x04, 0x05, 0x0B, 0x06, 7, 0, 0, 0}
	if legacy := EncodeLegacySplitItem(split); !bytes.Equal(legacy, wantBody) {
		t.Fatalf("legacy split=%x want=%x", legacy, wantBody)
	}

	// A main-backpack source slot is remapped.
	backpack, err := ParseSplitItem([]byte{0x00, 0xFF, 35, 0x0B, 0x06, 1, 0, 0, 0})
	if err != nil || backpack.SrcPack != 0xFF || backpack.SrcSlot != 23 {
		t.Fatalf("backpack split=%+v err=%v", backpack, err)
	}

	// Zero or negative quantities are rejected.
	if _, err := ParseSplitItem([]byte{0x00, 0x04, 0x05, 0x0B, 0x06, 0, 0, 0, 0}); err == nil {
		t.Fatal("expected zero split quantity to error")
	}
	if _, err := ParseSplitItem([]byte{0x00, 0x04, 0x05, 0x0B, 0x06}); err == nil {
		t.Fatal("expected truncated split quantity to error")
	}
}

func TestParseAutoStoreBagItem(t *testing.T) {
	// Modern serializes destination container first, then source container, slot.
	store, err := ParseAutoStoreBagItem([]byte{0x00, 0x04, 0x03, 0x05})
	if err != nil {
		t.Fatal(err)
	}
	if store.SrcPack != 0x03 || store.SrcSlot != 0x05 || store.DstPack != 0x04 {
		t.Fatalf("auto-store=%+v", store)
	}
	if legacy := EncodeLegacyAutoStoreBagItem(store); !bytes.Equal(legacy, []byte{0x03, 0x05, 0x04}) {
		t.Fatalf("legacy auto-store=%x", legacy)
	}
	// Container indices in the 3.4 remap window are adjusted to WotLK.
	remapped, err := ParseAutoStoreBagItem([]byte{0x00, 0x2F, 0x25, 0x02})
	if err != nil || remapped.DstPack != 0x23 || remapped.SrcPack != 0x19 {
		t.Fatalf("remapped auto-store=%+v err=%v", remapped, err)
	}
}

func TestParseWrapItem(t *testing.T) {
	wrapped, err := ParseWrapItem([]byte{0x00, 0x02, 0x03, 0x04, 0x05})
	if err != nil {
		t.Fatal(err)
	}
	if wrapped.GiftPack != 0x02 || wrapped.GiftSlot != 0x03 || wrapped.ItemPack != 0x04 || wrapped.ItemSlot != 0x05 {
		t.Fatalf("wrap=%+v", wrapped)
	}
	if legacy := EncodeLegacyWrapItem(wrapped); !bytes.Equal(legacy, []byte{0x02, 0x03, 0x04, 0x05}) {
		t.Fatalf("legacy wrap=%x", legacy)
	}
	if _, err := ParseWrapItem([]byte{0x00, 0x02, 0x03, 0x04}); err == nil {
		t.Fatal("expected truncated wrap body to error")
	}
}

func TestParseCancelTempEnchantment(t *testing.T) {
	slot, err := ParseCancelTempEnchantment(binary.LittleEndian.AppendUint32(nil, 3))
	if err != nil || slot != 3 {
		t.Fatalf("slot=%d err=%v", slot, err)
	}
	if _, err := ParseCancelTempEnchantment([]byte{1, 2}); err == nil {
		t.Fatal("expected short cancel-temp body to error")
	}
}

func TestSocketGemsTranslation(t *testing.T) {
	item := GUID128{Low: 0x681, High: 1}
	gem := GUID128{Low: 0x99, High: 1}
	body := appendPackedGUID128(nil, item.Low, item.High)
	body = appendPackedGUID128(body, gem.Low, gem.High)
	body = appendPackedGUID128(body, 0, 0)
	body = appendPackedGUID128(body, 0, 0)

	request, err := ParseSocketGems(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Item != item || request.Gems[0] != gem || request.Gems[1] != (GUID128{}) || request.Gems[2] != (GUID128{}) {
		t.Fatalf("request=%+v", request)
	}

	legacy := EncodeLegacySocketGems(0x4000000000000681, [3]uint64{0x4000000000000099, 0, 0})
	if len(legacy) != 32 {
		t.Fatalf("legacy socket-gems has %d bytes", len(legacy))
	}
	if binary.LittleEndian.Uint64(legacy[0:8]) != 0x4000000000000681 || binary.LittleEndian.Uint64(legacy[8:16]) != 0x4000000000000099 {
		t.Fatalf("legacy socket-gems=%x", legacy)
	}

	success := EncodeSocketGemsSuccess(item)
	got, err := ParsePackedGUID128Exact(success)
	if err != nil || got != item {
		t.Fatalf("success=%#v err=%v", got, err)
	}
	if _, err := ParseSocketGems(body[:len(body)-1]); err == nil {
		t.Fatal("expected truncated socket-gems body to error")
	}
}
