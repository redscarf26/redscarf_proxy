package modernworld

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const (
	CMSGSendContactList  = uint16(0x36D7)
	CMSGAddFriend        = uint16(0x36D8)
	CMSGDelFriend        = uint16(0x36D9)
	CMSGSetContactNotes  = uint16(0x36DA)
	CMSGAddIgnore        = uint16(14044)
	CMSGDelIgnore        = uint16(0x36DD)
	SMSGContactList      = uint16(0x278C)
	SMSGFriendStatus     = uint16(0x278D)
	maxContactNoteBytes  = 1023
)

type AddFriendRequest struct {
	Name string
	Note string
}

type LegacyFriendStatus struct {
	Result  byte
	GUID    uint64
	Notes   string
	Status  byte
	AreaID  uint32
	Level   uint32
	ClassID uint32
}

type ContactInfo struct {
	GUID      uint64
	TypeFlags uint32
	Notes     string
	Status    byte
	AreaID    uint32
	Level     uint32
	ClassID   uint32
}

type ContactList struct {
	Flags    uint32
	Contacts []ContactInfo
}

func ParseSendContactList(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("send-contact-list has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func ParseAddFriend(body []byte) (AddFriendRequest, error) {
	var request AddFriendRequest
	r := movementReader{data: body}
	nameLength, err := r.bits(9)
	if err != nil {
		return request, fmt.Errorf("read add-friend name length: %w", err)
	}
	noteLength, err := r.bits(10)
	if err != nil {
		return request, fmt.Errorf("read add-friend note length: %w", err)
	}
	if request.Name, err = r.stringN(int(nameLength)); err != nil {
		return request, fmt.Errorf("read add-friend name: %w", err)
	}
	if request.Note, err = r.stringN(int(noteLength)); err != nil {
		return request, fmt.Errorf("read add-friend note: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("add-friend has %d trailing bytes", r.remaining())
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return request, fmt.Errorf("add-friend name is empty")
	}
	return request, nil
}

func EncodeLegacyAddFriend(request AddFriendRequest) []byte {
	// WotLK does not support cross-realm friends. legacy proxy strips the realm
	// suffix before forwarding the CString to the 3.3.5a server.
	name := request.Name
	if base, _, found := strings.Cut(name, "-"); found {
		name = base
	}
	body := append([]byte(nil), name...)
	body = append(body, 0)
	body = append(body, request.Note...)
	return append(body, 0)
}

// AddIgnoreRequest names the player whose whispers should be ignored.
type AddIgnoreRequest struct {
	Name string
}

func ParseAddIgnore(body []byte) (AddIgnoreRequest, error) {
	var request AddIgnoreRequest
	r := movementReader{data: body}
	nameLength, err := r.bits(9)
	if err != nil {
		return request, fmt.Errorf("read add-ignore name length: %w", err)
	}
	if request.Name, err = r.stringN(int(nameLength)); err != nil {
		return request, fmt.Errorf("read add-ignore name: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("add-ignore has %d trailing bytes", r.remaining())
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return request, fmt.Errorf("add-ignore name is empty")
	}
	return request, nil
}

// EncodeLegacyAddIgnore writes the WotLK CMSG_ADD_IGNORE CString name,
// stripping any unsupported cross-realm suffix like the friend path does.
func EncodeLegacyAddIgnore(name string) []byte {
	if base, _, found := strings.Cut(name, "-"); found {
		name = base
	}
	return append([]byte(name), 0)
}

// DelFriendRequest is the 3.4.3 CMSG_DEL_FRIEND / CMSG_DEL_IGNORE body:
// virtual realm address plus the contact's PackedGuid128.
type DelFriendRequest struct {
	RealmAddress uint32
	GUID         GUID128
}

func ParseDelFriend(body []byte) (DelFriendRequest, error) {
	var request DelFriendRequest
	r := movementReader{data: body}
	var err error
	if request.RealmAddress, err = r.u32(); err != nil {
		return request, fmt.Errorf("read del-friend realm: %w", err)
	}
	if request.GUID, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read del-friend GUID: %w", err)
	}
	if request.GUID.Low == 0 && request.GUID.High == 0 {
		return request, fmt.Errorf("del-friend GUID is empty")
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("del-friend has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacyDelFriend(guid uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, guid)
}

// SetContactNotesRequest is CMSG_SET_CONTACT_NOTES: realm, PackedGuid128,
// then a 10-bit note length and the note bytes, matching Hermes SetContactNotes.
type SetContactNotesRequest struct {
	RealmAddress uint32
	GUID         GUID128
	Notes        string
}

func ParseSetContactNotes(body []byte) (SetContactNotesRequest, error) {
	var request SetContactNotesRequest
	r := movementReader{data: body}
	var err error
	if request.RealmAddress, err = r.u32(); err != nil {
		return request, fmt.Errorf("read set-contact-notes realm: %w", err)
	}
	if request.GUID, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read set-contact-notes GUID: %w", err)
	}
	if request.GUID.Low == 0 && request.GUID.High == 0 {
		return request, fmt.Errorf("set-contact-notes GUID is empty")
	}
	noteLength, err := r.bits(10)
	if err != nil {
		return request, fmt.Errorf("read set-contact-notes length: %w", err)
	}
	if request.Notes, err = r.stringN(int(noteLength)); err != nil {
		return request, fmt.Errorf("read set-contact-notes body: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("set-contact-notes has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func EncodeLegacySetContactNotes(guid uint64, notes string) []byte {
	body := binary.LittleEndian.AppendUint64(nil, guid)
	body = append(body, truncateWireString(notes, maxContactNoteBytes)...)
	return append(body, 0)
}

func ParseLegacyContactList(body []byte) (ContactList, error) {
	var list ContactList
	r := movementReader{data: body}
	var err error
	if list.Flags, err = r.u32(); err != nil {
		return list, fmt.Errorf("read contact-list flags: %w", err)
	}
	count, err := r.u32()
	if err != nil {
		return list, fmt.Errorf("read contact-list count: %w", err)
	}
	if count > 255 {
		return list, fmt.Errorf("contact-list has %d contacts, maximum is 255", count)
	}
	list.Contacts = make([]ContactInfo, count)
	for index := range list.Contacts {
		contact := &list.Contacts[index]
		if contact.GUID, err = r.u64(); err != nil {
			return list, fmt.Errorf("read contact %d GUID: %w", index, err)
		}
		if contact.TypeFlags, err = r.u32(); err != nil {
			return list, fmt.Errorf("read contact %d flags: %w", index, err)
		}
		note, readErr := readLegacyCString(&r, 1023)
		if readErr != nil {
			return list, fmt.Errorf("read contact %d note: %w", index, readErr)
		}
		contact.Notes = note
		if contact.TypeFlags&1 != 0 {
			if contact.Status, err = r.u8(); err != nil {
				return list, fmt.Errorf("read contact %d status: %w", index, err)
			}
			if contact.Status != 0 {
				if contact.AreaID, err = r.u32(); err != nil {
					return list, fmt.Errorf("read contact %d area: %w", index, err)
				}
				if contact.Level, err = r.u32(); err != nil {
					return list, fmt.Errorf("read contact %d level: %w", index, err)
				}
				if contact.ClassID, err = r.u32(); err != nil {
					return list, fmt.Errorf("read contact %d class: %w", index, err)
				}
			}
		}
	}
	if r.remaining() != 0 {
		return list, fmt.Errorf("contact-list has %d trailing bytes", r.remaining())
	}
	return list, nil
}

func ParseLegacyFriendStatus(body []byte) (LegacyFriendStatus, error) {
	var status LegacyFriendStatus
	r := movementReader{data: body}
	var err error
	if status.Result, err = r.u8(); err != nil {
		return status, fmt.Errorf("read friend result: %w", err)
	}
	if status.GUID, err = r.u64(); err != nil {
		return status, fmt.Errorf("read friend GUID: %w", err)
	}
	switch status.Result {
	case 7: // FRIEND_ADDED_OFFLINE
		if status.Notes, err = readLegacyCString(&r, 1023); err != nil {
			return status, fmt.Errorf("read offline friend note: %w", err)
		}
	case 6: // FRIEND_ADDED_ONLINE
		if status.Notes, err = readLegacyCString(&r, 1023); err != nil {
			return status, fmt.Errorf("read online friend note: %w", err)
		}
		fallthrough
	case 2: // FRIEND_ONLINE
		if status.Status, err = r.u8(); err != nil {
			return status, fmt.Errorf("read friend status: %w", err)
		}
		if status.AreaID, err = r.u32(); err != nil {
			return status, fmt.Errorf("read friend area: %w", err)
		}
		if status.Level, err = r.u32(); err != nil {
			return status, fmt.Errorf("read friend level: %w", err)
		}
		if status.ClassID, err = r.u32(); err != nil {
			return status, fmt.Errorf("read friend class: %w", err)
		}
	}
	if r.remaining() != 0 {
		return status, fmt.Errorf("friend status has %d trailing bytes", r.remaining())
	}
	return status, nil
}

func readLegacyCString(r *movementReader, maximum int) (string, error) {
	r.align()
	start := r.offset
	for r.offset < len(r.data) && r.data[r.offset] != 0 {
		r.offset++
		if r.offset-start > maximum {
			return "", fmt.Errorf("string exceeds %d bytes", maximum)
		}
	}
	if r.offset == len(r.data) {
		return "", fmt.Errorf("string is not NUL terminated")
	}
	value := string(r.data[start:r.offset])
	r.offset++
	return value, nil
}

func EncodeLegacySendContactList(flags uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, flags)
}

func EncodeFriendStatus(status LegacyFriendStatus, realmAddress uint32, resolve func(uint64) GUID128, accountResolvers ...func(uint64) GUID128) []byte {
	body := []byte{status.Result}
	guid := resolve(status.GUID)
	body = appendPackedGUID128(body, guid.Low, guid.High)
	account := resolveWowAccountGUID(status.GUID, accountResolvers)
	body = appendPackedGUID128(body, account.Low, account.High)
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = append(body, status.Status)
	body = binary.LittleEndian.AppendUint32(body, status.AreaID)
	body = binary.LittleEndian.AppendUint32(body, status.Level)
	body = binary.LittleEndian.AppendUint32(body, status.ClassID)
	notes := truncateWireString(status.Notes, 1023)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(notes)), 10)
	bits.writeBit(false) // not a mobile contact
	body = bits.flush()
	return append(body, notes...)
}

func EncodeContactList(list ContactList, realmAddress uint32, resolve func(uint64) GUID128, accountResolvers ...func(uint64) GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, list.Flags)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(list.Contacts)), 8)
	body = bits.flush()
	for _, contact := range list.Contacts {
		guid := resolve(contact.GUID)
		body = appendPackedGUID128(body, guid.Low, guid.High)
		account := resolveWowAccountGUID(contact.GUID, accountResolvers)
		body = appendPackedGUID128(body, account.Low, account.High)
		body = binary.LittleEndian.AppendUint32(body, realmAddress)
		body = binary.LittleEndian.AppendUint32(body, realmAddress)
		body = binary.LittleEndian.AppendUint32(body, contact.TypeFlags)
		body = append(body, contact.Status)
		body = binary.LittleEndian.AppendUint32(body, contact.AreaID)
		body = binary.LittleEndian.AppendUint32(body, contact.Level)
		body = binary.LittleEndian.AppendUint32(body, contact.ClassID)
		notes := truncateWireString(contact.Notes, 1023)
		bits = newBitWriter(body)
		bits.writeBits(uint32(len(notes)), 10)
		bits.writeBit(false) // not a mobile contact
		body = bits.flush()
		body = append(body, notes...)
	}
	return body
}

