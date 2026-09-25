package bnet

import (
	"bytes"
	"encoding/binary"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestFrameRoundTrip(t *testing.T) {
	want := Frame{
		Header: Header{
			ServiceID:   2,
			MethodID:    7,
			Token:       42,
			ObjectID:    99,
			Status:      3,
			Timeout:     5000,
			IsResponse:  true,
			ServiceHash: 0x65446991,
		},
		Payload: []byte{1, 2, 3, 4},
	}
	var wire bytes.Buffer
	if err := WriteFrame(&wire, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("payload %x, want %x", got.Payload, want.Payload)
	}
	if got.Header.ServiceID != want.Header.ServiceID ||
		got.Header.MethodID != want.Header.MethodID ||
		got.Header.Token != want.Header.Token ||
		got.Header.ServiceHash != want.Header.ServiceHash ||
		got.Header.Size != uint32(len(want.Payload)) {
		t.Fatalf("header %#v, want %#v", got.Header, want.Header)
	}
}

func TestMarshalHeaderWritesRequiredZeroServiceID(t *testing.T) {
	// Live 3.4.3 ConnectRequest header captured on 7000:
	// 000d 0800 1001 1800 283e 5d91694465
	want := []byte{0x08, 0x00, 0x10, 0x01, 0x18, 0x00, 0x28, 0x3e, 0x5d, 0x91, 0x69, 0x44, 0x65}
	got := MarshalHeader(Header{
		MethodID:    1,
		Size:        62,
		ServiceHash: 0x65446991,
	})
	if !bytes.Equal(got, want) {
		t.Fatalf("header %x, want %x", got, want)
	}
}

func TestParseHeaderKeepsUnknownFields(t *testing.T) {
	data := MarshalHeader(Header{ServiceID: 1, MethodID: 2, Token: 3})
	data = protowire.AppendTag(data, 12, protowire.BytesType)
	data = protowire.AppendBytes(data, []byte("client-id"))
	header, err := ParseHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if header.ServiceID != 1 || header.Token != 3 || len(header.Remainder) == 0 {
		t.Fatalf("unexpected header: %#v", header)
	}
	roundTrip := MarshalHeader(Header{ServiceID: 0xfe, Token: 3, Remainder: header.Remainder})
	got, err := ParseHeader(roundTrip)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Remainder, header.Remainder) {
		t.Fatalf("remainder %x, want %x", got.Remainder, header.Remainder)
	}
}

func TestParseHeaderAcceptsVarintServiceHash(t *testing.T) {
	data := appendVarint(nil, 3, 9, true)
	data = protowire.AppendTag(data, 11, protowire.VarintType)
	data = protowire.AppendVarint(data, 0x65446991)
	header, err := ParseHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if header.ServiceHash != 0x65446991 {
		t.Fatalf("service hash 0x%08X", header.ServiceHash)
	}
}

func TestReadFrameRejectsOversizedHeader(t *testing.T) {
	var wire bytes.Buffer
	var prefix [2]byte
	binary.BigEndian.PutUint16(prefix[:], MaxHeaderSize+1)
	wire.Write(prefix[:])
	if _, err := ReadFrame(&wire); err == nil {
		t.Fatal("expected oversized header error")
	}
}
