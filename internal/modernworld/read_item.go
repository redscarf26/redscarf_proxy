package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Readable-item translation (build 54261 client <-> 3.3.5a AzerothCore).
// Right-clicking an in-bag book/letter opens the reading frame through a two
// step chain: CMSG_READ_ITEM triggers SMSG_READ_ITEM_RESULT_OK carrying the
// item GUID, then the client fetches the page text with CMSG_QUERY_PAGE_TEXT
// and receives it in SMSG_QUERY_PAGE_TEXT_RESPONSE. The page text is not inline
// in the read result. Layouts mirror HermesProxy-WOTLK World/Server/Packets
// {ReadItem,QueryPageText,QueryPageTextResponse,ReadItemResultOK,
// ReadItemResultFailed}.cs and World/Server/WorldSocket.cs / WorldClient.cs.

const (
	CMSGReadItem              = uint16(0x32C7) // 12999
	CMSGQueryPageText         = uint16(0x3274) // 12916
	SMSGReadItemResultOK      = uint16(0x27A1) // 10145
	SMSGReadItemResultFailed  = uint16(0x27A9) // 10153
	SMSGQueryPageTextResponse = uint16(0x2917) // 10519
	SMSGQueryItemTextResponse = uint16(0x291E) // 10526

	readItemMaxPageText = 4095 // modern page text length is 12 bits
	itemTextMaxBytes    = 8191 // modern item text length is 13 bits
)

// ReadItemRequest is the two-byte CMSG_READ_ITEM body: the container
// (equipped-bag index or 0xFF for the main backpack) followed by the slot.
type ReadItemRequest struct {
	PackSlot uint8
	Slot     uint8
}

