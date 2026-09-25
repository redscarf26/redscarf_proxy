package modernworld

import (
	"embed"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	CMSGDBQueryBulk   = uint16(13797)
	SMSGDBReply       = uint16(10510)
	CMSGHotfixRequest = uint16(13798)
	SMSGHotfixConnect = uint16(10513)

	hotfixChoiceBegin      = uint32(3_000_000)
	hotfixOptionBegin      = uint32(3_100_000)
	hashChoice             = uint32(0x681D0F3D)
	hashOption             = uint32(0x2FB7905B)
	maxHotfixRequest       = 4096
	hashTactKey            = uint32(0xDF2F53CF)
	BroadcastTextTableHash = uint32(35137211)
)

//go:embed hotfixdata/*.csv
var hotfixFiles embed.FS

type HotfixRecord struct {
	PushID    uint32
	UniqueID  uint32
	TableHash uint32
	RecordID  uint32
	Content   []byte
}

type DBQueryBulk struct {
	TableHash uint32
	RecordIDs []uint32
}

var customizationHotfixes, customizationHotfixByID, customizationHotfixError = loadCustomizationHotfixes()

func EncodeAvailableHotfixes(realmAddress uint32) ([]byte, error) {
	if customizationHotfixError != nil {
		return nil, customizationHotfixError
	}
	body := binary.LittleEndian.AppendUint32(nil, realmAddress)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(customizationHotfixes)))
	for _, record := range customizationHotfixes {
		body = binary.LittleEndian.AppendUint32(body, record.PushID)
		body = binary.LittleEndian.AppendUint32(body, record.UniqueID)
	}
	return body, nil
}

func ParseHotfixRequest(body []byte) ([]uint32, error) {
	if len(body) < 12 {
		return nil, fmt.Errorf("hotfix request has %d bytes, want at least 12", len(body))
	}
	count := int(binary.LittleEndian.Uint32(body[8:12]))
	if count < 0 || count > maxHotfixRequest {
		return nil, fmt.Errorf("hotfix request count %d is invalid", count)
	}
	if len(body) != 12+count*4 {
		return nil, fmt.Errorf("hotfix request has %d bytes for %d IDs", len(body), count)
	}
	ids := make([]uint32, count)
	for index := range ids {
		ids[index] = binary.LittleEndian.Uint32(body[12+index*4 : 16+index*4])
	}
	return ids, nil
}

func ParseDBQueryBulk(body []byte) (DBQueryBulk, error) {
	var query DBQueryBulk
	if len(body) < 6 {
		return query, fmt.Errorf("DB query bulk has %d bytes, want at least 6", len(body))
	}
	query.TableHash = binary.LittleEndian.Uint32(body[:4])
	count := int(binary.BigEndian.Uint16(body[4:6]) >> 3)
	if count > maxHotfixRequest || len(body) != 6+count*4 {
		return query, fmt.Errorf("DB query bulk has %d bytes for %d records", len(body), count)
	}
	query.RecordIDs = make([]uint32, count)
	for index := range query.RecordIDs {
		query.RecordIDs[index] = binary.LittleEndian.Uint32(body[6+index*4 : 10+index*4])
	}
	return query, nil
}

// EncodeDBReply returns an empty record. TactKey uses NotPublic so the client
// falls back to baseline/unprotected DB2 data; other unknown records are Invalid.
func EncodeDBReply(tableHash, recordID uint32, timestamp int64) []byte {
	return encodeDBReply(tableHash, recordID, timestamp, 3, nil)
}

