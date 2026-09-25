package modernworld

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestMailGetListRoundTrip(t *testing.T) {
	mailbox := GUID128{Low: 0x9abc, High: 0x1}
	body := appendPackedGUID128(nil, mailbox.Low, mailbox.High)
	request, err := ParseMailboxRequest(body)
	if err != nil || request.Mailbox != mailbox {
		t.Fatalf("mailbox=%#v err=%v body=%x", request.Mailbox, err, body)
	}
	if legacy := EncodeLegacyMailGetList(0x1122334455667788); !isUint64At(legacy, 0, 0x1122334455667788) || len(legacy) != 8 {
		t.Fatalf("legacy mail get list=%x", legacy)
	}
	if _, err := ParseMailboxRequest(append(body, 1)); err == nil {
		t.Fatal("mailbox request with trailing bytes must error")
	}
}

func TestMailIDAndTakeItemParsers(t *testing.T) {
	mailbox := GUID128{Low: 0x9abc, High: 0x1}
	prefix := appendPackedGUID128(nil, mailbox.Low, mailbox.High)
	mailID := binary.LittleEndian.AppendUint64(nil, 7)
	if request, err := ParseMailIDRequest(append(append([]byte(nil), prefix...), mailID...)); err != nil || request.Mailbox != mailbox || request.MailID != 7 {
		t.Fatalf("mail-id request=%#v err=%v", request, err)
	}
	take := append(append([]byte(nil), prefix...), mailID...)
	take = binary.LittleEndian.AppendUint64(take, 11)
	request, err := ParseMailTakeItemRequest(take)
	if err != nil || request.Mailbox != mailbox || request.MailID != 7 || request.Attach != 11 {
		t.Fatalf("take-item request=%#v err=%v", request, err)
	}
	// Mail id larger than 32 bits must be rejected.
	huge := append(append([]byte(nil), prefix...), binary.LittleEndian.AppendUint64(nil, 1<<40)...)
	if _, err := ParseMailIDRequest(huge); err == nil {
		t.Fatal("40-bit mail id must error")
	}
}

func TestMailTakeMoneyParser(t *testing.T) {
	mailbox := GUID128{Low: 0x9abc, High: 0x1}
	body := appendPackedGUID128(nil, mailbox.Low, mailbox.High)
	body = binary.LittleEndian.AppendUint64(body, 7)
	body = binary.LittleEndian.AppendUint64(body, 12345)
	request, err := ParseMailTakeMoneyRequest(body)
	if err != nil || request.Mailbox != mailbox || request.MailID != 7 || request.Money != 12345 {
		t.Fatalf("take-money request=%#v err=%v", request, err)
	}
	// The 3.4.3 take-money packet is mailbox + mailID + i64 money. The
	// mark-as-read parser must keep rejecting that trailing amount so those
	// opcodes stay strict.
	if _, err := ParseMailIDRequest(body); err == nil {
		t.Fatal("mail-id parser must reject the take-money amount field")
	}
	if _, err := ParseMailTakeMoneyRequest(append(body, 1)); err == nil {
		t.Fatal("take-money with extra trailing bytes must error")
	}
	huge := appendPackedGUID128(nil, mailbox.Low, mailbox.High)
	huge = binary.LittleEndian.AppendUint64(huge, 1<<40)
	huge = binary.LittleEndian.AppendUint64(huge, 1)
	if _, err := ParseMailTakeMoneyRequest(huge); err == nil {
		t.Fatal("40-bit mail id must error")
	}
}

func TestMailDeleteAndReturnParsers(t *testing.T) {
	deleteBody := binary.LittleEndian.AppendUint64(nil, 4)
	deleteBody = binary.LittleEndian.AppendUint32(deleteBody, 1)
	request, err := ParseMailDeleteRequest(deleteBody)
	if err != nil || request.MailID != 4 || request.Reason != 1 {
		t.Fatalf("delete request=%#v err=%v", request, err)
	}
	sender := GUID128{Low: 0x42, High: 0x1}
	returnBody := binary.LittleEndian.AppendUint64(nil, 9)
	returnBody = appendPackedGUID128(returnBody, sender.Low, sender.High)
	ret, err := ParseMailReturnRequest(returnBody)
	if err != nil || ret.MailID != 9 || ret.Sender != sender {
		t.Fatalf("return request=%#v err=%v", ret, err)
	}
}

