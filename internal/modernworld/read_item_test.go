package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadItemTranslation(t *testing.T) {
	// CMSG_READ_ITEM carries the same two bag-coordinate bytes as open-item.
	request, err := ParseReadItem([]byte{0xff, 5})
	if err != nil || request.PackSlot != 0xff || request.Slot != 5 {
		t.Fatalf("read item=%+v err=%v", request, err)
	}
	legacy := EncodeLegacyReadItem(request)
	if len(legacy) != 2 {
		t.Fatalf("legacy read item=%x", legacy)
	}
	if _, err := ParseReadItem([]byte{1}); err == nil {
		t.Fatal("one-byte read item must error")
	}
}

func TestQueryPageTextTranslation(t *testing.T) {
	item := GUID128{Low: 0x12345678, High: 0x20000001000042}
	body := binary.LittleEndian.AppendUint32(nil, 0xABCD)
	body = appendPackedGUID128(body, item.Low, item.High)
	request, err := ParseQueryPageText(body)
	if err != nil || request.PageTextID != 0xABCD || request.Item != item {
		t.Fatalf("query page text=%+v err=%v", request, err)
	}
	legacy := EncodeLegacyQueryPageText(request.PageTextID, 0xdeadbeef)
	if !isUint32At(legacy, 0, 0xABCD) || !isUint64At(legacy, 4, 0xdeadbeef) {
		t.Fatalf("legacy query page text=%x", legacy)
	}
	if _, err := ParseQueryPageText(body[:len(body)-1]); err == nil {
		t.Fatal("truncated query page text must error")
	}
}

func TestPageTextResponseTranslation(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 0x100)
	legacy = appendCString(legacy, "Once upon a time…")
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x200) // next page
	page, err := ParseLegacyPageText(legacy)
	if err != nil || page.PageTextID != 0x100 || page.Text != "Once upon a time…" || page.NextPageID != 0x200 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	modern := EncodePageTextResponse(page)
	if !isUint32At(modern, 0, 0x100) {
		t.Fatalf("modern page id=%x", modern)
	}
	// allow bit byte at offset 4 (0x80 = true), then pages count 1 at 5..9.
	if modern[4]&0x80 == 0 || !isUint32At(modern, 5, 1) {
		t.Fatalf("modern page header=%x", modern[4:9])
	}
	if !isUint32At(modern, 9, 0x100) || !isUint32At(modern, 13, 0x200) {
		t.Fatalf("modern page record=%x", modern[9:])
	}
	// After id(4)+next(4)+condition(4)+flags(1) comes the 12-bit length block at
	// bytes 22..23, then the raw text starts at byte 24.
	text := "Once upon a time…"
	if len(modern) != 24+len(text) {
		t.Fatalf("modern page length: total=%d want=%d", len(modern), 24+len(text))
	}
	if !bytesEndWith(modern, []byte(text)) {
		t.Fatalf("modern page text=%x", modern)
	}
}

func TestReadItemResults(t *testing.T) {
	item := GUID128{Low: 0x42, High: 0x20000001000042}
	legacyItem := uint64(0x4000)<<48 | 0x42
	ok := EncodeReadItemResultOK(item)
	low, high, consumed, err := readPackedGUID128(ok)
	if err != nil || consumed != len(ok) || low != item.Low || high != item.High {
		t.Fatalf("read ok=%x err=%v", ok, err)
	}
	failed := EncodeReadItemResultFailed(item)
	if len(failed) != len(ok)+5 {
		t.Fatalf("read failed=%x", failed)
	}
	if !isUint32At(failed, len(ok), 0) || failed[len(failed)-1]&0x80 == 0 {
		t.Fatalf("read failed delay/subcode=%x", failed)
	}
	gotLegacy, err := ParseLegacyReadItemResult(binary.LittleEndian.AppendUint64(nil, legacyItem))
	if err != nil || gotLegacy != legacyItem {
		t.Fatalf("legacy item=%#x err=%v", gotLegacy, err)
	}
	// The failed body appends delay + subcode after the GUID; parse must accept it.
	failedLegacy := binary.LittleEndian.AppendUint64(nil, legacyItem)
	failedLegacy = binary.LittleEndian.AppendUint32(failedLegacy, 500)
	failedLegacy = append(failedLegacy, 1)
	if gotLegacy, err = ParseLegacyReadItemResult(failedLegacy); err != nil || gotLegacy != legacyItem {
		t.Fatalf("legacy failed item=%#x err=%v", gotLegacy, err)
	}
}

func bytesEndWith(body, suffix []byte) bool {
	if len(body) < len(suffix) {
		return false
	}
	start := len(body) - len(suffix)
	for i := range suffix {
		if body[start+i] != suffix[i] {
			return false
		}
	}
	return true
}

func TestQueryItemTextResponse(t *testing.T) {
	const legacyGUID = uint64(0x40000000000099)
	legacy := append([]byte{0}, appendPackedGUID64(nil, legacyGUID)...)
	legacy = append(legacy, "A folded letter.\x00"...)
	guid, text, present, err := ParseLegacyQueryItemText(legacy)
	if err != nil || !present || guid != legacyGUID || text != "A folded letter." {
		t.Fatalf("guid=%x text=%q present=%v err=%v", guid, text, present, err)
	}
	modern, err := EncodeQueryItemTextResponse(GUID128{Low: 0x99, High: 3}, text)
	if err != nil {
		t.Fatal(err)
	}
	if modern[0] != 0x80 {
		t.Fatalf("item text valid bit=%x", modern[0])
	}
	reader := movementReader{data: modern[1:]}
	length, err := reader.bits(13)
	body, textErr := reader.stringN(int(length))
	if err != nil || textErr != nil || body != text {
		t.Fatalf("length=%d text=%q errors=%v/%v body=%x", length, body, err, textErr, modern)
	}
	low, high, consumed, err := readPackedGUID128(modern[1+reader.offset:])
	if err != nil || low != 0x99 || high != 3 || 1+reader.offset+consumed != len(modern) {
		t.Fatalf("item guid low=%x high=%x consumed=%d err=%v body=%x", low, high, consumed, err, modern)
	}

	missGUID, missText, present, err := ParseLegacyQueryItemText([]byte{1})
	if err != nil || present || missGUID != 0 || missText != "" {
		t.Fatalf("miss guid=%x text=%q present=%v err=%v", missGUID, missText, present, err)
	}
	if _, _, _, err := ParseLegacyQueryItemText([]byte{1, 0}); err == nil {
		t.Fatal("item-text miss with a trailing byte was accepted")
	}
	if _, _, _, err := ParseLegacyQueryItemText([]byte{2}); err == nil {
		t.Fatal("item-text status 2 was accepted")
	}
	if _, err := EncodeQueryItemTextResponse(GUID128{}, string(bytes.Repeat([]byte{'c'}, 8192))); err == nil {
		t.Fatal("8192-byte item text was accepted")
	}
}
