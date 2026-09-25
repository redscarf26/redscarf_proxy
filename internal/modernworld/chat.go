package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

const (
	CMSGChatMessageGuild         = uint16(14289)
	CMSGChatMessageOfficer       = uint16(14290)
	CMSGChatMessageWhisper       = uint16(14288)
	CMSGChatMessageSay           = uint16(14311)
	CMSGChatMessageYell          = uint16(14313)
	CMSGChatMessageParty         = uint16(14314)
	CMSGChatMessageRaid          = uint16(14315)
	CMSGChatMessageInstanceChat  = uint16(14316)
	CMSGChatMessageRaidWarning   = uint16(14317)
	CMSGChatJoinChannel          = uint16(0x37C8)
	CMSGChatLeaveChannel         = uint16(0x37C9)
	CMSGChatMessageChannel       = uint16(0x37CF)
	CMSGChatRegisterPrefixes     = uint16(0x37CD)
	CMSGChatUnregisterPrefixes   = uint16(0x37CE)
	CMSGChatAddonMessage         = uint16(0x37EE)
	CMSGChatAddonMessageWhisper  = uint16(0x37EF)
	SMSGChat                     = uint16(11181)
	SMSGChatPlayerNotFound       = uint16(0x2BB7)
	SMSGPrintNotification        = uint16(9674)
	SMSGDefenseMessage           = uint16(0x2BB6)
	SMSGChatServerMessage        = uint16(0x2BC5)
	CMSGChatChannelList          = uint16(0x37D5)
	CMSGChatChannelDisplayList   = uint16(0x37D6)
	CMSGChatChannelOwner         = uint16(0x37D9)
	CMSGChatChannelAnnouncements = uint16(0x37E3)
	CMSGChatChannelDeclineInvite = uint16(0x37E6)
	SMSGChannelNotify            = uint16(0x2BC1)
	SMSGChannelNotifyJoined      = uint16(0x2BC2)
	SMSGChannelNotifyLeft        = uint16(0x2BC3)
	SMSGChannelList              = uint16(0x2BC4)

	chatNotifyChannelOwner     = uint8(11)
	chatNotifyAnnouncementsOn  = uint8(13)
	chatNotifyAnnouncementsOff = uint8(14)
	maxChannelCommandName      = (1 << 7) - 1
	maxChannelNotifySender     = (1 << 6) - 1
	maxChannelListMembers      = 4096

	defenseMessageMaxBytes    = 4095 // 12-bit length
	chatServerMessageMaxBytes = 2047 // 11-bit length

	legacyChatSystem           = uint8(0)
	legacyChatSay              = uint32(1)
	legacyChatParty            = uint32(2)
	legacyChatRaid             = uint32(3)
	legacyChatGuild            = uint32(4)
	legacyChatOfficer          = uint32(5)
	legacyChatYell             = uint32(6)
	legacyChatWhisperForeign   = uint8(8)
	legacyChatMonsterSay       = uint8(12)
	legacyChatMonsterParty     = uint8(13)
	legacyChatMonsterYell      = uint8(14)
	legacyChatMonsterWhisper   = uint8(15)
	legacyChatMonsterEmote     = uint8(16)
	legacyChatChannel          = uint8(17)
	legacyChatBGSystemNeutral  = uint8(36)
	legacyChatBGSystemAlliance = uint8(37)
	legacyChatBGSystemHorde    = uint8(38)
	legacyChatRaidWarning      = uint32(40)
	legacyChatRaidBossWhisper  = uint8(41)
	legacyChatRaidBossEmote    = uint8(42)
	legacyChatBattleNet        = uint8(47)
	legacyLanguageAddon        = uint32(0xffffffff)
	modernLanguageAddon        = uint32(183)
	modernLanguageLogged       = uint32(184)
	legacyChatAchievement      = uint8(48)
	legacyGuildAchievement     = uint8(49)
	legacyChatPartyLeader      = uint8(51)
	maxLegacyChatText          = 255
)

type ChatMessageRequest struct {
	Language uint32
	Text     string
}

type ChatWhisperRequest struct {
	Language uint32
	Target   string
	Text     string
}

type ChatChannelRequest struct {
	ChannelID int32
	Name      string
	Password  string
}

// ChatChannelMessageRequest is the 54261 channel-message shape. ChannelGUID
// identifies the local channel instance but the WotLK server selects the
// channel by name, so it is intentionally not emitted to the legacy packet.
type ChatChannelMessageRequest struct {
	Language    uint32
	ChannelGUID GUID128
	Target      string
	Text        string
}

type ChatAddonMessageRequest struct {
	Type        uint32
	Prefix      string
	Text        string
	Target      string
	ChannelGUID GUID128
	Logged      bool
}

type LegacyChatMessage struct {
	Type          uint8
	Language      uint32
	Sender        uint64
	Receiver      uint64
	SenderName    string
	ReceiverName  string
	Channel       string
	Text          string
	Flags         uint16
	AchievementID uint32
	AddonPrefix   string
}

type LegacyChannelNotify struct {
	Type      uint8
	Channel   string
	Flags     uint8
	ChannelID int32
	Suspended bool
}

