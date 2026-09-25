package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	CMSGDuelResponse = uint16(0x34E2)
	CMSGCanDuel      = uint16(0x3664)

	SMSGDuelRequested   = uint16(0x2940)
	SMSGDuelOutOfBounds = uint16(0x2942)
	SMSGDuelInBounds    = uint16(0x2943)
	SMSGDuelCountdown   = uint16(0x2944)
	SMSGDuelComplete    = uint16(0x2945)
	SMSGDuelWinner      = uint16(0x2946)
	SMSGCanDuelResult   = uint16(0x2947)
)

type DuelResponse struct {
	Arbiter   GUID128
	Accepted  bool
	Forfeited bool
}

type LegacyDuelRequest struct {
	Arbiter     uint64
	RequestedBy uint64
}

type LegacyDuelWinner struct {
	Fled       bool
	BeatenName string
	WinnerName string
}

func ParseCanDuel(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func EncodeCanDuelResult(target GUID128, allowed bool) []byte {
	body := appendPackedGUID128(nil, target.Low, target.High)
	bits := newBitWriter(body)
	bits.writeBit(allowed)
	return bits.flush()
}

func ParseDuelResponse(body []byte) (DuelResponse, error) {
	var response DuelResponse
	r := movementReader{data: body}
	var err error
	if response.Arbiter, err = r.guid128(); err != nil {
		return response, fmt.Errorf("read duel arbiter: %w", err)
	}
	if response.Accepted, err = r.bit(); err != nil {
		return response, fmt.Errorf("read duel accepted bit: %w", err)
	}
	if response.Forfeited, err = r.bit(); err != nil {
		return response, fmt.Errorf("read duel forfeited bit: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return response, fmt.Errorf("duel response has %d trailing bytes", r.remaining())
	}
	return response, nil
}

func EncodeLegacyDuelResponse(arbiter uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, arbiter)
}

func ParseLegacyDuelRequested(body []byte) (LegacyDuelRequest, error) {
	var request LegacyDuelRequest
	if len(body) != 16 {
		return request, fmt.Errorf("duel requested has %d bytes, want 16", len(body))
	}
	request.Arbiter = binary.LittleEndian.Uint64(body)
	request.RequestedBy = binary.LittleEndian.Uint64(body[8:])
	return request, nil
}

func EncodeDuelRequested(arbiter, requestedBy, requestedByAccount GUID128) []byte {
	body := appendPackedGUID128(nil, arbiter.Low, arbiter.High)
	body = appendPackedGUID128(body, requestedBy.Low, requestedBy.High)
	return appendPackedGUID128(body, requestedByAccount.Low, requestedByAccount.High)
}

func ParseLegacyDuelCountdown(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("duel countdown has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

func EncodeDuelCountdown(countdown uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, countdown)
}

func ParseLegacyDuelComplete(body []byte) (bool, error) {
	if len(body) != 1 {
		return false, fmt.Errorf("duel complete has %d bytes, want 1", len(body))
	}
	return body[0] != 0, nil
}

func EncodeDuelComplete(started bool) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(started)
	return bits.flush()
}

func ParseLegacyDuelWinner(body []byte) (LegacyDuelWinner, error) {
	var winner LegacyDuelWinner
	r := movementReader{data: body}
	fled, err := r.u8()
	if err != nil {
		return winner, fmt.Errorf("read duel-winner fled flag: %w", err)
	}
	winner.Fled = fled != 0
	if winner.BeatenName, err = readLegacyCString(&r, 255); err != nil {
		return winner, fmt.Errorf("read duel-winner beaten name: %w", err)
	}
	if winner.WinnerName, err = readLegacyCString(&r, 255); err != nil {
		return winner, fmt.Errorf("read duel-winner winner name: %w", err)
	}
	if r.remaining() != 0 {
		return winner, fmt.Errorf("duel winner has %d trailing bytes", r.remaining())
	}
	return winner, nil
}

func EncodeDuelWinner(winner LegacyDuelWinner, realmAddress uint32) []byte {
	beatenName := truncateWireString(winner.BeatenName, 63)
	winnerName := truncateWireString(winner.WinnerName, 63)
	bits := newBitWriter(nil)
	bits.writeBits(uint32(len(beatenName)), 6)
	bits.writeBits(uint32(len(winnerName)), 6)
	bits.writeBit(winner.Fled)
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = append(body, beatenName...)
	return append(body, winnerName...)
}

func ValidateLegacyDuelBoundary(body []byte) error {
	if len(body) != 0 {
		return fmt.Errorf("duel boundary event has %d bytes, want 0", len(body))
	}
	return nil
}
