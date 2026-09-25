package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGBinderActivate           = uint16(13490)
	CMSGBankerActivate           = uint16(13491)
	CMSGBuyBankSlot              = uint16(13492)
	CMSGTalkToGossip             = uint16(13458)
	CMSGSpiritHealerActivate     = uint16(13487)
	SMSGNpcInteractionOpenResult = uint16(10378)

	PlayerInteractionBanker       int32 = 8
	PlayerInteractionSpiritHealer int32 = 18
	PlayerInteractionBinder       int32 = 20
	PlayerInteractionAuctioneer   int32 = 21
	PlayerInteractionStableMaster int32 = 22
)

// ParseBankNPCActivate reads the modern banker GUID128 for CMSG_BANKER_ACTIVATE
// and CMSG_BUY_BANK_SLOT.
func ParseBankNPCActivate(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseTalkToGossip(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

const (
	legacyNPCFlagGossip     = uint32(0x00000001)
	legacyNPCFlagSpellClick = uint32(0x01000000)
	legacyHighGuidVehicle   = uint16(0xF150)
)

// LegacyUnitUsesSpellClick reports units the 3.4 client shows as a gear even
// though WotLK boards them with CMSG_SPELLCLICK (ICC gunship cannons).
func LegacyUnitUsesSpellClick(legacyGUID uint64, npcFlags uint32) bool {
	if npcFlags&legacyNPCFlagSpellClick != 0 {
		return true
	}
	if uint16(legacyGUID>>48) != legacyHighGuidVehicle {
		return false
	}
	return npcFlags&legacyNPCFlagGossip == 0
}

func ParseBinderActivate(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseSpiritHealerActivate(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func LegacyNPCFlags(fields map[int]uint32) uint32 {
	if fields == nil {
		return 0
	}
	return fields[legacyUnitNPCFlags]
}

func ParseLegacyPackedGUID(body []byte) (uint64, error) {
	if len(body) == 8 {
		return binary.LittleEndian.Uint64(body), nil
	}
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, fmt.Errorf("read packed GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("packed GUID has %d trailing bytes", r.remaining())
	}
	return guid, nil
}

func EncodeNpcInteraction(guid GUID128, interaction int32, success bool) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, uint32(interaction))
	bits := newBitWriter(body)
	bits.writeBit(success)
	return bits.flush()
}