func ParseReadItem(body []byte) (ReadItemRequest, error) {
	var request ReadItemRequest
	r := movementReader{data: body}
	var err error
	if request.PackSlot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read read-item container: %w", err)
	}
	if request.Slot, err = r.u8(); err != nil {
		return request, fmt.Errorf("read read-item slot: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("read-item has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// EncodeLegacyReadItem adjusts the bag coordinates to the 3.3.5 layout exactly
// like the open-item command: an item in the main backpack (PackSlot 0xFF)
// carries an adjusted slot, while an item inside an equipped bag keeps its slot
// and only the bag index moves.
func EncodeLegacyReadItem(request ReadItemRequest) []byte {
	container := request.PackSlot
	slot := request.Slot
	if container != 0xff {
		container = AdjustInventorySlot(container)
	} else {
		slot = AdjustInventorySlot(slot)
	}
	return []byte{container, slot}
}

// QueryPageTextRequest is the modern CMSG_QUERY_PAGE_TEXT payload: the page
// text id followed by the packed item GUID.
type QueryPageTextRequest struct {
	PageTextID uint32
	Item       GUID128
}

func ParseQueryPageText(body []byte) (QueryPageTextRequest, error) {
	var request QueryPageTextRequest
	r := movementReader{data: body}
	var err error
	if request.PageTextID, err = r.u32(); err != nil {
		return request, fmt.Errorf("read query-page-text id: %w", err)
	}
	if request.Item, err = r.guid128(); err != nil {
		return request, fmt.Errorf("read query-page-text item: %w", err)
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("query-page-text has %d trailing bytes", r.remaining())
	}
	return request, nil
}

// EncodeLegacyQueryPageText writes the 3.3.5 CMSG_QUERY_PAGE_TEXT body: the
// page text id followed by the raw 64-bit item GUID.
func EncodeLegacyQueryPageText(pageTextID uint32, item uint64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, pageTextID)
	return binary.LittleEndian.AppendUint64(body, item)
}

// LegacyPageText is the parsed 3.3.5a SMSG_QUERY_PAGE_TEXT_RESPONSE payload: a
// single page identified by its id with a null-terminated text body.
type LegacyPageText struct {
	PageTextID uint32
	Text       string
	NextPageID uint32
}

func ParseLegacyPageText(body []byte) (LegacyPageText, error) {
	var page LegacyPageText
	r := movementReader{data: body}
	var err error
	if page.PageTextID, err = r.u32(); err != nil {
		return page, fmt.Errorf("read page-text id: %w", err)
	}
	if page.Text, err = r.cstring(); err != nil {
		return page, fmt.Errorf("read page-text body: %w", err)
	}
	if page.NextPageID, err = r.u32(); err != nil {
		return page, fmt.Errorf("read page-text next id: %w", err)
	}
	if r.remaining() != 0 {
		return page, fmt.Errorf("page-text has %d trailing bytes", r.remaining())
	}
	return page, nil
}

// EncodePageTextResponse re-emits the page as the build-54261 layout: the outer
// allow bit (always set when the server answered) followed by a single page
// record whose text length rides in a trailing 12-bit block.
func EncodePageTextResponse(page LegacyPageText) []byte {
	textBytes := len(page.Text)
	if textBytes > readItemMaxPageText {
		textBytes = readItemMaxPageText
	}
	body := binary.LittleEndian.AppendUint32(nil, page.PageTextID)
	bits := newBitWriter(body)
	bits.writeBit(true) // allow
	body = bits.flush()
	body = binary.LittleEndian.AppendUint32(body, 1) // pages count
	body = binary.LittleEndian.AppendUint32(body, page.PageTextID)
	body = binary.LittleEndian.AppendUint32(body, page.NextPageID)
	body = binary.LittleEndian.AppendUint32(body, 0) // player condition id
	body = append(body, 0)                           // flags
	bits = newBitWriter(body)
	bits.writeBits(uint32(textBytes), 12)
	body = bits.flush()
	return append(body, page.Text[:textBytes]...)
}

// ParseLegacyReadItemResult reads the item GUID64 that heads both the 3.3.5a
// SMSG_READ_ITEM_RESULT_OK and FAILED bodies. The failed variant appends a
// delay and a subcode that the modern packet does not reuse (Hermes zeroes them
// and picks subcode 2), so any trailing bytes are ignored.
func ParseLegacyReadItemResult(body []byte) (uint64, error) {
	if len(body) < 8 {
		return 0, fmt.Errorf("read-item result has %d bytes, want at least 8", len(body))
	}
	return binary.LittleEndian.Uint64(body[:8]), nil
}

func EncodeReadItemResultOK(item GUID128) []byte {
	return appendPackedGUID128(nil, item.Low, item.High)
}

// ParseLegacyQueryItemText reads the WotLK (expansion 80) item-text response.
// A leading 0 carries a packed item GUID and a CString. A leading 1 means the
// item has no text; present is then false and the caller must not emit a
// modern packet. This is not the pre-3.3 mail-body query (u32 text id + CString).
func ParseLegacyQueryItemText(body []byte) (guid uint64, text string, present bool, err error) {
	r := movementReader{data: body}
	status, err := r.u8()
	if err != nil {
		return 0, "", false, fmt.Errorf("read item-text status: %w", err)
	}
	switch status {
	case 1:
		if r.remaining() != 0 {
			return 0, "", false, fmt.Errorf("item-text miss has %d trailing bytes", r.remaining())
		}
		return 0, "", false, nil
	case 0:
	default:
		return 0, "", false, fmt.Errorf("item-text has invalid status %d", status)
	}
	if guid, err = r.guid64(); err != nil {
		return 0, "", false, fmt.Errorf("read item-text GUID: %w", err)
	}
	if text, err = r.cstring(); err != nil {
		return 0, "", false, fmt.Errorf("read item-text body: %w", err)
	}
	if r.remaining() != 0 {
		return 0, "", false, fmt.Errorf("item-text has %d trailing bytes", r.remaining())
	}
	if len(text) > itemTextMaxBytes {
		return 0, "", false, fmt.Errorf("item-text has %d bytes, maximum is %d", len(text), itemTextMaxBytes)
	}
	return guid, text, true, nil
}

// EncodeQueryItemTextResponse writes the build-54261 item-text packet in legacy proxy's
// expansion-80 order: a set Valid bit, a 13-bit text length, the text bytes,
// then the packed item GUID.
func EncodeQueryItemTextResponse(guid GUID128, text string) ([]byte, error) {
	if len(text) > itemTextMaxBytes {
		return nil, fmt.Errorf("item-text has %d bytes, maximum is %d", len(text), itemTextMaxBytes)
	}
	bits := newBitWriter(nil)
	bits.writeBit(true)
	body := bits.flush()
	bits = newBitWriter(body)
	bits.writeBits(uint32(len(text)), 13)
	body = bits.flush()
	body = append(body, text...)
	return appendPackedGUID128(body, guid.Low, guid.High), nil
}

func EncodeReadItemResultFailed(item GUID128) []byte {
	body := appendPackedGUID128(nil, item.Low, item.High)
	body = binary.LittleEndian.AppendUint32(body, 0) // delay
	bits := newBitWriter(body)
	bits.writeBits(2, 2) // subcode: not readable
	return bits.flush()
}
