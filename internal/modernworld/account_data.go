package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	AccountDataCount       = 15
	CMSGRequestAccountData = uint16(13972)
	CMSGUpdateAccountData  = uint16(13973)
	SMSGUpdateAccountData  = uint16(9993)
	SMSGAccountDataTimes   = uint16(9994)
	maxAccountDataSize     = 4 << 20
)

type AccountDataRequest struct {
	PlayerGUID uint64
	DataType   byte
}

type AccountData struct {
	PlayerGUID       uint64
	Time             int64
	UncompressedSize uint32
	DataType         byte
	CompressedData   []byte
}

func ParseAccountDataRequest(body []byte) (AccountDataRequest, error) {
	var request AccountDataRequest
	low, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		return request, err
	}
	if consumed+1 != len(body) {
		return request, fmt.Errorf("account-data request has %d trailing/missing bytes", len(body)-consumed-1)
	}
	request.PlayerGUID = uint64(uint32(low))
	request.DataType = body[consumed] >> 4
	if request.DataType >= AccountDataCount {
		return request, fmt.Errorf("account-data type %d is out of range", request.DataType)
	}
	return request, nil
}

func EncodeAccountDataRequest(request AccountDataRequest) []byte {
	body := appendModernPlayerGUID(nil, request.PlayerGUID)
	return append(body, request.DataType<<4)
}

func ParseAccountDataUpdate(body []byte) (AccountData, error) {
	var data AccountData
	low, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		return data, err
	}
	const fixed = 8 + 4 + 1 + 4
	if consumed+fixed > len(body) {
		return data, fmt.Errorf("account-data update has %d bytes after GUID, want at least %d", len(body)-consumed, fixed)
	}
	data.PlayerGUID = uint64(uint32(low))
	data.Time = int64(binary.LittleEndian.Uint64(body[consumed : consumed+8]))
	consumed += 8
	data.UncompressedSize = binary.LittleEndian.Uint32(body[consumed : consumed+4])
	consumed += 4
	data.DataType = body[consumed] >> 4
	consumed++
	if data.DataType >= AccountDataCount {
		return data, fmt.Errorf("account-data type %d is out of range", data.DataType)
	}
	compressedSize := int(binary.LittleEndian.Uint32(body[consumed : consumed+4]))
	consumed += 4
	if compressedSize < 0 || compressedSize > maxAccountDataSize || consumed+compressedSize != len(body) {
		return data, fmt.Errorf("account-data compressed size %d does not match remaining %d", compressedSize, len(body)-consumed)
	}
	if data.UncompressedSize > maxAccountDataSize {
		return data, fmt.Errorf("account-data uncompressed size %d exceeds limit", data.UncompressedSize)
	}
	data.CompressedData = append([]byte(nil), body[consumed:]...)
	return data, nil
}

func EncodeAccountDataUpdate(data AccountData) ([]byte, error) {
	if data.DataType >= AccountDataCount {
		return nil, fmt.Errorf("account-data type %d is out of range", data.DataType)
	}
	if len(data.CompressedData) > maxAccountDataSize || data.UncompressedSize > maxAccountDataSize {
		return nil, fmt.Errorf("account-data payload exceeds limit")
	}
	body := appendModernPlayerGUID(nil, data.PlayerGUID)
	body = binary.LittleEndian.AppendUint64(body, uint64(data.Time))
	body = binary.LittleEndian.AppendUint32(body, data.UncompressedSize)
	body = append(body, data.DataType<<4)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(data.CompressedData)))
	return append(body, data.CompressedData...), nil
}

func EncodeAccountDataTimes(playerGUID uint64, serverTime int64, accountTimes [AccountDataCount]int64) []byte {
	body := appendModernPlayerGUID(nil, playerGUID)
	body = binary.LittleEndian.AppendUint64(body, uint64(serverTime))
	for _, timestamp := range accountTimes {
		body = binary.LittleEndian.AppendUint64(body, uint64(timestamp))
	}
	return body
}

func appendModernPlayerGUID(destination []byte, guid uint64) []byte {
	if guid == 0 {
		return appendPackedGUID128(destination, 0, 0)
	}
	high := uint64(2)<<58 | uint64(1)<<42
	return appendPackedGUID128(destination, uint64(uint32(guid)), high)
}
