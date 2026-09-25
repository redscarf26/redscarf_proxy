package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestChatStatusMessageRoundTrip(t *testing.T) {
	for _, text := range []string{"hi", "", "back in five minutes"} {
		body := newBitWriter(nil)
		body.writeBits(uint32(len(text)), 11)
		payload := body.flush()
		payload = append(payload, text...)

		parsed, err := ParseChatStatusMessage(payload)
		if err != nil || parsed != text {
			t.Fatalf("status text=%q err=%v payload=%x", parsed, err, payload)
		}

		encoded, err := EncodeLegacyChatStatus(LegacyChatTypeAFK, 2, text)
		if err != nil {
			t.Fatal(err)
		}
		want := []byte{0x17, 0, 0, 0, 0, 0, 0, 0}
		want = append(want, text...)
		want = append(want, 0)
		if !bytes.Equal(encoded, want) {
			t.Fatalf("legacy AFK body = %x, want %x", encoded, want)
		}
	}

	for _, tc := range []struct {
		opcode uint16
		want   uint32
	}{
		{CMSGChatMessageAFK, 0x17},
		{CMSGChatMessageDND, 0x18},
		{CMSGChatMessageSay, 0},
	} {
		if got := LegacyChatStatusType(tc.opcode); got != tc.want {
			t.Fatalf("status type for opcode %d = %d, want %d", tc.opcode, got, tc.want)
		}
	}

	// A length that exceeds the remaining payload is rejected.
	short := newBitWriter(nil)
	short.writeBits(5, 9)
	payload := short.flush()
	payload = append(payload, 'a')
	if _, err := ParseChatStatusMessage(payload); err == nil {
		t.Fatal("expected truncated status text to error")
	}
}

func TestChatChannelMessageTranslation54261(t *testing.T) {
	const (
		channel = "General"
		text    = "hello everyone"
	)
	body := binary.LittleEndian.AppendUint32(nil, 0)
	body = appendPackedGUID128(body, 0x1234, uint64(5)<<58)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(channel)), 9)
	bits.writeBits(uint32(len(text)), 11)
	bits.writeBit(true)  // secure flag is present
	bits.writeBit(false) // not secure
	body = bits.flush()
	body = append(body, channel...)
	body = append(body, text...)

	message, err := ParseChatMessageChannel(body)
	if err != nil || message.Target != channel || message.Text != text || message.ChannelGUID.Low != 0x1234 {
		t.Fatalf("message=%#v err=%v body=%x", message, err, body)
	}
	legacy, err := EncodeLegacyChatChannelMessage(message)
	want := []byte{17, 0, 0, 0, 0, 0, 0, 0}
	want = append(want, channel...)
	want = append(want, 0)
	want = append(want, text...)
	want = append(want, 0)
	if err != nil || !bytes.Equal(legacy, want) {
		t.Fatalf("legacy=%x want=%x err=%v", legacy, want, err)
	}
	if _, err := ParseChatMessageChannel(append(body, 0)); err == nil {
		t.Fatal("channel-message parser accepted a trailing byte")
	}
}

func TestEmoteUsesFactionLanguage(t *testing.T) {
	for _, tc := range []struct {
		race uint8
		lang uint32
	}{
		{1, 7},  // human, common
		{4, 7},  // night elf, common
		{2, 1},  // orc, orcish
		{10, 1}, // blood elf, orcish
		{0, 7},  // unknown race still must not be universal
	} {
		if got := LegacyChatStatusLanguage(LegacyChatTypeEmote, tc.race); got != tc.lang {
			t.Fatalf("emote language for race %d = %d, want %d", tc.race, got, tc.lang)
		}
		if got := LegacyChatStatusLanguage(LegacyChatTypeAFK, tc.race); got != 0 {
			t.Fatalf("AFK language for race %d = %d, want universal", tc.race, got)
		}
		encoded, err := EncodeLegacyChatStatus(LegacyChatTypeEmote, tc.race, "喂食了")
		if err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint32(encoded[4:8]) != tc.lang {
			t.Fatalf("encoded emote language for race %d = %d", tc.race, binary.LittleEndian.Uint32(encoded[4:8]))
		}
	}
}

