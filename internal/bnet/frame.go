// Package bnet implements the Battle.net RPC transport framing used by the
// 3.4.3 client. Message payload schemas live above this package.
package bnet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	MaxHeaderSize  = 4096
	MaxPayloadSize = 16 << 20
)

// Header is the scalar subset of bgs.protocol.Header required to frame and
// dispatch RPC messages. Remainder preserves unknown fields such as ClientId
// so responses can echo them.
type Header struct {
	ServiceID   uint32
	MethodID    uint32
	Token       uint32
	ObjectID    uint64
	Size        uint32
	Status      uint32
	Timeout     uint64
	IsResponse  bool
	ServiceHash uint32
	Remainder   []byte
}

type Frame struct {
	Header  Header
	Payload []byte
}

func ReadFrame(r io.Reader) (Frame, error) {
	var prefix [2]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return Frame{}, err
	}
	headerSize := int(binary.BigEndian.Uint16(prefix[:]))
	if headerSize == 0 || headerSize > MaxHeaderSize {
		return Frame{}, fmt.Errorf("invalid BNet header size %d", headerSize)
	}
	headerBytes := make([]byte, headerSize)
	if _, err := io.ReadFull(r, headerBytes); err != nil {
		return Frame{}, fmt.Errorf("read BNet header: %w", err)
	}
	header, err := ParseHeader(headerBytes)
	if err != nil {
		return Frame{}, err
	}
	if header.Size > MaxPayloadSize {
		return Frame{}, fmt.Errorf("BNet payload size %d exceeds limit", header.Size)
	}
	payload := make([]byte, int(header.Size))
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, fmt.Errorf("read BNet payload: %w", err)
	}
	return Frame{Header: header, Payload: payload}, nil
}

func WriteFrame(w io.Writer, frame Frame) error {
	if len(frame.Payload) > MaxPayloadSize {
		return fmt.Errorf("BNet payload size %d exceeds limit", len(frame.Payload))
	}
	frame.Header.Size = uint32(len(frame.Payload))
	header := MarshalHeader(frame.Header)
	if len(header) == 0 || len(header) > MaxHeaderSize || len(header) > int(^uint16(0)) {
		return fmt.Errorf("invalid encoded BNet header size %d", len(header))
	}
	buf := make([]byte, 2+len(header)+len(frame.Payload))
	binary.BigEndian.PutUint16(buf, uint16(len(header)))
	copy(buf[2:], header)
	copy(buf[2+len(header):], frame.Payload)
	_, err := w.Write(buf)
	return err
}

func ParseHeader(data []byte) (Header, error) {
	var header Header
	for len(data) > 0 {
		before := data
		number, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return Header{}, protowire.ParseError(n)
		}
		data = data[n:]
		switch number {
		case 1, 2, 3, 4, 5, 6, 8, 9:
			if typ != protowire.VarintType {
				return Header{}, fmt.Errorf("BNet header field %d has wire type %d", number, typ)
			}
			value, consumed := protowire.ConsumeVarint(data)
			if consumed < 0 {
				return Header{}, protowire.ParseError(consumed)
			}
			data = data[consumed:]
			switch number {
			case 1:
				header.ServiceID = uint32(value)
			case 2:
				header.MethodID = uint32(value)
			case 3:
				header.Token = uint32(value)
			case 4:
				header.ObjectID = value
			case 5:
				header.Size = uint32(value)
			case 6:
				header.Status = uint32(value)
			case 8:
				header.Timeout = value
			case 9:
				header.IsResponse = value != 0
			}
		case 11:
			switch typ {
			case protowire.Fixed32Type:
				value, consumed := protowire.ConsumeFixed32(data)
				if consumed < 0 {
					return Header{}, protowire.ParseError(consumed)
				}
				header.ServiceHash = value
				data = data[consumed:]
			case protowire.VarintType:
				value, consumed := protowire.ConsumeVarint(data)
				if consumed < 0 {
					return Header{}, protowire.ParseError(consumed)
				}
				header.ServiceHash = uint32(value)
				data = data[consumed:]
			default:
				return Header{}, fmt.Errorf("BNet service_hash has wire type %d", typ)
			}
		default:
			consumed := protowire.ConsumeFieldValue(number, typ, data)
			if consumed < 0 {
				return Header{}, protowire.ParseError(consumed)
			}
			header.Remainder = append(header.Remainder, before[:n+consumed]...)
			data = data[consumed:]
		}
	}
	return header, nil
}

func MarshalHeader(header Header) []byte {
	var data []byte
	// service_id and token are proto2 required fields. The 3.4.3 client
	// (BGS C++ SDK) always emits service_id even when it is 0. Omitting it
	// makes server-initiated RPCs such as OnExternalChallenge fail to parse,
	// so the login webview never starts and REST sees no connections.
	data = appendVarint(data, 1, uint64(header.ServiceID), true)
	data = appendVarint(data, 2, uint64(header.MethodID), header.MethodID != 0)
	data = appendVarint(data, 3, uint64(header.Token), true)
	data = appendVarint(data, 4, header.ObjectID, header.ObjectID != 0)
	data = appendVarint(data, 5, uint64(header.Size), header.Size != 0)
	data = appendVarint(data, 6, uint64(header.Status), header.Status != 0)
	data = appendVarint(data, 8, header.Timeout, header.Timeout != 0)
	data = appendVarint(data, 9, boolVarint(header.IsResponse), header.IsResponse)
	if header.ServiceHash != 0 {
		data = protowire.AppendTag(data, 11, protowire.Fixed32Type)
		data = protowire.AppendFixed32(data, header.ServiceHash)
	}
	return append(data, header.Remainder...)
}

func appendVarint(data []byte, number protowire.Number, value uint64, present bool) []byte {
	if !present {
		return data
	}
	data = protowire.AppendTag(data, number, protowire.VarintType)
	return protowire.AppendVarint(data, value)
}

func boolVarint(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

var ErrUnsupportedRPC = errors.New("unsupported BNet RPC")
