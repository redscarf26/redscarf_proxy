package modernworld

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Mailbox translation (build 54261 client <-> 3.3.5a AzerothCore). The modern
// engine re-serialized the mailbox controls with GUID128 mailbox/item GUIDs and
// wider id/currency fields, and moved strings into a length-bit-block after the
// fixed header. Layouts mirror HermesProxy-WOTLK World/Server/Packets
// {MailGetList,MailMarkAsRead,MailTakeItem,MailTakeMoney,MailDelete,
// MailReturnToSender,MailCreateTextItem,SendMail,MailListResult,
// MailAttachedItem,ItemInstance,MailCommandResult,NotifyReceivedMail}.cs and
// World/Server/WorldSocket.cs / World/Client/WorldClient.cs. The only mail
// path previously handled here was the next-mail-time ping.

const (
	// Client -> server mailbox controls (build 54261).
	CMSGMailGetList         = uint16(0x3536) // 13622
	CMSGMailTakeMoney       = uint16(0x3537) // 13623
	CMSGMailTakeItem        = uint16(0x3538) // 13624
	CMSGMailMarkAsRead      = uint16(0x353A) // 13626
	CMSGMailCreateTextItem  = uint16(0x353B) // 13627
	CMSGMailDelete          = uint16(0x3225) // 12837
	CMSGMailReturnToSender  = uint16(0x3658) // 13912
	CMSGSendMail            = uint16(0x35FB) // 13819

	// Server -> client mailbox data.
	SMSGMailListResult       = uint16(0x2756) // 10070
	SMSGMailCommandResult    = uint16(0x263B) // 9787
	SMSGNotifyReceivedMail   = uint16(0x263C) // 9788
)

const (
	mailMaxAttachments = 12
	mailMaxEnchants    = 7
	mailMaxRecords     = 50
	mailMaxItemEnchant = 12
)

// MailboxRequest is a client->server mailbox command whose payload is only the
// mailbox object GUID (the mailbox GameObject or mail NPC the player opened).
type MailboxRequest struct {
	Mailbox GUID128
}

// MailIDRequest adds the mail's list id to the mailbox GUID.
type MailIDRequest struct {
	Mailbox GUID128
	MailID  uint64
}

// MailTakeMoneyRequest is CMSG_MAIL_TAKE_MONEY: mailbox + mail id + the gold
// amount the 3.4.3 client thinks it is taking. AzerothCore looks the letter up
// by id only, so Money is parsed then dropped on the legacy wire.
type MailTakeMoneyRequest struct {
	Mailbox GUID128
	MailID  uint64
	Money   int64
}

// MailTakeItemRequest adds the attached-item id.
type MailTakeItemRequest struct {
	Mailbox GUID128
	MailID  uint64
	Attach  uint64
}

// MailDeleteRequest carries only the mail id plus the modern delete reason
// (dropped; 3.3.5a has no equivalent). The mailbox GUID is supplied by the
// proxy from the tracked current-interacted GameObject.
type MailDeleteRequest struct {
	MailID  uint64
	Reason  int32
}

// MailReturnRequest adds the original sender GUID.
type MailReturnRequest struct {
	MailID  uint64
	Sender  GUID128
}

// SendMailAttachment is one item attached to an outbound letter.
type SendMailAttachment struct {
	Position uint8
	Item     GUID128
}

// SendMailRequest is the modern CMSG_SEND_MAIL payload.
type SendMailRequest struct {
	Mailbox     GUID128
	Stationery  int32
	SendMoney   int64
	COD         int64
	Target      string
	Subject     string
	Body        string
	Attachments []SendMailAttachment
}

