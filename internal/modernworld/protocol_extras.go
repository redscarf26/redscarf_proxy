package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGPushQuestToParty        = uint16(0x349F)
	CMSGGetAccountNotifications = uint16(0x373C)
	CMSGFarSight                = uint16(0x34E8)
	SMSGRaidGroupOnly           = uint16(0x27AF)
)

// ParseFarSight reads build 54261 CMSG_FAR_SIGHT's single MSB-first flag.
func ParseFarSight(body []byte) (bool, error) {
	r := movementReader{data: body}
	enabled, err := r.bit()
	if err != nil {
		return false, fmt.Errorf("read far-sight enabled flag: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return false, fmt.Errorf("far-sight has %d trailing bytes", r.remaining())
	}
	return enabled, nil
}

func ParsePushQuestToParty(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("push-quest-to-party has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodeLegacyQuestID(questID uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, questID)
}

func ParseGetAccountNotifications(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("get-account-notifications has %d bytes, want 0", len(body))
	}
	return nil
}

func TranslateRaidGroupOnly(body []byte) ([]byte, error) {
	if len(body) != 8 {
		return nil, fmt.Errorf("raid-group-only has %d bytes, want 8", len(body))
	}
	return append([]byte(nil), body...), nil
}

func ParseLegacyPreResurrect(body []byte) (uint64, error) {
	r := movementReader{data: body}
	guid, err := r.guid64()
	if err != nil {
		return 0, fmt.Errorf("read pre-resurrect player: %w", err)
	}
	if r.remaining() != 0 {
		return 0, fmt.Errorf("pre-resurrect has %d trailing bytes", r.remaining())
	}
	return guid, nil
}
