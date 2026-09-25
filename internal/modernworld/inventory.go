package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGSwapItem          = uint16(0x399A)
	CMSGSwapInvItem       = uint16(0x399B)
	CMSGAutoEquipItem     = uint16(0x3998)
	CMSGAutoEquipItemSlot = uint16(0x399D)
	CMSGSetAmmo           = uint16(0x355E)

	// Bank deposit/withdraw carry the same container+slot body as auto-equip but
	// are routed to the dedicated WotLK opcodes.
	CMSGAutobankItem          = uint16(14743) // 0x3997
	CMSGAutostoreBankItem     = uint16(14742) // 0x3996
	CMSGAutoStoreBagItem      = uint16(14745) // 0x3999
	CMSGSplitItem             = uint16(14748) // 0x399C
	CMSGWrapItem              = uint16(14740) // 0x3994
	CMSGCancelTempEnchantment = uint16(13554)
	CMSGDestroyItem           = uint16(0x3293)
	CMSGRepairItem            = uint16(0x34EC)
	CMSGOpenItem              = uint16(0x32C6)
	CMSGSocketGems            = uint16(13547) // 0x34EB
	SMSGSocketGemsSuccess     = uint16(10023) // 0x2727

	SMSGInventoryChangeFailure = uint16(0x2DA5)
)

type InventorySwap struct {
	DstBag  uint8
	DstSlot uint8
	SrcBag  uint8
	SrcSlot uint8
}

type InventoryInvSwap struct {
	SrcSlot uint8
	DstSlot uint8
}

type AutoEquipItemRequest struct {
	PackSlot uint8
	Slot     uint8
}

type AutoEquipItemSlotRequest struct {
	Item    GUID128
	DstSlot uint8
}

type DestroyItemRequest struct {
	Count     uint32
	Container uint8
	Slot      uint8
}

// OpenItemRequest is the build-54261 CMSG_OPEN_ITEM body. Right-clicking a
// bagged container (a chest, a wrapped present, a locked box) sends the bag or
// backpack index in PackSlot and the item's slot in Slot. A PackSlot of 0xFF
// means the main backpack.
type OpenItemRequest struct {
	PackSlot uint8
	Slot     uint8
}

type RepairItemRequest struct {
	Vendor       GUID128
	Item         GUID128
	UseGuildBank bool
}

type SocketGemsRequest struct {
	Item GUID128
	Gems [3]GUID128
}

type InventoryChangeFailure struct {
	Result         int32
	ItemA          uint64
	ItemB          uint64
	ContainerBSlot uint8
	Extra          int32
	HasExtra       bool
	SrcContainer   uint64
	SrcSlot        int32
	DstContainer   uint64
	HasBindConfirm bool
}

func skipInvUpdate(r *movementReader) error {
	count, err := r.bits(2)
	if err != nil {
		return fmt.Errorf("read inv-update count: %w", err)
	}
	r.align()
	for index := uint32(0); index < count; index++ {
		if _, err := r.u8(); err != nil {
			return fmt.Errorf("read inv-update %d container: %w", index, err)
		}
		if _, err := r.u8(); err != nil {
			return fmt.Errorf("read inv-update %d slot: %w", index, err)
		}
	}
	return nil
}

// AdjustInventorySlot is legacy proxy spell.AdjustInventorySlot for expansion >= 80:
// 3.4 bag/pack/bank indices down to WotLK 3.3.5 slots. 0xFF stays 0xFF.
func AdjustInventorySlot(slot uint8) uint8 {
	value := int(slot)
	switch {
	case value >= 30 && value <= 33:
		return uint8(value - 11)
	case value >= 35 && value <= 58:
		return uint8(value - 12)
	case value >= 59 && value <= 137:
		return uint8(value - 20)
	default:
		return slot
	}
}