func EncodeBroadcastTextDBReply(record BroadcastTextRecord, timestamp int64) []byte {
	var content []byte
	content = appendCString(content, record.MaleText)
	content = appendCString(content, record.FemaleText)
	content = binary.LittleEndian.AppendUint32(content, record.ID)
	content = binary.LittleEndian.AppendUint32(content, record.Language)
	content = binary.LittleEndian.AppendUint32(content, 0)
	content = binary.LittleEndian.AppendUint16(content, 0)
	content = append(content, 0)
	content = binary.LittleEndian.AppendUint32(content, 0)
	content = binary.LittleEndian.AppendUint32(content, 0)
	content = binary.LittleEndian.AppendUint32(content, 0)
	content = binary.LittleEndian.AppendUint32(content, 0)
	for _, emote := range record.Emotes {
		content = binary.LittleEndian.AppendUint16(content, emote)
	}
	for _, delay := range record.EmoteDelays {
		content = binary.LittleEndian.AppendUint16(content, delay)
	}
	return encodeDBReply(BroadcastTextTableHash, record.ID, timestamp, 1, content)
}

func encodeDBReply(tableHash, recordID uint32, timestamp int64, status byte, content []byte) []byte {
	body := binary.LittleEndian.AppendUint32(nil, tableHash)
	body = binary.LittleEndian.AppendUint32(body, recordID)
	body = binary.LittleEndian.AppendUint32(body, uint32(timestamp))
	if tableHash == hashTactKey {
		status = 4 // HotfixStatus.NotPublic
	}
	body = append(body, status<<5) // 3-bit MSB-first HotfixStatus
	body = binary.LittleEndian.AppendUint32(body, uint32(len(content)))
	return append(body, content...)
}

func EncodeHotfixConnect(requested []uint32) ([]byte, int, error) {
	if customizationHotfixError != nil {
		return nil, 0, customizationHotfixError
	}
	records := make([]HotfixRecord, 0, len(requested))
	for _, id := range requested {
		if record, ok := customizationHotfixByID[id]; ok {
			records = append(records, record)
		}
	}
	body := binary.LittleEndian.AppendUint32(nil, uint32(len(records)))
	var totalSize uint64
	for _, record := range records {
		totalSize += uint64(len(record.Content))
		if totalSize > uint64(^uint32(0)) {
			return nil, 0, fmt.Errorf("hotfix payload is too large")
		}
		body = binary.LittleEndian.AppendUint32(body, record.PushID)
		body = binary.LittleEndian.AppendUint32(body, record.UniqueID)
		body = binary.LittleEndian.AppendUint32(body, record.TableHash)
		body = binary.LittleEndian.AppendUint32(body, record.RecordID)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(record.Content)))
		body = append(body, 0x20) // HotfixStatus.Valid in a 3-bit MSB-first field
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(totalSize))
	for _, record := range records {
		body = append(body, record.Content...)
	}
	return body, len(records), nil
}

func loadCustomizationHotfixes() ([]HotfixRecord, map[uint32]HotfixRecord, error) {
	selectedOptions := make(map[uint32]bool, len(modernCustomizationChoices))
	for optionID := range modernCustomizationChoices {
		selectedOptions[optionID] = true
	}
	choices, err := loadChoiceHotfixes(selectedOptions)
	if err != nil {
		return nil, nil, err
	}
	options, err := loadOptionHotfixes(selectedOptions)
	if err != nil {
		return nil, nil, err
	}
	records := append(choices, options...)
	triggers, err := loadAreaTriggerHotfixes()
	if err != nil {
		return nil, nil, err
	}
	records = append(records, triggers...)
	sort.Slice(records, func(i, j int) bool { return records[i].PushID < records[j].PushID })
	byID := make(map[uint32]HotfixRecord, len(records))
	for _, record := range records {
		byID[record.PushID] = record
	}
	return records, byID, nil
}

