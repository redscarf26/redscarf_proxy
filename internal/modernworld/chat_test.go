package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestChatMessageSayTranslation(t *testing.T) {
	text := ".additem 6948"
	body := binary.LittleEndian.AppendUint32(nil, 0)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(text)), 11)
	body = bits.flush()
	body = append(body, text...)
	message, err := ParseChatMessage(body)
	if err != nil || message.Language != 0 || message.Text != text {
		t.Fatalf("message=%+v err=%v", message, err)
	}
	messageType, ok := LegacyChatTypeForOpcode(CMSGChatMessageSay)
	if !ok || messageType != legacyChatSay {
		t.Fatalf("type=%d ok=%v", messageType, ok)
	}
	legacy, err := EncodeLegacyChatMessage(messageType, message.Language, message.Text)
	if err != nil || binary.LittleEndian.Uint32(legacy) != legacyChatSay || !bytes.Equal(legacy[8:], append([]byte(text), 0)) {
		t.Fatalf("legacy=%x err=%v", legacy, err)
	}
}

func TestChatWhisperTranslation54261(t *testing.T) {
	target := "Alice"
	text := "hello"
	body := binary.LittleEndian.AppendUint32(nil, 0)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(target)), 9)
	bits.writeBits(uint32(len(text)), 11)
	body = bits.flush()
	body = append(body, target...)
	body = append(body, text...)

	whisper, err := ParseChatWhisper(body)
	if err != nil || whisper.Language != 0 || whisper.Target != target || whisper.Text != text {
		t.Fatalf("whisper=%+v err=%v body=%x", whisper, err, body)
	}
	legacy, err := EncodeLegacyChatWhisper(whisper)
	if err != nil || binary.LittleEndian.Uint32(legacy[:4]) != 7 || !bytes.Equal(legacy[8:], []byte("Alice\x00hello\x00")) {
		t.Fatalf("legacy=%x err=%v", legacy, err)
	}
	if _, err := ParseChatWhisper(append(body, 0)); err == nil {
		t.Fatal("whisper parser accepted a trailing byte")
	}
}

func TestLegacySystemChatTranslation(t *testing.T) {
	legacy := []byte{legacyChatSystem}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 12)
	legacy = append(legacy, []byte("Added item.\x00")...)
	legacy = append(legacy, 0)
	message, err := ParseLegacyChatMessage(legacy)
	if err != nil || message.Type != legacyChatSystem || message.Receiver != 0x42 || message.Text != "Added item." {
		t.Fatalf("message=%+v err=%v", message, err)
	}
	modern := EncodeChatMessage(GUID128{}, GUID128{Low: 0x42, High: 1}, message, 0)
	if modern[0] != 0 || !bytes.Contains(modern, []byte("Added item.")) {
		t.Fatalf("modern=%x", modern)
	}
}

func TestLegacyMonsterChatTranslation(t *testing.T) {
	legacy := []byte{legacyChatMonsterSay}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(len("Innkeeper\x00")))
	legacy = append(legacy, "Innkeeper\x00"...)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(len("Welcome!\x00")))
	legacy = append(legacy, "Welcome!\x00"...)
	legacy = append(legacy, 0)

	message, err := ParseLegacyChatMessage(legacy)
	if err != nil || message.SenderName != "Innkeeper" || message.Receiver != 0x42 || message.Text != "Welcome!" {
		t.Fatalf("message=%+v err=%v", message, err)
	}
	modern := EncodeChatMessage(GUID128{Low: 0x43, High: 1}, GUID128{Low: 0x42, High: 1}, message, 0)
	if !bytes.Contains(modern, []byte("Innkeeper")) || !bytes.Contains(modern, []byte("Welcome!")) {
		t.Fatalf("modern=%x", modern)
	}
}