func ParseChatJoinChannel(body []byte) (ChatChannelRequest, error) {
	var request ChatChannelRequest
	r := movementReader{data: body}
	channelID, err := r.i32()
	if err != nil {
		return request, fmt.Errorf("read join-channel ID: %w", err)
	}
	request.ChannelID = channelID
	if _, err = r.bit(); err != nil {
		return request, fmt.Errorf("read join-channel voice-session flag: %w", err)
	}
	if _, err = r.bit(); err != nil {
		return request, fmt.Errorf("read join-channel internal flag: %w", err)
	}
	nameLen, err := r.bits(7)
	if err != nil {
		return request, fmt.Errorf("read join-channel name length: %w", err)
	}
	passwordLen, err := r.bits(7)
	if err != nil {
		return request, fmt.Errorf("read join-channel password length: %w", err)
	}
	if request.Name, err = r.stringN(int(nameLen)); err != nil {
		return request, fmt.Errorf("read join-channel name: %w", err)
	}
	if request.Password, err = r.stringN(int(passwordLen)); err != nil {
		return request, fmt.Errorf("read join-channel password: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("join-channel has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyChatJoinChannel(request ChatChannelRequest) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(request.ChannelID))
	body = append(body, 0, 0) // voice, joined by zone update
	body = append(body, request.Name...)
	body = append(body, 0)
	body = append(body, request.Password...)
	return append(body, 0)
}

func ParseChatLeaveChannel(body []byte) (ChatChannelRequest, error) {
	var request ChatChannelRequest
	r := movementReader{data: body}
	channelID, err := r.i32()
	if err != nil {
		return request, fmt.Errorf("read leave-channel ID: %w", err)
	}
	request.ChannelID = channelID
	nameLen, err := r.bits(7)
	if err != nil {
		return request, fmt.Errorf("read leave-channel name length: %w", err)
	}
	if request.Name, err = r.stringN(int(nameLen)); err != nil {
		return request, fmt.Errorf("read leave-channel name: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("leave-channel has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyChatLeaveChannel(request ChatChannelRequest) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(request.ChannelID))
	body = append(body, request.Name...)
	return append(body, 0)
}

// ParseChatChannelCommand reads the shared 54261 channel-admin request:
// a 7-bit channel name and nothing else. List, display-list, owner,
// announcements and decline-invite all use this shape.
func ParseChatChannelCommand(body []byte) (string, error) {
	r := movementReader{data: body}
	length, err := r.bits(7)
	if err != nil {
		return "", fmt.Errorf("read channel-command name length: %w", err)
	}
	name, err := r.stringN(int(length))
	if err != nil {
		return "", fmt.Errorf("read channel-command name: %w", err)
	}
	if r.remaining() != 0 {
		return "", fmt.Errorf("channel command has %d trailing bytes", r.remaining())
	}
	return name, nil
}

func EncodeLegacyChatChannelCommand(name string) []byte {
	return append([]byte(name), 0)
}

type LegacyChannelMember struct {
	GUID  uint64
	Flags uint8
}

type LegacyChannelList struct {
	Display bool
	Name    string
	Flags   uint8
	Members []LegacyChannelMember
}

func ParseLegacyChannelList(body []byte) (LegacyChannelList, error) {
	var list LegacyChannelList
	r := movementReader{data: body}
	display, err := r.u8()
	if err != nil {
		return list, fmt.Errorf("read channel-list display: %w", err)
	}
	list.Display = display != 0
	if list.Name, err = r.cstring(); err != nil {
		return list, fmt.Errorf("read channel-list name: %w", err)
	}
	if list.Flags, err = r.u8(); err != nil {
		return list, fmt.Errorf("read channel-list flags: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return list, fmt.Errorf("read channel-list count: %w", err)
	}
	if count > maxChannelListMembers {
		return list, fmt.Errorf("channel list has %d members", count)
	}
	list.Members = make([]LegacyChannelMember, count)
	for index := range list.Members {
		if list.Members[index].GUID, err = r.guid64(); err != nil {
			return list, fmt.Errorf("read channel-list member %d guid: %w", index, err)
		}
		if list.Members[index].Flags, err = r.u8(); err != nil {
			return list, fmt.Errorf("read channel-list member %d flags: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return list, fmt.Errorf("channel list has %d trailing bytes", r.remaining())
	}
	return list, nil
}

// EncodeChannelList writes build-54261 SMSG_CHANNEL_LIST. The legacy flag
// byte is widened as-is; it is not remapped through the DBC channel-flag enum.
func EncodeChannelList(list LegacyChannelList, realmAddress uint32, guidOf func(uint64) GUID128) ([]byte, error) {
	if len(list.Name) > maxChannelCommandName {
		return nil, fmt.Errorf("channel list name has %d bytes", len(list.Name))
	}
	if len(list.Members) > maxChannelListMembers {
		return nil, fmt.Errorf("channel list has %d members", len(list.Members))
	}
	bits := newBitWriter(nil)
	bits.writeBit(list.Display)
	bits.writeBits(uint32(len(list.Name)), 7)
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, uint32(list.Flags))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(list.Members)))
	body = append(body, list.Name...)
	for _, member := range list.Members {
		guid := GUID128{}
		if guidOf != nil {
			guid = guidOf(member.GUID)
		}
		body = appendPackedGUID128(body, guid.Low, guid.High)
		body = binary.LittleEndian.AppendUint32(body, realmAddress)
		body = append(body, member.Flags)
	}
	return body, nil
}

// ParseChatMessageChannel reads build 54261 CMSG_CHAT_MESSAGE_CHANNEL. The
// text length is 11 bits (not 9), followed by a presence bit and an optional
// secure-channel flag. This layout is independently documented by the current
// Hermes 3.4.3 implementation and is required for short messages to work.
func ParseChatMessageChannel(body []byte) (ChatChannelMessageRequest, error) {
	var request ChatChannelMessageRequest
	r := movementReader{data: body}
	var err error
	if request.Language, err = r.u32(); err != nil {
		return request, fmt.Errorf("read channel-message language: %w", err)
	}
	if request.ChannelGUID, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read channel-message GUID: %w", err)
	}
	targetLength, err := r.bits(9)
	if err != nil {
		return request, fmt.Errorf("read channel-message target length: %w", err)
	}
	textLength, err := r.bits(11)
	if err != nil {
		return request, fmt.Errorf("read channel-message text length: %w", err)
	}
	hasSecure, err := r.bit()
	if err != nil {
		return request, fmt.Errorf("read channel-message secure flag presence: %w", err)
	}
	if hasSecure {
		if _, err = r.bit(); err != nil {
			return request, fmt.Errorf("read channel-message secure flag: %w", err)
		}
	}
	if request.Target, err = r.stringN(int(targetLength)); err != nil {
		return request, fmt.Errorf("read channel-message target: %w", err)
	}
	if request.Text, err = r.stringN(int(textLength)); err != nil {
		return request, fmt.Errorf("read channel-message text: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("channel message has %d trailing bytes", r.remaining())
	}
	if request.Target == "" {
		return request, fmt.Errorf("channel-message target is empty")
	}
	return request, nil
}

func EncodeLegacyChatChannelMessage(request ChatChannelMessageRequest) ([]byte, error) {
	request.Text = legacyChatItemLinks(request.Text, request.Language)
	if len(request.Text) > maxLegacyChatText {
		return nil, fmt.Errorf("channel message has %d bytes, legacy limit is %d", len(request.Text), maxLegacyChatText)
	}
	body := binary.LittleEndian.AppendUint32(nil, uint32(legacyChatChannel))
	body = binary.LittleEndian.AppendUint32(body, request.Language)
	body = append(body, request.Target...)
	body = append(body, 0)
	body = append(body, request.Text...)
	return append(body, 0), nil
}

func ParseLegacyChatPlayerNotFound(body []byte) (string, error) {
	r := movementReader{data: body}
	name, err := r.cstring()
	if err != nil {
		return "", fmt.Errorf("read missing player name: %w", err)
	}
	if r.remaining() != 0 {
		return "", fmt.Errorf("player-not-found has %d trailing bytes", r.remaining())
	}
	return name, nil
}

func EncodeChatPlayerNotFound(name string) []byte {
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(name)), 9)
	body := bits.flush()
	return append(body, name...)
}

