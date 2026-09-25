package modernworld

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestMirrorTimerTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 15000)
	legacy = binary.LittleEndian.AppendUint32(legacy, 60000)
	negativeScale := int32(-1)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(negativeScale))
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1234)
	timer, err := ParseLegacyStartMirrorTimer(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if timer.Timer != 1 || timer.Value != 15000 || timer.Maximum != 60000 || timer.Scale != -1 || timer.SpellID != 1234 || !timer.Paused {
		t.Fatalf("unexpected timer: %#v", timer)
	}
	modern := EncodeStartMirrorTimer(timer)
	if len(modern) != 21 || binary.LittleEndian.Uint32(modern[16:20]) != 1234 || modern[20]&0x80 == 0 {
		t.Fatalf("unexpected modern mirror timer: %x", modern)
	}
	stopped, err := ParseLegacyStopMirrorTimer(binary.LittleEndian.AppendUint32(nil, 2))
	if err != nil || stopped != 2 || len(EncodeStopMirrorTimer(stopped)) != 4 {
		t.Fatalf("stop timer=%d err=%v", stopped, err)
	}

	timerID, paused, err := ParseLegacyPauseMirrorTimer(append(binary.LittleEndian.AppendUint32(nil, 1), 1))
	if err != nil || timerID != 1 || !paused {
		t.Fatalf("pause timer=%d paused=%v err=%v", timerID, paused, err)
	}
	pausedBody := EncodePauseMirrorTimer(timerID, paused)
	if len(pausedBody) != 5 || binary.LittleEndian.Uint32(pausedBody[:4]) != 1 || pausedBody[4] != 0x80 {
		t.Fatalf("paused mirror timer=%x", pausedBody)
	}
	cleared := EncodePauseMirrorTimer(timerID, false)
	if len(cleared) != 5 || cleared[4] != 0 {
		t.Fatalf("unpaused mirror timer=%x", cleared)
	}
	if _, _, err := ParseLegacyPauseMirrorTimer(append(binary.LittleEndian.AppendUint32(nil, 1), 2)); err == nil {
		t.Fatal("pause flag 2 was accepted")
	}
	if _, _, err := ParseLegacyPauseMirrorTimer(binary.LittleEndian.AppendUint32(nil, 1)); err == nil {
		t.Fatal("short pause timer was accepted")
	}
	if _, _, err := ParseLegacyPauseMirrorTimer(append(binary.LittleEndian.AppendUint32(nil, 1), 0, 0)); err == nil {
		t.Fatal("pause timer with a trailing byte was accepted")
	}
}

func TestPlayMusicAndZoneUnderAttack(t *testing.T) {
	sound, err := ParseLegacyPlayMusic(binary.LittleEndian.AppendUint32(nil, 0x1234))
	if err != nil || sound != 0x1234 {
		t.Fatalf("music=%d err=%v", sound, err)
	}
	if got := EncodePlayMusic(sound); !bytes.Equal(got, binary.LittleEndian.AppendUint32(nil, 0x1234)) {
		t.Fatalf("music body=%x", got)
	}
	if _, err := ParseLegacyPlayMusic([]byte{1, 2, 3}); err == nil {
		t.Fatal("short play-music was accepted")
	}
	if _, err := ParseLegacyPlayMusic([]byte{1, 2, 3, 4, 5}); err == nil {
		t.Fatal("play-music with a trailing byte was accepted")
	}

	area, err := ParseLegacyZoneUnderAttack(binary.LittleEndian.AppendUint32(nil, 3518))
	if err != nil || area != 3518 {
		t.Fatalf("area=%d err=%v", area, err)
	}
	if got := EncodeZoneUnderAttack(area); !bytes.Equal(got, binary.LittleEndian.AppendUint32(nil, 3518)) {
		t.Fatalf("zone body=%x", got)
	}
	if _, err := ParseLegacyZoneUnderAttack([]byte{1, 2, 3}); err == nil {
		t.Fatal("short zone-under-attack was accepted")
	}
}

func TestInvalidatePlayerAndSpecialMountAnim(t *testing.T) {
	const legacyGUID = uint64(0xf110000001000043)
	legacy := appendPackedGUID64(nil, legacyGUID)
	parsed, err := ParseLegacyInvalidatePlayer(legacy)
	if err != nil || parsed != legacyGUID {
		t.Fatalf("invalidate guid=%x err=%v", parsed, err)
	}
	modern := EncodeInvalidatePlayer(GUID128{Low: 0x43, High: 1})
	low, high, consumed, err := readPackedGUID128(modern)
	if err != nil || low != 0x43 || high != 1 || consumed != len(modern) {
		t.Fatalf("invalidate=%x consumed=%d err=%v", modern, consumed, err)
	}
	if _, err := ParseLegacyInvalidatePlayer(append(legacy, 1)); err == nil {
		t.Fatal("invalidate-player with a trailing byte was accepted")
	}

	mountGUID, err := ParseLegacySpecialMountAnim(legacy)
	if err != nil || mountGUID != legacyGUID {
		t.Fatalf("mount guid=%x err=%v", mountGUID, err)
	}
	mount := EncodeSpecialMountAnim(GUID128{Low: 0x43, High: 1})
	low, high, consumed, err = readPackedGUID128(mount)
	if err != nil || low != 0x43 || high != 1 {
		t.Fatalf("mount guid=%x err=%v", mount, err)
	}
	if binary.LittleEndian.Uint32(mount[consumed:]) != 0 || binary.LittleEndian.Uint32(mount[consumed+4:]) != 0 || consumed+8 != len(mount) {
		t.Fatalf("mount kits=%x", mount[consumed:])
	}
	if _, err := ParseLegacySpecialMountAnim(append(legacy, 9)); err == nil {
		t.Fatal("special-mount-anim with a trailing byte was accepted")
	}
}

