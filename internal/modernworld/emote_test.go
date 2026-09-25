package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestEmoteAndTextEmote(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 14)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x42)
	guid, emoteID, err := ParseLegacyEmote(legacy)
	if err != nil || guid != 0x42 || emoteID != 14 {
		t.Fatalf("guid=%x emote=%d err=%v", guid, emoteID, err)
	}
	body := EncodeEmote(GUID128{Low: 0x42, High: 1}, 14)
	if binary.LittleEndian.Uint32(body[len(body)-8:len(body)-4]) != 0 {
		t.Fatalf("visual count missing: %x", body)
	}

	textLegacy := binary.LittleEndian.AppendUint64(nil, 0x42)
	textLegacy = binary.LittleEndian.AppendUint32(textLegacy, ^uint32(0))
	textLegacy = binary.LittleEndian.AppendUint32(textLegacy, 5)
	textLegacy = binary.LittleEndian.AppendUint32(textLegacy, 4)
	textLegacy = append(textLegacy, 'N', 'P', 'C', 0)
	source, textEmote, sound, err := ParseLegacyTextEmote(textLegacy)
	if err != nil || source != 0x42 || textEmote != -1 || sound != 5 {
		t.Fatalf("source=%x emote=%d sound=%d err=%v", source, textEmote, sound, err)
	}

	requestBody := appendPackedGUID128(nil, 7, 1)
	requestBody = binary.LittleEndian.AppendUint32(requestBody, 1)
	requestBody = binary.LittleEndian.AppendUint32(requestBody, 0)
	requestBody = binary.LittleEndian.AppendUint32(requestBody, 0)
	requestBody = binary.LittleEndian.AppendUint32(requestBody, 0)
	request, err := ParseSendTextEmote(requestBody)
	if err != nil || request.Target.Low != 7 || request.EmoteID != 1 {
		t.Fatalf("request=%#v err=%v", request, err)
	}
}
