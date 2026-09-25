package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestContactListTranslation(t *testing.T) {
	flags, err := ParseSendContactList(binary.LittleEndian.AppendUint32(nil, 3))
	if err != nil || flags != 3 {
		t.Fatalf("flags=%d err=%v", flags, err)
	}
	legacy := binary.LittleEndian.AppendUint32(nil, 3)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = append(legacy, 'h', 'i', 0, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 12)
	legacy = binary.LittleEndian.AppendUint32(legacy, 80)
	legacy = binary.LittleEndian.AppendUint32(legacy, 8)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x43)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	legacy = append(legacy, 0)
	list, err := ParseLegacyContactList(legacy)
	if err != nil || len(list.Contacts) != 2 || list.Contacts[0].Notes != "hi" || list.Contacts[0].Level != 80 || list.Contacts[1].Status != 0 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	body := EncodeContactList(list, 0x12340001, func(guid uint64) GUID128 { return GUID128{Low: guid, High: 1} })
	if binary.LittleEndian.Uint32(body[:4]) != 3 || body[4] != 2 {
		t.Fatalf("modern contact-list=%x", body)
	}
	r := movementReader{data: body}
	_, _ = r.u32()
	count, _ := r.bits(8)
	contactGUID, _ := r.guid128()
	accountGUID, _ := r.guid128()
	if count != 2 || contactGUID != (GUID128{Low: 0x42, High: 1}) || accountGUID != ModernWowAccountGUIDForLegacy(0x42) {
		t.Fatalf("contact correlation count=%d player=%#v account=%#v body=%x", count, contactGUID, accountGUID, body)
	}
	if binary.LittleEndian.Uint32(EncodeLegacySendContactList(flags)) != 3 {
		t.Fatal("legacy contact-list flags mismatch")
	}
}

func TestAddFriendAndFriendStatusTranslation(t *testing.T) {
	name := "Alice-TestRealm"
	note := "healer"
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(name)), 9)
	bits.writeBits(uint32(len(note)), 10)
	modernRequest := append(bits.flush(), name...)
	modernRequest = append(modernRequest, note...)
	request, err := ParseAddFriend(modernRequest)
	if err != nil || request.Name != name || request.Note != note {
		t.Fatalf("request=%#v err=%v", request, err)
	}
	legacyRequest := EncodeLegacyAddFriend(request)
	if !stringSlicesEqual(legacyRequest, []byte("Alice\x00healer\x00")) {
		t.Fatalf("legacy add-friend=%x", legacyRequest)
	}

	legacyStatus := []byte{6} // FRIEND_ADDED_ONLINE
	legacyStatus = binary.LittleEndian.AppendUint64(legacyStatus, 0x43)
	legacyStatus = append(legacyStatus, "healer"...)
	legacyStatus = append(legacyStatus, 0, 1)
	legacyStatus = binary.LittleEndian.AppendUint32(legacyStatus, 1519)
	legacyStatus = binary.LittleEndian.AppendUint32(legacyStatus, 80)
	legacyStatus = binary.LittleEndian.AppendUint32(legacyStatus, 5)
	status, err := ParseLegacyFriendStatus(legacyStatus)
	if err != nil || status.Result != 6 || status.GUID != 0x43 || status.Notes != "healer" || status.Status != 1 || status.AreaID != 1519 || status.Level != 80 || status.ClassID != 5 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	modernStatus := EncodeFriendStatus(status, 0x01010001, func(guid uint64) GUID128 {
		return GUID128{Low: guid, High: 1}
	})
	r := movementReader{data: modernStatus}
	result, _ := r.u8()
	guid, _ := r.guid128()
	account, _ := r.guid128()
	realmAddress, _ := r.u32()
	online, _ := r.u8()
	area, _ := r.u32()
	level, _ := r.u32()
	classID, _ := r.u32()
	noteLength, _ := r.bits(10)
	mobile, _ := r.bit()
	encodedNote, noteErr := r.stringN(int(noteLength))
	wantAccount := GUID128{Low: 0x43, High: uint64(29) << 58}
	if noteErr != nil || result != 6 || guid != (GUID128{Low: 0x43, High: 1}) || account != wantAccount ||
		realmAddress != 0x01010001 || online != 1 || area != 1519 || level != 80 || classID != 5 ||
		mobile || encodedNote != "healer" || r.remaining() != 0 {
		t.Fatalf("modern friend status result=%d guid=%#v account=%#v realm=%x status=%d area=%d level=%d class=%d note=%q mobile=%v remaining=%d err=%v body=%x",
			result, guid, account, realmAddress, online, area, level, classID, encodedNote, mobile, r.remaining(), noteErr, modernStatus)
	}

	if _, err := ParseAddFriend(modernRequest[:len(modernRequest)-1]); err == nil {
		t.Fatal("truncated add-friend request was accepted")
	}
	if _, err := ParseLegacyFriendStatus(append(legacyStatus, 0)); err == nil {
		t.Fatal("friend status with trailing data was accepted")
	}
}

