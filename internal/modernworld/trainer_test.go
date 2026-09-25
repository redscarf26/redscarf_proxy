package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestTrainerRequestsAndList(t *testing.T) {
	trainerGUID := GUID128{Low: 0x42, High: 8 << 58}
	body := appendPackedGUID128(nil, trainerGUID.Low, trainerGUID.High)
	body = binary.LittleEndian.AppendUint32(body, 1)
	body = binary.LittleEndian.AppendUint32(body, 2259)
	request, err := ParseTrainerBuySpell(body)
	if err != nil || request.SpellID != 2259 || request.TrainerID != 1 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	legacyBuy := EncodeLegacyTrainerBuySpell(0xf130001234000042, request.SpellID)
	if len(legacyBuy) != 12 || binary.LittleEndian.Uint32(legacyBuy[8:]) != 2259 {
		t.Fatalf("legacy buy=%x", legacyBuy)
	}

	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130001234000042)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2259)
	legacy = append(legacy, 0) // legacy Available -> modern Available(1)
	for _, value := range []uint32{100, 0, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	legacy = append(legacy, 5)
	for _, value := range []uint32{171, 1, 0, 0, 0} {
		legacy = binary.LittleEndian.AppendUint32(legacy, value)
	}
	legacy = append(legacy, "Hello, apprentice."...)
	legacy = append(legacy, 0)
	trainer, err := ParseLegacyTrainerList(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(trainer.Spells) != 1 || trainer.Spells[0].Usable != 1 || trainer.Spells[0].ReqSkillLine != 171 {
		t.Fatalf("trainer=%+v", trainer)
	}
	if modern := EncodeTrainerList(trainerGUID, trainer); len(modern) < 50 {
		t.Fatalf("modern trainer-list=%x", modern)
	}
}

func TestTalentRequestsAndData(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 123)
	body = binary.LittleEndian.AppendUint16(body, 2)
	request, err := ParseLearnTalent(body)
	if err != nil || request.TalentID != 123 || request.Rank != 2 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	legacyRequest := EncodeLegacyLearnTalent(request)
	if len(legacyRequest) != 8 || binary.LittleEndian.Uint32(legacyRequest[4:]) != 2 {
		t.Fatalf("legacy request=%x", legacyRequest)
	}

	legacy := []byte{0}
	legacy = binary.LittleEndian.AppendUint32(legacy, 3)
	legacy = append(legacy, 1, 0, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 123)
	legacy = append(legacy, 2, 1)
	legacy = binary.LittleEndian.AppendUint16(legacy, 456)
	data, err := ParseLegacyTalentData(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if data.Unspent != 3 || len(data.Groups) != 1 || data.Groups[0].Talents[0].Rank != 2 || data.Groups[0].Glyphs[0] != 456 {
		t.Fatalf("data=%+v", data)
	}
	modern := EncodeTalentData(data)
	if binary.LittleEndian.Uint32(modern[:4]) != 3 || modern[4] != 0 || binary.LittleEndian.Uint32(modern[5:9]) != 1 {
		t.Fatalf("modern talent=%x", modern)
	}
}

func TestPetTalentDataTranslation(t *testing.T) {
	legacy := []byte{1}
	legacy = binary.LittleEndian.AppendUint32(legacy, 3)
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2111)
	legacy = append(legacy, 4)

	data, err := ParseLegacyTalentData(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !data.IsPet || data.Unspent != 3 || data.Active != 0 || len(data.Groups) != 1 ||
		len(data.Groups[0].Talents) != 1 || data.Groups[0].Talents[0].TalentID != 2111 ||
		data.Groups[0].Talents[0].Rank != 4 {
		t.Fatalf("unexpected pet talent data: %#v", data)
	}
	modern := EncodeTalentData(data)
	if modern[len(modern)-1]&0x80 == 0 {
		t.Fatalf("modern pet bit not set: %x", modern)
	}

	empty, err := ParseLegacyTalentData([]byte{1, 0, 0, 0, 0, 0})
	if err != nil || !empty.IsPet || len(empty.Groups) != 0 {
		t.Fatalf("empty pet talents=%#v err=%v", empty, err)
	}
}

func TestTalentWipeConfirmRoundTrip(t *testing.T) {
	trainer := GUID128{Low: 0x42, High: uint64(3) << 58}
	body := appendPackedGUID128(nil, trainer.Low, trainer.High)
	body = append(body, SpecResetTalents)
	request, err := ParseConfirmRespecWipe(body)
	if err != nil || request.RespecType != SpecResetTalents || request.Trainer != trainer {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	legacyConfirm := EncodeLegacyTalentWipeConfirm(0xf130001234000042)
	if len(legacyConfirm) != 8 || binary.LittleEndian.Uint64(legacyConfirm) != 0xf130001234000042 {
		t.Fatalf("legacy confirm=%x", legacyConfirm)
	}

	legacyWipe := binary.LittleEndian.AppendUint64(nil, 0xf130001234000042)
	legacyWipe = binary.LittleEndian.AppendUint32(legacyWipe, 10000)
	parsed, err := ParseLegacyTalentWipeConfirm(legacyWipe)
	if err != nil || parsed.Trainer != 0xf130001234000042 || parsed.Cost != 10000 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	modern := EncodeRespecWipeConfirm(trainer, parsed)
	if modern[0] != SpecResetTalents || binary.LittleEndian.Uint32(modern[1:5]) != 10000 {
		t.Fatalf("modern wipe=%x", modern)
	}
	got, err := ParsePackedGUID128Exact(modern[5:])
	if err != nil || got != trainer {
		t.Fatalf("modern trainer=%+v err=%v", got, err)
	}
}