func appendPackedGUID64(destination []byte, guid uint64) []byte {
	mask, packed := packUint64(guid)
	destination = append(destination, mask)
	return append(destination, packed...)
}

func TestWorldSignalTranslation(t *testing.T) {
	sound, err := ParseLegacyPlaySound(binary.LittleEndian.AppendUint32(nil, 987))
	if err != nil || sound != 987 {
		t.Fatalf("sound=%d err=%v", sound, err)
	}
	played := EncodePlaySound(sound, GUID128{Low: 0x42, High: 1})
	low, high, consumed, err := readPackedGUID128(played[4:])
	if err != nil || low != 0x42 || high != 1 || consumed+8 != len(played) {
		t.Fatalf("play sound=%x consumed=%d err=%v", played, consumed, err)
	}

	legacyGUID := uint64(0xf110000001000043)
	parsed, err := ParseLegacyUnpackedGUID(binary.LittleEndian.AppendUint64(nil, legacyGUID))
	if err != nil || parsed != legacyGUID {
		t.Fatalf("guid=%x err=%v", parsed, err)
	}
	despawn := EncodeGameObjectDespawn(GUID128{Low: 0x43, High: 2})
	low, high, consumed, err = readPackedGUID128(despawn)
	if err != nil || low != 0x43 || high != 2 || consumed != len(despawn) {
		t.Fatalf("despawn=%x err=%v", despawn, err)
	}

	legacyAnim := binary.LittleEndian.AppendUint64(nil, legacyGUID)
	legacyAnim = binary.LittleEndian.AppendUint32(legacyAnim, 7)
	anim, err := ParseLegacyGameObjectCustomAnim(legacyAnim)
	if err != nil || anim.GUID != legacyGUID || anim.CustomAnim != 7 {
		t.Fatalf("custom anim=%#v err=%v", anim, err)
	}
	modernAnim := EncodeGameObjectCustomAnim(GUID128{Low: 0x43, High: 2}, anim.CustomAnim)
	low, high, consumed, err = readPackedGUID128(modernAnim)
	if err != nil || low != 0x43 || high != 2 || binary.LittleEndian.Uint32(modernAnim[consumed:]) != 7 || modernAnim[len(modernAnim)-1] != 0 {
		t.Fatalf("custom anim=%x consumed=%d err=%v", modernAnim, consumed, err)
	}
	reset := EncodeGameObjectResetState(GUID128{Low: 0x43, High: 2})
	low, high, consumed, err = readPackedGUID128(reset)
	if err != nil || low != 0x43 || high != 2 || consumed != len(reset) {
		t.Fatalf("reset=%x err=%v", reset, err)
	}
	if err := ValidateLegacyFishEvent(nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLegacyFishEvent([]byte{1}); err == nil {
		t.Fatal("fish event with payload was accepted")
	}
}

func TestLootListTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint64(nil, 0xf130000001000043)
	legacy = appendLegacyPackedGUID(legacy, 0x42)
	legacy = appendLegacyPackedGUID(legacy, 0x44)
	list, err := ParseLegacyLootList(legacy)
	if err != nil || list.Master != 0x42 || list.RoundRobinWinner != 0x44 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	body := EncodeLootList(
		GUID128{Low: 0x43, High: 1},
		GUID128{Low: 0x43, High: 2},
		GUID128{Low: 0x42, High: 1},
		GUID128{Low: 0x44, High: 1},
	)
	_, _, first, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	_, _, second, err := readPackedGUID128(body[first:])
	if err != nil || body[first+second]&0xc0 != 0xc0 {
		t.Fatalf("loot list=%x err=%v", body, err)
	}
}

