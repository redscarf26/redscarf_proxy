package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	CMSGInitiateTrade  = uint16(0x3156)
	CMSGBeginTrade     = uint16(0x3157)
	CMSGBusyTrade      = uint16(0x3158)
	CMSGIgnoreTrade    = uint16(0x3159)
	CMSGAcceptTrade    = uint16(0x315A)
	CMSGUnacceptTrade  = uint16(0x315B)
	CMSGCancelTrade    = uint16(0x315C)
	CMSGSetTradeItem   = uint16(0x315D)
	CMSGClearTradeItem = uint16(0x315E)
	CMSGSetTradeGold   = uint16(0x315F)

	SMSGTradeUpdated = uint16(0x2581)
	SMSGTradeStatus  = uint16(0x2582)

	legacyTradeStatusProposed     = uint32(1)
	legacyTradeStatusInitiated    = uint32(2)
	legacyTradeStatusFailed       = uint32(12)
	legacyTradeStatusWrongRealm   = uint32(22)
	legacyTradeStatusNotOnTaplist = uint32(23)
	legacyTradeSlotCount          = 7
)

type TradeItemRequest struct {
	TradeSlot uint8
	PackSlot  uint8
	ItemSlot  uint8
}

type LegacyTradeStatus struct {
	Status        uint32
	Partner       uint64
	TradeID       uint32
	Failure       int32
	FailureForYou bool
	ItemID        uint32
	TradeSlot     uint8
}

type LegacyTradeItem struct {
	Slot               uint8
	ItemID             uint32
	StackCount         int32
	Wrapped            bool
	GiftCreator        uint64
	EnchantID          int32
	Creator            uint64
	Charges            int32
	RandomPropertySeed uint32
	RandomPropertyID   uint32
	Locked             bool
	MaxDurability      uint32
	Durability         uint32
}

type LegacyTradeUpdate struct {
	WhichPlayer         uint8
	Gold                uint32
	ProposedEnchantment int32
	Items               []LegacyTradeItem
}

func ParseInitiateTrade(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseEmptyTrade(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("empty trade packet has %d bytes", len(body))
	}
	return nil
}

func ParseCancelTrade(body []byte) error { return ParseEmptyTrade(body) }

func ParseAcceptTrade(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("accept-trade has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func ParseClearTradeItem(body []byte) (uint8, error) {
	if len(body) != 1 {
		return 0, fmt.Errorf("clear-trade-item has %d bytes, want 1", len(body))
	}
	return body[0], nil
}

func ParseSetTradeGold(body []byte) (uint32, error) {
	if len(body) != 8 {
		return 0, fmt.Errorf("set-trade-gold has %d bytes, want 8", len(body))
	}
	gold := binary.LittleEndian.Uint64(body)
	if gold > math.MaxUint32 {
		return 0, fmt.Errorf("set-trade-gold value %d exceeds legacy uint32", gold)
	}
	return uint32(gold), nil
}

func ParseSetTradeItem(body []byte) (TradeItemRequest, error) {
	if len(body) != 3 {
		return TradeItemRequest{}, fmt.Errorf("set-trade-item has %d bytes, want 3", len(body))
	}
	request := TradeItemRequest{TradeSlot: body[0], PackSlot: body[1], ItemSlot: body[2]}
	if request.TradeSlot >= legacyTradeSlotCount {
		return TradeItemRequest{}, fmt.Errorf("trade slot %d is invalid", request.TradeSlot)
	}
	if request.PackSlot == 0xff {
		request.ItemSlot = AdjustInventorySlot(request.ItemSlot)
	} else {
		request.PackSlot = AdjustInventorySlot(request.PackSlot)
	}
	return request, nil
}

func EncodeLegacyTradeGUID(guid uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, guid)
}

func EncodeLegacyAcceptTrade(stateIndex uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, stateIndex)
}

func EncodeLegacySetTradeGold(gold uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, gold)
}

func EncodeLegacySetTradeItem(item TradeItemRequest) []byte {
	return []byte{item.TradeSlot, item.PackSlot, item.ItemSlot}
}