func ParseMailboxRequest(body []byte) (MailboxRequest, error) {
	var request MailboxRequest
	r := movementReader{data: body}
	var err error
	if request.Mailbox, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read mailbox GUID: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("mailbox request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseMailIDRequest(body []byte) (MailIDRequest, error) {
	var request MailIDRequest
	r := movementReader{data: body}
	var err error
	if request.Mailbox, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read mailbox GUID: %w", err)
	}
	if request.MailID, err = r.u64(); err != nil {
		return request, fmt.Errorf("read mail id: %w", err)
	}
	if request.MailID > uint64(^uint32(0)) {
		return request, fmt.Errorf("mail id %d exceeds 32 bits", request.MailID)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("mail-id request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseMailTakeMoneyRequest(body []byte) (MailTakeMoneyRequest, error) {
	var request MailTakeMoneyRequest
	r := movementReader{data: body}
	var err error
	if request.Mailbox, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read mailbox GUID: %w", err)
	}
	if request.MailID, err = r.u64(); err != nil {
		return request, fmt.Errorf("read mail id: %w", err)
	}
	if request.MailID > uint64(^uint32(0)) {
		return request, fmt.Errorf("mail id %d exceeds 32 bits", request.MailID)
	}
	money, readErr := r.u64()
	if readErr != nil {
		return request, fmt.Errorf("read mail take-money amount: %w", readErr)
	}
	request.Money = int64(money)
	if r.remaining() != 0 {
		return request, fmt.Errorf("mail take-money request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseMailTakeItemRequest(body []byte) (MailTakeItemRequest, error) {
	var request MailTakeItemRequest
	r := movementReader{data: body}
	var err error
	if request.Mailbox, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read mailbox GUID: %w", err)
	}
	if request.MailID, err = r.u64(); err != nil {
		return request, fmt.Errorf("read mail id: %w", err)
	}
	if request.MailID > uint64(^uint32(0)) {
		return request, fmt.Errorf("mail id %d exceeds 32 bits", request.MailID)
	}
	if request.Attach, err = r.u64(); err != nil {
		return request, fmt.Errorf("read mail attach id: %w", err)
	}
	if request.Attach > uint64(^uint32(0)) {
		return request, fmt.Errorf("mail attach id %d exceeds 32 bits", request.Attach)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("mail take-item request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseMailDeleteRequest(body []byte) (MailDeleteRequest, error) {
	var request MailDeleteRequest
	r := movementReader{data: body}
	var err error
	if request.MailID, err = r.u64(); err != nil {
		return request, fmt.Errorf("read mail delete id: %w", err)
	}
	if request.MailID > uint64(^uint32(0)) {
		return request, fmt.Errorf("mail delete id %d exceeds 32 bits", request.MailID)
	}
	reason, readErr := r.i32()
	if readErr != nil {
		return request, fmt.Errorf("read mail delete reason: %w", readErr)
	}
	request.Reason = reason
	if r.remaining() != 0 {
		return request, fmt.Errorf("mail delete request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseMailReturnRequest(body []byte) (MailReturnRequest, error) {
	var request MailReturnRequest
	r := movementReader{data: body}
	var err error
	if request.MailID, err = r.u64(); err != nil {
		return request, fmt.Errorf("read mail return id: %w", err)
	}
	if request.MailID > uint64(^uint32(0)) {
		return request, fmt.Errorf("mail return id %d exceeds 32 bits", request.MailID)
	}
	if request.Sender, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read mail return sender: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("mail return request has %d trailing bytes", r.remaining())
	}
	return request, nil
}

func ParseSendMail(body []byte) (SendMailRequest, error) {
	var request SendMailRequest
	r := movementReader{data: body}
	var err error
	if request.Mailbox, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read send-mail mailbox: %w", err)
	}
	if request.Stationery, err = r.i32(); err != nil {
		return request, fmt.Errorf("read send-mail stationery: %w", err)
	}
	money, readErr := r.u64()
	if readErr != nil {
		return request, fmt.Errorf("read send-mail money: %w", readErr)
	}
	request.SendMoney = int64(money)
	cod, readErr := r.u64()
	if readErr != nil {
		return request, fmt.Errorf("read send-mail cod: %w", readErr)
	}
	request.COD = int64(cod)
	targetLength, readErr := r.bits(9)
	if readErr != nil {
		return request, fmt.Errorf("read send-mail target length: %w", readErr)
	}
	subjectLength, readErr := r.bits(9)
	if readErr != nil {
		return request, fmt.Errorf("read send-mail subject length: %w", readErr)
	}
	bodyLength, readErr := r.bits(11)
	if readErr != nil {
		return request, fmt.Errorf("read send-mail body length: %w", readErr)
	}
	attachCount, readErr := r.bits(5)
	if readErr != nil {
		return request, fmt.Errorf("read send-mail attachment count: %w", readErr)
	}
	if attachCount > mailMaxAttachments {
		return request, fmt.Errorf("send-mail has %d attachments, maximum is %d", attachCount, mailMaxAttachments)
	}
	if request.Target, err = r.stringN(int(targetLength)); err != nil {
		return request, fmt.Errorf("read send-mail target: %w", err)
	}
	if request.Subject, err = r.stringN(int(subjectLength)); err != nil {
		return request, fmt.Errorf("read send-mail subject: %w", err)
	}
	if request.Body, err = r.stringN(int(bodyLength)); err != nil {
		return request, fmt.Errorf("read send-mail body: %w", err)
	}
	request.Attachments = make([]SendMailAttachment, 0, attachCount)
	for index := uint32(0); index < attachCount; index++ {
		var attachment SendMailAttachment
		if attachment.Position, err = r.u8(); err != nil {
			return request, fmt.Errorf("read send-mail attachment %d position: %w", index, err)
		}
		if attachment.Item, err = r.guid128(); err != nil {
			return request, fmt.Errorf("read send-mail attachment %d item: %w", index, err)
		}
		request.Attachments = append(request.Attachments, attachment)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("send-mail has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// EncodeLegacyMailboxCommand writes a WotLK mailbox GUID (raw 64-bit) followed
// by the optional mail/attachment ids, matching AzerothCore's CMSG handlers.
func EncodeLegacyMailboxCommand(mailbox uint64, rest []byte) []byte {
	body := binary.LittleEndian.AppendUint64(nil, mailbox)
	return append(body, rest...)
}

func EncodeLegacyMailGetList(mailbox uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, mailbox)
}

func EncodeLegacyMailIDCommand(mailbox uint64, mailID uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, mailbox)
	return binary.LittleEndian.AppendUint32(body, uint32(mailID))
}

func EncodeLegacyMailTakeItem(mailbox uint64, mailID uint64, attach uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, mailbox)
	body = binary.LittleEndian.AppendUint32(body, uint32(mailID))
	return binary.LittleEndian.AppendUint32(body, uint32(attach))
}

// EncodeLegacyMailDelete writes the WotLK CMSG_MAIL_DELETE body: mailbox GUID
// followed by the mail id and the trailing zero long present since 2.0.1.
func EncodeLegacyMailDelete(mailbox uint64, mailID uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, mailbox)
	body = binary.LittleEndian.AppendUint32(body, uint32(mailID))
	return binary.LittleEndian.AppendUint32(body, 0)
}

// EncodeLegacyMailReturn writes the WotLK CMSG_MAIL_RETURN_TO_SENDER body:
// mailbox GUID, mail id, then the original sender GUID64.
func EncodeLegacyMailReturn(mailbox uint64, mailID uint64, sender uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, mailbox)
	body = binary.LittleEndian.AppendUint32(body, uint32(mailID))
	return binary.LittleEndian.AppendUint64(body, sender)
}

func EncodeLegacySendMail(request SendMailRequest, mailbox uint64, resolve func(GUID128) uint64) []byte {
	body := binary.LittleEndian.AppendUint64(nil, mailbox)
	body = appendCString(body, request.Target)
	body = appendCString(body, request.Subject)
	body = appendCString(body, request.Body)
	body = binary.LittleEndian.AppendUint32(body, uint32(request.Stationery))
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = append(body, byte(len(request.Attachments)))
	for _, attachment := range request.Attachments {
		body = append(body, attachment.Position)
		body = binary.LittleEndian.AppendUint64(body, resolve(attachment.Item))
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(request.SendMoney))
	body = binary.LittleEndian.AppendUint32(body, uint32(request.COD))
	body = binary.LittleEndian.AppendUint64(body, 0)
	return append(body, 0)
}

// LegacyMailListItemEnchant is one enchantment slot carried on an attached item.
type LegacyMailListItemEnchant struct {
	Slot       byte
	Charges    int32
	Expiration uint32
	ID         uint32
}

// LegacyMailAttachedItem mirrors 3.3.5a SMSG_MAIL_LIST_RESULT attached items.
type LegacyMailAttachedItem struct {
	Position            uint8
	AttachID            int32
	ItemID              uint32
	Enchants            []LegacyMailListItemEnchant
	RandomPropertiesID  uint32
	RandomPropertiesSeed uint32
	Count               uint32
	Charges             int32
	MaxDurability       uint32
	Durability          uint32
	Unlocked            bool
}

// LegacyMailEntry is one mail message in the 3.3.5a mail list.
type LegacyMailEntry struct {
	MailID         int32
	SenderType     uint8
	SenderGUID     uint64
	AltSenderID    uint32
	COD            uint32
	StationeryID   int32
	SentMoney      uint32
	Flags          uint32
	DaysLeft       float32
	MailTemplateID int32
	Subject        string
	Body           string
	Attachments    []LegacyMailAttachedItem
}

// LegacyMailListResult is the parsed 3.3.5a SMSG_MAIL_LIST_RESULT payload.
type LegacyMailListResult struct {
	Total int32
	Mails []LegacyMailEntry
}

func readLegacyMailItem(r *movementReader) (LegacyMailAttachedItem, error) {
	var item LegacyMailAttachedItem
	var err error
	if item.Position, err = r.u8(); err != nil {
		return item, fmt.Errorf("read mail item position: %w", err)
	}
	if item.AttachID, err = r.i32(); err != nil {
		return item, fmt.Errorf("read mail item attach id: %w", err)
	}
	if item.ItemID, err = r.u32(); err != nil {
		return item, fmt.Errorf("read mail item id: %w", err)
	}
	for slot := byte(0); slot < mailMaxEnchants; slot++ {
		var enchant LegacyMailListItemEnchant
		enchant.Slot = slot
		if enchant.Charges, err = r.i32(); err != nil {
			return item, fmt.Errorf("read mail item enchant %d charges: %w", slot, err)
		}
		if enchant.Expiration, err = r.u32(); err != nil {
			return item, fmt.Errorf("read mail item enchant %d expiration: %w", slot, err)
		}
		if enchant.ID, err = r.u32(); err != nil {
			return item, fmt.Errorf("read mail item enchant %d id: %w", slot, err)
		}
		if enchant.ID != 0 {
			item.Enchants = append(item.Enchants, enchant)
		}
	}
	if item.RandomPropertiesID, err = r.u32(); err != nil {
		return item, fmt.Errorf("read mail item random property: %w", err)
	}
	if item.RandomPropertiesSeed, err = r.u32(); err != nil {
		return item, fmt.Errorf("read mail item random property seed: %w", err)
	}
	count, readErr := r.u32()
	if readErr != nil {
		return item, fmt.Errorf("read mail item count: %w", readErr)
	}
	if count > 0x7fffffff {
		return item, fmt.Errorf("mail item count %d overflows", count)
	}
	item.Count = count
	if item.Charges, err = r.i32(); err != nil {
		return item, fmt.Errorf("read mail item charges: %w", err)
	}
	if item.MaxDurability, err = r.u32(); err != nil {
		return item, fmt.Errorf("read mail item max durability: %w", err)
	}
	if item.Durability, err = r.u32(); err != nil {
		return item, fmt.Errorf("read mail item durability: %w", err)
	}
	unlocked, readErr := r.u8()
	if readErr != nil {
		return item, fmt.Errorf("read mail item unlocked flag: %w", readErr)
	}
	item.Unlocked = unlocked != 0
	return item, nil
}

func ParseLegacyMailListResult(body []byte) (LegacyMailListResult, error) {
	var result LegacyMailListResult
	r := movementReader{data: body}
	var err error
	total, readErr := r.i32()
	if readErr != nil {
		return result, fmt.Errorf("read mail list total: %w", readErr)
	}
	result.Total = total
	count, readErr := r.u8()
	if readErr != nil {
		return result, fmt.Errorf("read mail list count: %w", readErr)
	}
	if count > mailMaxRecords {
		return result, fmt.Errorf("mail list has %d entries, maximum is %d", count, mailMaxRecords)
	}
	result.Mails = make([]LegacyMailEntry, 0, count)
	for index := 0; index < int(count); index++ {
		var mail LegacyMailEntry
		if _, err = r.u16(); err != nil {
			return result, fmt.Errorf("read mail %d size: %w", index, err)
		}
		if mail.MailID, err = r.i32(); err != nil {
			return result, fmt.Errorf("read mail %d id: %w", index, err)
		}
		if mail.SenderType, err = r.u8(); err != nil {
			return result, fmt.Errorf("read mail %d sender type: %w", index, err)
		}
		switch mail.SenderType {
		case 0: // normal player sender
			if mail.SenderGUID, err = r.u64(); err != nil {
				return result, fmt.Errorf("read mail %d sender: %w", index, err)
			}
		default: // auction / creature / gameobject / item
			if mail.AltSenderID, err = r.u32(); err != nil {
				return result, fmt.Errorf("read mail %d alternate sender: %w", index, err)
			}
		}
		if mail.COD, err = r.u32(); err != nil {
			return result, fmt.Errorf("read mail %d cod: %w", index, err)
		}
		if _, err = r.u32(); err != nil { // discarded item-text slot
			return result, fmt.Errorf("read mail %d text slot: %w", index, err)
		}
		if mail.StationeryID, err = r.i32(); err != nil {
			return result, fmt.Errorf("read mail %d stationery: %w", index, err)
		}
		if mail.SentMoney, err = r.u32(); err != nil {
			return result, fmt.Errorf("read mail %d sent money: %w", index, err)
		}
		if mail.Flags, err = r.u32(); err != nil {
			return result, fmt.Errorf("read mail %d flags: %w", index, err)
		}
		if mail.DaysLeft, err = r.f32(); err != nil {
			return result, fmt.Errorf("read mail %d days left: %w", index, err)
		}
		if mail.MailTemplateID, err = r.i32(); err != nil {
			return result, fmt.Errorf("read mail %d template: %w", index, err)
		}
		if mail.Subject, err = r.cstring(); err != nil {
			return result, fmt.Errorf("read mail %d subject: %w", index, err)
		}
		if mail.Body, err = r.cstring(); err != nil {
			return result, fmt.Errorf("read mail %d body: %w", index, err)
		}
		itemsCount, readErr := r.u8()
		if readErr != nil {
			return result, fmt.Errorf("read mail %d items count: %w", index, readErr)
		}
		if itemsCount > mailMaxAttachments {
			return result, fmt.Errorf("mail %d has %d attachments, maximum is %d", index, itemsCount, mailMaxAttachments)
		}
		mail.Attachments = make([]LegacyMailAttachedItem, 0, itemsCount)
		for itemIndex := 0; itemIndex < int(itemsCount); itemIndex++ {
			item, readErr := readLegacyMailItem(&r)
			if readErr != nil {
				return result, fmt.Errorf("mail %d item %d: %w", index, itemIndex, readErr)
			}
			mail.Attachments = append(mail.Attachments, item)
		}
		result.Mails = append(result.Mails, mail)
	}
	if r.remaining() != 0 {
		return result, fmt.Errorf("mail list result has %d trailing bytes", r.remaining())
	}
	return result, nil
}

// encodeMailAttachedItem writes one attached item in the build-54261 layout.
func encodeMailAttachedItem(item LegacyMailAttachedItem) []byte {
	body := make([]byte, 0, 48)
	body = append(body, item.Position)
	body = binary.LittleEndian.AppendUint64(body, uint64(uint32(item.AttachID)))
	body = binary.LittleEndian.AppendUint32(body, item.Count)
	body = binary.LittleEndian.AppendUint32(body, uint32(item.Charges))
	body = binary.LittleEndian.AppendUint32(body, item.MaxDurability)
	body = binary.LittleEndian.AppendUint32(body, item.Durability)
	// ItemInstance: the modern client swaps the random-property seed/id order
	// and inserts two always-empty bit blocks (ItemBonus + ItemModList).
	body = binary.LittleEndian.AppendUint32(body, item.ItemID)
	body = binary.LittleEndian.AppendUint32(body, item.RandomPropertiesSeed)
	body = binary.LittleEndian.AppendUint32(body, item.RandomPropertiesID)
	bits := newBitWriter(body)
	bits.writeBit(false) // ItemBonus absent
	body = bits.flush()
	bits = newBitWriter(body)
	bits.writeBits(0, 6) // ItemModList count
	body = bits.flush()
	bits = newBitWriter(body)
	bits.writeBits(uint32(len(item.Enchants)), 4)
	bits.writeBits(0, 2) // gem count
	bits.writeBit(item.Unlocked)
	body = bits.flush()
	for _, enchant := range item.Enchants {
		body = binary.LittleEndian.AppendUint32(body, enchant.ID)
		body = binary.LittleEndian.AppendUint32(body, enchant.Expiration)
		body = binary.LittleEndian.AppendUint32(body, uint32(enchant.Charges))
		body = append(body, enchant.Slot)
	}
	return body
}

// EncodeMailListResult re-emits the 3.3.5a mail list with build-54261 ordering:
// count first (not last), 64-bit mail id / cod / money, packed sender GUID, and
// a trailing bit block for the subject/body lengths.
func EncodeMailListResult(result LegacyMailListResult, resolve func(uint64) GUID128) []byte {
	body := make([]byte, 0, 128+len(result.Mails)*64)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(result.Mails)))
	body = binary.LittleEndian.AppendUint32(body, uint32(result.Total))
	for _, mail := range result.Mails {
		body = binary.LittleEndian.AppendUint64(body, uint64(uint32(mail.MailID)))
		body = binary.LittleEndian.AppendUint32(body, uint32(mail.SenderType))
		body = binary.LittleEndian.AppendUint64(body, uint64(mail.COD))
		body = binary.LittleEndian.AppendUint32(body, uint32(mail.StationeryID))
		body = binary.LittleEndian.AppendUint64(body, uint64(mail.SentMoney))
		body = binary.LittleEndian.AppendUint32(body, mail.Flags)
		body = binary.LittleEndian.AppendUint32(body, math.Float32bits(mail.DaysLeft))
		body = binary.LittleEndian.AppendUint32(body, uint32(mail.MailTemplateID))
		body = binary.LittleEndian.AppendUint32(body, uint32(len(mail.Attachments)))
		switch mail.SenderType {
		case 0:
			sender := resolve(mail.SenderGUID)
			body = appendPackedGUID128(body, sender.Low, sender.High)
		default:
			body = binary.LittleEndian.AppendUint32(body, mail.AltSenderID)
		}
		subjectBytes := len(mail.Subject)
		bodyBytes := len(mail.Body)
		if subjectBytes > 255 || bodyBytes > 8191 {
			subjectBytes, bodyBytes = 255, 8191
		}
		bits := newBitWriter(body)
		bits.writeBits(uint32(subjectBytes), 8)
		bits.writeBits(uint32(bodyBytes), 13)
		body = bits.flush()
		for _, item := range mail.Attachments {
			body = append(body, encodeMailAttachedItem(item)...)
		}
		body = append(body, mail.Subject[:subjectBytes]...)
		body = append(body, mail.Body[:bodyBytes]...)
	}
	return body
}