func ParseSwapItem(body []byte) (InventorySwap, error) {
	r := movementReader{data: body}
	if err := skipInvUpdate(&r); err != nil {
		return InventorySwap{}, err
	}
	dstBag, err := r.u8()
	if err != nil {
		return InventorySwap{}, fmt.Errorf("read swap dest bag: %w", err)
	}
	srcBag, err := r.u8()
	if err != nil {
		return InventorySwap{}, fmt.Errorf("read swap source bag: %w", err)
	}
	dstSlot, err := r.u8()
	if err != nil {
		return InventorySwap{}, fmt.Errorf("read swap dest slot: %w", err)
	}
	srcSlot, err := r.u8()
	if err != nil {
		return InventorySwap{}, fmt.Errorf("read swap source slot: %w", err)
	}
	if r.remaining() != 0 {
		return InventorySwap{}, fmt.Errorf("swap-item has %d trailing bytes", r.remaining())
	}
	return InventorySwap{
		DstBag:  AdjustInventorySlot(dstBag),
		DstSlot: AdjustInventorySlot(dstSlot),
		SrcBag:  AdjustInventorySlot(srcBag),
		SrcSlot: AdjustInventorySlot(srcSlot),
	}, nil
}

func ParseSwapInvItem(body []byte) (InventoryInvSwap, error) {
	r := movementReader{data: body}
	if err := skipInvUpdate(&r); err != nil {
		return InventoryInvSwap{}, err
	}
	dstSlot, err := r.u8()
	if err != nil {
		return InventoryInvSwap{}, fmt.Errorf("read swap-inv dest: %w", err)
	}
	srcSlot, err := r.u8()
	if err != nil {
		return InventoryInvSwap{}, fmt.Errorf("read swap-inv source: %w", err)
	}
	if r.remaining() != 0 {
		return InventoryInvSwap{}, fmt.Errorf("swap-inv-item has %d trailing bytes", r.remaining())
	}
	return InventoryInvSwap{
		SrcSlot: AdjustInventorySlot(srcSlot),
		DstSlot: AdjustInventorySlot(dstSlot),
	}, nil
}

func EncodeLegacySwapItem(swap InventorySwap) []byte {
	return []byte{swap.DstBag, swap.DstSlot, swap.SrcBag, swap.SrcSlot}
}

func EncodeLegacySwapInvItem(swap InventoryInvSwap) []byte {
	// WotLK HandleSwapInvItemOpcode reads destination first, then source.
	return []byte{swap.DstSlot, swap.SrcSlot}
}

func ParseAutoEquipItem(body []byte) (AutoEquipItemRequest, error) {
	r := movementReader{data: body}
	if err := skipInvUpdate(&r); err != nil {
		return AutoEquipItemRequest{}, err
	}
	packSlot, err := r.u8()
	if err != nil {
		return AutoEquipItemRequest{}, fmt.Errorf("read auto-equip pack slot: %w", err)
	}
	slot, err := r.u8()
	if err != nil {
		return AutoEquipItemRequest{}, fmt.Errorf("read auto-equip slot: %w", err)
	}
	if r.remaining() != 0 {
		return AutoEquipItemRequest{}, fmt.Errorf("auto-equip-item has %d trailing bytes", r.remaining())
	}
	if packSlot != 0xff {
		packSlot = AdjustInventorySlot(packSlot)
	} else {
		slot = AdjustInventorySlot(slot)
	}
	return AutoEquipItemRequest{PackSlot: packSlot, Slot: slot}, nil
}

func EncodeLegacyAutoEquipItem(item AutoEquipItemRequest) []byte {
	return []byte{item.PackSlot, item.Slot}
}

// adjustInvSlotPair remaps a modern container/slot pair to its WotLK slots:
// when the container is the main backpack (0xFF) the slot itself is remapped,
// otherwise only the container index is remapped.
func adjustInvSlotPair(pack, slot uint8) (uint8, uint8) {
	if pack != 0xff {
		return AdjustInventorySlot(pack), slot
	}
	return pack, AdjustInventorySlot(slot)
}

type SplitItemRequest struct {
	SrcPack, SrcSlot uint8
	DstPack, DstSlot uint8
	Quantity         uint32
}