func TestChatMessageIncludesPlayerNamesAndRealm54261(t *testing.T) {
	sender := ModernGUIDForLegacy(0x43, 0)
	receiver := ModernGUIDForLegacy(0x42, 0)
	message := LegacyChatMessage{Type: 7, SenderName: "Alice", ReceiverName: "Bob", Text: "hello"}
	const realmAddress = uint32(0x01010001)
	body := EncodeChatMessage(sender, receiver, message, realmAddress)
	r := movementReader{data: body}
	messageType, _ := r.u8()
	_, _ = r.u32()
	decodedSender, _ := r.guid128()
	_, _ = r.guid128()
	_, _ = r.guid128()
	decodedReceiver, _ := r.guid128()
	targetRealm, _ := r.u32()
	senderRealm, _ := r.u32()
	_, _ = r.i32()
	_, _ = r.u32()
	_, _ = r.i32()
	senderLength, _ := r.bits(11)
	receiverLength, _ := r.bits(11)
	_, _ = r.bits(5)
	_, _ = r.bits(7)
	textLength, _ := r.bits(12)
	_, _ = r.bits(15)
	for range 4 {
		_, _ = r.bit()
	}
	decodedSenderName, _ := r.stringN(int(senderLength))
	decodedReceiverName, _ := r.stringN(int(receiverLength))
	decodedText, err := r.stringN(int(textLength))
	if err != nil || messageType != 7 || decodedSender != sender || decodedReceiver != receiver || targetRealm != realmAddress || senderRealm != realmAddress ||
		decodedSenderName != "Alice" || decodedReceiverName != "Bob" || decodedText != "hello" || r.remaining() != 0 {
		t.Fatalf("type=%d sender=%#v receiver=%#v realms=%x/%x names=%q/%q text=%q remaining=%d err=%v body=%x",
			messageType, decodedSender, decodedReceiver, targetRealm, senderRealm, decodedSenderName, decodedReceiverName, decodedText, r.remaining(), err, body)
	}
}

func TestLegacyMonsterChatReceiverName(t *testing.T) {
	legacy := []byte{legacyChatRaidBossWhisper}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(len("Boss\x00")))
	legacy = append(legacy, "Boss\x00"...)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130000002000044)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(len("Target\x00")))
	legacy = append(legacy, "Target\x00"...)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(len("Run!\x00")))
	legacy = append(legacy, "Run!\x00"...)
	legacy = append(legacy, 0)

	message, err := ParseLegacyChatMessage(legacy)
	if err != nil || message.SenderName != "Boss" || message.ReceiverName != "Target" || message.Text != "Run!" {
		t.Fatalf("message=%+v err=%v", message, err)
	}
}

func TestChatChannelTranslation(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 2)
	bits := newBitWriter(body)
	bits.writeBit(false)
	bits.writeBit(false)
	bits.writeBits(7, 7)
	bits.writeBits(2, 7)
	body = bits.flush()
	body = append(body, "General"...)
	body = append(body, "pw"...)
	request, err := ParseChatJoinChannel(body)
	if err != nil {
		t.Fatal(err)
	}
	if request.ChannelID != 2 || request.Name != "General" || request.Password != "pw" {
		t.Fatalf("request %#v", request)
	}
	legacy := EncodeLegacyChatJoinChannel(request)
	if binary.LittleEndian.Uint32(legacy) != 2 || !bytes.Equal(legacy[6:], []byte("General\x00pw\x00")) {
		t.Fatalf("legacy %x", legacy)
	}
}

func TestChatPlayerNotFoundTranslation(t *testing.T) {
	name, err := ParseLegacyChatPlayerNotFound([]byte("Missing\x00"))
	if err != nil || name != "Missing" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	body := EncodeChatPlayerNotFound(name)
	r := movementReader{data: body}
	length, err := r.bits(9)
	if err != nil || length != 7 {
		t.Fatalf("length=%d err=%v body=%x", length, err, body)
	}
	got, err := r.stringN(int(length))
	if err != nil || got != name || r.remaining() != 0 {
		t.Fatalf("name=%q remaining=%d err=%v", got, r.remaining(), err)
	}
}