func TestExplorationExperienceAndDismountTranslation(t *testing.T) {
	legacyExperience := binary.LittleEndian.AppendUint32(nil, 42)
	legacyExperience = binary.LittleEndian.AppendUint32(legacyExperience, 900)
	experience, err := ParseLegacyExplorationExperience(legacyExperience)
	if err != nil || experience.AreaID != 42 || experience.Experience != 900 {
		t.Fatalf("experience=%#v err=%v", experience, err)
	}
	if modern := EncodeExplorationExperience(experience); !bytes.Equal(modern, legacyExperience) {
		t.Fatalf("modern exploration=%x want=%x", modern, legacyExperience)
	}

	legacyDismount := appendLegacyPackedGUID(nil, 0xf130000001000042)
	legacyGUID, err := ParseLegacyDismount(legacyDismount)
	if err != nil || legacyGUID != 0xf130000001000042 {
		t.Fatalf("dismount guid=%x err=%v", legacyGUID, err)
	}
	modernGUID := GUID128{Low: 0x42, High: 1}
	modernDismount := EncodeDismount(modernGUID)
	decoded, err := ParsePackedGUID128Exact(modernDismount)
	if err != nil || decoded != modernGUID {
		t.Fatalf("modern dismount=%#v err=%v body=%x", decoded, err, modernDismount)
	}
}

func TestFactionStandingTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, math.Float32bits(0.25))
	legacy = append(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	legacy = binary.LittleEndian.AppendUint32(legacy, 5)
	hated := int32(-42000)
	legacy = binary.LittleEndian.AppendUint32(legacy, uint32(hated))
	legacy = binary.LittleEndian.AppendUint32(legacy, 8)
	legacy = binary.LittleEndian.AppendUint32(legacy, 21000)
	update, err := ParseLegacyFactionStanding(legacy)
	if err != nil || !update.ShowVisual || len(update.Factions) != 2 || update.Factions[0].Standing != -42000 {
		t.Fatalf("update=%#v err=%v", update, err)
	}
	modern := EncodeFactionStanding(update)
	// Two bonus floats (ReferAFriendBonus + BonusFromAchievementSystem), the
	// faction count, then 2x8 bytes of faction data and the trailing visual bit.
	if len(modern) != 4+4+4+16+1 {
		t.Fatalf("modern faction standing length=%d body=%x", len(modern), modern)
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(modern[4:8])) != 0 {
		t.Fatalf("modern faction standing bonus-from-achievements=%x", modern[4:8])
	}
	if binary.LittleEndian.Uint32(modern[8:12]) != 2 || modern[len(modern)-1]&0x80 == 0 {
		t.Fatalf("modern faction standing=%x", modern)
	}
}

func TestCriteriaUpdateTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 123)
	legacy = appendLegacyPackedGUID(legacy, 5000)
	legacy = appendLegacyPackedGUID(legacy, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x12345678)
	legacy = binary.LittleEndian.AppendUint32(legacy, 20)
	legacy = binary.LittleEndian.AppendUint32(legacy, 21)
	update, err := ParseLegacyCriteriaUpdate(legacy)
	if err != nil || update.CriteriaID != 123 || update.Quantity != 5000 || update.Player != 0x42 || update.Flags != 1 {
		t.Fatalf("update=%#v err=%v", update, err)
	}
	modern := EncodeCriteriaUpdate(update, GUID128{Low: 0x42, High: 1})
	if binary.LittleEndian.Uint32(modern[0:4]) != 123 || binary.LittleEndian.Uint64(modern[4:12]) != 5000 {
		t.Fatalf("modern criteria prefix=%x", modern)
	}
	_, _, consumed, err := readPackedGUID128(modern[12:])
	if err != nil {
		t.Fatal(err)
	}
	position := 12 + consumed
	if binary.LittleEndian.Uint32(modern[position+4:position+8]) != 1 || modern[len(modern)-1] != 0 {
		t.Fatalf("modern criteria=%x", modern)
	}
}

func TestAllAchievementDataTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 6)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x12345678)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.MaxUint32)
	legacy = binary.LittleEndian.AppendUint32(legacy, 123)
	legacy = appendLegacyPackedGUID(legacy, 5000)
	legacy = appendLegacyPackedGUID(legacy, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x23456789)
	legacy = binary.LittleEndian.AppendUint32(legacy, 20)
	legacy = binary.LittleEndian.AppendUint32(legacy, 21)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.MaxUint32)
	data, err := ParseLegacyAllAchievementData(legacy)
	if err != nil || len(data.Earned) != 1 || len(data.Progress) != 1 || data.Earned[0].AchievementID != 6 || data.Progress[0].Quantity != 5000 {
		t.Fatalf("data=%#v err=%v", data, err)
	}
	body := EncodeAllAchievementData(data, GUID128{Low: 0x42, High: 1}, 0x12340001)
	if binary.LittleEndian.Uint32(body[0:4]) != 1 || binary.LittleEndian.Uint32(body[4:8]) != 1 || binary.LittleEndian.Uint32(body[8:12]) != 6 || binary.LittleEndian.Uint32(body[12:16]) != 0x12345678 {
		t.Fatalf("modern all-achievement prefix=%x", body)
	}
	_, _, consumed, err := readPackedGUID128(body[16:])
	if err != nil {
		t.Fatal(err)
	}
	realmPosition := 16 + consumed
	if binary.LittleEndian.Uint32(body[realmPosition:realmPosition+4]) != 0x12340001 || binary.LittleEndian.Uint32(body[realmPosition+4:realmPosition+8]) != 0x12340001 {
		t.Fatalf("modern all-achievement realm=%x", body)
	}
}
