package modernworld

import "fmt"

// Away/Do-not-disturb and emote messages (build 54261 client <-> 3.3.5a
// AzerothCore). The modern client sends these on their own opcodes with only a
// 11-bit-length status text; WotLK folds them into CMSG_MESSAGECHAT.
// legacy proxy expansion 80 confirms the 11-bit length (older Hermes uses 9 bits).
//
// AzerothCore rejects LANG_UNIVERSAL on every type except AFK and DND, and
// answers with notification 805 ("未知的语言"). Emotes therefore go out in the
// speaker's faction language. The server rebroadcasts the emote as universal.

const (
	CMSGChatMessageAFK   = uint16(14291)
	CMSGChatMessageDND   = uint16(14292)
	CMSGChatMessageEmote = uint16(14312)

	LegacyChatTypeAFK   = uint32(0x17) // CHAT_MSG_AFK
	LegacyChatTypeDND   = uint32(0x18) // CHAT_MSG_DND
	LegacyChatTypeEmote = uint32(0x0a)

	legacyLanguageOrcish = uint32(1)
	legacyLanguageCommon = uint32(7)
)

// LegacyChatStatusType maps a modern AFK/DND/emote chat opcode to its WotLK
// CHAT_MSG_* value, returning 0 for any other opcode.
func LegacyChatStatusType(opcode uint16) uint32 {
	switch opcode {
	case CMSGChatMessageAFK:
		return LegacyChatTypeAFK
	case CMSGChatMessageDND:
		return LegacyChatTypeDND
	case CMSGChatMessageEmote:
		return LegacyChatTypeEmote
	default:
		return 0
	}
}

// ParseChatStatusMessage reads the 11-bit-length status text used by build
// 54261. Earlier Classic clients used nine bits.
func ParseChatStatusMessage(body []byte) (string, error) {
	r := movementReader{data: body}
	length, err := r.bits(11)
	if err != nil {
		return "", fmt.Errorf("read status text length: %w", err)
	}
	r.align()
	text, err := r.take(int(length))
	if err != nil {
		return "", fmt.Errorf("read status text: %w", err)
	}
	if r.remaining() != 0 {
		return "", fmt.Errorf("status message has %d trailing bytes", r.remaining())
	}
	return string(text), nil
}

// UnitRaceFromFields reads the race byte from a legacy UNIT_FIELD_BYTES_0 value map.
func UnitRaceFromFields(fields map[int]uint32) byte {
	if fields == nil {
		return 0
	}
	return byte(fields[legacyUnitBytes0])
}

// LegacyChatStatusLanguage is the CMSG_MESSAGECHAT language for an AFK, DND,
// or emote. AFK and DND stay universal. Emotes use Orcish or Common so
// AzerothCore does not reject them as an unknown language.
func LegacyChatStatusLanguage(messageType uint32, race byte) uint32 {
	if messageType != LegacyChatTypeEmote {
		return 0
	}
	if FactionGroupForRace(race) == 1 {
		return legacyLanguageOrcish
	}
	return legacyLanguageCommon
}

// EncodeLegacyChatStatus emits the WotLK CMSG_MESSAGECHAT body for an AFK,
// DND, or emote message. race selects the emote language; it is ignored for
// AFK and DND.
func EncodeLegacyChatStatus(messageType uint32, race byte, text string) ([]byte, error) {
	return EncodeLegacyChatMessage(messageType, LegacyChatStatusLanguage(messageType, race), text)
}