const modernHighGuidChatChannel = 26

// chatChannelIDs is HermesProxy CSV/ChatChannels.csv: GetChatChannelIdFromName
// matches when the joined name contains the DBC name (e.g. "General - Elwynn Forest").
var chatChannelIDs = []struct {
	id   int32
	name string
}{
	{1, "General"},
	{2, "Trade"},
	{22, "LocalDefense"},
	{23, "WorldDefense"},
	{24, "LookingForGroup"},
	{25, "GuildRecruitment"},
}

func ChatChannelIDFromName(name string) int32 {
	for _, channel := range chatChannelIDs {
		if strings.Contains(name, channel.name) {
			return channel.id
		}
	}
	return 0
}

// ModernChatChannelGUID is Hermes WowGuid128.Create(HighGuidType703.ChatChannel,
// mapId, zoneId, channelId) / legacy proxy wow_guid.MapSpecificCreate.
func ModernChatChannelGUID(mapID uint16, zoneID uint32, channelID int32) GUID128 {
	high := uint64(modernHighGuidChatChannel)<<58 | uint64(1)<<42 | uint64(mapID&0x1fff)<<29 | (uint64(zoneID)&0x7fffff)<<6
	return GUID128{Low: uint64(uint32(channelID)) & 0xffffffffff, High: high}
}

// ChannelPlayerLookup resolves a legacy player on announcement notices.
// Owner notices carry the name in the packet and do not use this lookup.
type ChannelPlayerLookup struct {
	GUID func(uint64) GUID128
	Name func(uint64) string
}