func ParseSplitItem(body []byte) (SplitItemRequest, error) {
	var request SplitItemRequest
	r := movementReader{data: body}
	if err := skipInvUpdate(&r); err != nil {
		return request, err
	}
	var err error
	if request.SrcPack, err = r.u8(); err != nil {
		return request, fmt.Errorf("read split source pack: %w", err)
	}
	if request.SrcSlot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read split source slot: %w", err)
	}
	if request.DstPack, err = r.u8(); err != nil {
		return request, fmt.Errorf("read split dest pack: %w", err)
	}
	if request.DstSlot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read split dest slot: %w", err)
	}
	quantity, err := r.i32()
	if err != nil {
		return request, fmt.Errorf("read split quantity: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("split-item has %d trailing bytes", r.remaining())
	}
	if quantity <= 0 {
		return request, fmt.Errorf("split quantity %d is not positive", quantity)
	}
	request.SrcPack, request.SrcSlot = adjustInvSlotPair(request.SrcPack, request.SrcSlot)
	request.DstPack, request.DstSlot = adjustInvSlotPair(request.DstPack, request.DstSlot)
	request.Quantity = uint32(quantity)
	return request, nil
}

func EncodeLegacySplitItem(request SplitItemRequest) []byte {
	body := []byte{request.SrcPack, request.SrcSlot, request.DstPack, request.DstSlot}
	return binary.LittleEndian.AppendUint32(body, request.Quantity)
}

// AutoStoreBagItemRequest moves an item from a source bag/slot into a
// destination bag at an auto-chosen slot (WotLK CMSG_AUTOSTORE_BAG_ITEM).
type AutoStoreBagItemRequest struct {
	SrcPack uint8
	SrcSlot uint8
	DstPack uint8
}

func ParseAutoStoreBagItem(body []byte) (AutoStoreBagItemRequest, error) {
	var request AutoStoreBagItemRequest
	r := movementReader{data: body}
	if err := skipInvUpdate(&r); err != nil {
		return request, err
	}
	// The modern packet serializes the destination container before the source.
	dstPack, err := r.u8()
	if err != nil {
		return request, fmt.Errorf("read auto-store destination: %w", err)
	}
	srcPack, err := r.u8()
	if err != nil {
		return request, fmt.Errorf("read auto-store source: %w", err)
	}
	slot, err := r.u8()
	if err != nil {
		return request, fmt.Errorf("read auto-store slot: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("auto-store-bag-item has %d trailing bytes", r.remaining())
	}
	request.DstPack = AdjustInventorySlot(dstPack)
	request.SrcPack = AdjustInventorySlot(srcPack)
	request.SrcSlot = slot
	return request, nil
}

func EncodeLegacyAutoStoreBagItem(request AutoStoreBagItemRequest) []byte {
	return []byte{request.SrcPack, request.SrcSlot, request.DstPack}
}

// WrapItemRequest identifies the gift-wrap paper and the item being wrapped.
type WrapItemRequest struct {
	GiftPack, GiftSlot uint8
	ItemPack, ItemSlot uint8
}

func ParseWrapItem(body []byte) (WrapItemRequest, error) {
	var request WrapItemRequest
	r := movementReader{data: body}
	if _, err := r.u8(); err != nil {
		return request, fmt.Errorf("read wrap-item header: %w", err)
	}
	var err error
	if request.GiftPack, err = r.u8(); err != nil {
		return request, fmt.Errorf("read gift pack: %w", err)
	}
	if request.GiftSlot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read gift slot: %w", err)
	}
	if request.ItemPack, err = r.u8(); err != nil {
		return request, fmt.Errorf("read item pack: %w", err)
	}
	if request.ItemSlot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read item slot: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("wrap-item has %d trailing bytes", r.remaining())
	}
	request.GiftPack, request.GiftSlot = adjustInvSlotPair(request.GiftPack, request.GiftSlot)
	request.ItemPack, request.ItemSlot = adjustInvSlotPair(request.ItemPack, request.ItemSlot)
	return request, nil
}

func EncodeLegacyWrapItem(request WrapItemRequest) []byte {
	return []byte{request.GiftPack, request.GiftSlot, request.ItemPack, request.ItemSlot}
}

