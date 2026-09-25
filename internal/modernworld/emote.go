package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGSendTextEmote  = uint16(13448)
	SMSGEmote          = uint16(10185)
	SMSGTextEmote      = uint16(9850)
	maxEmoteVisualKits = 4
)

type TextEmoteRequest struct {
	Target     GUID128
	EmoteID    int32
	SoundIndex int32
}

func ParseLegacyEmote(body []byte) (uint64, uint32, error) {
	r := movementReader{data: body}
	emoteID, err := r.u32()
	if err != nil {
		return 0, 0, fmt.Errorf("read emote id: %w", err)
	}
	guid, err := r.u64()
	if err != nil {
		return 0, 0, fmt.Errorf("read emote GUID: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, fmt.Errorf("emote has %d trailing bytes", r.remaining())
	}
	return guid, emoteID, nil
}

func EncodeEmote(guid GUID128, emoteID uint32) []byte {
	body := appendPackedGUID128(nil, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint32(body, emoteID)
	body = binary.LittleEndian.AppendUint32(body, 0) // SpellVisualKit count
	return binary.LittleEndian.AppendUint32(body, 0) // SequenceVariation
}

func TranslateEmote(legacy []byte, guid GUID128) ([]byte, error) {
	legacyGUID, emoteID, err := ParseLegacyEmote(legacy)
	if err != nil {
		return nil, err
	}
	if guid.Low == 0 && guid.High == 0 {
		guid = ModernGUIDForLegacy(legacyGUID, 0)
	}
	return EncodeEmote(guid, emoteID), nil
}

func ParseSendTextEmote(body []byte) (TextEmoteRequest, error) {
	r := movementReader{data: body}
	target, err := r.guid128()
	if err != nil {
		return TextEmoteRequest{}, fmt.Errorf("read text-emote target: %w", err)
	}
	emoteID, err := r.i32()
	if err != nil {
		return TextEmoteRequest{}, fmt.Errorf("read text-emote id: %w", err)
	}
	sound, err := r.i32()
	if err != nil {
		return TextEmoteRequest{}, fmt.Errorf("read text-emote sound: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return TextEmoteRequest{}, fmt.Errorf("read text-emote visual count: %w", err)
	}
	if count > maxEmoteVisualKits {
		return TextEmoteRequest{}, fmt.Errorf("text-emote visual count %d exceeds %d", count, maxEmoteVisualKits)
	}
	if _, err = r.i32(); err != nil {
		return TextEmoteRequest{}, fmt.Errorf("read text-emote sequence: %w", err)
	}
	for index := uint32(0); index < count; index++ {
		if _, err = r.u32(); err != nil {
			return TextEmoteRequest{}, fmt.Errorf("read text-emote visual %d: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return TextEmoteRequest{}, fmt.Errorf("text-emote has %d trailing bytes", r.remaining())
	}
	return TextEmoteRequest{Target: target, EmoteID: emoteID, SoundIndex: sound}, nil
}

func EncodeLegacyTextEmote(request TextEmoteRequest, target uint64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(request.EmoteID))
	body = binary.LittleEndian.AppendUint32(body, uint32(request.SoundIndex))
	return binary.LittleEndian.AppendUint64(body, target)
}

func ParseLegacyTextEmote(body []byte) (uint64, int32, int32, error) {
	r := movementReader{data: body}
	legacyGUID, err := r.u64()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read text-emote source: %w", err)
	}
	emoteID, err := r.i32()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read text-emote id: %w", err)
	}
	sound, err := r.i32()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read text-emote sound: %w", err)
	}
	nameLen, err := r.u32()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read text-emote name length: %w", err)
	}
	if int(nameLen) > r.remaining() {
		return 0, 0, 0, fmt.Errorf("text-emote name length %d exceeds %d remaining bytes", nameLen, r.remaining())
	}
	if _, err = r.take(int(nameLen)); err != nil {
		return 0, 0, 0, fmt.Errorf("read text-emote name: %w", err)
	}
	if r.remaining() != 0 {
		return 0, 0, 0, fmt.Errorf("text-emote has %d trailing bytes", r.remaining())
	}
	return legacyGUID, emoteID, sound, nil
}

func EncodeTextEmote(source GUID128, emoteID, sound int32) []byte {
	body := appendPackedGUID128(nil, source.Low, source.High)
	body = appendPackedGUID128(body, 0, 0) // SourceAccountGUID
	body = binary.LittleEndian.AppendUint32(body, uint32(emoteID))
	body = binary.LittleEndian.AppendUint32(body, uint32(sound))
	return appendPackedGUID128(body, 0, 0)
}

func TranslateTextEmote(legacy []byte, source GUID128) ([]byte, error) {
	legacyGUID, emoteID, sound, err := ParseLegacyTextEmote(legacy)
	if err != nil {
		return nil, err
	}
	if source.Low == 0 && source.High == 0 {
		source = ModernGUIDForLegacy(legacyGUID, 0)
	}
	return EncodeTextEmote(source, emoteID, sound), nil
}