func TranslateLegacyChannelNotify(body []byte, mapID uint16, zoneID uint32, players ChannelPlayerLookup) (uint16, []byte, bool, error) {
	r := movementReader{data: body}
	notifyType, err := r.u8()
	if err != nil {
		return 0, nil, true, fmt.Errorf("read channel-notify type: %w", err)
	}
	channel, err := r.cstring()
	if err != nil {
		return 0, nil, true, fmt.Errorf("read channel-notify name: %w", err)
	}
	switch notifyType {
	case 2: // CHAT_YOU_JOINED_NOTICE
		flags, readErr := r.u8()
		if readErr != nil {
			return 0, nil, true, fmt.Errorf("read joined-channel flags: %w", readErr)
		}
		channelID, readErr := r.i32()
		if readErr != nil {
			return 0, nil, true, fmt.Errorf("read joined-channel ID: %w", readErr)
		}
		if _, readErr = r.u32(); readErr != nil {
			return 0, nil, true, fmt.Errorf("read joined-channel instance: %w", readErr)
		}
		if r.remaining() != 0 {
			return 0, nil, true, fmt.Errorf("joined-channel notice has %d trailing bytes", r.remaining())
		}
		if channelID == 0 {
			channelID = ChatChannelIDFromName(channel)
		}
		guid := ModernChatChannelGUID(mapID, zoneID, channelID)
		bits := newBitWriter(nil)
		bits.writeBits(uint32(len(channel)), 7)
		bits.writeBits(0, 11) // welcome text
		modern := bits.flush()
		modern = binary.LittleEndian.AppendUint32(modern, uint32(flags))
		modern = binary.LittleEndian.AppendUint32(modern, uint32(channelID))
		modern = binary.LittleEndian.AppendUint64(modern, 0)
		modern = appendPackedGUID128(modern, guid.Low, guid.High)
		modern = append(modern, channel...)
		// Live 54261 accepts this packet but intentionally suppresses YOU_JOINED
		// for automatic rejoin / initial built-in channel setup. Do not resend
		// or synthesize chat text to bypass that state. See the investigation doc.
		return SMSGChannelNotifyJoined, modern, true, nil
	case 3: // CHAT_YOU_LEFT_NOTICE
		channelID, readErr := r.i32()
		if readErr != nil {
			return 0, nil, true, fmt.Errorf("read left-channel ID: %w", readErr)
		}
		suspended, readErr := r.u8()
		if readErr != nil {
			return 0, nil, true, fmt.Errorf("read left-channel suspended flag: %w", readErr)
		}
		if r.remaining() != 0 {
			return 0, nil, true, fmt.Errorf("left-channel notice has %d trailing bytes", r.remaining())
		}
		bits := newBitWriter(nil)
		bits.writeBits(uint32(len(channel)), 7)
		bits.writeBit(suspended != 0)
		modern := bits.flush()
		modern = binary.LittleEndian.AppendUint32(modern, uint32(channelID))
		modern = append(modern, channel...)
		return SMSGChannelNotifyLeft, modern, true, nil
	case chatNotifyChannelOwner:
		sender, readErr := r.cstring()
		if readErr != nil {
			return 0, nil, true, fmt.Errorf("read channel-owner name: %w", readErr)
		}
		if r.remaining() != 0 {
			return 0, nil, true, fmt.Errorf("channel-owner notice has %d trailing bytes", r.remaining())
		}
		modern, readErr := encodeChannelNotify(notifyType, channel, sender, GUID128{})
		if readErr != nil {
			return 0, nil, true, readErr
		}
		return SMSGChannelNotify, modern, true, nil
	case chatNotifyAnnouncementsOn, chatNotifyAnnouncementsOff:
		legacyGUID, readErr := r.guid64()
		if readErr != nil {
			return 0, nil, true, fmt.Errorf("read announcement player: %w", readErr)
		}
		if r.remaining() != 0 {
			return 0, nil, true, fmt.Errorf("announcement notice has %d trailing bytes", r.remaining())
		}
		var senderGUID GUID128
		sender := ""
		if players.GUID != nil {
			senderGUID = players.GUID(legacyGUID)
		}
		if players.Name != nil {
			sender = players.Name(legacyGUID)
		}
		modern, readErr := encodeChannelNotify(notifyType, channel, sender, senderGUID)
		if readErr != nil {
			return 0, nil, true, readErr
		}
		return SMSGChannelNotify, modern, true, nil
	default:
		// Other ChatNotify types (wrong password, not member/moderator/owner,
		// invites, kicks, mutes, owner/password changes, mode changes, ...) stay
		// dropped. Hermes and legacy proxy only consume them. opcode 0 tells the relay
		// to stay quiet instead of reporting a pending translation.
		return 0, nil, true, nil
	}
}