// LegacyMailCommandResult mirrors the variable-length 3.3.5a
// SMSG_MAIL_COMMAND_RESULT: the trailing fields only appear for Equip errors
// or AttachmentExpired.
type LegacyMailCommandResult struct {
	MailID         uint32
	Command        uint32
	ErrorCode      uint32
	BagResult      uint32
	AttachID       uint32
	QtyInInventory uint32
}

func ParseLegacyMailCommandResult(body []byte) (LegacyMailCommandResult, error) {
	var result LegacyMailCommandResult
	r := movementReader{data: body}
	var err error
	if result.MailID, err = r.u32(); err != nil {
		return result, fmt.Errorf("read mail command id: %w", err)
	}
	if result.Command, err = r.u32(); err != nil {
		return result, fmt.Errorf("read mail command: %w", err)
	}
	if result.ErrorCode, err = r.u32(); err != nil {
		return result, fmt.Errorf("read mail command error: %w", err)
	}
	switch {
	case result.ErrorCode == 1: // equip error carries the bag/equip result
		if result.BagResult, err = r.u32(); err != nil {
			return result, fmt.Errorf("read mail command bag result: %w", err)
		}
	case result.Command == 2: // attachment expired
		if result.AttachID, err = r.u32(); err != nil {
			return result, fmt.Errorf("read mail command attach id: %w", err)
		}
		if result.QtyInInventory, err = r.u32(); err != nil {
			return result, fmt.Errorf("read mail command quantity: %w", err)
		}
	}
	if r.remaining() != 0 {
		return result, fmt.Errorf("mail command result has %d trailing bytes", r.remaining())
	}
	return result, nil
}

// EncodeMailCommandResult always writes the six build-54261 fields, zero-filling
// the trailing ones absent from the legacy conditional layout.
func EncodeMailCommandResult(result LegacyMailCommandResult) []byte {
	body := binary.LittleEndian.AppendUint64(nil, uint64(result.MailID))
	body = binary.LittleEndian.AppendUint32(body, result.Command)
	body = binary.LittleEndian.AppendUint32(body, result.ErrorCode)
	body = binary.LittleEndian.AppendUint32(body, result.BagResult)
	body = binary.LittleEndian.AppendUint64(body, uint64(result.AttachID))
	return binary.LittleEndian.AppendUint32(body, result.QtyInInventory)
}

func ParseLegacyNotifyMail(body []byte) (float32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("notify-mail has %d bytes, want 4", len(body))
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(body)), nil
}

func EncodeNotifyMail(delay float32) []byte {
	return binary.LittleEndian.AppendUint32(nil, math.Float32bits(delay))
}