func ParseLegacyTradeStatus(body []byte) (LegacyTradeStatus, error) {
	var status LegacyTradeStatus
	r := movementReader{data: body}
	var err error
	if status.Status, err = r.u32(); err != nil {
		return status, fmt.Errorf("read trade status: %w", err)
	}
	if status.Status > 25 {
		return status, fmt.Errorf("trade status %d is invalid", status.Status)
	}
	switch status.Status {
	case legacyTradeStatusProposed:
		if status.Partner, err = r.u64(); err != nil {
			return status, fmt.Errorf("read trade partner: %w", err)
		}
	case legacyTradeStatusInitiated:
		if status.TradeID, err = r.u32(); err != nil {
			return status, fmt.Errorf("read trade ID: %w", err)
		}
	case legacyTradeStatusFailed:
		failure, readErr := r.u32()
		if readErr != nil {
			return status, fmt.Errorf("read trade failure: %w", readErr)
		}
		status.Failure = ModernInventoryResult(int32(failure))
		failureForYou, readErr := r.u8()
		if readErr != nil || failureForYou > 1 {
			return status, fmt.Errorf("read trade target-failure flag")
		}
		status.FailureForYou = failureForYou != 0
		if status.ItemID, err = r.u32(); err != nil {
			return status, fmt.Errorf("read trade failure item: %w", err)
		}
	case legacyTradeStatusWrongRealm, legacyTradeStatusNotOnTaplist:
		if status.TradeSlot, err = r.u8(); err != nil {
			return status, fmt.Errorf("read trade failure slot: %w", err)
		}
	}
	if r.remaining() != 0 {
		return status, fmt.Errorf("trade-status has %d trailing bytes", r.remaining())
	}
	return status, nil
}

func EncodeTradeStatus(status LegacyTradeStatus, partner, partnerAccount GUID128) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(false) // PartnerIsSameBnetAccount
	bits.writeBits(status.Status, 5)
	if status.Status == legacyTradeStatusFailed {
		bits.writeBit(status.FailureForYou)
	}
	body := bits.flush()
	switch status.Status {
	case legacyTradeStatusFailed:
		body = binary.LittleEndian.AppendUint32(body, uint32(status.Failure))
		body = binary.LittleEndian.AppendUint32(body, status.ItemID)
	case legacyTradeStatusInitiated:
		body = binary.LittleEndian.AppendUint32(body, status.TradeID)
	case legacyTradeStatusProposed:
		body = appendPackedGUID128(body, partner.Low, partner.High)
		body = appendPackedGUID128(body, partnerAccount.Low, partnerAccount.High)
	case legacyTradeStatusWrongRealm, legacyTradeStatusNotOnTaplist:
		body = append(body, status.TradeSlot)
	}
	return body
}