func encodeChannelNotify(notifyType uint8, channel, sender string, senderGUID GUID128) ([]byte, error) {
	if len(channel) > maxChannelCommandName {
		return nil, fmt.Errorf("channel-notify name has %d bytes", len(channel))
	}
	if len(sender) > maxChannelNotifySender {
		return nil, fmt.Errorf("channel-notify sender has %d bytes", len(sender))
	}
	bits := newBitWriter(nil)
	bits.writeBits(uint32(notifyType), 6)
	bits.writeBits(uint32(len(channel)), 7)
	bits.writeBits(uint32(len(sender)), 6)
	body := bits.flush()
	body = appendPackedGUID128(body, senderGUID.Low, senderGUID.High)
	body = appendPackedGUID128(body, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = appendPackedGUID128(body, 0, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = append(body, channel...)
	return append(body, sender...), nil
}

func ParseChatAddonPrefixes(body []byte) ([]string, error) {
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read addon-prefix count: %w", err)
	}
	if count > 64 {
		return nil, fmt.Errorf("addon-prefix count %d exceeds 64", count)
	}
	prefixes := make([]string, count)
	for index := range prefixes {
		length, readErr := r.bits(5)
		if readErr != nil {
			return nil, fmt.Errorf("read addon-prefix %d length: %w", index, readErr)
		}
		if prefixes[index], readErr = r.stringN(int(length)); readErr != nil {
			return nil, fmt.Errorf("read addon-prefix %d: %w", index, readErr)
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("addon-prefix registration has %d trailing bytes", r.remaining())
	}
	return prefixes, nil
}

func ParseChatAddonMessage(body []byte, targeted bool) (ChatAddonMessageRequest, error) {
	var request ChatAddonMessageRequest
	r := movementReader{data: body}
	targetLen := uint32(0)
	var err error
	if targeted {
		if targetLen, err = r.bits(9); err != nil {
			return request, fmt.Errorf("read addon target length: %w", err)
		}
		r.align()
	}
	prefixLen, err := r.bits(5)
	if err != nil {
		return request, fmt.Errorf("read addon prefix length: %w", err)
	}
	textLen, err := r.bits(8)
	if err != nil {
		return request, fmt.Errorf("read addon text length: %w", err)
	}
	if request.Logged, err = r.bit(); err != nil {
		return request, fmt.Errorf("read addon logged bit: %w", err)
	}
	if request.Type, err = r.u32(); err != nil {
		return request, fmt.Errorf("read addon chat type: %w", err)
	}
	if request.Prefix, err = r.stringN(int(prefixLen)); err != nil {
		return request, fmt.Errorf("read addon prefix: %w", err)
	}
	if request.Text, err = r.stringN(int(textLen)); err != nil {
		return request, fmt.Errorf("read addon text: %w", err)
	}
	if targeted {
		if request.ChannelGUID, err = r.guid128(); err != nil {
			return request, fmt.Errorf("read addon channel GUID: %w", err)
		}
		if request.Target, err = r.stringN(int(targetLen)); err != nil {
			return request, fmt.Errorf("read addon target: %w", err)
		}
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("addon chat has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyChatAddonMessage(request ChatAddonMessageRequest) ([]byte, error) {
	// The 3.4 packet stores prefix and text separately (up to 31 + 255 bytes),
	// while AzerothCore reconstructs both in one legacy CString and rejects it
	// above 255 bytes. Preserve the prefix and the largest representable payload
	// instead of failing the whole world-packet handler.
	maxText := maxLegacyChatText - len(request.Prefix) - 1
	if maxText < 0 {
		return nil, fmt.Errorf("addon prefix has %d bytes, legacy limit is %d", len(request.Prefix), maxLegacyChatText-1)
	}
	text := request.Text
	if len(text) > maxText {
		text = text[:maxText]
	}
	payload := request.Prefix + "\t" + text
	body := binary.LittleEndian.AppendUint32(nil, request.Type)
	body = binary.LittleEndian.AppendUint32(body, legacyLanguageAddon)
	switch request.Type {
	case 7: // whisper
		body = append(body, LegacySameRealmName(request.Target)...)
		body = append(body, 0)
	case 17: // channel; targeted packets carry the channel name in Target
		body = append(body, request.Target...)
		body = append(body, 0)
	}
	body = append(body, payload...)
	return append(body, 0), nil
}

// LegacySameRealmName strips a 3.4.3 Name-Realm suffix. WotLK whispers are
// same-realm CStrings; AzerothCore looks up "Name-Realm" as a literal name.
func LegacySameRealmName(name string) string {
	if base, _, found := strings.Cut(name, "-"); found {
		return base
	}
	return name
}

func PrepareLegacyAddonMessage(message *LegacyChatMessage, registered map[string]struct{}) bool {
	if message == nil || message.Language != legacyLanguageAddon {
		return true
	}
	parts := strings.SplitN(message.Text, "\t", 2)
	if len(parts) != 2 {
		return false
	}
	if _, ok := registered[parts[0]]; !ok {
		return false
	}
	message.AddonPrefix = parts[0]
	message.Text = parts[1]
	message.Language = modernLanguageAddon
	return true
}

func LegacyChatTypeForOpcode(opcode uint16) (uint32, bool) {
	switch opcode {
	case CMSGChatMessageSay:
		return legacyChatSay, true
	case CMSGChatMessageYell:
		return legacyChatYell, true
	case CMSGChatMessageGuild:
		return legacyChatGuild, true
	case CMSGChatMessageOfficer:
		return legacyChatOfficer, true
	case CMSGChatMessageParty, CMSGChatMessageInstanceChat:
		return legacyChatParty, true
	case CMSGChatMessageRaid:
		return legacyChatRaid, true
	case CMSGChatMessageRaidWarning:
		return legacyChatRaidWarning, true
	default:
		return 0, false
	}
}

func ParseChatMessage(body []byte) (ChatMessageRequest, error) {
	var message ChatMessageRequest
	r := movementReader{data: body}
	var err error
	if message.Language, err = r.u32(); err != nil {
		return message, fmt.Errorf("read chat language: %w", err)
	}
	length, err := r.bits(11)
	if err != nil {
		return message, fmt.Errorf("read chat text length: %w", err)
	}
	r.align()
	text, err := r.take(int(length))
	if err != nil {
		return message, fmt.Errorf("read chat text: %w", err)
	}
	if r.remaining() != 0 {
		return message, fmt.Errorf("chat message has %d trailing bytes", r.remaining())
	}
	message.Text = string(text)
	return message, nil
}

func ParseChatWhisper(body []byte) (ChatWhisperRequest, error) {
	var whisper ChatWhisperRequest
	r := movementReader{data: body}
	var err error
	if whisper.Language, err = r.u32(); err != nil {
		return whisper, fmt.Errorf("read whisper language: %w", err)
	}
	targetLength, err := r.bits(9)
	if err != nil {
		return whisper, fmt.Errorf("read whisper target length: %w", err)
	}
	textLength, err := r.bits(11)
	if err != nil {
		return whisper, fmt.Errorf("read whisper text length: %w", err)
	}
	if whisper.Target, err = r.stringN(int(targetLength)); err != nil {
		return whisper, fmt.Errorf("read whisper target: %w", err)
	}
	if whisper.Text, err = r.stringN(int(textLength)); err != nil {
		return whisper, fmt.Errorf("read whisper text: %w", err)
	}
	if r.remaining() != 0 {
		return whisper, fmt.Errorf("whisper has %d trailing bytes", r.remaining())
	}
	if whisper.Target == "" {
		return whisper, fmt.Errorf("whisper target is empty")
	}
	return whisper, nil
}

func EncodeLegacyChatWhisper(whisper ChatWhisperRequest) ([]byte, error) {
	whisper.Text = legacyChatItemLinks(whisper.Text, whisper.Language)
	if len(whisper.Text) > maxLegacyChatText {
		return nil, fmt.Errorf("whisper text has %d bytes, legacy limit is %d", len(whisper.Text), maxLegacyChatText)
	}
	body := binary.LittleEndian.AppendUint32(nil, 7) // CHAT_MSG_WHISPER
	body = binary.LittleEndian.AppendUint32(body, whisper.Language)
	body = append(body, whisper.Target...)
	body = append(body, 0)
	body = append(body, whisper.Text...)
	return append(body, 0), nil
}

func EncodeLegacyChatMessage(messageType, language uint32, text string) ([]byte, error) {
	text = legacyChatItemLinks(text, language)
	if len(text) > maxLegacyChatText {
		return nil, fmt.Errorf("chat text has %d bytes, legacy limit is %d", len(text), maxLegacyChatText)
	}
	body := binary.LittleEndian.AppendUint32(nil, messageType)
	body = binary.LittleEndian.AppendUint32(body, language)
	body = append(body, text...)
	return append(body, 0), nil
}

func ParseLegacyChatMessage(body []byte) (LegacyChatMessage, error) {
	return parseLegacyChatMessage(body, false)
}

// ParseLegacyGMChatMessage reads SMSG_GM_MESSAGECHAT. Apart from its opcode it
// is SMSG_CHAT, with a length-prefixed GM name inserted before the channel and
// receiver fields for ordinary chat types.
func ParseLegacyGMChatMessage(body []byte) (LegacyChatMessage, error) {
	return parseLegacyChatMessage(body, true)
}

func parseLegacyChatMessage(body []byte, gmMessage bool) (LegacyChatMessage, error) {
	var message LegacyChatMessage
	r := movementReader{data: body}
	var err error
	if message.Type, err = r.u8(); err != nil {
		return message, fmt.Errorf("read legacy chat type: %w", err)
	}
	if message.Language, err = r.u32(); err != nil {
		return message, fmt.Errorf("read legacy chat language: %w", err)
	}
	if message.Sender, err = r.u64(); err != nil {
		return message, fmt.Errorf("read legacy chat sender: %w", err)
	}
	if _, err = r.u32(); err != nil { // sender realm/unk
		return message, fmt.Errorf("read legacy chat sender realm: %w", err)
	}
	switch message.Type {
	case legacyChatAchievement, legacyGuildAchievement:
		if message.Receiver, err = r.u64(); err != nil {
			return message, fmt.Errorf("read legacy chat receiver: %w", err)
		}
	case legacyChatWhisperForeign:
		if message.SenderName, err = readLegacyChatName(&r); err != nil {
			return message, fmt.Errorf("read legacy chat sender name: %w", err)
		}
		if message.Receiver, err = r.u64(); err != nil {
			return message, fmt.Errorf("read legacy chat receiver: %w", err)
		}
	case legacyChatBGSystemNeutral, legacyChatBGSystemAlliance, legacyChatBGSystemHorde:
		if message.Receiver, err = r.u64(); err != nil {
			return message, fmt.Errorf("read legacy chat receiver: %w", err)
		}
		if message.Receiver != 0 && uint16(message.Receiver>>48) != 0 {
			if message.ReceiverName, err = readLegacyChatName(&r); err != nil {
				return message, fmt.Errorf("read legacy chat receiver name: %w", err)
			}
		}
	case legacyChatMonsterSay, legacyChatMonsterParty, legacyChatMonsterYell,
		legacyChatMonsterWhisper, legacyChatMonsterEmote, legacyChatRaidBossWhisper,
		legacyChatRaidBossEmote, legacyChatBattleNet:
		if message.SenderName, err = readLegacyChatName(&r); err != nil {
			return message, fmt.Errorf("read legacy chat sender name: %w", err)
		}
		if message.Receiver, err = r.u64(); err != nil {
			return message, fmt.Errorf("read legacy chat receiver: %w", err)
		}
		if legacyChatGUIDHasName(message.Receiver) {
			if message.ReceiverName, err = readLegacyChatName(&r); err != nil {
				return message, fmt.Errorf("read legacy chat receiver name: %w", err)
			}
		}
	default:
		if gmMessage {
			if _, err = readLegacyChatName(&r); err != nil {
				return message, fmt.Errorf("read legacy GM chat name: %w", err)
			}
		}
		if message.Type == legacyChatChannel {
			if message.Channel, err = r.cstring(); err != nil {
				return message, fmt.Errorf("read legacy chat channel: %w", err)
			}
		}
		if message.Receiver, err = r.u64(); err != nil {
			return message, fmt.Errorf("read legacy chat receiver: %w", err)
		}
	}
	textLength, err := r.u32()
	if err != nil {
		return message, fmt.Errorf("read legacy chat text length: %w", err)
	}
	if textLength > 1<<20 {
		return message, fmt.Errorf("legacy chat text length %d is unreasonable", textLength)
	}
	text, err := r.take(int(textLength))
	if err != nil {
		return message, fmt.Errorf("read legacy chat text: %w", err)
	}
	message.Text = strings.TrimSuffix(string(text), "\x00")
	flags, err := r.u8()
	if err != nil {
		return message, fmt.Errorf("read legacy chat flags: %w", err)
	}
	message.Flags = uint16(flags)
	if message.Type == legacyChatAchievement || message.Type == legacyGuildAchievement {
		if message.AchievementID, err = r.u32(); err != nil {
			return message, fmt.Errorf("read legacy chat achievement: %w", err)
		}
	}
	if r.remaining() != 0 {
		return message, fmt.Errorf("legacy chat has %d trailing bytes", r.remaining())
	}
	return message, nil
}

// TranslateLegacyPrintNotification converts the legacy CString packet to the
// 54261 12-bit-length notification packet.
func TranslateLegacyPrintNotification(body []byte) ([]byte, error) {
	r := movementReader{data: body}
	text, err := r.cstring()
	if err != nil {
		return nil, fmt.Errorf("read print-notification text: %w", err)
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("print-notification has %d trailing bytes", r.remaining())
	}
	if len(text) > 0xFFF {
		return nil, fmt.Errorf("print-notification text has %d bytes, maximum is 4095", len(text))
	}
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(text)), 12)
	return append(bits.flush(), text...), nil
}

// TranslateLegacyAreaTriggerMessage preserves the server's entrance denial text
// as a 54261 print notification. The modern area-trigger-message packet carries
// only a trigger ID, so using it would lose server-specific level/item reasons.
// AzerothCore WorldSession::SendAreaTriggerMessage includes the NUL in length.
func TranslateLegacyAreaTriggerMessage(body []byte) ([]byte, error) {
	r := movementReader{data: body}
	length, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read area-trigger-message length: %w", err)
	}
	if length == 0 || uint64(length) != uint64(r.remaining()) {
		return nil, fmt.Errorf("area-trigger-message length %d does not match %d bytes", length, r.remaining())
	}
	return TranslateLegacyPrintNotification(body[4:])
}

func readLegacyChatName(r *movementReader) (string, error) {
	length, err := r.u32()
	if err != nil {
		return "", err
	}
	if length == 0 || length > 1<<20 {
		return "", fmt.Errorf("invalid length %d", length)
	}
	name, err := r.stringN(int(length))
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(name, "\x00"), nil
}

func legacyChatGUIDHasName(guid uint64) bool {
	high := uint16(guid >> 48)
	return guid != 0 && high != 0 && high != legacyPetHighGuid
}

func EncodeChatMessage(sender, receiver GUID128, message LegacyChatMessage, realmAddress uint32) []byte {
	body := []byte{modernChatType(message.Type)}
	body = binary.LittleEndian.AppendUint32(body, message.Language)
	body = appendPackedGUID128(body, sender.Low, sender.High)
	body = appendPackedGUID128(body, 0, 0) // sender guild
	body = appendPackedGUID128(body, 0, 0) // sender account
	body = appendPackedGUID128(body, receiver.Low, receiver.High)
	targetRealm := uint32(0)
	if receiver.Low != 0 || receiver.High != 0 {
		targetRealm = realmAddress
	}
	senderRealm := uint32(0)
	if sender.Low != 0 || sender.High != 0 {
		senderRealm = realmAddress
	}
	body = binary.LittleEndian.AppendUint32(body, targetRealm)
	body = binary.LittleEndian.AppendUint32(body, senderRealm)
	body = binary.LittleEndian.AppendUint32(body, message.AchievementID)
	body = binary.LittleEndian.AppendUint32(body, math.Float32bits(0))
	body = binary.LittleEndian.AppendUint32(body, 0) // spell
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(message.SenderName)), 11)
	bits.writeBits(uint32(len(message.ReceiverName)), 11)
	bits.writeBits(uint32(len(message.AddonPrefix)), 5)
	bits.writeBits(uint32(len(message.Channel)), 7)
	bits.writeBits(uint32(len(message.Text)), 12)
	bits.writeBits(uint32(message.Flags), 15)
	bits.writeBit(false) // hide chat log
	bits.writeBit(false) // fake sender name
	bits.writeBit(false) // unused value
	bits.writeBit(false) // channel GUID
	body = bits.flush()
	body = append(body, message.SenderName...)
	body = append(body, message.ReceiverName...)
	body = append(body, message.AddonPrefix...)
	body = append(body, message.Channel...)
	return append(body, message.Text...)
}