// ParseCancelTempEnchantment reads the uint32 weapon-enchant slot id.
func ParseCancelTempEnchantment(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("cancel-temp-enchantment has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func ParseSocketGems(body []byte) (SocketGemsRequest, error) {
	var request SocketGemsRequest
	r := movementReader{data: body}
	item, err := r.guid128()
	if err != nil {
		return request, fmt.Errorf("read socket-gems item GUID: %w", err)
	}
	request.Item = item
	for index := range request.Gems {
		gem, gemErr := r.guid128()
		if gemErr != nil {
			return request, fmt.Errorf("read socket-gems gem %d GUID: %w", index, gemErr)
		}
		request.Gems[index] = gem
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("socket-gems has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacySocketGems(item uint64, gems [3]uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, item)
	for _, gem := range gems {
		body = binary.LittleEndian.AppendUint64(body, gem)
	}
	return body
}

func EncodeSocketGemsSuccess(item GUID128) []byte {
	return appendPackedGUID128(nil, item.Low, item.High)
}

func ParseAutoEquipItemSlot(body []byte) (AutoEquipItemSlotRequest, error) {
	r := movementReader{data: body}
	if err := skipInvUpdate(&r); err != nil {
		return AutoEquipItemSlotRequest{}, err
	}
	item, err := r.guid128()
	if err != nil {
		return AutoEquipItemSlotRequest{}, fmt.Errorf("read auto-equip item GUID: %w", err)
	}
	dstSlot, err := r.u8()
	if err != nil {
		return AutoEquipItemSlotRequest{}, fmt.Errorf("read auto-equip destination: %w", err)
	}
	if r.remaining() != 0 {
		return AutoEquipItemSlotRequest{}, fmt.Errorf("auto-equip-item-slot has %d trailing bytes", r.remaining())
	}
	return AutoEquipItemSlotRequest{Item: item, DstSlot: AdjustInventorySlot(dstSlot)}, nil
}

func EncodeLegacyAutoEquipItemSlot(itemGUID uint64, dstSlot uint8) []byte {
	body := binary.LittleEndian.AppendUint64(nil, itemGUID)
	return append(body, dstSlot)
}

func ParseSetAmmo(body []byte) (uint32, error) {
	r := movementReader{data: body}
	itemID, err := r.u32()
	if err != nil {
		return 0, fmt.Errorf("read set-ammo item ID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("set-ammo has %d trailing bytes", r.remaining())
	}
	return itemID, nil
}

func EncodeLegacySetAmmo(itemID uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, itemID)
}

func ParseDestroyItem(body []byte) (DestroyItemRequest, error) {
	var request DestroyItemRequest
	r := movementReader{data: body}
	var err error
	if request.Count, err = r.u32(); err != nil {
		return request, fmt.Errorf("read destroy-item count: %w", err)
	}
	if request.Container, err = r.u8(); err != nil {
		return request, fmt.Errorf("read destroy-item container: %w", err)
	}
	if request.Slot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read destroy-item slot: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("destroy-item has %d trailing bytes", r.remaining())
	}
	if request.Count > 0xff {
		return request, fmt.Errorf("destroy-item count %d exceeds legacy uint8", request.Count)
	}
	if request.Container != 0xff {
		request.Container = AdjustInventorySlot(request.Container)
	} else {
		request.Slot = AdjustInventorySlot(request.Slot)
	}
	return request, nil
}

func EncodeLegacyDestroyItem(request DestroyItemRequest) []byte {
	return []byte{request.Container, request.Slot, uint8(request.Count), 0, 0, 0}
}

// ParseOpenItem decodes the two-byte build-54261 CMSG_OPEN_ITEM body: the
// container (equipped-bag index or 0xFF for the main backpack) followed by the
// slot inside it. AzerothCore reads the same pair from its 3.3.5
// CMSG_OPEN_ITEM, so the wire shapes already match.
func ParseOpenItem(body []byte) (OpenItemRequest, error) {
	var request OpenItemRequest
	r := movementReader{data: body}
	var err error
	if request.PackSlot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read open-item container: %w", err)
	}
	if request.Slot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read open-item slot: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("open-item has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// EncodeLegacyOpenItem converts the modern bag coordinates to the 3.3.5 layout,
// exactly as legacy proxy's spell.AdjustInventorySlot handler does: an item in the
// main backpack (PackSlot 0xFF) carries an adjusted slot, while an item inside
// an equipped bag keeps its slot and only the bag index moves.
func EncodeLegacyOpenItem(request OpenItemRequest) []byte {
	container := request.PackSlot
	slot := request.Slot
	if container != 0xff {
		container = AdjustInventorySlot(container)
	} else {
		slot = AdjustInventorySlot(slot)
	}
	return []byte{container, slot}
}

func ParseRepairItem(body []byte) (RepairItemRequest, error) {
	var request RepairItemRequest
	r := movementReader{data: body}
	var err error
	if request.Vendor, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read repair vendor GUID: %w", err)
	}
	if request.Item, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read repair item GUID: %w", err)
	}
	if request.UseGuildBank, err = r.bit(); err != nil {
		return request, fmt.Errorf("read repair guild-bank bit: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return request, fmt.Errorf("repair-item has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyRepairItem(request RepairItemRequest, vendor, item uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, vendor)
	body = binary.LittleEndian.AppendUint64(body, item)
	guildBank := byte(0)
	if request.UseGuildBank {
		guildBank = 1
	}
	return append(body, guildBank)
}

func ParseLegacyInventoryChangeFailure(body []byte) (InventoryChangeFailure, error) {
	var failure InventoryChangeFailure
	r := movementReader{data: body}
	result, err := r.u8()
	if err != nil {
		return failure, fmt.Errorf("read inventory result: %w", err)
	}
	failure.Result = int32(result)
	if result == 0 {
		return failure, nil
	}
	if failure.ItemA, err = r.u64(); err != nil {
		return failure, fmt.Errorf("read inventory item A: %w", err)
	}
	if failure.ItemB, err = r.u64(); err != nil {
		return failure, fmt.Errorf("read inventory item B: %w", err)
	}
	if failure.ContainerBSlot, err = r.u8(); err != nil {
		return failure, fmt.Errorf("read inventory container slot: %w", err)
	}
	// WotLK appends one int32 for level- and limit-category failures. The
	// bind-confirm variant carries GUID/slot/GUID instead and is left intact
	// only when enough bytes are available; ordinary equip failures are the
	// path exercised by dragging and auto-equipping items.
	switch result {
	case 1, 84, 85, 87, 89:
		if failure.Extra, err = r.i32(); err != nil {
			return failure, fmt.Errorf("read inventory failure extra: %w", err)
		}
		failure.HasExtra = true
	case 81:
		if failure.SrcContainer, err = r.u64(); err != nil {
			return failure, fmt.Errorf("read bind-confirm source container: %w", err)
		}
		if failure.SrcSlot, err = r.i32(); err != nil {
			return failure, fmt.Errorf("read bind-confirm source slot: %w", err)
		}
		if failure.DstContainer, err = r.u64(); err != nil {
			return failure, fmt.Errorf("read bind-confirm destination container: %w", err)
		}
		failure.HasBindConfirm = true
	}
	if r.remaining() != 0 {
		return failure, fmt.Errorf("inventory-change-failure has %d trailing bytes", r.remaining())
	}
	return failure, nil
}

func EncodeInventoryChangeFailure(failure InventoryChangeFailure, itemA, itemB, srcContainer, dstContainer GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(ModernInventoryResult(failure.Result)))
	body = appendPackedGUID128(body, itemA.Low, itemA.High)
	body = appendPackedGUID128(body, itemB.Low, itemB.High)
	body = append(body, failure.ContainerBSlot)
	if failure.HasExtra {
		body = binary.LittleEndian.AppendUint32(body, uint32(failure.Extra))
	}
	if failure.HasBindConfirm {
		body = appendPackedGUID128(body, srcContainer.Low, srcContainer.High)
		body = binary.LittleEndian.AppendUint32(body, uint32(failure.SrcSlot))
		body = appendPackedGUID128(body, dstContainer.Low, dstContainer.High)
	}
	return body
}

// ModernInventoryResult maps the 3.3.5 InventoryResult enum to build 54261.
// Classic inserted EQUIP_ERR_CANT_TRADE_GOLD at 29, shifting the common
// inventory/loot errors by one. Passing the legacy number through makes
// INVENTORY_FULL (50) appear as LOOT_GONE (50) to the modern client.
func ModernInventoryResult(legacy int32) int32 {
	switch {
	case legacy >= 0 && legacy <= 28:
		return legacy
	case legacy >= 29 && legacy <= 72:
		return legacy + 1
	case legacy == 73:
		// NO_SPLIT_WHILE_PROSPECTING no longer has a dedicated result.
		return 40 // CLIENT_LOCKED_OUT / cannot do that right now
	case legacy >= 75 && legacy <= 82:
		return legacy + 1
	case legacy == 83:
		return 84 // no-output placeholder
	case legacy >= 84 && legacy <= 89:
		return legacy + 1
	default:
		return legacy
	}
}