func loadChoiceHotfixes(selected map[uint32]bool) ([]HotfixRecord, error) {
	rows, err := readHotfixCSV("hotfixdata/ChrCustomizationChoice3.csv")
	if err != nil {
		return nil, err
	}
	result := make([]HotfixRecord, 0, len(rows))
	for index, row := range rows {
		if len(row) != 12 {
			return nil, fmt.Errorf("choice row %d has %d fields", index+1, len(row))
		}
		optionID, err := parseUint32(row[2])
		if err != nil {
			return nil, fmt.Errorf("choice row %d option: %w", index+1, err)
		}
		if !selected[optionID] {
			continue
		}
		values, err := parseIntFields(row, 1, 3, 4, 7, 8, 9, 10, 11)
		if err != nil {
			return nil, fmt.Errorf("choice row %d: %w", index+1, err)
		}
		sortOrder, err := parseUint16(row[5])
		if err != nil {
			return nil, err
		}
		uiOrder, err := parseUint16(row[6])
		if err != nil {
			return nil, err
		}
		var content []byte
		content = append(content, row[0]...)
		content = append(content, 0)
		content = appendInt32(content, values[0])
		content = appendInt32(content, int64(optionID))
		content = appendInt32(content, values[1])
		content = appendInt32(content, values[2])
		content = binary.LittleEndian.AppendUint16(content, sortOrder)
		content = binary.LittleEndian.AppendUint16(content, uiOrder)
		for _, value := range values[3:] {
			content = appendInt32(content, value)
		}
		recordID := uint32(values[0])
		pushID := hotfixChoiceBegin + uint32(index+1)
		result = append(result, HotfixRecord{PushID: pushID, UniqueID: pushID, TableHash: hashChoice, RecordID: recordID, Content: content})
	}
	return result, nil
}

func loadOptionHotfixes(selected map[uint32]bool) ([]HotfixRecord, error) {
	rows, err := readHotfixCSV("hotfixdata/ChrCustomizationOption3.csv")
	if err != nil {
		return nil, err
	}
	result := make([]HotfixRecord, 0, len(rows))
	for index, row := range rows {
		if len(row) != 12 {
			return nil, fmt.Errorf("option row %d has %d fields", index+1, len(row))
		}
		recordID, err := parseUint32(row[1])
		if err != nil {
			return nil, err
		}
		if !selected[recordID] {
			continue
		}
		secondaryID, err := parseUint16(row[2])
		if err != nil {
			return nil, err
		}
		values, err := parseIntFields(row, 3, 4, 5, 6, 7, 9, 10, 11)
		if err != nil {
			return nil, fmt.Errorf("option row %d: %w", index+1, err)
		}
		cost, err := strconv.ParseFloat(strings.TrimSpace(row[8]), 32)
		if err != nil {
			return nil, err
		}
		var content []byte
		content = append(content, row[0]...)
		content = append(content, 0)
		content = appendInt32(content, int64(recordID))
		content = binary.LittleEndian.AppendUint16(content, secondaryID)
		for _, value := range values[:5] {
			content = appendInt32(content, value)
		}
		content = binary.LittleEndian.AppendUint32(content, math.Float32bits(float32(cost)))
		for _, value := range values[5:] {
			content = appendInt32(content, value)
		}
		pushID := hotfixOptionBegin + uint32(index+1)
		result = append(result, HotfixRecord{PushID: pushID, UniqueID: pushID, TableHash: hashOption, RecordID: recordID, Content: content})
	}
	return result, nil
}

func readHotfixCSV(path string) ([][]string, error) {
	file, err := hotfixFiles.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.ReuseRecord = false
	if _, err := reader.Read(); err != nil {
		return nil, err
	}
	var rows [][]string
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseIntFields(row []string, indexes ...int) ([]int64, error) {
	values := make([]int64, len(indexes))
	for position, index := range indexes {
		value, err := strconv.ParseInt(strings.TrimSpace(row[index]), 10, 32)
		if err != nil {
			return nil, err
		}
		values[position] = value
	}
	return values, nil
}

func parseUint32(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	return uint32(parsed), err
}

func parseUint16(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 16)
	return uint16(parsed), err
}

func appendInt32(destination []byte, value int64) []byte {
	return binary.LittleEndian.AppendUint32(destination, uint32(int32(value)))
}