func TestChannelJoinedAndLeftTranslation(t *testing.T) {
	const (
		mapID  = uint16(609)
		zoneID = uint32(4298)
	)
	joined := []byte{2}
	joined = append(joined, "General"...)
	joined = append(joined, 0, 0x10)
	joined = binary.LittleEndian.AppendUint32(joined, 1)
	joined = binary.LittleEndian.AppendUint32(joined, 0)
	opcode, body, handled, err := TranslateLegacyChannelNotify(joined, mapID, zoneID, ChannelPlayerLookup{})
	if err != nil || !handled || opcode != SMSGChannelNotifyJoined || !bytes.Contains(body, []byte("General")) {
		t.Fatalf("joined opcode=%#x handled=%v body=%x err=%v", opcode, handled, body, err)
	}
	wantGUID := ModernChatChannelGUID(mapID, zoneID, 1)
	if !bytes.Contains(body, appendPackedGUID128(nil, wantGUID.Low, wantGUID.High)) {
		t.Fatalf("joined ChannelGUID missing Hermes/legacy proxy ChatChannel GUID, body=%x guid=%#v", body, wantGUID)
	}
	if binary.LittleEndian.Uint32(body[3:7]) != 0x10 || binary.LittleEndian.Uint32(body[7:11]) != 1 {
		t.Fatalf("joined flags/id %x", body[3:11])
	}

	zeroID := []byte{2}
	zeroID = append(zeroID, "General - Elwynn Forest"...)
	zeroID = append(zeroID, 0, 0x10)
	zeroID = binary.LittleEndian.AppendUint32(zeroID, 0)
	zeroID = binary.LittleEndian.AppendUint32(zeroID, 0)
	opcode, body, handled, err = TranslateLegacyChannelNotify(zeroID, mapID, zoneID, ChannelPlayerLookup{})
	if err != nil || !handled || opcode != SMSGChannelNotifyJoined {
		t.Fatalf("name-lookup opcode=%#x handled=%v err=%v", opcode, handled, err)
	}
	if binary.LittleEndian.Uint32(body[7:11]) != 1 {
		t.Fatalf("Hermes GetChatChannelIdFromName should map General to id 1, got %d", binary.LittleEndian.Uint32(body[7:11]))
	}

	left := []byte{3}
	left = append(left, "General"...)
	left = append(left, 0)
	left = binary.LittleEndian.AppendUint32(left, 2)
	left = append(left, 1)
	opcode, body, handled, err = TranslateLegacyChannelNotify(left, mapID, zoneID, ChannelPlayerLookup{})
	if err != nil || !handled || opcode != SMSGChannelNotifyLeft || !bytes.Contains(body, []byte("General")) {
		t.Fatalf("left opcode=%#x handled=%v body=%x err=%v", opcode, handled, body, err)
	}

	// Admin/error notices (wrong password etc.) are deliberately dropped: the
	// translator returns handled with a zero opcode so the relay stays quiet.
	opcode, _, handled, err = TranslateLegacyChannelNotify([]byte{4, 'X', 0}, mapID, zoneID, ChannelPlayerLookup{})
	if err != nil || !handled || opcode != 0 {
		t.Fatalf("dropped notice opcode=%#x handled=%v err=%v", opcode, handled, err)
	}
	opcode, _, handled, err = TranslateLegacyChannelNotify([]byte{12, 'G', 0}, mapID, zoneID, ChannelPlayerLookup{})
	if err != nil || !handled || opcode != 0 {
		t.Fatalf("mode-change notice opcode=%#x handled=%v err=%v", opcode, handled, err)
	}
	opcode, _, handled, err = TranslateLegacyChannelNotify([]byte{8, 'G', 0}, mapID, zoneID, ChannelPlayerLookup{})
	if err != nil || !handled || opcode != 0 {
		t.Fatalf("owner-changed notice opcode=%#x handled=%v err=%v", opcode, handled, err)
	}
}