func TestSendMailRoundTrip(t *testing.T) {
	mailbox := GUID128{Low: 0x9abc, High: 0x1}
	item := GUID128{Low: 0x8888, High: 0x2}
	body := appendPackedGUID128(nil, mailbox.Low, mailbox.High)
	body = binary.LittleEndian.AppendUint32(body, 61) // stationery
	body = binary.LittleEndian.AppendUint64(body, 150)  // money
	body = binary.LittleEndian.AppendUint64(body, 200)  // cod
	target := "Alice"
	subject := "Hello"
	text := "body text"
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(target)), 9)
	bits.writeBits(uint32(len(subject)), 9)
	bits.writeBits(uint32(len(text)), 11)
	bits.writeBits(1, 5) // one attachment
	body = bits.flush()
	body = append(body, target...)
	body = append(body, subject...)
	body = append(body, text...)
	body = append(body, 3) // bag slot
	body = appendPackedGUID128(body, item.Low, item.High)

	request, err := ParseSendMail(body)
	if err != nil {
		t.Fatalf("parse send mail: %v", err)
	}
	if request.Mailbox != mailbox || request.Stationery != 61 || request.SendMoney != 150 || request.COD != 200 {
		t.Fatalf("send mail header=%+v", request)
	}
	if request.Target != target || request.Subject != subject || request.Body != text || len(request.Attachments) != 1 {
		t.Fatalf("send mail strings=%q/%q/%q att=%d", request.Target, request.Subject, request.Body, len(request.Attachments))
	}
	if request.Attachments[0].Position != 3 || request.Attachments[0].Item != item {
		t.Fatalf("send mail attachment=%+v", request.Attachments[0])
	}

	legacy := EncodeLegacySendMail(request, 0x0f00000000000003, func(g GUID128) uint64 {
		if g == item {
			return 0x4000000000000005
		}
		return 0
	})
	if !isUint64At(legacy, 0, 0x0f00000000000003) {
		t.Fatalf("legacy send mail mailbox=%x", legacy)
	}
	want := appendCString(nil, target)
	want = appendCString(want, subject)
	want = appendCString(want, text)
	if !strings.HasPrefix(string(legacy[8:]), string(want)) {
		t.Fatalf("legacy send mail strings=%q", legacy[8:])
	}
	// The item GUID is followed by money(4) + cod(4) + unk2(8) + unk3(1) = 17
	// trailing bytes, so it starts at len-25.
	if !isUint64At(legacy, len(legacy)-25, 0x4000000000000005) {
		t.Fatalf("legacy send mail item guid not before money: %x", legacy)
	}
	if !isUint32At(legacy, len(legacy)-17, 150) || !isUint32At(legacy, len(legacy)-13, 200) {
		t.Fatalf("legacy send mail money/cod=%x", legacy)
	}
	if _, err := ParseSendMail(append(body, 0)); err == nil {
		t.Fatal("send mail with trailing bytes must error")
	}
}