func TestLegacyGMChatAndPrintNotificationTranslation(t *testing.T) {
	legacy := []byte{legacyChatSystem}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(len("GM\x00")))
	legacy = append(legacy, "GM\x00"...)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(len("Please read the rules.\x00")))
	legacy = append(legacy, "Please read the rules.\x00"...)
	legacy = append(legacy, 1)
	message, err := ParseLegacyGMChatMessage(legacy)
	if err != nil || message.Receiver != 0x42 || message.Text != "Please read the rules." || message.Flags != 1 {
		t.Fatalf("message=%#v err=%v", message, err)
	}

	notification, err := TranslateLegacyPrintNotification([]byte("You are now AFK.\x00"))
	if err != nil {
		t.Fatal(err)
	}
	r := movementReader{data: notification}
	length, err := r.bits(12)
	got, textErr := r.stringN(int(length))
	if err != nil || textErr != nil || got != "You are now AFK." || r.remaining() != 0 {
		t.Fatalf("notification length=%d text=%q errors=%v/%v remaining=%d body=%x", length, got, err, textErr, r.remaining(), notification)
	}
}

func TestDefenseAndChatServerMessage(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 1519)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(len("Hold the line.")+1))
	legacy = append(legacy, "Hold the line.\x00"...)
	defense, err := ParseLegacyDefenseMessage(legacy)
	if err != nil || defense.ZoneID != 1519 || defense.Text != "Hold the line." {
		t.Fatalf("defense=%#v err=%v", defense, err)
	}
	encoded, err := EncodeDefenseMessage(defense)
	if err != nil {
		t.Fatal(err)
	}
	reader := movementReader{data: encoded}
	zone, err := reader.u32()
	length, bitErr := reader.bits(12)
	text, textErr := reader.stringN(int(length))
	if err != nil || bitErr != nil || textErr != nil || zone != 1519 || text != "Hold the line." || reader.remaining() != 0 {
		t.Fatalf("encoded defense zone=%d text=%q remaining=%d body=%x errors=%v/%v/%v", zone, text, reader.remaining(), encoded, err, bitErr, textErr)
	}
	tooLong := DefenseMessage{ZoneID: 1, Text: string(bytes.Repeat([]byte{'a'}, 4096))}
	if _, err := EncodeDefenseMessage(tooLong); err == nil {
		t.Fatal("4096-byte defense message was accepted")
	}
	if _, err := ParseLegacyDefenseMessage(append(legacy, 1)); err == nil {
		t.Fatal("defense message with a trailing byte was accepted")
	}

	serverLegacy := binary.LittleEndian.AppendUint32(nil, 3)
	serverLegacy = append(serverLegacy, "30 seconds\x00"...)
	serverMessage, err := ParseLegacyChatServerMessage(serverLegacy)
	if err != nil || serverMessage.MessageID != 3 || serverMessage.StringParam != "30 seconds" {
		t.Fatalf("server message=%#v err=%v", serverMessage, err)
	}
	encoded, err = EncodeChatServerMessage(serverMessage)
	if err != nil {
		t.Fatal(err)
	}
	reader = movementReader{data: encoded}
	id, err := reader.i32()
	length, bitErr = reader.bits(11)
	text, textErr = reader.stringN(int(length))
	if err != nil || bitErr != nil || textErr != nil || id != 3 || text != "30 seconds" || reader.remaining() != 0 {
		t.Fatalf("encoded server message id=%d text=%q remaining=%d body=%x errors=%v/%v/%v", id, text, reader.remaining(), encoded, err, bitErr, textErr)
	}
	if _, err := EncodeChatServerMessage(ChatServerMessage{StringParam: string(bytes.Repeat([]byte{'b'}, 2048))}); err == nil {
		t.Fatal("2048-byte server message was accepted")
	}
	if _, err := ParseLegacyChatServerMessage(append(serverLegacy, 7)); err == nil {
		t.Fatal("server message with a trailing byte was accepted")
	}
}