// ModernWowAccountGUIDForLegacy supplies the stable account identity that the
// 3.4.3 social cache uses to correlate contact-list rows with later online/offline
// SMSG_FRIEND_STATUS updates. WotLK does not expose the real account ID, so the
// player counter is used exactly as HermesProxy's fallback does.
func ModernWowAccountGUIDForLegacy(guid uint64) GUID128 {
	if guid == 0 {
		return GUID128{}
	}
	return ModernWowAccountGUID(uint64(uint32(guid)))
}

func ModernWowAccountGUID(accountID uint64) GUID128 {
	if accountID == 0 {
		return GUID128{}
	}
	return GUID128{Low: accountID, High: uint64(29) << 58}
}

// ModernBNetAccountGUIDForLegacy supplies the stable Battle.net account
// identity used by the 3.4.3 player name cache when no real account mapping is
// available from the legacy server.
func ModernBNetAccountGUIDForLegacy(guid uint64) GUID128 {
	if guid == 0 {
		return GUID128{}
	}
	return ModernBNetAccountGUID(uint64(uint32(guid)))
}

func ModernBNetAccountGUID(accountID uint64) GUID128 {
	if accountID == 0 {
		return GUID128{}
	}
	return GUID128{Low: accountID, High: uint64(30) << 58}
}

func resolveWowAccountGUID(legacyGUID uint64, resolvers []func(uint64) GUID128) GUID128 {
	if len(resolvers) != 0 && resolvers[0] != nil {
		if account := resolvers[0](legacyGUID); account != (GUID128{}) {
			return account
		}
	}
	return ModernWowAccountGUIDForLegacy(legacyGUID)
}
