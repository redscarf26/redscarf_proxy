package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAccountDataRequestAndUpdateRoundTrip(t *testing.T) {
	request := AccountDataRequest{PlayerGUID: 0x42, DataType: 12}
	parsedRequest, err := ParseAccountDataRequest(EncodeAccountDataRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if parsedRequest != request {
		t.Fatalf("request mismatch: got=%#v want=%#v", parsedRequest, request)
	}

	want := AccountData{
		PlayerGUID:       0x42,
		Time:             1_700_000_000,
		UncompressedSize: 25,
		DataType:         3,
		CompressedData:   []byte{0x78, 0x9c, 1, 2, 3},
	}
	body, err := EncodeAccountDataUpdate(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseAccountDataUpdate(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlayerGUID != want.PlayerGUID || got.Time != want.Time || got.UncompressedSize != want.UncompressedSize || got.DataType != want.DataType || !bytes.Equal(got.CompressedData, want.CompressedData) {
		t.Fatalf("update mismatch: got=%#v want=%#v", got, want)
	}
}

func TestAccountDataTimesUsesBuild54261Count(t *testing.T) {
	var times [AccountDataCount]int64
	for index := range times {
		times[index] = int64(100 + index)
	}
	body := EncodeAccountDataTimes(0x42, 99, times)
	_, _, consumed, err := readPackedGUID128(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body)-consumed != 8+15*8 {
		t.Fatalf("account-data-times has %d bytes after GUID, want %d", len(body)-consumed, 8+15*8)
	}
	if binary.LittleEndian.Uint64(body[consumed:consumed+8]) != 99 || binary.LittleEndian.Uint64(body[len(body)-8:]) != 114 {
		t.Fatalf("unexpected account-data-times body: %x", body)
	}
}