// DefenseMessage is a zone-wide broadcast. The legacy u32 length is discarded;
// the text is the following CString, matching Hermes and legacy proxy.
type DefenseMessage struct {
	ZoneID uint32
	Text   string
}

func ParseLegacyDefenseMessage(body []byte) (DefenseMessage, error) {
	var message DefenseMessage
	r := movementReader{data: body}
	var err error
	if message.ZoneID, err = r.u32(); err != nil {
		return message, fmt.Errorf("read defense-message zone: %w", err)
	}
	if _, err = r.u32(); err != nil {
		return message, fmt.Errorf("read defense-message length: %w", err)
	}
	if message.Text, err = r.cstring(); err != nil {
		return message, fmt.Errorf("read defense-message text: %w", err)
	}
	if r.remaining() != 0 {
		return message, fmt.Errorf("defense-message has %d trailing bytes", r.remaining())
	}
	if len(message.Text) > defenseMessageMaxBytes {
		return message, fmt.Errorf("defense-message text has %d bytes, maximum is %d", len(message.Text), defenseMessageMaxBytes)
	}
	return message, nil
}

func EncodeDefenseMessage(message DefenseMessage) ([]byte, error) {
	if len(message.Text) > defenseMessageMaxBytes {
		return nil, fmt.Errorf("defense-message text has %d bytes, maximum is %d", len(message.Text), defenseMessageMaxBytes)
	}
	body := binary.LittleEndian.AppendUint32(nil, message.ZoneID)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(message.Text)), 12)
	body = bits.flush()
	return append(body, message.Text...), nil
}