func TestSocialPacketsUseResolvedGameAccount(t *testing.T) {
	account := ModernWowAccountGUID(0x12345678)
	resolvePlayer := func(guid uint64) GUID128 { return ModernGUIDForLegacy(guid, 0) }
	resolveAccount := func(uint64) GUID128 { return account }
	status := LegacyFriendStatus{Result: 2, GUID: 0x43, Status: 1, AreaID: 12, Level: 80, ClassID: 8}
	body := EncodeFriendStatus(status, 1, resolvePlayer, resolveAccount)
	r := movementReader{data: body}
	_, _ = r.u8()
	_, _ = r.guid128()
	encodedAccount, err := r.guid128()
	if err != nil || encodedAccount != account {
		t.Fatalf("friend account=%#v want=%#v err=%v", encodedAccount, account, err)
	}

	list := ContactList{Contacts: []ContactInfo{{GUID: 0x43, TypeFlags: 1, Status: 1}}}
	body = EncodeContactList(list, 1, resolvePlayer, resolveAccount)
	r = movementReader{data: body}
	_, _ = r.u32()
	_, _ = r.bits(8)
	_, _ = r.guid128()
	encodedAccount, err = r.guid128()
	if err != nil || encodedAccount != account {
		t.Fatalf("contact account=%#v want=%#v err=%v", encodedAccount, account, err)
	}
}

func stringSlicesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestAddIgnoreTranslation(t *testing.T) {
	name := "Bob-Realm"
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(name)), 9)
	modernRequest := append(bits.flush(), name...)

	request, err := ParseAddIgnore(modernRequest)
	if err != nil || request.Name != name {
		t.Fatalf("request=%#v err=%v", request, err)
	}
	legacyRequest := EncodeLegacyAddIgnore(request.Name)
	if !stringSlicesEqual(legacyRequest, []byte("Bob\x00")) {
		t.Fatalf("legacy add-ignore=%x", legacyRequest)
	}
	if _, err := ParseAddIgnore(modernRequest[:len(modernRequest)-1]); err == nil {
		t.Fatal("truncated add-ignore request was accepted")
	}
	if _, err := ParseAddIgnore([]byte{0x00, 0x00}); err == nil {
		t.Fatal("empty add-ignore name was accepted")
	}
}

func TestDelFriendAndSetContactNotesTranslation(t *testing.T) {
	guid := GUID128{Low: 0x43, High: uint64(2)<<58 | uint64(1)<<42}
	packed := appendPackedGUID128(nil, guid.Low, guid.High)
	delBody := binary.LittleEndian.AppendUint32(nil, 0x01010001)
	delBody = append(delBody, packed...)
	request, err := ParseDelFriend(delBody)
	if err != nil || request.RealmAddress != 0x01010001 || request.GUID != guid {
		t.Fatalf("del-friend=%#v err=%v", request, err)
	}
	legacy := EncodeLegacyDelFriend(0x43)
	if binary.LittleEndian.Uint64(legacy) != 0x43 || len(legacy) != 8 {
		t.Fatalf("legacy del-friend=%x", legacy)
	}
	if _, err := ParseDelFriend(delBody[:len(delBody)-1]); err == nil {
		t.Fatal("truncated del-friend was accepted")
	}
	if _, err := ParseDelFriend(append(binary.LittleEndian.AppendUint32(nil, 1), appendPackedGUID128(nil, 0, 0)...)); err == nil {
		t.Fatal("empty del-friend GUID was accepted")
	}

	note := "healer"
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(note)), 10)
	notesBody := binary.LittleEndian.AppendUint32(nil, 0x01010001)
	notesBody = append(notesBody, packed...)
	notesBody = append(notesBody, bits.flush()...)
	notesBody = append(notesBody, note...)
	set, err := ParseSetContactNotes(notesBody)
	if err != nil || set.GUID != guid || set.Notes != note {
		t.Fatalf("set-notes=%#v err=%v", set, err)
	}
	legacyNotes := EncodeLegacySetContactNotes(0x43, note)
	want := binary.LittleEndian.AppendUint64(nil, 0x43)
	want = append(want, "healer"...)
	want = append(want, 0)
	if !stringSlicesEqual(legacyNotes, want) {
		t.Fatalf("legacy set-notes=%x", legacyNotes)
	}
	if _, err := ParseSetContactNotes(notesBody[:len(notesBody)-1]); err == nil {
		t.Fatal("truncated set-contact-notes was accepted")
	}
}