func ParseLegacyTradeUpdate(body []byte) (LegacyTradeUpdate, error) {
	var update LegacyTradeUpdate
	r := movementReader{data: body}
	var err error
	if update.WhichPlayer, err = r.u8(); err != nil {
		return update, fmt.Errorf("read trade-update player: %w", err)
	}
	if _, err = r.u32(); err != nil { // legacy trade ID
		return update, fmt.Errorf("read trade-update ID: %w", err)
	}
	if _, err = r.u32(); err != nil { // slot count
		return update, fmt.Errorf("read trade-update slot count: %w", err)
	}
	if _, err = r.u32(); err != nil { // slot count duplicate
		return update, fmt.Errorf("read trade-update slot count duplicate: %w", err)
	}
	if update.Gold, err = r.u32(); err != nil {
		return update, fmt.Errorf("read trade-update gold: %w", err)
	}
	proposed, err := r.u32()
	if err != nil {
		return update, fmt.Errorf("read trade-update enchantment: %w", err)
	}
	update.ProposedEnchantment = int32(proposed)
	update.Items = make([]LegacyTradeItem, legacyTradeSlotCount)
	for index := range update.Items {
		item := &update.Items[index]
		if item.Slot, err = r.u8(); err != nil {
			return update, fmt.Errorf("read trade item %d slot: %w", index, err)
		}
		if item.ItemID, err = r.u32(); err != nil {
			return update, fmt.Errorf("read trade item %d ID: %w", index, err)
		}
		if _, err = r.u32(); err != nil { // display ID
			return update, fmt.Errorf("read trade item %d display: %w", index, err)
		}
		stack, readErr := r.u32()
		if readErr != nil {
			return update, fmt.Errorf("read trade item %d stack: %w", index, readErr)
		}
		item.StackCount = int32(stack)
		wrapped, readErr := r.u32()
		if readErr != nil {
			return update, fmt.Errorf("read trade item %d wrapped: %w", index, readErr)
		}
		item.Wrapped = wrapped != 0
		if item.GiftCreator, err = r.u64(); err != nil {
			return update, fmt.Errorf("read trade item %d gift creator: %w", index, err)
		}
		enchant, readErr := r.u32()
		if readErr != nil {
			return update, fmt.Errorf("read trade item %d enchant: %w", index, readErr)
		}
		item.EnchantID = int32(enchant)
		if _, err = r.take(12); err != nil { // three socket enchant IDs
			return update, fmt.Errorf("read trade item %d socket enchants: %w", index, err)
		}
		if item.Creator, err = r.u64(); err != nil {
			return update, fmt.Errorf("read trade item %d creator: %w", index, err)
		}
		charges, readErr := r.u32()
		if readErr != nil {
			return update, fmt.Errorf("read trade item %d charges: %w", index, readErr)
		}
		item.Charges = int32(charges)
		if item.RandomPropertySeed, err = r.u32(); err != nil {
			return update, fmt.Errorf("read trade item %d seed: %w", index, err)
		}
		if item.RandomPropertyID, err = r.u32(); err != nil {
			return update, fmt.Errorf("read trade item %d property: %w", index, err)
		}
		locked, readErr := r.u32()
		if readErr != nil {
			return update, fmt.Errorf("read trade item %d lock: %w", index, readErr)
		}
		item.Locked = locked != 0
		if item.MaxDurability, err = r.u32(); err != nil {
			return update, fmt.Errorf("read trade item %d max durability: %w", index, err)
		}
		if item.Durability, err = r.u32(); err != nil {
			return update, fmt.Errorf("read trade item %d durability: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return update, fmt.Errorf("trade-update has %d trailing bytes", r.remaining())
	}
	return update, nil
}

func EncodeTradeUpdated(update LegacyTradeUpdate, tradeID, clientStateIndex, currentStateIndex uint32, resolve func(uint64) GUID128) []byte {
	body := []byte{update.WhichPlayer}
	body = binary.LittleEndian.AppendUint32(body, tradeID)
	body = binary.LittleEndian.AppendUint32(body, clientStateIndex)
	body = binary.LittleEndian.AppendUint32(body, currentStateIndex)
	body = binary.LittleEndian.AppendUint64(body, uint64(update.Gold))
	body = binary.LittleEndian.AppendUint32(body, 0) // CurrencyType
	body = binary.LittleEndian.AppendUint32(body, 0) // CurrencyQuantity
	body = binary.LittleEndian.AppendUint32(body, uint32(update.ProposedEnchantment))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(update.Items)))
	for _, item := range update.Items {
		body = append(body, item.Slot)
		body = binary.LittleEndian.AppendUint32(body, uint32(item.StackCount))
		giftCreator := resolve(item.GiftCreator)
		body = appendPackedGUID128(body, giftCreator.Low, giftCreator.High)
		body = appendItemInstance(body, item.ItemID, item.RandomPropertySeed, item.RandomPropertyID)
		hasUnwrapped := item.ItemID != 0 && !item.Wrapped
		bits := newBitWriter(body)
		bits.writeBit(hasUnwrapped)
		body = bits.flush()
		if !hasUnwrapped {
			continue
		}
		body = binary.LittleEndian.AppendUint32(body, uint32(item.EnchantID))
		body = binary.LittleEndian.AppendUint32(body, 0) // OnUseEnchantmentID
		creator := resolve(item.Creator)
		body = appendPackedGUID128(body, creator.Low, creator.High)
		body = binary.LittleEndian.AppendUint32(body, uint32(item.Charges))
		body = binary.LittleEndian.AppendUint32(body, item.MaxDurability)
		body = binary.LittleEndian.AppendUint32(body, item.Durability)
		unwrappedBits := newBitWriter(body)
		unwrappedBits.writeBits(0, 2) // Gem count
		unwrappedBits.writeBit(item.Locked)
		body = unwrappedBits.flush()
	}
	return body
}
