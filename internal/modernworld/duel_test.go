package modernworld

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDuelClientRequests(t *testing.T) {
	target := GUID128{Low: 0x42, High: 1}
	canDuel := appendPackedGUID128(nil, target.Low, target.High)
	parsedTarget, err := ParseCanDuel(canDuel)
	if err != nil || parsedTarget != target {
		t.Fatalf("can-duel target=%#v err=%v", parsedTarget, err)
	}
	result := EncodeCanDuelResult(target, true)
	low, high, consumed, err := readPackedGUID128(result)
	if err != nil || (GUID128{Low: low, High: high}) != target || len(result) != consumed+1 || result[consumed] != 0x80 {
		t.Fatalf("can-duel result=%x guid=%x:%x consumed=%d err=%v", result, low, high, consumed, err)
	}

	responseBody := appendPackedGUID128(nil, target.Low, target.High)
	bits := newBitWriter(responseBody)
	bits.writeBit(true)
	bits.writeBit(false)
	response, err := ParseDuelResponse(bits.flush())
	if err != nil || response.Arbiter != target || !response.Accepted || response.Forfeited {
		t.Fatalf("duel response=%#v err=%v", response, err)
	}
	legacy := EncodeLegacyDuelResponse(0xf110000001000042)
	if len(legacy) != 8 || binary.LittleEndian.Uint64(legacy) != 0xf110000001000042 {
		t.Fatalf("legacy duel response=%x", legacy)
	}
	if _, err := ParseDuelResponse(append(bits.flush(), 0)); err == nil {
		t.Fatal("expected duel-response trailing-byte error")
	}
}

func TestDuelServerEvents(t *testing.T) {
	legacyRequest := binary.LittleEndian.AppendUint64(nil, 0xf110000001000042)
	legacyRequest = binary.LittleEndian.AppendUint64(legacyRequest, 0x43)
	request, err := ParseLegacyDuelRequested(legacyRequest)
	if err != nil || request.Arbiter != 0xf110000001000042 || request.RequestedBy != 0x43 {
		t.Fatalf("duel request=%#v err=%v", request, err)
	}
	arbiter := GUID128{Low: 0x42, High: 1}
	requestedBy := GUID128{Low: 0x43, High: 2}
	account := ModernWowAccountGUIDForLegacy(0x43)
	requestedBody := EncodeDuelRequested(arbiter, requestedBy, account)
	position := 0
	for index, want := range []GUID128{arbiter, requestedBy, account} {
		low, high, consumed, readErr := readPackedGUID128(requestedBody[position:])
		if readErr != nil || (GUID128{Low: low, High: high}) != want {
			t.Fatalf("duel requested GUID %d=%x:%x want=%#v err=%v", index, low, high, want, readErr)
		}
		position += consumed
	}
	if position != len(requestedBody) {
		t.Fatalf("duel requested has %d trailing bytes", len(requestedBody)-position)
	}

	countdown, err := ParseLegacyDuelCountdown([]byte{0xb8, 0x0b, 0, 0})
	if body := EncodeDuelCountdown(countdown); err != nil || countdown != 3000 || !bytes.Equal(body, []byte{0xb8, 0x0b, 0, 0}) {
		t.Fatalf("duel countdown=%d body=%x err=%v", countdown, body, err)
	}
	started, err := ParseLegacyDuelComplete([]byte{1})
	if err != nil || !started || len(EncodeDuelComplete(started)) != 1 || EncodeDuelComplete(started)[0] != 0x80 {
		t.Fatalf("duel complete started=%v body=%x err=%v", started, EncodeDuelComplete(started), err)
	}
	if err := ValidateLegacyDuelBoundary(nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLegacyDuelBoundary([]byte{0}); err == nil {
		t.Fatal("expected non-empty boundary error")
	}
}

func TestDuelWinnerEncoding(t *testing.T) {
	legacy := append([]byte{1}, []byte("Loser\x00Winner\x00")...)
	winner, err := ParseLegacyDuelWinner(legacy)
	if err != nil || !winner.Fled || winner.BeatenName != "Loser" || winner.WinnerName != "Winner" {
		t.Fatalf("duel winner=%#v err=%v", winner, err)
	}
	body := EncodeDuelWinner(winner, 0x01010007)
	r := movementReader{data: body}
	beatenLength, _ := r.bits(6)
	winnerLength, _ := r.bits(6)
	fled, _ := r.bit()
	realmOne, _ := r.u32()
	realmTwo, _ := r.u32()
	names, _ := r.take(r.remaining())
	if beatenLength != 5 || winnerLength != 6 || !fled || realmOne != 0x01010007 || realmTwo != realmOne || string(names) != "LoserWinner" {
		t.Fatalf("modern duel winner=%x lengths=%d/%d fled=%v realms=%x/%x names=%q", body, beatenLength, winnerLength, fled, realmOne, realmTwo, names)
	}

	longWinner := LegacyDuelWinner{BeatenName: strings.Repeat("你", 30), WinnerName: strings.Repeat("W", 70)}
	truncated := EncodeDuelWinner(longWinner, 1)
	r = movementReader{data: truncated}
	beatenLength, _ = r.bits(6)
	winnerLength, _ = r.bits(6)
	_, _ = r.bit()
	_, _ = r.take(8)
	encodedNames, _ := r.take(r.remaining())
	if beatenLength > 63 || winnerLength > 63 || !utf8.Valid(encodedNames[:beatenLength]) || winnerLength != 63 {
		t.Fatalf("truncated winner lengths=%d/%d names=%x", beatenLength, winnerLength, encodedNames)
	}
}
