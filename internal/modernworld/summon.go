package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGSummonResponse = uint16(0x366C)
	SMSGSummonRequest  = uint16(0x2721)
)

type SummonResponse struct {
	Summoner GUID128
	Accept   bool
}

type SummonRequest struct {
	Summoner GUID128
	AreaID   int32
}

func ParseSummonResponse(body []byte) (SummonResponse, error) {
	r := movementReader{data: body}
	summoner, err := r.guid128()
	if err != nil {
		return SummonResponse{}, fmt.Errorf("read summon-response summoner: %w", err)
	}
	accept, err := r.bit()
	if err != nil {
		return SummonResponse{}, fmt.Errorf("read summon-response accept bit: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return SummonResponse{}, fmt.Errorf("summon-response has %d trailing bytes", r.remaining())
	}
	return SummonResponse{Summoner: summoner, Accept: accept}, nil
}

func EncodeLegacySummonResponse(summoner uint64, accept bool) []byte {
	body := binary.LittleEndian.AppendUint64(nil, summoner)
	if accept {
		return append(body, 1)
	}
	return append(body, 0)
}

func ParseLegacySummonRequest(body []byte) (uint64, int32, error) {
	if len(body) != 16 {
		return 0, 0, fmt.Errorf("legacy summon-request has %d bytes, want 16", len(body))
	}
	return binary.LittleEndian.Uint64(body), int32(binary.LittleEndian.Uint32(body[8:12])), nil
}

func EncodeSummonRequest(summoner GUID128, realmAddress uint32, areaID int32) []byte {
	body := appendPackedGUID128(nil, summoner.Low, summoner.High)
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = binary.LittleEndian.AppendUint32(body, uint32(areaID))
	body = append(body, 0) // SummonReason::Spell
	bits := newBitWriter(body)
	bits.writeBit(false) // SkipStartingArea
	return bits.flush()
}