// ChatServerMessage is the WotLK shutdown/restart notice: a message id plus one
// CString argument. Build 54261 stores that argument in an 11-bit length.
type ChatServerMessage struct {
	MessageID   int32
	StringParam string
}

func ParseLegacyChatServerMessage(body []byte) (ChatServerMessage, error) {
	var message ChatServerMessage
	r := movementReader{data: body}
	id, err := r.i32()
	if err != nil {
		return message, fmt.Errorf("read chat-server-message id: %w", err)
	}
	message.MessageID = id
	if message.StringParam, err = r.cstring(); err != nil {
		return message, fmt.Errorf("read chat-server-message text: %w", err)
	}
	if r.remaining() != 0 {
		return message, fmt.Errorf("chat-server-message has %d trailing bytes", r.remaining())
	}
	if len(message.StringParam) > chatServerMessageMaxBytes {
		return message, fmt.Errorf("chat-server-message text has %d bytes, maximum is %d", len(message.StringParam), chatServerMessageMaxBytes)
	}
	return message, nil
}

func EncodeChatServerMessage(message ChatServerMessage) ([]byte, error) {
	if len(message.StringParam) > chatServerMessageMaxBytes {
		return nil, fmt.Errorf("chat-server-message text has %d bytes, maximum is %d", len(message.StringParam), chatServerMessageMaxBytes)
	}
	body := binary.LittleEndian.AppendUint32(nil, uint32(message.MessageID))
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(message.StringParam)), 11)
	body = bits.flush()
	return append(body, message.StringParam...), nil
}

func modernChatType(legacy uint8) byte {
	switch legacy {
	case 44:
		return 62 // battleground
	case 45:
		return 63 // battleground leader
	case 46:
		return 44 // restricted
	case legacyChatAchievement:
		return 46
	case legacyGuildAchievement:
		return 47
	case legacyChatPartyLeader:
		return 49
	default:
		return legacy
	}
	// The passthrough would mis-map three legacy ChatMsg values that share a
	// number with an unrelated modern enum entry (8 WHISPER_FOREIGN -> modern
	// Whisper2, 47 BATTLENET -> GuildAchievement, 50 ARENA_POINTS ->
	// Targeticons). AzerothCore 3.3.5a never emits any of them (dead enum
	// values), so the passthrough is unreachable; no guard is required.
}