func TestLegacyMailListResultToModern(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 1) // total records
	legacy = append(legacy, 1)                         // one mail
	legacy = binary.LittleEndian.AppendUint16(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 5)  // mail id
	legacy = append(legacy, 0)                            // sender type normal
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x2f) // sender player
	legacy = binary.LittleEndian.AppendUint32(legacy, 1000) // cod
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)    // discarded text slot
	legacy = binary.LittleEndian.AppendUint32(legacy, 61)   // stationery
	legacy = binary.LittleEndian.AppendUint32(legacy, 5000) // sent money
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)    // flags
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(1.5))
	legacy = binary.LittleEndian.AppendUint32(legacy, 0) // template
	legacy = appendCString(legacy, "subject")
	legacy = appendCString(legacy, "mail body text")
	legacy = append(legacy, 1) // one attachment
	// attached item
	legacy = append(legacy, 0)            // position
	legacy = binary.LittleEndian.AppendUint32(legacy, 3) // attach id
	legacy = binary.LittleEndian.AppendUint32(legacy, 6948)
	for slot := 0; slot < 7; slot++ {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
		if slot == 0 {
			legacy = binary.LittleEndian.AppendUint32(legacy, 190) // enchant id
		} else {
			legacy = binary.LittleEndian.AppendUint32(legacy, 0)
		}
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x11111111) // seed
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)          // count
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)          // charges
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)          // max durability
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)          // durability
	legacy = append(legacy, 0)                                    // unlocked

	result, err := ParseLegacyMailListResult(legacy)
	if err != nil {
		t.Fatalf("parse mail list: %v", err)
	}
	if result.Total != 1 || len(result.Mails) != 1 {
		t.Fatalf("mail list header=%+v", result)
	}
	mail := result.Mails[0]
	if mail.MailID != 5 || mail.SenderType != 0 || mail.SenderGUID != 0x2f || mail.COD != 1000 ||
		mail.SentMoney != 5000 || mail.Subject != "subject" || mail.Body != "mail body text" {
		t.Fatalf("mail entry=%+v", mail)
	}
	if len(mail.Attachments) != 1 || mail.Attachments[0].ItemID != 6948 || len(mail.Attachments[0].Enchants) != 1 ||
		mail.Attachments[0].Enchants[0].ID != 190 || mail.Attachments[0].RandomPropertiesSeed != 0x11111111 {
		t.Fatalf("mail attachment=%+v", mail.Attachments[0])
	}

	modern := EncodeMailListResult(result, func(legacy uint64) GUID128 {
		return GUID128{Low: legacy, High: 0x10}
	})
	if !isUint32At(modern, 0, 1) || !isUint32At(modern, 4, 1) {
		t.Fatalf("modern mail list header=%x", modern)
	}
	if !isUint64At(modern, 8, 5) {
		t.Fatalf("modern mail id=%x", modern[8:])
	}
	// sender type + cod
	if !isUint64At(modern, 20, 1000) {
		t.Fatalf("modern cod=%x", modern[20:])
	}
	if !isUint64At(modern, 32, 5000) {
		t.Fatalf("modern sent money=%x", modern[32:])
	}
	if !isUint32At(modern, 44, math.Float32bits(1.5)) {
		t.Fatalf("modern days-left=%x", modern[44:])
	}
	if !isUint32At(modern, 52, 1) {
		t.Fatalf("modern attachment count=%x", modern[52:])
	}
}

func TestMailCommandResultTranslation(t *testing.T) {
	// equip error form
	legacy := binary.LittleEndian.AppendUint32(nil, 9)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // money taken
	legacy = binary.LittleEndian.AppendUint32(legacy, 1) // equip error
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x47)
	result, err := ParseLegacyMailCommandResult(legacy)
	if err != nil || result.MailID != 9 || result.Command != 1 || result.ErrorCode != 1 || result.BagResult != 0x47 {
		t.Fatalf("command result=%+v err=%v", result, err)
	}
	modern := EncodeMailCommandResult(result)
	if !isUint64At(modern, 0, 9) || !isUint32At(modern, 8, 1) || !isUint32At(modern, 12, 1) || !isUint32At(modern, 16, 0x47) {
		t.Fatalf("modern command result=%x", modern)
	}
	// attachment-expired form adds attach id + quantity
	legacy = binary.LittleEndian.AppendUint32(nil, 3)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2) // attachment expired
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	result, err = ParseLegacyMailCommandResult(legacy)
	if err != nil || result.AttachID != 7 || result.QtyInInventory != 2 {
		t.Fatalf("expired command result=%+v err=%v", result, err)
	}
}

func TestNotifyReceivedMail(t *testing.T) {
	delay := float32(3.25)
	body := binary.LittleEndian.AppendUint32(nil, math.Float32bits(delay))
	got, err := ParseLegacyNotifyMail(body)
	if err != nil || got != delay {
		t.Fatalf("notify delay=%v err=%v", got, err)
	}
	if encoded := EncodeNotifyMail(delay); !isUint32At(encoded, 0, math.Float32bits(delay)) {
		t.Fatalf("notify mail=%x", encoded)
	}
}

func isUint64At(body []byte, offset int, want uint64) bool {
	if offset < 0 || offset+8 > len(body) {
		return false
	}
	return binary.LittleEndian.Uint64(body[offset:]) == want
}

func isUint32At(body []byte, offset int, want uint32) bool {
	if offset < 0 || offset+4 > len(body) {
		return false
	}
	return binary.LittleEndian.Uint32(body[offset:]) == want
}