func TestChannelAdminCommandsListAndNotices(t *testing.T) {
	const channel = "Trade"
	command := newBitWriter(nil)
	command.writeBits(uint32(len(channel)), 7)
	body := append(command.flush(), channel...)
	name, err := ParseChatChannelCommand(body)
	if err != nil || name != channel {
		t.Fatalf("name=%q err=%v", name, err)
	}
	if !bytes.Equal(EncodeLegacyChatChannelCommand(name), []byte("Trade\x00")) {
		t.Fatalf("legacy command %x", EncodeLegacyChatChannelCommand(name))
	}
	if _, err = ParseChatChannelCommand(append(body, 1)); err == nil {
		t.Fatal("channel command accepted a trailing byte")
	}
	for _, opcode := range []uint16{
		CMSGChatChannelList,
		CMSGChatChannelDisplayList,
		CMSGChatChannelOwner,
		CMSGChatChannelAnnouncements,
		CMSGChatChannelDeclineInvite,
	} {
		if opcode == 0 {
			t.Fatalf("channel command opcode is zero")
		}
	}
	if CMSGChatChannelList == CMSGChatChannelDisplayList {
		t.Fatal("display-list collapsed onto list")
	}

	const (
		legacyGUID   = uint64(0x123456)
		realmAddress = uint32(0x01010001)
	)
	legacy := []byte{1}
	legacy = append(legacy, channel...)
	legacy = append(legacy, 0, 0x04)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = appendLegacyPackedGUID(legacy, legacyGUID)
	legacy = append(legacy, 0x01)
	list, err := ParseLegacyChannelList(legacy)
	if err != nil || !list.Display || list.Name != channel || list.Flags != 0x04 || len(list.Members) != 1 || list.Members[0].Flags != 0x01 || list.Members[0].GUID != legacyGUID {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	modern, err := EncodeChannelList(list, realmAddress, func(id uint64) GUID128 {
		if id != legacyGUID {
			t.Fatalf("guid lookup %x", id)
		}
		return GUID128{Low: 7, High: 9}
	})
	if err != nil {
		t.Fatal(err)
	}
	reader := movementReader{data: modern}
	display, err := reader.bit()
	if err != nil || !display {
		t.Fatalf("display=%v err=%v", display, err)
	}
	nameLen, err := reader.bits(7)
	if err != nil || nameLen != uint32(len(channel)) {
		t.Fatalf("name length=%d err=%v", nameLen, err)
	}
	flags, err := reader.u32()
	if err != nil || flags != 0x04 {
		t.Fatalf("flags=%#x err=%v", flags, err)
	}
	count, err := reader.i32()
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	gotName, err := reader.stringN(int(nameLen))
	if err != nil || gotName != channel {
		t.Fatalf("name=%q err=%v", gotName, err)
	}
	guid, err := reader.guid128()
	if err != nil || guid != (GUID128{Low: 7, High: 9}) {
		t.Fatalf("guid=%#v err=%v", guid, err)
	}
	gotRealm, err := reader.u32()
	if err != nil || gotRealm != realmAddress {
		t.Fatalf("realm=%#x err=%v", gotRealm, err)
	}
	memberFlags, err := reader.u8()
	if err != nil || memberFlags != 0x01 || reader.remaining() != 0 {
		t.Fatalf("member flags=%#x remaining=%d err=%v", memberFlags, reader.remaining(), err)
	}
	if _, err = ParseLegacyChannelList(append(legacy, 9)); err == nil {
		t.Fatal("channel list accepted a trailing byte")
	}

	hidden := []byte{0, 'G', 0, 0x10, 0, 0, 0, 0}
	empty, err := ParseLegacyChannelList(hidden)
	if err != nil || empty.Display || empty.Flags != 0x10 || len(empty.Members) != 0 {
		t.Fatalf("empty list=%+v err=%v", empty, err)
	}

	owner := append([]byte{chatNotifyChannelOwner}, "General\x00Alice\x00"...)
	opcode, notice, handled, err := TranslateLegacyChannelNotify(owner, 0, 0, ChannelPlayerLookup{})
	if err != nil || !handled || opcode != SMSGChannelNotify {
		t.Fatalf("owner opcode=%#x handled=%v err=%v", opcode, handled, err)
	}
	assertChannelNotify(t, notice, chatNotifyChannelOwner, "General", "Alice", GUID128{})

	announceGUID := GUID128{Low: 3, High: 4}
	announce := []byte{chatNotifyAnnouncementsOn}
	announce = append(announce, "Trade\x00"...)
	announce = appendLegacyPackedGUID(announce, legacyGUID)
	opcode, notice, handled, err = TranslateLegacyChannelNotify(announce, 0, 0, ChannelPlayerLookup{
		GUID: func(id uint64) GUID128 {
			if id != legacyGUID {
				t.Fatalf("announce guid %x", id)
			}
			return announceGUID
		},
		Name: func(uint64) string { return "Mage" },
	})
	if err != nil || !handled || opcode != SMSGChannelNotify {
		t.Fatalf("announce-on opcode=%#x handled=%v err=%v", opcode, handled, err)
	}
	assertChannelNotify(t, notice, chatNotifyAnnouncementsOn, "Trade", "Mage", announceGUID)

	off := []byte{chatNotifyAnnouncementsOff}
	off = append(off, "Trade\x00"...)
	off = appendLegacyPackedGUID(off, legacyGUID)
	opcode, notice, handled, err = TranslateLegacyChannelNotify(off, 0, 0, ChannelPlayerLookup{
		GUID: func(uint64) GUID128 { return announceGUID },
	})
	if err != nil || !handled || opcode != SMSGChannelNotify {
		t.Fatalf("announce-off opcode=%#x handled=%v err=%v", opcode, handled, err)
	}
	assertChannelNotify(t, notice, chatNotifyAnnouncementsOff, "Trade", "", announceGUID)

	if _, _, _, err = TranslateLegacyChannelNotify(append(owner, 1), 0, 0, ChannelPlayerLookup{}); err == nil {
		t.Fatal("owner notice accepted a trailing byte")
	}
}

func assertChannelNotify(t *testing.T, body []byte, notifyType uint8, channel, sender string, senderGUID GUID128) {
	t.Helper()
	reader := movementReader{data: body}
	gotType, err := reader.bits(6)
	if err != nil || gotType != uint32(notifyType) {
		t.Fatalf("type=%d err=%v body=%x", gotType, err, body)
	}
	channelLen, err := reader.bits(7)
	if err != nil || channelLen != uint32(len(channel)) {
		t.Fatalf("channel length=%d err=%v", channelLen, err)
	}
	senderLen, err := reader.bits(6)
	if err != nil || senderLen != uint32(len(sender)) {
		t.Fatalf("sender length=%d err=%v", senderLen, err)
	}
	guid, err := reader.guid128()
	if err != nil || guid != senderGUID {
		t.Fatalf("sender guid=%#v err=%v", guid, err)
	}
	account, err := reader.guid128()
	if err != nil || account != (GUID128{}) {
		t.Fatalf("account guid=%#v err=%v", account, err)
	}
	senderRealm, err := reader.u32()
	if err != nil || senderRealm != 0 {
		t.Fatalf("sender realm=%#x err=%v", senderRealm, err)
	}
	target, err := reader.guid128()
	if err != nil || target != (GUID128{}) {
		t.Fatalf("target guid=%#v err=%v", target, err)
	}
	targetRealm, err := reader.u32()
	if err != nil || targetRealm != 0 {
		t.Fatalf("target realm=%#x err=%v", targetRealm, err)
	}
	channelID, err := reader.i32()
	if err != nil || channelID != 0 {
		t.Fatalf("channel id=%d err=%v", channelID, err)
	}
	gotChannel, err := reader.stringN(int(channelLen))
	if err != nil || gotChannel != channel {
		t.Fatalf("channel=%q err=%v", gotChannel, err)
	}
	gotSender, err := reader.stringN(int(senderLen))
	if err != nil || gotSender != sender || reader.remaining() != 0 {
		t.Fatalf("sender=%q remaining=%d err=%v", gotSender, reader.remaining(), err)
	}
}

func TestAddonPrefixAndWhisperTranslation(t *testing.T) {
	prefixesBody := binary.LittleEndian.AppendUint32(nil, 2)
	bits := newBitWriter(prefixesBody)
	bits.writeBits(3, 5)
	prefixesBody = bits.flush()
	prefixesBody = append(prefixesBody, "Foo"...)
	bits = newBitWriter(prefixesBody)
	bits.writeBits(3, 5)
	prefixesBody = bits.flush()
	prefixesBody = append(prefixesBody, "Bar"...)
	prefixes, err := ParseChatAddonPrefixes(prefixesBody)
	if err != nil || len(prefixes) != 2 || prefixes[0] != "Foo" || prefixes[1] != "Bar" {
		t.Fatalf("prefixes=%v err=%v", prefixes, err)
	}

	target := "Player"
	prefix := "Foo"
	text := "ping"
	body := newBitWriter(nil)
	body.writeBits(uint32(len(target)), 9)
	encoded := body.flush()
	params := newBitWriter(encoded)
	params.writeBits(uint32(len(prefix)), 5)
	params.writeBits(uint32(len(text)), 8)
	params.writeBit(false)
	encoded = params.flush()
	encoded = binary.LittleEndian.AppendUint32(encoded, 7)
	encoded = append(encoded, prefix...)
	encoded = append(encoded, text...)
	encoded = appendPackedGUID128(encoded, 0, 0)
	encoded = append(encoded, target...)
	request, err := ParseChatAddonMessage(encoded, true)
	if err != nil {
		t.Fatal(err)
	}
	if request.Type != 7 || request.Prefix != prefix || request.Text != text || request.Target != target {
		t.Fatalf("request %#v", request)
	}
	legacy, err := EncodeLegacyChatAddonMessage(request)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(legacy[:4]) != 7 || binary.LittleEndian.Uint32(legacy[4:8]) != legacyLanguageAddon || !bytes.Equal(legacy[8:], []byte("Player\x00Foo\tping\x00")) {
		t.Fatalf("legacy %x", legacy)
	}

	request.Target = "Player-AzerothCore"
	legacy, err = EncodeLegacyChatAddonMessage(request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(legacy[8:], []byte("Player\x00Foo\tping\x00")) {
		t.Fatalf("legacy same-realm whisper %x", legacy)
	}

	message := LegacyChatMessage{Language: legacyLanguageAddon, Text: "Foo\tpong"}
	if !PrepareLegacyAddonMessage(&message, map[string]struct{}{"Foo": {}}) || message.Language != modernLanguageAddon || message.AddonPrefix != "Foo" || message.Text != "pong" {
		t.Fatalf("prepared message %#v", message)
	}
	modern := EncodeChatMessage(GUID128{}, GUID128{}, message, 0)
	if !bytes.Contains(modern, []byte("Foopong")) {
		t.Fatalf("modern %x", modern)
	}
}
