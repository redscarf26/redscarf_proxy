package modernworld

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEncodeGluePackets(t *testing.T) {
	timeZone := EncodeSetTimeZoneInformation("Asia/Shanghai", "Asia/Shanghai")
	if len(timeZone) != 2+2*len("Asia/Shanghai") || timeZone[0] != 0x1a || timeZone[1] != 0x34 {
		t.Fatalf("unexpected timezone body: %x", timeZone)
	}
	features := EncodeFeatureSystemStatusGlue()
	if len(features) != 72 {
		t.Fatalf("feature-system body has %d bytes, want 72", len(features))
	}
	if got := binary.LittleEndian.Uint32(features[20:24]); got != 10 {
		t.Fatalf("max characters = %d, want 10", got)
	}
	if got := EncodeBattleNetConnectionStatus(1, false); len(got) != 1 || got[0] != 0x40 {
		t.Fatalf("unexpected bnet status: %x", got)
	}
}

func TestChangeRealmTicket(t *testing.T) {
	body := binary.LittleEndian.AppendUint32(nil, 0x11223344)
	secret := bytes.Repeat([]byte{0xab}, 32)
	body = append(body, secret...)
	request, err := ParseChangeRealmTicket(body)
	if err != nil || request.Token != 0x11223344 || !bytes.Equal(request.Secret[:], secret) {
		t.Fatalf("ticket=%#v err=%v", request, err)
	}
	response := EncodeChangeRealmTicketResponse(request.Token)
	if len(response) != 10 || binary.LittleEndian.Uint32(response[:4]) != 0x11223344 || response[4] != 0x80 || binary.LittleEndian.Uint32(response[5:9]) != 1 || response[9] != 0 {
		t.Fatalf("ticket response=%x", response)
	}
	if _, err := ParseChangeRealmTicket(body[:35]); err == nil {
		t.Fatal("short change-realm-ticket was accepted")
	}
}
